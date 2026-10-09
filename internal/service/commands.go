package service

import (
	"context"
	"fmt"
	"sort"
	"strconv"

	"github.com/rm4n0s/bos/internal/command"
	"github.com/rm4n0s/bos/internal/domain"
	"github.com/rm4n0s/errors"
)

// loraCheckMaxJobs is the size above which the preview does not expand a
// command to look for LoRAs that a server lacks: the check needs the jobs
// themselves.
const loraCheckMaxJobs = 10_000

// CommandService manages commands and creates their jobs (SPEC §7).
type CommandService struct{ base }

// CommandInput is what a client sends to add or edit a command.
type CommandInput struct {
	Name        string
	Description string
	// Code is the command text (SPEC §6).
	Code string
	// InsertAt is the 1-based position of a new command; a value below 1 or past
	// the end appends. It is ignored when editing.
	InsertAt int
}

func checkCommandInput(in CommandInput) error {
	if in.Name != "" {
		if err := checkName("command", in.Name, maxName); err != nil {
			return err
		}
	}
	return nil
}

// compile parses and validates command text against the catalogue and expands
// it into its plan. Nothing is stored. The parsed form carries the `sample`
// seed, generated if the text had none (SPEC §6.7).
func (s *CommandService) compile(ctx context.Context, r Repos, code string) (*command.Command, *command.Expansion, *catalogue, error) {
	parsed, err := command.Parse(code)
	if err != nil {
		return nil, nil, nil, err
	}
	command.EnsureSampleSeed(parsed, s.NewSeed)
	cat := newCatalogue(ctx, r)
	x, err := command.Expand(parsed, cat, s.Limits.options())
	if cat.err != nil {
		return nil, nil, nil, cat.err
	}
	if err != nil {
		return nil, nil, nil, err
	}
	return parsed, x, cat, nil
}

// Preview is the dry run of SPEC §7.2: it reports what adding the text would
// create and persists nothing. Problems with the text are part of the result,
// never an error; only a failure of the store is.
func (s *CommandService) Preview(ctx context.Context, code string) (*command.Preview, error) {
	parsed, err := command.Parse(code)
	if err != nil {
		p, ok := command.ProblemOf(err)
		if !ok {
			return nil, err
		}
		return &command.Preview{Problems: []command.Problem{p}, Warnings: []command.Warning{}, PerServer: []command.ServerCount{}}, nil
	}
	command.EnsureSampleSeed(parsed, s.NewSeed)
	cat := newCatalogue(ctx, s.Store.Repos())
	pv := command.PreviewOf(parsed, cat, s.Limits.options())
	if cat.err != nil {
		return nil, cat.err
	}
	if !pv.Valid {
		return &pv, nil
	}
	if limit := s.Limits.PreviewSoftLimit; limit > 0 && pv.Jobs > limit {
		pv.Warnings = append(pv.Warnings, command.Warning{
			Code:    "TooManyJobs",
			Message: fmt.Sprintf("the command creates %d jobs, more than the soft limit of %d", pv.Jobs, limit),
		})
	}
	if s.Fleet != nil {
		warnings, err := s.fleetWarnings(ctx, parsed, cat, pv.Jobs)
		if err != nil {
			return nil, err
		}
		pv.Warnings = append(pv.Warnings, warnings...)
	}
	return &pv, nil
}

// fleetWarnings reports servers of the command that are offline, and LoRAs
// that an online server does not have (SPEC §6.11).
func (s *CommandService) fleetWarnings(ctx context.Context, parsed *command.Command, cat *catalogue, jobs int) ([]command.Warning, error) {
	type target struct {
		name command.Name
		srv  *domain.Server
	}
	var targets []target
	seen := map[string]bool{}
	add := func(n command.Name) {
		if !seen[n.Text] {
			seen[n.Text] = true
			targets = append(targets, target{name: n, srv: cat.server(n.Text)})
		}
	}
	for _, t := range parsed.Servers {
		add(t.Server)
	}
	for _, f := range parsed.Fixers {
		if f.On != nil {
			add(*f.On)
		}
	}
	var warnings []command.Warning
	var online []target
	for _, t := range targets {
		if t.srv == nil {
			continue
		}
		if s.Fleet.Available(t.srv.ID) {
			online = append(online, t)
		} else {
			warnings = append(warnings, command.Warning{
				Code: "ServerOffline", Message: "server " + strconv.Quote(t.name.Text) + " is offline", Position: t.name.Pos,
			})
		}
	}
	if len(online) == 0 || jobs > loraCheckMaxJobs {
		return warnings, nil
	}
	x, err := command.Expand(parsed, cat, s.Limits.options())
	if cat.err != nil {
		return nil, cat.err
	}
	if err != nil {
		return nil, err
	}
	usedSet := map[string]bool{}
	for i := range x.Jobs {
		for _, l := range x.Jobs[i].LoRAs {
			usedSet[l.Name] = true
		}
		for _, r := range x.Jobs[i].Regions {
			for _, l := range r.LoRAs {
				usedSet[l.Name] = true
			}
		}
	}
	if len(usedSet) == 0 {
		return warnings, nil
	}
	used := make([]string, 0, len(usedSet))
	for n := range usedSet {
		used = append(used, n)
	}
	sort.Strings(used)
	for _, t := range online {
		client, err := s.Fleet.Client(ctx, *t.srv)
		if err == nil {
			var installed []string
			if installed, err = client.ListModels(ctx, "loras"); err == nil {
				for _, name := range missingLoRAs(used, installed) {
					warnings = append(warnings, command.Warning{
						Code:     "LoRAMissing",
						Message:  "server " + strconv.Quote(t.name.Text) + " does not have the LoRA " + strconv.Quote(name),
						Position: t.name.Pos,
					})
				}
				continue
			}
		}
		warnings = append(warnings, command.Warning{
			Code: "ModelsUnknown", Message: "could not list the LoRAs of server " + strconv.Quote(t.name.Text), Position: t.name.Pos,
		})
	}
	return warnings, nil
}

func (s *CommandService) busyError(c *domain.Command) error {
	if busy(c.Status) {
		return errors.New("CommandBusy", "the command is queued or running; stop it first", "command_id", c.ID, "status", string(c.Status))
	}
	return nil
}

// Add validates a command, stores it in the project and creates its jobs
// (SPEC §7.1, §7.2). Nothing is stored when the text is not valid.
func (s *CommandService) Add(ctx context.Context, project domain.ID, in CommandInput) (*domain.Command, error) {
	if err := checkCommandInput(in); err != nil {
		return nil, err
	}
	r := s.Store.Repos()
	if _, err := r.Projects().Get(ctx, project); err != nil {
		return nil, err
	}
	parsed, x, cat, err := s.compile(ctx, r, in.Code)
	if err != nil {
		return nil, err
	}
	form, err := command.ToJSON(parsed)
	if err != nil {
		return nil, err
	}
	cmd := &domain.Command{
		ProjectID: project, Name: in.Name, Description: in.Description, CommandCode: in.Code,
		CommandJSON: form, Status: domain.StatusIdle,
	}
	err = s.Store.InTx(ctx, func(tx Repos) error {
		if err := tx.Commands().Create(ctx, cmd, in.InsertAt); err != nil {
			return err
		}
		return tx.Jobs().CreateMany(ctx, s.jobsOf(cmd, parsed, x, cat))
	})
	if err != nil {
		return nil, err
	}
	return cmd, nil
}

// Edit changes a command. When the text changed it is parsed and validated
// again, the old jobs and their files are deleted and new jobs are created;
// otherwise only the name and description change (SPEC §7.2). A command that is
// queued or running cannot be edited.
func (s *CommandService) Edit(ctx context.Context, id domain.ID, in CommandInput) (*domain.Command, error) {
	if err := checkCommandInput(in); err != nil {
		return nil, err
	}
	r := s.Store.Repos()
	cmd, err := r.Commands().Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := s.busyError(cmd); err != nil {
		return nil, err
	}
	cmd.Name, cmd.Description = in.Name, in.Description
	if in.Code == cmd.CommandCode {
		if err := r.Commands().Update(ctx, cmd); err != nil {
			return nil, err
		}
		return cmd, nil
	}
	parsed, x, cat, err := s.compile(ctx, r, in.Code)
	if err != nil {
		return nil, err
	}
	form, err := command.ToJSON(parsed)
	if err != nil {
		return nil, err
	}
	cmd.CommandCode, cmd.CommandJSON = in.Code, form
	return s.replaceJobs(ctx, cmd, parsed, x, cat)
}

// Recreate deletes the jobs of a command and creates them again from its stored
// parsed form, so a `sample` picks the same combinations (SPEC §6.7, §7.2). The
// command becomes idle again.
func (s *CommandService) Recreate(ctx context.Context, id domain.ID) (*domain.Command, error) {
	r := s.Store.Repos()
	cmd, err := r.Commands().Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := s.busyError(cmd); err != nil {
		return nil, err
	}
	parsed, err := command.FromJSON(cmd.CommandJSON)
	if err != nil {
		return nil, err
	}
	cat := newCatalogue(ctx, r)
	x, err := command.Expand(parsed, cat, s.Limits.options())
	if cat.err != nil {
		return nil, cat.err
	}
	if err != nil {
		return nil, err
	}
	return s.replaceJobs(ctx, cmd, parsed, x, cat)
}

// replaceJobs stores cmd, deletes its old jobs and creates the planned ones, in
// one transaction; the command is idle afterwards and any export archive is
// stale. The files of the old jobs and archive are removed after the commit.
func (s *CommandService) replaceJobs(ctx context.Context, cmd *domain.Command, parsed *command.Command, x *command.Expansion, cat *catalogue) (*domain.Command, error) {
	oldArchive := cmd.DownloadFile
	cmd.Status, cmd.DownloadFile = domain.StatusIdle, ""
	var files []string
	err := s.Store.InTx(ctx, func(tx Repos) error {
		var err error
		if files, err = tx.Jobs().DeleteByCommand(ctx, cmd.ID); err != nil {
			return err
		}
		if err := tx.Commands().Update(ctx, cmd); err != nil {
			return err
		}
		return tx.Jobs().CreateMany(ctx, s.jobsOf(cmd, parsed, x, cat))
	})
	if err != nil {
		return nil, err
	}
	s.removeFiles(append(files, oldArchive))
	return cmd, nil
}

// Delete removes a command with its jobs and files and closes the gap in the
// order (SPEC §7.2). A command that is queued or running cannot be deleted.
func (s *CommandService) Delete(ctx context.Context, id domain.ID) error {
	r := s.Store.Repos()
	cmd, err := r.Commands().Get(ctx, id)
	if err != nil {
		return err
	}
	if err := s.busyError(cmd); err != nil {
		return err
	}
	files, err := r.Commands().Delete(ctx, id)
	if err != nil {
		return err
	}
	s.removeFiles(files)
	return nil
}

// Get returns one command.
func (s *CommandService) Get(ctx context.Context, id domain.ID) (*domain.Command, error) {
	return s.Store.Repos().Commands().Get(ctx, id)
}

// List returns one page of a project's commands in order, with the progress of
// their jobs.
func (s *CommandService) List(ctx context.Context, project domain.ID, p domain.Page) (domain.Paged[domain.CommandSummary], error) {
	return s.Store.Repos().Commands().ListByProject(ctx, project, p)
}

// Move puts a command at a position of its project, clamped to the ends, and
// returns where it ended up (SPEC §7.2).
func (s *CommandService) Move(ctx context.Context, id domain.ID, order int) (int, error) {
	return s.Store.Repos().Commands().Move(ctx, id, order)
}

// Up moves a command one place towards the start; at the start it stays.
func (s *CommandService) Up(ctx context.Context, id domain.ID) (int, error) {
	return s.step(ctx, id, -1)
}

// Down moves a command one place towards the end; at the end it stays.
func (s *CommandService) Down(ctx context.Context, id domain.ID) (int, error) {
	return s.step(ctx, id, 1)
}

func (s *CommandService) step(ctx context.Context, id domain.ID, delta int) (int, error) {
	cmds := s.Store.Repos().Commands()
	cmd, err := cmds.Get(ctx, id)
	if err != nil {
		return 0, err
	}
	return cmds.Move(ctx, id, cmd.Order+delta)
}

// Renumber reassigns the orders of a project's commands to 1…N.
func (s *CommandService) Renumber(ctx context.Context, project domain.ID) error {
	r := s.Store.Repos()
	if _, err := r.Projects().Get(ctx, project); err != nil {
		return err
	}
	return r.Commands().Renumber(ctx, project)
}

// Run queues the idle jobs of a command and hands it to the scheduler
// (SPEC §7.2). It fails with CommandNotRunnable while the command is already
// queued or running, or when it has no idle job.
func (s *CommandService) Run(ctx context.Context, id domain.ID) (*domain.Command, error) {
	r := s.Store.Repos()
	cmd, err := r.Commands().Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if busy(cmd.Status) {
		return nil, errors.New("CommandNotRunnable", "the command is already queued or running", "command_id", id, "status", string(cmd.Status))
	}
	n, err := r.Jobs().QueueCommand(ctx, id)
	if err != nil {
		return nil, err
	}
	if n == 0 {
		return nil, errors.New("CommandNotRunnable", "the command has no idle job to run", "command_id", id)
	}
	if err := r.Commands().SetStatus(ctx, id, domain.StatusQueued); err != nil {
		return nil, err
	}
	s.wake()
	return r.Commands().Get(ctx, id)
}

// Stop returns the queued jobs of a command to idle; a job that is running
// finishes (SPEC §7.2). It returns how many jobs it stopped.
func (s *CommandService) Stop(ctx context.Context, id domain.ID) (int, error) {
	r := s.Store.Repos()
	if _, err := r.Commands().Get(ctx, id); err != nil {
		return 0, err
	}
	n, err := r.Jobs().StopQueued(ctx, id, 0)
	if err != nil {
		return 0, err
	}
	if err := s.syncCommand(ctx, r, id); err != nil {
		return 0, err
	}
	return n, nil
}
