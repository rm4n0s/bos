package scheduler

import (
	"context"
	"encoding/json"
	"log/slog"
	"maps"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/rm4n0s/bos/internal/comfy"
	"github.com/rm4n0s/bos/internal/domain"
	"github.com/rm4n0s/bos/internal/events"
	"github.com/rm4n0s/bos/internal/workflow"
	"github.com/rm4n0s/errors"
)

// saveTimeout bounds a write of job state that must happen even though the
// job's own context has ended.
const saveTimeout = 10 * time.Second

// execute runs the next stage of a claimed job — its generation, or its next
// fixer step — and records the outcome (SPEC §8.2, §8.3). With resume the
// generation prompt was submitted by an earlier run of the application and is
// only waited for.
func (s *Scheduler) execute(ctx context.Context, r *runner, client comfy.Client, job *domain.Job, resume bool) {
	generation := !job.IsGenerated
	var err error
	switch {
	case resume:
		err = s.resumeGeneration(ctx, r, client, job)
	case generation:
		err = s.generate(ctx, r, client, job)
	default:
		err = s.fix(ctx, r, client, job)
	}
	s.finish(ctx, r, client, job, generation, err)
}

// ---- generation ----

func outputTitle(g *domain.Generator) string {
	if g.OutputNodeTitle != "" {
		return g.OutputNodeTitle
	}
	return g.OutputNodeClassType
}

// generate builds the workflow of a job and runs it (SPEC §8.2).
func (s *Scheduler) generate(ctx context.Context, r *runner, client comfy.Client, job *domain.Job) error {
	gen, err := s.DB.Repos().Generators.GetByCode(ctx, job.GeneratorCodeName)
	if err != nil {
		return err
	}
	g, err := workflow.Parse(gen.Workflow)
	if err != nil {
		return err
	}
	out, err := workflow.FindByTitle(g, outputTitle(gen))
	if err != nil {
		return err
	}
	if err := workflow.SetPrompts(g, gen.PositivePromptTitle, job.PositivePrompt, gen.NegativePromptTitle, job.NegativePrompt); err != nil {
		return err
	}
	s.warnUnsentPrompts(g, gen, job)
	if err := workflow.ApplyAttributes(g, job.OutputAttributes); err != nil {
		return err
	}
	if gen.HasRandomSeed && job.Seed == nil {
		seed := s.NewSeed()
		if err := workflow.SetSeed(g, seed); err != nil {
			return err
		}
		job.Seed = &seed
	}
	if err := s.inject(ctx, client, g, job); err != nil {
		return err
	}
	if err := s.submit(ctx, r, client, g, job); err != nil {
		return err
	}
	res, err := client.Wait(ctx, job.PromptID)
	if err != nil {
		return err
	}
	return s.collect(ctx, r, client, job, res, out.ID)
}

// resumeGeneration waits for a prompt that an earlier run submitted.
func (s *Scheduler) resumeGeneration(ctx context.Context, r *runner, client comfy.Client, job *domain.Job) error {
	gen, err := s.DB.Repos().Generators.GetByCode(ctx, job.GeneratorCodeName)
	if err != nil {
		return err
	}
	g, err := workflow.Parse(gen.Workflow)
	if err != nil {
		return err
	}
	out, err := workflow.FindByTitle(g, outputTitle(gen))
	if err != nil {
		return err
	}
	res, err := client.Wait(ctx, job.PromptID)
	if err != nil {
		return err
	}
	return s.collect(ctx, r, client, job, res, out.ID)
}

// inject applies the IP-Adapters, LoRAs, ControlNets and regions of a job, in
// that order, uploading the images they refer to (SPEC §8.2, steps 4 to 7).
func (s *Scheduler) inject(ctx context.Context, client comfy.Client, g workflow.Graph, job *domain.Job) error {
	var adapters []workflow.IPAdapter
	for _, a := range job.IPAdapters {
		image, err := s.upload(ctx, client, a.ImageFile)
		if err != nil {
			return err
		}
		adapters = append(adapters, workflow.IPAdapter{
			Image: image, Weight: a.Weight, WeightType: a.WeightType, StartAt: a.StartAt, EndAt: a.EndAt,
			ClipVisionModel: a.ClipVisionModel, ModelName: a.ModelName,
		})
	}
	if err := workflow.InjectIPAdapters(g, adapters); err != nil {
		return err
	}

	loras := make([]workflow.LoRA, len(job.LoRAs))
	for i, l := range job.LoRAs {
		loras[i] = workflow.LoRA{Name: l.Name, StrengthModel: l.StrengthModel, StrengthClip: l.StrengthClip}
	}
	if err := workflow.InjectLoRAs(g, loras); err != nil {
		return err
	}

	var nets []workflow.ControlNet
	for _, c := range job.ControlNets {
		image, err := s.upload(ctx, client, c.ImagePath)
		if err != nil {
			return err
		}
		nets = append(nets, workflow.ControlNet{Type: c.Type, Image: image, IsReference: c.IsReference, ModelName: c.ModelPattern, Strength: c.Strength})
	}
	if err := workflow.InjectControlNets(g, nets); err != nil {
		return err
	}

	var regions []workflow.Region
	for _, rp := range job.RegionPrompts {
		reg := workflow.Region{Keyword: rp.Keyword, Prompt: rp.Prompt}
		if rp.MaskFile != "" {
			mask, err := s.upload(ctx, client, rp.MaskFile)
			if err != nil {
				return err
			}
			reg.Mask = mask
		}
		if c := rp.Coordinates; c != nil {
			reg.Rect = &workflow.Rect{Width: c.Width, Height: c.Height, X: c.X, Y: c.Y}
		}
		regions = append(regions, reg)
	}
	return workflow.InjectRegions(g, regions)
}

// upload makes a stored file available to a server and returns the value for a
// LoadImage input (SPEC §8.7, §18.6).
func (s *Scheduler) upload(ctx context.Context, client comfy.Client, path string) (string, error) {
	rc, err := s.Files.Open(path)
	if err != nil {
		return "", err
	}
	value, upErr := client.Upload(ctx, filepath.Base(path), rc)
	if closeErr := rc.Close(); upErr == nil && closeErr != nil {
		upErr = errors.NewErr("FileStoreFailure", closeErr).SetMetadata("path", path, "operation", "close")
	}
	return value, upErr
}

// submit queues the workflow, records the prompt id and the warnings on the job
// and announces the stage (SPEC §8.2).
func (s *Scheduler) submit(ctx context.Context, r *runner, client comfy.Client, g workflow.Graph, job *domain.Job) error {
	s.logQueued(ctx, r, g, job)
	id, warnings, err := client.QueuePrompt(ctx, g)
	if err != nil {
		return err
	}
	job.PromptID = id
	for _, w := range warnings {
		job.Warnings = append(job.Warnings, "node "+w.NodeID+": "+w.Message)
	}
	if err := s.DB.Repos().Jobs.Update(ctx, job); err != nil {
		return err
	}
	s.emit(events.Processing, job, r.code())
	return nil
}

// warnUnsentPrompts says so in the log when a job has a prompt that no node of
// the workflow can receive, because the generator names no prompt node for it:
// ComfyUI then runs the text the workflow file has, for every job alike.
func (s *Scheduler) warnUnsentPrompts(g workflow.Graph, gen *domain.Generator, job *domain.Job) {
	var missing []string
	if strings.TrimSpace(job.PositivePrompt) != "" && strings.TrimSpace(gen.PositivePromptTitle) == "" {
		missing = append(missing, "positive")
	}
	if strings.TrimSpace(job.NegativePrompt) != "" && strings.TrimSpace(gen.NegativePromptTitle) == "" &&
		!workflow.NegativeGoesWithPositive(g, gen.PositivePromptTitle) {
		missing = append(missing, "negative")
	}
	if len(missing) == 0 {
		return
	}
	s.Logger.Warn("the generator has no prompt node for the job's prompt, so it is not sent to ComfyUI: set the prompt node titles of the generator",
		"job", job.ID, "command", job.CommandID, "generator", gen.CodeName, "missing", strings.Join(missing, " and "))
}

// logQueued records what is about to be sent to ComfyUI: for each sampler, the
// node its positive and negative conditioning ends in and the text that node
// holds, read back from the finished workflow rather than from the job. When a
// server draws the same picture for different jobs, this tells whether the
// prompts reached the workflow. The whole workflow is logged at debug level.
func (s *Scheduler) logQueued(ctx context.Context, r *runner, g workflow.Graph, job *domain.Job) {
	step := "generation"
	if job.IsGenerated {
		step = "fixer " + strconv.Itoa(job.FinishedFixers+1)
	}
	base := []any{"job", job.ID, "command", job.CommandID, "server", r.code(), "step", step}
	if job.Seed != nil {
		base = append(base, "seed", *job.Seed)
	}
	samplers := workflow.EffectivePrompts(g)
	if len(samplers) == 0 {
		s.Logger.Info("sending a prompt to ComfyUI", append(base, "sampler", "none found")...)
	}
	for _, p := range samplers {
		attrs := append(slices.Clone(base), "sampler", p.Sampler,
			promptGroup("positive", p.Positive), promptGroup("negative", p.Negative))
		s.Logger.Info("sending a prompt to ComfyUI", attrs...)
	}
	if s.Logger.Enabled(ctx, slog.LevelDebug) {
		if raw, err := json.Marshal(g); err == nil {
			s.Logger.Debug("workflow sent to ComfyUI", append(base, "workflow", string(raw))...)
		}
	}
}

// promptGroup describes the node a prompt comes from as one log attribute.
func promptGroup(name string, src workflow.PromptSource) slog.Attr {
	attrs := []any{"node", src.Node, "class", src.Class, "title", src.Title}
	for _, input := range slices.Sorted(maps.Keys(src.Texts)) {
		attrs = append(attrs, input, src.Texts[input])
	}
	if len(src.Linked) > 0 {
		attrs = append(attrs, "linked_inputs", strings.Join(src.Linked, ","))
	}
	return slog.Group(name, attrs...)
}

// collect saves the first file the output node produced as the job's result.
// The planned name keeps its stem; its extension follows the file the server
// made. Once the copy is safe, the server's own file is removed (SPEC §8.2).
func (s *Scheduler) collect(ctx context.Context, r *runner, client comfy.Client, job *domain.Job, res comfy.Result, nodeID string) error {
	files, err := res.Pick(nodeID)
	if err != nil {
		return err
	}
	rc, err := client.Fetch(ctx, files[0])
	if err != nil {
		return err
	}
	path := withExtension(job.ResultPath, files[0].Filename)
	writeErr := s.Files.Write(path, rc)
	if closeErr := rc.Close(); writeErr == nil && closeErr != nil {
		writeErr = errors.NewErr("FileStoreFailure", closeErr).SetMetadata("path", path, "operation", "close")
	}
	if writeErr != nil {
		return writeErr
	}
	if path != job.ResultPath && job.ResultPath != "" {
		if err := s.Files.Remove(job.ResultPath); err != nil {
			logError(s.Logger, "could not remove a result file that was replaced", err, "path", job.ResultPath)
		}
	}
	job.ResultPath = path
	s.removeFromComfy(ctx, r, client, job, files[0])
	return nil
}

// removeFromComfy deletes a result from the output folder of the server that
// made it, through the comfyui-model-crud extension, when the setting is on.
// The job has its copy already, so a file that cannot be removed is a warning,
// never a failure of the job.
func (s *Scheduler) removeFromComfy(ctx context.Context, r *runner, client comfy.Client, job *domain.Job, f comfy.OutputFile) {
	if !s.DeleteOutputs {
		return
	}
	if err := client.DeleteOutput(ctx, f); err != nil {
		logError(s.Logger, "could not remove a result from the ComfyUI output folder", err,
			"job", job.ID, "command", job.CommandID, "server", r.code(), "file", f.Filename)
	}
}

// withExtension gives path the extension of name, if name has one.
func withExtension(path, name string) string {
	ext := strings.ToLower(filepath.Ext(name))
	if ext == "" || ext == strings.ToLower(filepath.Ext(path)) {
		return path
	}
	return strings.TrimSuffix(path, filepath.Ext(path)) + ext
}

// ---- fixing ----

// fix runs the next fixer step of a generated job (SPEC §8.3): the current
// result goes into the fixer's image input and the fixer's output replaces it.
func (s *Scheduler) fix(ctx context.Context, r *runner, client comfy.Client, job *domain.Job) error {
	if job.FinishedFixers >= len(job.Fixers) {
		return nil // every step is done; finish closes the job
	}
	step := job.Fixers[job.FinishedFixers]
	repos := s.DB.Repos()
	fixer, err := repos.Fixers.Get(ctx, step.FixerID)
	if err != nil {
		if appErr, ok := errors.FromError(err); !ok || appErr.Tag != "FixerNotFound" {
			return err
		}
		if fixer, err = repos.Fixers.GetByCode(ctx, step.FixerCode); err != nil {
			return err
		}
	}
	g, err := workflow.Parse(fixer.Workflow)
	if err != nil {
		return err
	}
	out, err := workflow.FindByTitle(g, fixer.SaveImageTitle)
	if err != nil {
		return err
	}
	image, err := s.upload(ctx, client, job.ResultPath)
	if err != nil {
		return err
	}
	if err := workflow.SetInput(g, fixer.LoadImageTitle, "image", image); err != nil {
		return err
	}
	if err := workflow.SetPrompts(g, fixer.PositivePromptTitle, fixer.PositivePrompt, fixer.NegativePromptTitle, fixer.NegativePrompt); err != nil {
		return err
	}
	if err := s.submit(ctx, r, client, g, job); err != nil {
		return err
	}
	res, err := client.Wait(ctx, job.PromptID)
	if err != nil {
		return err
	}
	return s.collect(ctx, r, client, job, res, out.ID)
}

// ---- outcome ----

// finish records what became of a stage.
func (s *Scheduler) finish(ctx context.Context, r *runner, client comfy.Client, job *domain.Job, generation bool, err error) {
	if err == nil {
		s.stageDone(ctx, r, job, generation)
		return
	}
	switch classify(ctx, err) {
	case outcomeShutdown:
		// The application is stopping: the job stays `processing` and the next
		// start recovers it.
	case outcomeLost:
		s.cancelPrompt(ctx, client, job)
		s.afterLoss(ctx, job, r.code())
	default:
		s.failed(ctx, r, job, err)
	}
	r.s.Wake()
}

type outcome int

const (
	outcomeFailed outcome = iota
	outcomeLost
	outcomeShutdown
)

// classify tells a failure of the workflow from the loss of the server and from
// the application stopping (SPEC §8.2, §8.7).
func classify(ctx context.Context, err error) outcome {
	if serverLost(ctx) {
		return outcomeLost
	}
	if ctx.Err() != nil {
		return outcomeShutdown
	}
	if appErr, ok := errors.FromError(err); ok {
		switch appErr.Tag {
		case "ServerUnreachable", "ComfyUIPromptLost":
			return outcomeLost
		case "ComfyUICallCancelled":
			return outcomeShutdown
		}
	}
	return outcomeFailed
}

// stageDone records a finished generation or fixer step, and either closes the
// job or offers it to the scheduler again for the next step (SPEC §8.2, §8.3).
func (s *Scheduler) stageDone(ctx context.Context, r *runner, job *domain.Job, generation bool) {
	if generation {
		job.IsGenerated = true
	} else {
		job.FinishedFixers++
	}
	job.Error, job.ErrorTrace = "", ""
	done := job.IsGenerated && job.FinishedFixers >= len(job.Fixers)
	if done {
		job.IsFixed = len(job.Fixers) > 0
		job.Status = domain.StatusFinished
	} else {
		job.Status = domain.StatusQueued
	}
	s.save(ctx, job)
	if done {
		s.emit(events.Finished, job, r.code())
	}
	s.syncCommand(ctx, job.CommandID)
}

// afterLoss handles a job whose server was lost: it is queued again, and after
// MaxAttempts it fails with a message that names the server. The rest of its
// command carries on (SPEC §8.7).
func (s *Scheduler) afterLoss(ctx context.Context, job *domain.Job, server string) {
	job.Attempts++
	if job.Attempts >= s.MaxAttempts {
		name := server
		if name == "" {
			name = job.ServerHost
		}
		job.Status = domain.StatusError
		job.Error = "server " + name + " became unreachable; gave up after " + strconv.Itoa(job.Attempts) + " attempts"
		job.ErrorTrace = ""
		s.save(ctx, job)
		s.emit(events.Error, job, server)
	} else {
		job.Status = domain.StatusQueued
		s.save(ctx, job)
	}
	s.syncCommand(ctx, job.CommandID)
	s.Wake()
}

// failed records a workflow failure: the error and its trace go on the job, the
// other queued jobs of the command return to idle so the batch halts, and the
// jobs already running elsewhere finish (SPEC §8.2).
func (s *Scheduler) failed(ctx context.Context, r *runner, job *domain.Job, err error) {
	job.Status = domain.StatusError
	job.Error, job.ErrorTrace = describe(err)
	s.save(ctx, job)
	if job.CommandID > 0 {
		bg, cancel := context.WithTimeout(context.WithoutCancel(ctx), saveTimeout)
		defer cancel()
		if _, stopErr := s.DB.Repos().Jobs.StopQueued(bg, job.CommandID, job.ID); stopErr != nil {
			logError(s.Logger, "could not halt the queued jobs of a command", stopErr, "command", job.CommandID)
		}
	}
	s.emit(events.Error, job, r.code())
	s.syncCommand(ctx, job.CommandID)
}

// cancelPrompt asks a server to drop the prompt of a job it was running, as far
// as it can still be reached.
func (s *Scheduler) cancelPrompt(ctx context.Context, client comfy.Client, job *domain.Job) {
	if job.PromptID == "" {
		return
	}
	bg, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer cancel()
	if err := client.Cancel(bg, job.PromptID); err != nil {
		s.Logger.Debug("could not cancel a prompt", "prompt", job.PromptID, "error", err.Error())
	}
}

// describe turns an error into the message and the replayable trace stored on a
// failed job (SPEC §17.2).
func describe(err error) (message, trace string) {
	appErr, ok := errors.FromError(err)
	if !ok {
		return err.Error(), ""
	}
	message = appErr.Tag + ": " + appErr.Message
	if j, jerr := appErr.ToJson(); jerr == nil {
		if b, merr := json.Marshal(j); merr == nil {
			trace = string(b)
		}
	}
	return message, trace
}

// save writes a job even when its context has ended.
func (s *Scheduler) save(ctx context.Context, job *domain.Job) {
	bg, cancel := context.WithTimeout(context.WithoutCancel(ctx), saveTimeout)
	defer cancel()
	if err := s.DB.Repos().Jobs.Update(bg, job); err != nil {
		logError(s.Logger, "could not save a job", err, "job", job.ID)
	}
}

func (s *Scheduler) emit(t events.Type, job *domain.Job, server string) {
	s.Events.Append(events.Event{Type: t, JobID: job.ID, CommandID: job.CommandID, ProjectID: job.ProjectID, Server: server})
}
