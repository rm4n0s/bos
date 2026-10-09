package service

import (
	"context"

	"github.com/rm4n0s/bos/internal/domain"
	"github.com/rm4n0s/errors"
)

// JobService reads jobs and runs, stops and edits single ones (SPEC §8.4).
type JobService struct{ base }

// Get returns a job with its error and trace.
func (s *JobService) Get(ctx context.Context, id domain.ID) (*domain.Job, error) {
	return s.Store.Repos().Jobs().Get(ctx, id)
}

// ListByCommand returns one page of a command's jobs in job order; a non-empty
// status keeps the jobs in that status.
func (s *JobService) ListByCommand(ctx context.Context, command domain.ID, status domain.Status, p domain.Page) (domain.Paged[domain.Job], error) {
	r := s.Store.Repos()
	if _, err := r.Commands().Get(ctx, command); err != nil {
		return domain.Paged[domain.Job]{}, err
	}
	if status != "" && !status.Valid() {
		return domain.Paged[domain.Job]{}, errors.New("InvalidRequest", "unknown job status", "status", string(status))
	}
	return r.Jobs().ListByCommand(ctx, command, status, p)
}

// Run queues an idle job (SPEC §8.4). Any other status is JobNotRunnable.
func (s *JobService) Run(ctx context.Context, id domain.ID) (*domain.Job, error) {
	r := s.Store.Repos()
	job, err := r.Jobs().Get(ctx, id)
	if err != nil {
		return nil, err
	}
	queued, err := r.Jobs().QueueJob(ctx, id)
	if err != nil {
		return nil, err
	}
	if !queued {
		return nil, errors.New("JobNotRunnable", "only an idle job can be run", "job_id", id, "status", string(job.Status))
	}
	if job.CommandID > 0 {
		if err := s.syncCommand(ctx, r, job.CommandID); err != nil {
			return nil, err
		}
	}
	s.wake()
	return r.Jobs().Get(ctx, id)
}

// Stop returns a queued job to idle and resets its progress; a job that is
// running finishes. Any other status is JobNotRunnable.
func (s *JobService) Stop(ctx context.Context, id domain.ID) (*domain.Job, error) {
	r := s.Store.Repos()
	job, err := r.Jobs().Get(ctx, id)
	if err != nil {
		return nil, err
	}
	stopped, err := r.Jobs().StopJob(ctx, id)
	if err != nil {
		return nil, err
	}
	if !stopped {
		return nil, errors.New("JobNotRunnable", "only a queued job can be stopped", "job_id", id, "status", string(job.Status))
	}
	if job.CommandID > 0 {
		if err := s.syncCommand(ctx, r, job.CommandID); err != nil {
			return nil, err
		}
	}
	return r.Jobs().Get(ctx, id)
}

// EditPrompts changes the prompts of a job. If either changed, the job starts
// over: its result is removed, its generation and fixing flags and its fixer
// progress are reset and it becomes idle (SPEC §8.4). A job that is running
// cannot be edited (JobBusy).
func (s *JobService) EditPrompts(ctx context.Context, id domain.ID, positive, negative string) (*domain.Job, error) {
	r := s.Store.Repos()
	job, err := r.Jobs().Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if job.Status == domain.StatusProcessing {
		return nil, errors.New("JobBusy", "the job is running; wait for it to end", "job_id", id)
	}
	if job.PositivePrompt == positive && job.NegativePrompt == negative {
		return job, nil
	}
	result := job.ResultPath
	job.PositivePrompt, job.NegativePrompt = positive, negative
	job.IsGenerated, job.IsFixed, job.FinishedFixers = false, false, 0
	job.PromptID, job.Error, job.ErrorTrace = "", "", ""
	job.Status = domain.StatusIdle
	if err := r.Jobs().Update(ctx, job); err != nil {
		return nil, err
	}
	s.removeFiles([]string{result})
	if job.CommandID > 0 {
		if err := s.syncCommand(ctx, r, job.CommandID); err != nil {
			return nil, err
		}
	}
	return job, nil
}

// Counts returns how many jobs of a command are in each status.
func (s *JobService) Counts(ctx context.Context, command domain.ID) (map[domain.Status]int, error) {
	r := s.Store.Repos()
	if _, err := r.Commands().Get(ctx, command); err != nil {
		return nil, err
	}
	return r.Jobs().StatusCounts(ctx, command)
}
