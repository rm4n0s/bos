package service

import (
	"hash/fnv"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/rm4n0s/bos/internal/command"
	"github.com/rm4n0s/bos/internal/domain"
	"github.com/rm4n0s/bos/internal/workflow"
)

// maxFileStem bounds the readable part of a result file name; longer names are
// cut and given a hash of the whole, so two different jobs never collide.
const maxFileStem = 150

// jobsOf turns the plan of an expansion into the jobs to store (SPEC §7.1).
// Every job is a snapshot: it copies what it needs from the catalogue. cat must
// already have read every entity the expansion names.
func (s *CommandService) jobsOf(cmd *domain.Command, parsed *command.Command, x *command.Expansion, cat *catalogue) []*domain.Job {
	fixers := make([]domain.FixerStep, len(x.Fixers))
	for i, f := range x.Fixers {
		step := domain.FixerStep{FixerID: cat.fixer(f.Fixer).ID, FixerCode: f.Fixer}
		if f.On != "" {
			step.ServerID, step.ServerCode = cat.server(f.On).ID, f.On
		}
		fixers[i] = step
	}
	generator := parsed.Generator.Text
	repeats := parsed.Repeat != nil && parsed.Repeat.N > 1
	used := make(map[string]bool, len(x.Jobs))

	jobs := make([]*domain.Job, len(x.Jobs))
	for i := range x.Jobs {
		pj := &x.Jobs[i]
		out := x.EffectiveOutput(pj)
		j := &domain.Job{
			ProjectID: cmd.ProjectID, CommandID: cmd.ID, CommandText: cmd.CommandCode, Status: domain.StatusIdle,
			Fixers: slices.Clone(fixers), RepeatIndex: pj.Repeat, RegionIndex: pj.RegionIndex,
			GeneratorCodeName: generator, OutputType: domain.OutputType(out.Type), OutputAttributes: attributesOf(out.Attrs),
			PositivePrompt: pj.Positive, NegativePrompt: pj.Negative, Pinned: pj.Pinned,
		}
		for _, ref := range pj.Items {
			j.Items = append(j.Items, domain.UsedItem{
				GroupID: cat.group(ref.Group).group.ID, GroupCode: ref.Group, ItemID: ref.ID, ItemCode: ref.Code,
			})
		}
		if pj.Preferred != "" {
			id := cat.server(pj.Preferred).ID
			j.PreferredServerID = &id
		}
		// A generator with a random seed gets a fresh seed when the job runs,
		// unless the command sets one.
		if _, set := x.Settings["seed"]; set || !x.Generator.HasRandomSeed {
			if seed, ok := out.Attrs["seed"]; ok && seed.Kind == command.ValueInt {
				n := seed.Int
				j.Seed = &n
			}
		}
		for _, l := range pj.LoRAs {
			j.LoRAs = append(j.LoRAs, domain.LoRA{Name: l.Name, StrengthModel: l.StrengthModel, StrengthClip: l.StrengthClip})
		}
		for _, c := range pj.ControlNets {
			j.ControlNets = append(j.ControlNets, domain.ControlNet{
				Type: c.Type, ImagePath: c.ImagePath, IsReference: c.IsReference, ModelPattern: c.ModelPattern, Strength: c.Strength,
			})
		}
		for _, ip := range pj.IPAdapters {
			j.IPAdapters = append(j.IPAdapters, domain.IPAdapter{
				ImageFile: ip.ImageFile, Weight: ip.Weight, WeightType: ip.WeightType, StartAt: ip.StartAt, EndAt: ip.EndAt,
				ClipVisionModel: ip.ClipVisionModel, ModelName: ip.ModelName,
			})
		}
		for _, r := range pj.Regions {
			rp := domain.RegionPrompt{Keyword: r.Keyword, MaskFile: r.MaskFile, Prompt: r.Prompt}
			if r.Coordinates != nil {
				rp.Coordinates = &domain.CoordinatedRegion{
					Keyword: r.Coordinates.Keyword, Width: r.Coordinates.Width, Height: r.Coordinates.Height,
					X: r.Coordinates.X, Y: r.Coordinates.Y,
				}
			}
			// A model is patched once for the whole graph, so the LoRAs of a
			// region join the job's list, each name once.
			for _, l := range r.LoRAs {
				lora := domain.LoRA{Name: l.Name, StrengthModel: l.StrengthModel, StrengthClip: l.StrengthClip}
				rp.LoRAs = append(rp.LoRAs, lora)
				if !hasLoRA(j.LoRAs, l.Name) {
					j.LoRAs = append(j.LoRAs, lora)
				}
			}
			j.RegionPrompts = append(j.RegionPrompts, rp)
		}
		j.ResultPath = s.resultPath(cmd.ID, generator, pj, repeats, string(j.OutputType), j.OutputAttributes, i, used)
		jobs[i] = j
	}
	return jobs
}

// uniqueLoRAs keeps the first entry of each LoRA file, in order.
func uniqueLoRAs(list []domain.LoRA) []domain.LoRA {
	var out []domain.LoRA
	for _, l := range list {
		if !hasLoRA(out, l.Name) {
			out = append(out, l)
		}
	}
	return out
}

func hasLoRA(list []domain.LoRA, name string) bool {
	for _, l := range list {
		if workflow.SameLoRA(l.Name, name) {
			return true
		}
	}
	return false
}

// resultPath plans where the result of a job is saved (SPEC §7.1): a name made
// of the generator, the command, the chosen items, the region and the repeat
// index. It does not name the server, which is unknown under dynamic spread.
func (s *CommandService) resultPath(commandID int64, generator string, pj *command.Job, repeats bool, outputType string,
	attrs domain.OutputAttributes, index int, used map[string]bool) string {
	var parts []string
	parts = append(parts, generator, "c"+strconv.FormatInt(commandID, 10))
	for _, p := range pj.Parts {
		if p.Group == "" {
			parts = append(parts, fileSafe(p.Code))
		} else {
			parts = append(parts, fileSafe(p.Group)+"-"+fileSafe(p.Code))
		}
	}
	if pj.RegionIndex >= 0 {
		parts = append(parts, "g"+strconv.Itoa(pj.RegionIndex))
	}
	if repeats {
		parts = append(parts, "r"+strconv.Itoa(pj.Repeat))
	}
	stem := strings.Join(parts, "_")
	if len(stem) > maxFileStem {
		h := fnv.New64a()
		_, _ = h.Write([]byte(stem)) // a hash.Hash never returns an error
		stem = stem[:maxFileStem] + "_" + strconv.FormatUint(h.Sum64(), 16)
	}
	ext := fileExtension(outputType, attrs)
	name := stem + "." + ext
	if used[name] {
		name = stem + "_" + strconv.Itoa(index) + "." + ext
	}
	used[name] = true
	return filepath.Join(s.Dirs.Results, strconv.FormatInt(commandID, 10), name)
}

// fileSafe keeps letters, digits, underscore and hyphen.
func fileSafe(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '_' || r == '-' || r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' {
			return r
		}
		return '_'
	}, s)
}

// fileExtension is the extension of the planned result: the file type of the
// output attributes, or the usual one of the output type. The scheduler corrects
// it if the server answers with another kind of file.
func fileExtension(outputType string, attrs domain.OutputAttributes) string {
	if a, ok := attrs["file_type"]; ok && a.Kind == domain.AttrText {
		if ext := strings.ToLower(strings.TrimPrefix(a.Text, ".")); ext != "" && fileSafe(ext) == ext {
			return ext
		}
	}
	switch domain.OutputType(outputType) {
	case domain.OutputVideo:
		return "mp4"
	case domain.Output3D:
		return "glb"
	case domain.OutputImage:
		return "png"
	}
	return "bin"
}
