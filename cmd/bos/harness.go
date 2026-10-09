package main

// The harness is TEMPORARY, for Milestone A of docs/IMPLEMENTATION.md: it runs
// one command end to end on a real ComfyUI, through the services and the
// scheduler, so that the generation can be checked before the interfaces are
// built. It is removed in step 12.

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/rm4n0s/bos/internal/config"
	"github.com/rm4n0s/bos/internal/domain"
	"github.com/rm4n0s/bos/internal/files"
	"github.com/rm4n0s/bos/internal/service"
	"github.com/rm4n0s/errors"
)

// harnessOptions are the flags of `bos harness`.
type harnessOptions struct {
	config        string
	server        string
	workflow      string
	positiveTitle string
	negativeTitle string
	prompts       string
	fixerWorkflow string
	loadTitle     string
	saveTitle     string
	timeout       time.Duration
}

func parseHarness(args []string, out io.Writer) (harnessOptions, error) {
	var o harnessOptions
	fs := flag.NewFlagSet("bos harness", flag.ContinueOnError)
	fs.SetOutput(out)
	fs.StringVar(&o.config, "config", "", "path to the JSON configuration file")
	fs.StringVar(&o.server, "server", "http://127.0.0.1:8188", "address of the ComfyUI server")
	fs.StringVar(&o.workflow, "workflow", "", "generator workflow in ComfyUI API format (required)")
	fs.StringVar(&o.positiveTitle, "positive-title", "", "title of the positive prompt node (default: the first text-encode node)")
	fs.StringVar(&o.negativeTitle, "negative-title", "", "title of the negative prompt node (optional)")
	fs.StringVar(&o.prompts, "prompts", "a cat,a dog", "comma-separated prompts, one job each")
	fs.StringVar(&o.fixerWorkflow, "fixer-workflow", "", "optional fixer workflow, run on every result")
	fs.StringVar(&o.loadTitle, "fixer-load-title", "Load Image", "title of the fixer's image input node")
	fs.StringVar(&o.saveTitle, "fixer-save-title", "Save Image", "title of the fixer's image output node")
	fs.DurationVar(&o.timeout, "timeout", 10*time.Minute, "how long to wait for the command")
	if err := fs.Parse(args); err != nil {
		return o, err
	}
	if o.workflow == "" {
		return o, errors.New("InvalidRequest", "-workflow is required")
	}
	return o, nil
}

// runHarness creates a server, a generator, a group with one item per prompt and
// a command over them, runs it and prints how it goes. It fails if the command
// does not finish.
func runHarness(ctx context.Context, args []string, getenv func(string) string, logger *slog.Logger, out io.Writer, opts ...appOption) error {
	o, err := parseHarness(args, out)
	if err != nil {
		return err
	}
	cfg, err := config.Load(o.config, getenv)
	if err != nil {
		return err
	}
	if err := files.EnsureDirs(cfg.Directories()); err != nil {
		return err
	}
	a, err := newApp(ctx, cfg, logger, opts...)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := a.close(); cerr != nil {
			logError(logger, cerr)
		}
	}()
	stop := a.run(ctx, logger)
	defer stop()
	return harness(ctx, a, o, out)
}

func harness(ctx context.Context, a *app, o harnessOptions, out io.Writer) error {
	svc := a.services
	say := func(format string, args ...any) { _, _ = fmt.Fprintf(out, format+"\n", args...) } // a closed pipe is no reason to stop
	tag := "h" + strconv.FormatInt(time.Now().Unix(), 10)

	wf, err := os.ReadFile(o.workflow)
	if err != nil {
		return errors.NewErr("FileStoreFailure", err).SetMetadata("path", o.workflow, "operation", "read")
	}
	analysis, err := svc.Generators.Analyze(wf)
	if err != nil {
		return err
	}
	say("workflow: %s output, node %q, prompt nodes %q", analysis.OutputType, analysis.OutputNodeTitle, analysis.PromptTitles)
	if o.positiveTitle == "" && len(analysis.PromptTitles) > 0 {
		o.positiveTitle = analysis.PromptTitles[0]
	}

	srv, err := svc.Servers.Create(ctx, domain.Server{Name: tag, Host: o.server, CodeName: tag, MaxConcurrentJobs: 1})
	if err != nil {
		return err
	}
	gen, err := svc.Generators.Create(ctx, domain.Generator{
		Name: tag, CodeName: tag, Workflow: wf, PositivePromptTitle: o.positiveTitle, NegativePromptTitle: o.negativeTitle,
	})
	if err != nil {
		return err
	}
	grp, err := svc.Groups.Create(ctx, domain.Group{Name: tag, CodeName: tag})
	if err != nil {
		return err
	}
	for i, p := range strings.Split(o.prompts, ",") {
		if _, err := svc.Items.Create(ctx, grp.ID, domain.Item{CodeName: "p" + strconv.Itoa(i), PositivePrompt: strings.TrimSpace(p)}); err != nil {
			return err
		}
	}
	code := fmt.Sprintf("%s -$ %s: %s", srv.CodeName, gen.CodeName, grp.CodeName)
	if o.fixerWorkflow != "" {
		fw, err := os.ReadFile(o.fixerWorkflow)
		if err != nil {
			return errors.NewErr("FileStoreFailure", err).SetMetadata("path", o.fixerWorkflow, "operation", "read")
		}
		if _, err := svc.Fixers.Create(ctx, domain.Fixer{Name: tag, CodeName: tag, Workflow: fw, LoadImageTitle: o.loadTitle, SaveImageTitle: o.saveTitle}); err != nil {
			return err
		}
		code += " > " + tag
	}

	say("waiting for the server %s to answer ...", o.server)
	if err := waitFor(ctx, o.timeout, func() bool { return a.sched.Available(srv.ID) }); err != nil {
		return errors.New("ServerUnreachable", "the server did not answer", "host", o.server)
	}
	project, err := svc.Projects.Create(ctx, domain.Project{Name: tag})
	if err != nil {
		return err
	}
	pv, err := svc.Commands.Preview(ctx, code)
	if err != nil {
		return err
	}
	say("command: %s", code)
	say("preview: valid=%v jobs=%d warnings=%d problems=%d", pv.Valid, pv.Jobs, len(pv.Warnings), len(pv.Problems))
	for _, w := range pv.Warnings {
		say("  warning %s: %s", w.Code, w.Message)
	}
	cmd, err := svc.Commands.Add(ctx, project.ID, service.CommandInput{Name: tag, Code: code})
	if err != nil {
		return err
	}
	if _, err := svc.Commands.Run(ctx, cmd.ID); err != nil {
		return err
	}
	say("running command %d ...", cmd.ID)
	var last domain.Command
	err = waitFor(ctx, o.timeout, func() bool {
		c, gerr := svc.Commands.Get(ctx, cmd.ID)
		if gerr != nil {
			return false
		}
		last = *c
		return c.Status == domain.StatusFinished || c.Status == domain.StatusError
	})
	jobs, jerr := svc.Jobs.ListByCommand(ctx, cmd.ID, "", domain.Page{Size: domain.MaxPageSize})
	if jerr != nil {
		return jerr
	}
	for _, j := range jobs.Items {
		say("job %d: %s attempts=%d seed=%v result=%s", j.ID, j.Status, j.Attempts, seedOf(j), j.ResultPath)
		if j.Error != "" {
			say("  error: %s", j.Error)
		}
		for _, w := range j.Warnings {
			say("  warning: %s", w)
		}
	}
	if err != nil {
		return errors.New("CommandNotFinished", "the command did not end in time", "status", string(last.Status))
	}
	if last.Status != domain.StatusFinished {
		return errors.New("CommandNotFinished", "the command ended with errors", "status", string(last.Status))
	}
	say("done: every job finished")
	return nil
}

func seedOf(j domain.Job) string {
	if j.Seed == nil {
		return "-"
	}
	return strconv.FormatUint(*j.Seed, 10)
}

// waitFor polls cond until it holds, the timeout passes or ctx ends.
func waitFor(ctx context.Context, timeout time.Duration, cond func() bool) error {
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	for !cond() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return context.DeadlineExceeded
		case <-tick.C:
		}
	}
	return nil
}
