package store

import (
	"context"

	"github.com/rm4n0s/bos/internal/domain"
	"github.com/rm4n0s/errors"
	"gorm.io/gorm"
)

// JobRepo stores jobs (SPEC §3.9). Status changes that several workers could
// race for are single conditional statements, so a job is claimed by exactly
// one worker (SPEC §11, §14.3).
type JobRepo struct{ base }

// insertBatch is how many jobs one INSERT carries.
const insertBatch = 25

// CreateMany stores new jobs in the order given, which is job order, and sets
// their IDs and timestamps. All of them or none are stored.
func (r *JobRepo) CreateMany(ctx context.Context, jobs []*domain.Job) error {
	if len(jobs) == 0 {
		return nil
	}
	now := r.now()
	return r.atomically(ctx, func(tx *Repos) error {
		recs := make([]jobRecord, len(jobs))
		for i, j := range jobs {
			recs[i] = jobToRecord(j)
			recs[i].CreatedAt, recs[i].UpdatedAt = now, now
		}
		// gorm fills in the ids of the rows it inserts, in the order given.
		if err := tx.conn(ctx).CreateInBatches(&recs, insertBatch).Error; err != nil {
			return storeFailure(err, "write jobs")
		}
		for i, j := range jobs {
			j.ID, j.CreatedAt, j.UpdatedAt = recs[i].ID, now, now
		}
		return nil
	})
}

// Update writes every field of a job. A worker that owns a job (it claimed it)
// uses it to record progress.
func (r *JobRepo) Update(ctx context.Context, j *domain.Job) error {
	now := r.now()
	rec := jobToRecord(j)
	rec.UpdatedAt = now
	if err := affected(updateRow(r.conn(ctx), &rec, j.ID), "write job", "JobNotFound", "job", j.ID); err != nil {
		return err
	}
	j.UpdatedAt = now
	return nil
}

// Get returns one job.
func (r *JobRepo) Get(ctx context.Context, id domain.ID) (*domain.Job, error) {
	rec, err := find[jobRecord](r.conn(ctx).Where("id = ?", id), "JobNotFound", "job", id, "read job")
	if err != nil {
		return nil, err
	}
	v := rec.domain()
	return &v, nil
}

// ListByCommand returns one page of a command's jobs in job order. A non-empty
// status keeps only the jobs in that status.
func (r *JobRepo) ListByCommand(ctx context.Context, command domain.ID, status domain.Status, p domain.Page) (domain.Paged[domain.Job], error) {
	q := r.conn(ctx).Where("command_id = ?", command)
	if status != "" {
		q = q.Where("status = ?", string(status))
	}
	return pageOf(q, "id", p, jobRecord.domain, "list jobs")
}

// AllByCommand returns every job of a command in job order.
func (r *JobRepo) AllByCommand(ctx context.Context, command domain.ID) ([]domain.Job, error) {
	recs, err := all[jobRecord](r.conn(ctx).Where("command_id = ?", command).Order("id"), "list jobs")
	if err != nil {
		return nil, err
	}
	return mapAll(recs, jobRecord.domain), nil
}

// ListByStatus returns the jobs in a status in queue order. Recovery after a
// restart reads the processing jobs with it.
func (r *JobRepo) ListByStatus(ctx context.Context, status domain.Status) ([]domain.Job, error) {
	recs, err := all[jobRecord](r.conn(ctx).Where("status = ?", string(status)).Order("queue_seq, id"), "list jobs")
	if err != nil {
		return nil, err
	}
	return mapAll(recs, jobRecord.domain), nil
}

// Counts returns the number of jobs of a command and how many are finished. A
// command is finished when all of its jobs are (SPEC §7.1).
func (r *JobRepo) Counts(ctx context.Context, command domain.ID) (total, finished int, err error) {
	byStatus, err := r.StatusCounts(ctx, command)
	if err != nil {
		return 0, 0, err
	}
	for status, n := range byStatus {
		total += n
		if status == domain.StatusFinished {
			finished = n
		}
	}
	return total, finished, nil
}

// StatusCounts returns how many of a command's jobs are in each status.
func (r *JobRepo) StatusCounts(ctx context.Context, command domain.ID) (map[domain.Status]int, error) {
	var rows []struct {
		Status string
		N      int
	}
	err := r.conn(ctx).Model(&jobRecord{}).Select("status, COUNT(*) AS n").Where("command_id = ?", command).
		Group("status").Scan(&rows).Error
	if err != nil {
		return nil, storeFailure(err, "count jobs")
	}
	out := map[domain.Status]int{}
	for _, row := range rows {
		out[domain.Status(row.Status)] = row.N
	}
	return out, nil
}

// nextSeq returns the next queue sequence number. The caller is in a
// transaction, which holds the write lock.
func nextSeq(db *gorm.DB) (int64, error) {
	var highest int64
	if err := db.Model(&jobRecord{}).Select("COALESCE(MAX(queue_seq), 0)").Scan(&highest).Error; err != nil {
		return 0, storeFailure(err, "queue sequence")
	}
	return highest + 1, nil
}

// QueueCommand moves the idle jobs of a command to queued under one new queue
// sequence number, so the command run first is served first. It returns how
// many jobs it queued.
func (r *JobRepo) QueueCommand(ctx context.Context, command domain.ID) (int, error) {
	var n int64
	err := r.atomically(ctx, func(tx *Repos) error {
		db := tx.conn(ctx)
		seq, err := nextSeq(db)
		if err != nil {
			return err
		}
		res := db.Model(&jobRecord{}).Where("command_id = ? AND status = ?", command, string(domain.StatusIdle)).
			Updates(map[string]any{"status": string(domain.StatusQueued), "queue_seq": seq, "updated_at": r.now()})
		if res.Error != nil {
			return storeFailure(res.Error, "queue jobs")
		}
		n = res.RowsAffected
		return nil
	})
	return int(n), err
}

// QueueJob moves one idle job to queued under a new queue sequence number. It
// reports whether the job was idle.
func (r *JobRepo) QueueJob(ctx context.Context, id domain.ID) (bool, error) {
	var changed bool
	err := r.atomically(ctx, func(tx *Repos) error {
		db := tx.conn(ctx)
		seq, err := nextSeq(db)
		if err != nil {
			return err
		}
		res := db.Model(&jobRecord{}).Where("id = ? AND status = ?", id, string(domain.StatusIdle)).
			Updates(map[string]any{"status": string(domain.StatusQueued), "queue_seq": seq, "updated_at": r.now()})
		if res.Error != nil {
			return storeFailure(res.Error, "queue job")
		}
		changed = res.RowsAffected > 0
		return nil
	})
	return changed, err
}

// stopQueued returns queued jobs to idle and resets their progress (SPEC §8.4).
func (r *JobRepo) stopQueued(db *gorm.DB) *gorm.DB {
	return db.Model(&jobRecord{}).Where("status = ?", string(domain.StatusQueued)).Updates(map[string]any{
		"status": string(domain.StatusIdle), "is_generated": false, "is_fixed": false, "finished_fixers": 0,
		"prompt_id": "", "updated_at": r.now(),
	})
}

// StopQueued returns the queued jobs of a command to idle, except the job
// exceptID (0 for none). Jobs that are processing are not touched. It returns
// how many jobs it stopped.
func (r *JobRepo) StopQueued(ctx context.Context, command, exceptID domain.ID) (int, error) {
	res := r.stopQueued(r.conn(ctx).Where("command_id = ? AND id <> ?", command, exceptID))
	if res.Error != nil {
		return 0, storeFailure(res.Error, "stop jobs")
	}
	return int(res.RowsAffected), nil
}

// StopJob returns one queued job to idle. It reports whether the job was queued.
func (r *JobRepo) StopJob(ctx context.Context, id domain.ID) (bool, error) {
	res := r.stopQueued(r.conn(ctx).Where("id = ?", id))
	if res.Error != nil {
		return false, storeFailure(res.Error, "stop job")
	}
	return res.RowsAffected > 0, nil
}

// QueueCursor is a position in the queue: jobs are served by queue sequence
// number, then by id.
type QueueCursor struct {
	Seq int64
	ID  int64
}

// ListQueued returns up to limit queued jobs after the cursor, in the order
// they are served. A worker looks for the first job it may run, asking for the
// next page when none of a page fits.
func (r *JobRepo) ListQueued(ctx context.Context, after QueueCursor, limit int) ([]domain.Job, error) {
	recs, err := all[jobRecord](r.conn(ctx).
		Where("status = ?", string(domain.StatusQueued)).
		Where("queue_seq > ? OR (queue_seq = ? AND id > ?)", after.Seq, after.Seq, after.ID).
		Order("queue_seq, id").Limit(limit), "list queued jobs")
	if err != nil {
		return nil, err
	}
	return mapAll(recs, jobRecord.domain), nil
}

// Claim takes a queued job: it becomes processing, and only one caller can win
// (SPEC §14.3). It reports whether this caller won.
func (r *JobRepo) Claim(ctx context.Context, id domain.ID) (bool, error) {
	return r.Transition(ctx, id, domain.StatusQueued, domain.StatusProcessing)
}

// Transition changes a job from one status to another in a single conditional
// statement and reports whether the job was in the first status.
func (r *JobRepo) Transition(ctx context.Context, id domain.ID, from, to domain.Status) (bool, error) {
	res := r.conn(ctx).Model(&jobRecord{}).Where("id = ? AND status = ?", id, string(from)).
		Updates(map[string]any{"status": string(to), "updated_at": r.now()})
	if res.Error != nil {
		return false, storeFailure(res.Error, "change job status")
	}
	return res.RowsAffected > 0, nil
}

// deleteWhere removes the jobs that match a condition and returns their result
// files.
func (r *JobRepo) deleteWhere(ctx context.Context, cond string, args ...any) ([]string, error) {
	var files []string
	db := r.conn(ctx)
	if err := db.Model(&jobRecord{}).Where(cond, args...).Pluck("result_path", &files).Error; err != nil {
		return nil, storeFailure(err, "delete jobs")
	}
	if err := db.Where(cond, args...).Delete(&jobRecord{}).Error; err != nil {
		return nil, storeFailure(err, "delete jobs")
	}
	return compact(files), nil
}

// DeleteByCommand removes all jobs of a command and returns their result files.
func (r *JobRepo) DeleteByCommand(ctx context.Context, command domain.ID) ([]string, error) {
	var files []string
	err := r.atomically(ctx, func(tx *Repos) error {
		var err error
		files, err = tx.Jobs.deleteWhere(ctx, "command_id = ?", command)
		return err
	})
	return files, err
}

// GetRepl returns the reserved REPL job (SPEC §8.6), or JobNotFound.
func (r *JobRepo) GetRepl(ctx context.Context) (*domain.Job, error) {
	var rec jobRecord
	err := r.conn(ctx).Where("project_id = ? AND command_id = ?", domain.ReplID, domain.ReplID).Order("id DESC").Take(&rec).Error
	if isMissing(err) {
		return nil, errors.New("JobNotFound", "there is no previous REPL job")
	}
	if err != nil {
		return nil, storeFailure(err, "read job")
	}
	v := rec.domain()
	return &v, nil
}

// ReplaceRepl deletes any previous REPL job and stores the new one in a single
// transaction, so there is never more than one. It returns the previous job's
// result file for the caller to remove.
func (r *JobRepo) ReplaceRepl(ctx context.Context, j *domain.Job) ([]string, error) {
	var files []string
	err := r.atomically(ctx, func(tx *Repos) error {
		var err error
		if files, err = tx.Jobs.deleteWhere(ctx, "project_id = ? AND command_id = ?", domain.ReplID, domain.ReplID); err != nil {
			return err
		}
		j.ProjectID, j.CommandID = domain.ReplID, domain.ReplID
		return tx.Jobs.CreateMany(ctx, []*domain.Job{j})
	})
	if err != nil {
		return nil, err
	}
	return files, nil
}
