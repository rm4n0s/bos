package store

import (
	"context"
	"database/sql"
	"strconv"
	"strings"

	"github.com/rm4n0s/bos/internal/domain"
	"github.com/rm4n0s/errors"
)

// JobRepo stores jobs (SPEC §3.9). Status changes that several workers could
// race for are single conditional statements, so a job is claimed by exactly
// one worker (SPEC §11, §14.3).
type JobRepo struct{ base }

// jobColumns are the writable columns, in the order jobArgs returns them.
var jobColumns = []string{
	"project_id", "command_id", "items", "command_text", "status", "queue_seq", "server_id", "server_host",
	"preferred_server_id", "pinned", "fixers", "finished_fixers", "repeat_index", "region_index", "seed", "attempts",
	"generator_code_name", "output_type", "output_attributes", "prompt_id", "positive_prompt", "negative_prompt",
	"controlnets", "region_prompts", "ip_adapters", "loras", "result_path", "error", "error_trace", "warnings",
	"is_generated", "is_fixed",
}

var (
	jobSelect = `id, ` + strings.Join(jobColumns, ", ") + `, created_at, updated_at`
	jobUpdate = `UPDATE jobs SET ` + strings.Join(jobColumns, "=?, ") + `=?, updated_at=? WHERE id=?`
	// insertRowWidth counts the columns of one inserted row.
	insertRowWidth = len(jobColumns) + 2
)

func jobArgs(j *domain.Job) ([]any, error) {
	var texts [8]string
	var err error
	for i, col := range []struct {
		v        any
		fallback string
	}{
		{j.Items, "[]"}, {j.Fixers, "[]"}, {j.OutputAttributes, "{}"}, {j.ControlNets, "[]"}, {j.RegionPrompts, "[]"},
		{j.IPAdapters, "[]"}, {j.LoRAs, "[]"}, {j.Warnings, "[]"},
	} {
		if texts[i], err = encode(col.v, col.fallback); err != nil {
			return nil, err
		}
	}
	var seed any
	if j.Seed != nil {
		seed = strconv.FormatUint(*j.Seed, 10)
	}
	return []any{
		j.ProjectID, j.CommandID, texts[0], j.CommandText, string(j.Status), j.QueueSeq, nullInt(j.ServerID), j.ServerHost,
		nullInt(j.PreferredServerID), boolInt(j.Pinned), texts[1], j.FinishedFixers, j.RepeatIndex, j.RegionIndex, seed,
		j.Attempts, j.GeneratorCodeName, string(j.OutputType), texts[2], j.PromptID, j.PositivePrompt, j.NegativePrompt,
		texts[3], texts[4], texts[5], texts[6], j.ResultPath, j.Error, j.ErrorTrace, texts[7],
		boolInt(j.IsGenerated), boolInt(j.IsFixed),
	}, nil
}

func scanJob(s scanner) (*domain.Job, error) {
	var j domain.Job
	var status, outputType, created, updated string
	var items, fixers, attrs, cns, regions, ips, loras, warnings string
	var server, preferred sql.NullInt64
	var seed sql.NullString
	var pinned, generated, fixed int
	if err := s.Scan(&j.ID, &j.ProjectID, &j.CommandID, &items, &j.CommandText, &status, &j.QueueSeq, &server,
		&j.ServerHost, &preferred, &pinned, &fixers, &j.FinishedFixers, &j.RepeatIndex, &j.RegionIndex, &seed,
		&j.Attempts, &j.GeneratorCodeName, &outputType, &attrs, &j.PromptID, &j.PositivePrompt, &j.NegativePrompt,
		&cns, &regions, &ips, &loras, &j.ResultPath, &j.Error, &j.ErrorTrace, &warnings, &generated, &fixed,
		&created, &updated); err != nil {
		return nil, err
	}
	j.Status, j.OutputType = domain.Status(status), domain.OutputType(outputType)
	j.Pinned, j.IsGenerated, j.IsFixed = pinned != 0, generated != 0, fixed != 0
	if server.Valid {
		j.ServerID = &server.Int64
	}
	if preferred.Valid {
		j.PreferredServerID = &preferred.Int64
	}
	if seed.Valid {
		n, err := strconv.ParseUint(seed.String, 10, 64)
		if err != nil {
			return nil, storeFailure(err, "decode seed")
		}
		j.Seed = &n
	}
	for _, col := range []struct {
		text string
		dst  any
	}{
		{items, &j.Items}, {fixers, &j.Fixers}, {attrs, &j.OutputAttributes}, {cns, &j.ControlNets},
		{regions, &j.RegionPrompts}, {ips, &j.IPAdapters}, {loras, &j.LoRAs}, {warnings, &j.Warnings},
	} {
		if err := decode(col.text, col.dst); err != nil {
			return nil, err
		}
	}
	var err error
	if j.CreatedAt, err = parseTime(created); err != nil {
		return nil, err
	}
	if j.UpdatedAt, err = parseTime(updated); err != nil {
		return nil, err
	}
	return &j, nil
}

// insertBatch is how many jobs one INSERT carries.
const insertBatch = 25

// CreateMany stores new jobs in the order given, which is job order, and sets
// their IDs and timestamps. All of them or none are stored.
func (r *JobRepo) CreateMany(ctx context.Context, jobs []*domain.Job) error {
	if len(jobs) == 0 {
		return nil
	}
	now := r.now()
	return r.atomically(ctx, func(q dbtx) error {
		for start := 0; start < len(jobs); start += insertBatch {
			batch := jobs[start:min(start+insertBatch, len(jobs))]
			var args []any
			for _, j := range batch {
				a, err := jobArgs(j)
				if err != nil {
					return err
				}
				args = append(args, a...)
				args = append(args, formatTime(now), formatTime(now))
			}
			row := "(" + placeholders(insertRowWidth) + ")"
			rows := strings.TrimSuffix(strings.Repeat(row+", ", len(batch)), ", ")
			res, err := q.ExecContext(ctx, `INSERT INTO jobs (`+strings.Join(jobColumns, ", ")+`, created_at, updated_at) VALUES `+rows, args...)
			if err != nil {
				return storeFailure(err, "write jobs")
			}
			// The transaction holds the write lock, so the rows of one INSERT get
			// consecutive ids and the last one is what the driver reports.
			last, err := res.LastInsertId()
			if err != nil {
				return storeFailure(err, "job ids")
			}
			for i, j := range batch {
				j.ID = last - int64(len(batch)-1-i)
				j.CreatedAt, j.UpdatedAt = now, now
			}
		}
		return nil
	})
}

// Update writes every field of a job. A worker that owns a job (it claimed it)
// uses it to record progress.
func (r *JobRepo) Update(ctx context.Context, j *domain.Job) error {
	args, err := jobArgs(j)
	if err != nil {
		return err
	}
	now := r.now()
	res, err := r.q.ExecContext(ctx, jobUpdate, append(args, formatTime(now), j.ID)...)
	if err != nil {
		return storeFailure(err, "write job")
	}
	if n, err := res.RowsAffected(); err != nil {
		return storeFailure(err, "job rows")
	} else if n == 0 {
		return notFound("JobNotFound", "job", j.ID)
	}
	j.UpdatedAt = now
	return nil
}

// Get returns one job.
func (r *JobRepo) Get(ctx context.Context, id domain.ID) (*domain.Job, error) {
	v, err := scanJob(r.q.QueryRowContext(ctx, `SELECT `+jobSelect+` FROM jobs WHERE id=?`, id))
	return one(v, err, "JobNotFound", "job", id, "read job")
}

// ListByCommand returns one page of a command's jobs in job order. A non-empty
// status keeps only the jobs in that status.
func (r *JobRepo) ListByCommand(ctx context.Context, command domain.ID, status domain.Status, p domain.Page) (domain.Paged[domain.Job], error) {
	from, args := `FROM jobs WHERE command_id=?`, []any{command}
	if status != "" {
		from += ` AND status=?`
		args = append(args, string(status))
	}
	return queryPage(ctx, r.q, from, args, `ORDER BY id`, jobSelect, scanJob, p, "list jobs")
}

// AllByCommand returns every job of a command in job order.
func (r *JobRepo) AllByCommand(ctx context.Context, command domain.ID) ([]domain.Job, error) {
	return queryAll(ctx, r.q, `SELECT `+jobSelect+` FROM jobs WHERE command_id=? ORDER BY id`, scanJob, "list jobs", command)
}

// ListByStatus returns the jobs in a status in queue order. Recovery after a
// restart reads the processing jobs with it.
func (r *JobRepo) ListByStatus(ctx context.Context, status domain.Status) ([]domain.Job, error) {
	return queryAll(ctx, r.q, `SELECT `+jobSelect+` FROM jobs WHERE status=? ORDER BY queue_seq, id`, scanJob, "list jobs", string(status))
}

// Counts returns the number of jobs of a command and how many are finished. A
// command is finished when all of its jobs are (SPEC §7.1).
func (r *JobRepo) Counts(ctx context.Context, command domain.ID) (total, finished int, err error) {
	err = r.q.QueryRowContext(ctx,
		`SELECT COUNT(*), COALESCE(SUM(status = 'finished'), 0) FROM jobs WHERE command_id=?`, command).Scan(&total, &finished)
	if err != nil {
		return 0, 0, storeFailure(err, "count jobs")
	}
	return total, finished, nil
}

// StatusCounts returns how many of a command's jobs are in each status.
func (r *JobRepo) StatusCounts(ctx context.Context, command domain.ID) (map[domain.Status]int, error) {
	rows, err := r.q.QueryContext(ctx, `SELECT status, COUNT(*) FROM jobs WHERE command_id=? GROUP BY status`, command)
	if err != nil {
		return nil, storeFailure(err, "count jobs")
	}
	out := map[domain.Status]int{}
	for rows.Next() {
		var st string
		var n int
		if err := rows.Scan(&st, &n); err != nil {
			return nil, closeRows(rows, storeFailure(err, "count jobs"))
		}
		out[domain.Status(st)] = n
	}
	if err := rows.Err(); err != nil {
		return nil, closeRows(rows, storeFailure(err, "count jobs"))
	}
	if err := rows.Close(); err != nil {
		return nil, storeFailure(err, "count jobs")
	}
	return out, nil
}

// nextSeq returns the next queue sequence number. The caller is in a
// transaction, which holds the write lock.
func nextSeq(ctx context.Context, q dbtx) (int64, error) {
	var seq int64
	if err := q.QueryRowContext(ctx, `SELECT COALESCE(MAX(queue_seq), 0) + 1 FROM jobs`).Scan(&seq); err != nil {
		return 0, storeFailure(err, "queue sequence")
	}
	return seq, nil
}

// QueueCommand moves the idle jobs of a command to queued under one new queue
// sequence number, so the command run first is served first. It returns how
// many jobs it queued.
func (r *JobRepo) QueueCommand(ctx context.Context, command domain.ID) (int, error) {
	var n int64
	err := r.atomically(ctx, func(q dbtx) error {
		seq, err := nextSeq(ctx, q)
		if err != nil {
			return err
		}
		res, err := q.ExecContext(ctx, `UPDATE jobs SET status='queued', queue_seq=?, updated_at=? WHERE command_id=? AND status='idle'`,
			seq, formatTime(r.now()), command)
		if err != nil {
			return storeFailure(err, "queue jobs")
		}
		if n, err = res.RowsAffected(); err != nil {
			return storeFailure(err, "queue jobs")
		}
		return nil
	})
	return int(n), err
}

// QueueJob moves one idle job to queued under a new queue sequence number. It
// reports whether the job was idle.
func (r *JobRepo) QueueJob(ctx context.Context, id domain.ID) (bool, error) {
	var changed bool
	err := r.atomically(ctx, func(q dbtx) error {
		seq, err := nextSeq(ctx, q)
		if err != nil {
			return err
		}
		res, err := q.ExecContext(ctx, `UPDATE jobs SET status='queued', queue_seq=?, updated_at=? WHERE id=? AND status='idle'`,
			seq, formatTime(r.now()), id)
		if err != nil {
			return storeFailure(err, "queue job")
		}
		n, err := res.RowsAffected()
		if err != nil {
			return storeFailure(err, "queue job")
		}
		changed = n > 0
		return nil
	})
	return changed, err
}

// stopSQL returns queued jobs to idle and resets their progress (SPEC §8.4).
const stopSQL = `UPDATE jobs SET status='idle', is_generated=0, is_fixed=0, finished_fixers=0, prompt_id='', updated_at=?
	WHERE status='queued'`

// StopQueued returns the queued jobs of a command to idle, except the job
// exceptID (0 for none). Jobs that are processing are not touched. It returns
// how many jobs it stopped.
func (r *JobRepo) StopQueued(ctx context.Context, command, exceptID domain.ID) (int, error) {
	res, err := r.q.ExecContext(ctx, stopSQL+` AND command_id=? AND id<>?`, formatTime(r.now()), command, exceptID)
	if err != nil {
		return 0, storeFailure(err, "stop jobs")
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, storeFailure(err, "stop jobs")
	}
	return int(n), nil
}

// StopJob returns one queued job to idle. It reports whether the job was queued.
func (r *JobRepo) StopJob(ctx context.Context, id domain.ID) (bool, error) {
	res, err := r.q.ExecContext(ctx, stopSQL+` AND id=?`, formatTime(r.now()), id)
	if err != nil {
		return false, storeFailure(err, "stop job")
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, storeFailure(err, "stop job")
	}
	return n > 0, nil
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
	return queryAll(ctx, r.q,
		`SELECT `+jobSelect+` FROM jobs WHERE status='queued' AND (queue_seq > ? OR (queue_seq = ? AND id > ?))
		 ORDER BY queue_seq, id LIMIT ?`, scanJob, "list queued jobs", after.Seq, after.Seq, after.ID, limit)
}

// Claim takes a queued job: it becomes processing, and only one caller can win
// (SPEC §14.3). It reports whether this caller won.
func (r *JobRepo) Claim(ctx context.Context, id domain.ID) (bool, error) {
	return r.Transition(ctx, id, domain.StatusQueued, domain.StatusProcessing)
}

// Transition changes a job from one status to another in a single conditional
// statement and reports whether the job was in the first status.
func (r *JobRepo) Transition(ctx context.Context, id domain.ID, from, to domain.Status) (bool, error) {
	res, err := r.q.ExecContext(ctx, `UPDATE jobs SET status=?, updated_at=? WHERE id=? AND status=?`,
		string(to), formatTime(r.now()), id, string(from))
	if err != nil {
		return false, storeFailure(err, "change job status")
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, storeFailure(err, "change job status")
	}
	return n > 0, nil
}

// deleteWhere removes the jobs that match a condition and returns their result
// files.
func (r *JobRepo) deleteWhere(ctx context.Context, cond string, args ...any) ([]string, error) {
	rows, err := r.q.QueryContext(ctx, `SELECT result_path FROM jobs WHERE `+cond, args...)
	if err != nil {
		return nil, storeFailure(err, "delete jobs")
	}
	var files []string
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			return nil, closeRows(rows, storeFailure(err, "delete jobs"))
		}
		files = append(files, p)
	}
	if err := rows.Err(); err != nil {
		return nil, closeRows(rows, storeFailure(err, "delete jobs"))
	}
	if err := rows.Close(); err != nil {
		return nil, storeFailure(err, "delete jobs")
	}
	if _, err := r.q.ExecContext(ctx, `DELETE FROM jobs WHERE `+cond, args...); err != nil {
		return nil, storeFailure(err, "delete jobs")
	}
	return compact(files), nil
}

// DeleteByCommand removes all jobs of a command and returns their result files.
func (r *JobRepo) DeleteByCommand(ctx context.Context, command domain.ID) ([]string, error) {
	var files []string
	err := r.atomically(ctx, func(q dbtx) error {
		var err error
		files, err = (&JobRepo{base: base{q: q, clock: r.clock}}).deleteWhere(ctx, `command_id=?`, command)
		return err
	})
	return files, err
}

// GetRepl returns the reserved REPL job (SPEC §8.6), or JobNotFound.
func (r *JobRepo) GetRepl(ctx context.Context) (*domain.Job, error) {
	v, err := scanJob(r.q.QueryRowContext(ctx,
		`SELECT `+jobSelect+` FROM jobs WHERE project_id=? AND command_id=? ORDER BY id DESC LIMIT 1`, domain.ReplID, domain.ReplID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errors.New("JobNotFound", "there is no previous REPL job")
	}
	if err != nil {
		return nil, asStoreError(err, "read job")
	}
	return v, nil
}

// ReplaceRepl deletes any previous REPL job and stores the new one in a single
// transaction, so there is never more than one. It returns the previous job's
// result file for the caller to remove.
func (r *JobRepo) ReplaceRepl(ctx context.Context, j *domain.Job) ([]string, error) {
	var files []string
	err := r.atomically(ctx, func(q dbtx) error {
		inTx := &JobRepo{base: base{q: q, clock: r.clock}}
		var err error
		if files, err = inTx.deleteWhere(ctx, `project_id=? AND command_id=?`, domain.ReplID, domain.ReplID); err != nil {
			return err
		}
		j.ProjectID, j.CommandID = domain.ReplID, domain.ReplID
		return inTx.CreateMany(ctx, []*domain.Job{j})
	})
	if err != nil {
		return nil, err
	}
	return files, nil
}
