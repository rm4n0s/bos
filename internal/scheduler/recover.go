package scheduler

import (
	"context"
	"time"

	"github.com/rm4n0s/bos/internal/domain"
	"github.com/rm4n0s/bos/internal/events"
	"github.com/rm4n0s/errors"
)

// recoverJobs repairs what a previous run left behind (SPEC §11, Reliability).
//
// A job that was `processing` had a server working on it. If its generation
// prompt was submitted, the server that ran it is asked for the result when it
// is available again (the history of ComfyUI still has it). Every other job —
// a fixer step, or a job that had not submitted its prompt — is queued again; a
// fixer step starts over, which is safe because the image is replaced only when
// the step is done. Queued jobs stay queued. Finally the status of every command
// that was waiting or running is recomputed from its jobs.
func (s *Scheduler) recoverJobs(ctx context.Context) error {
	repos := s.DB.Repos()
	processing, err := repos.Jobs.ListByStatus(ctx, domain.StatusProcessing)
	if err != nil {
		return err
	}
	for i := range processing {
		j := &processing[i]
		if !j.IsGenerated && j.PromptID != "" && j.ServerID != nil {
			s.resMu.Lock()
			s.resumable[*j.ServerID] = append(s.resumable[*j.ServerID], j.ID)
			s.resMu.Unlock()
			continue
		}
		if _, err := repos.Jobs.Transition(ctx, j.ID, domain.StatusProcessing, domain.StatusQueued); err != nil {
			return err
		}
	}
	cmds, err := repos.Commands.ListByStatus(ctx, domain.StatusQueued, domain.StatusProcessing)
	if err != nil {
		return err
	}
	for _, c := range cmds {
		s.syncCommand(ctx, c.ID)
	}
	return nil
}

// takeResumable hands out one job to resume on a server.
func (s *Scheduler) takeResumable(server domain.ID) (domain.ID, bool) {
	s.resMu.Lock()
	defer s.resMu.Unlock()
	ids := s.resumable[server]
	if len(ids) == 0 {
		return 0, false
	}
	s.resumable[server] = ids[1:]
	return ids[0], true
}

// requeueResumable gives up waiting for the jobs of a server that is gone: they
// are tried again like jobs whose server was lost.
func (s *Scheduler) requeueResumable(server domain.ID) {
	s.resMu.Lock()
	ids := s.resumable[server]
	delete(s.resumable, server)
	root := s.root
	s.resMu.Unlock()
	if len(ids) == 0 {
		return
	}
	if root == nil {
		return
	}
	ctx := context.WithoutCancel(root)
	for _, id := range ids {
		job, err := s.DB.Repos().Jobs.Get(ctx, id)
		if err != nil || job.Status != domain.StatusProcessing {
			continue
		}
		s.afterLoss(ctx, job, "")
	}
}

// requeueStale gives up on the resumable jobs of a server that has been down
// since the given time, once that is longer than the grace period.
func (s *Scheduler) requeueStale(server domain.ID, since time.Time) {
	if s.Now().Sub(since) > s.Grace {
		s.requeueResumable(server)
	}
}

// syncCommand brings the status of a command in line with the statuses of its
// jobs, and announces a command that has just finished. The reserved REPL job
// belongs to no command.
func (s *Scheduler) syncCommand(ctx context.Context, id domain.ID) {
	if id <= 0 {
		return
	}
	ctx = context.WithoutCancel(ctx)
	repos := s.DB.Repos()
	counts, err := repos.Jobs.StatusCounts(ctx, id)
	if err != nil {
		logError(s.Logger, "could not count the jobs of a command", err, "command", id)
		return
	}
	cmd, err := repos.Commands.Get(ctx, id)
	if err != nil {
		if appErr, ok := errors.FromError(err); !ok || appErr.Tag != "CommandNotFound" {
			logError(s.Logger, "could not read a command", err, "command", id)
		}
		return
	}
	status := domain.CommandStatusOf(counts)
	if status == cmd.Status {
		return
	}
	if err := repos.Commands.SetStatus(ctx, id, status); err != nil {
		logError(s.Logger, "could not set the status of a command", err, "command", id)
		return
	}
	if status == domain.StatusFinished {
		s.Events.Append(events.Event{Type: events.CommandFinished, CommandID: id, ProjectID: cmd.ProjectID})
	}
}
