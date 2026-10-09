package store

import (
	"context"
	"database/sql"
	"strings"

	"github.com/rm4n0s/bos/internal/domain"
	"github.com/rm4n0s/errors"
)

// CommandRepo stores commands and keeps their order contiguous within a
// project (SPEC §3.8, §7.2).
type CommandRepo struct {
	base
	jobs *JobRepo
}

const commandColumns = `id, project_id, order_no, status, name, description, command_code, command_json,
	download_file, created_at, updated_at`

func scanCommand(s scanner) (*domain.Command, error) {
	var v domain.Command
	var status, cjson, created, updated string
	if err := s.Scan(&v.ID, &v.ProjectID, &v.Order, &status, &v.Name, &v.Description, &v.CommandCode, &cjson,
		&v.DownloadFile, &created, &updated); err != nil {
		return nil, err
	}
	v.Status = domain.Status(status)
	v.CommandJSON = []byte(cjson)
	var err error
	if v.CreatedAt, err = parseTime(created); err != nil {
		return nil, err
	}
	if v.UpdatedAt, err = parseTime(updated); err != nil {
		return nil, err
	}
	return &v, nil
}

// summaryColumns selects a command with the progress of its jobs.
func summaryColumns() string {
	cols := strings.Split(commandColumns, ",")
	for i, c := range cols {
		cols[i] = "c." + strings.TrimSpace(c)
	}
	return strings.Join(cols, ", ") + `,
	(SELECT COUNT(*) FROM jobs j WHERE j.command_id = c.id AND j.status = 'finished'),
	(SELECT COUNT(*) FROM jobs j WHERE j.command_id = c.id)`
}

func scanSummary(s scanner) (*domain.CommandSummary, error) {
	var v domain.CommandSummary
	var status, cjson, created, updated string
	if err := s.Scan(&v.ID, &v.ProjectID, &v.Order, &status, &v.Name, &v.Description, &v.CommandCode, &cjson,
		&v.DownloadFile, &created, &updated, &v.FinishedJobs, &v.TotalJobs); err != nil {
		return nil, err
	}
	v.Status = domain.Status(status)
	v.CommandJSON = []byte(cjson)
	var err error
	if v.CreatedAt, err = parseTime(created); err != nil {
		return nil, err
	}
	if v.UpdatedAt, err = parseTime(updated); err != nil {
		return nil, err
	}
	return &v, nil
}

// Create stores a new command in its project. insertAt is the 1-based position;
// a value below 1 or beyond the end appends. Later commands move up by one.
func (r *CommandRepo) Create(ctx context.Context, v *domain.Command, insertAt int) error {
	now := r.now()
	return r.atomically(ctx, func(q dbtx) error {
		var last int
		if err := q.QueryRowContext(ctx, `SELECT COALESCE(MAX(order_no), 0) FROM commands WHERE project_id=?`, v.ProjectID).Scan(&last); err != nil {
			return storeFailure(err, "write command")
		}
		order := last + 1
		if insertAt >= 1 && insertAt <= last {
			order = insertAt
			if _, err := q.ExecContext(ctx, `UPDATE commands SET order_no = order_no + 1 WHERE project_id=? AND order_no >= ?`, v.ProjectID, order); err != nil {
				return storeFailure(err, "write command")
			}
		}
		status := v.Status
		if status == "" {
			status = domain.StatusIdle
		}
		cjson := string(v.CommandJSON)
		if cjson == "" {
			cjson = "{}"
		}
		res, err := q.ExecContext(ctx,
			`INSERT INTO commands (project_id, order_no, status, name, description, command_code, command_json,
			   download_file, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			v.ProjectID, order, string(status), v.Name, v.Description, v.CommandCode, cjson, v.DownloadFile,
			formatTime(now), formatTime(now))
		if err != nil {
			if isForeignKey(err) {
				return notFound("ProjectNotFound", "project", v.ProjectID)
			}
			return storeFailure(err, "write command")
		}
		id, err := res.LastInsertId()
		if err != nil {
			return storeFailure(err, "command id")
		}
		v.ID, v.Order, v.Status, v.CreatedAt, v.UpdatedAt = id, order, status, now, now
		if v.CommandJSON == nil {
			v.CommandJSON = []byte(cjson)
		}
		return nil
	})
}

// Update replaces the editable fields of a command: name, description, text,
// parsed form, status and export path. Its position changes only through Move.
func (r *CommandRepo) Update(ctx context.Context, v *domain.Command) error {
	now := r.now()
	res, err := r.q.ExecContext(ctx,
		`UPDATE commands SET status=?, name=?, description=?, command_code=?, command_json=?, download_file=?, updated_at=?
		 WHERE id=?`,
		string(v.Status), v.Name, v.Description, v.CommandCode, string(v.CommandJSON), v.DownloadFile, formatTime(now), v.ID)
	if err != nil {
		return storeFailure(err, "write command")
	}
	if n, err := res.RowsAffected(); err != nil {
		return storeFailure(err, "command rows")
	} else if n == 0 {
		return notFound("CommandNotFound", "command", v.ID)
	}
	v.UpdatedAt = now
	return nil
}

// SetStatus changes the status of a command.
func (r *CommandRepo) SetStatus(ctx context.Context, id domain.ID, status domain.Status) error {
	res, err := r.q.ExecContext(ctx, `UPDATE commands SET status=?, updated_at=? WHERE id=?`, string(status), formatTime(r.now()), id)
	if err != nil {
		return storeFailure(err, "write command")
	}
	if n, err := res.RowsAffected(); err != nil {
		return storeFailure(err, "command rows")
	} else if n == 0 {
		return notFound("CommandNotFound", "command", id)
	}
	return nil
}

// Get returns one command.
func (r *CommandRepo) Get(ctx context.Context, id domain.ID) (*domain.Command, error) {
	v, err := scanCommand(r.q.QueryRowContext(ctx, `SELECT `+commandColumns+` FROM commands WHERE id=?`, id))
	return one(v, err, "CommandNotFound", "command", id, "read command")
}

// ListByProject returns one page of a project's commands in order, with the
// progress of their jobs.
func (r *CommandRepo) ListByProject(ctx context.Context, project domain.ID, p domain.Page) (domain.Paged[domain.CommandSummary], error) {
	var exists int
	if err := r.q.QueryRowContext(ctx, `SELECT 1 FROM projects WHERE id=?`, project).Scan(&exists); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.Paged[domain.CommandSummary]{}, notFound("ProjectNotFound", "project", project)
		}
		return domain.Paged[domain.CommandSummary]{}, storeFailure(err, "list commands")
	}
	return queryPage(ctx, r.q, `FROM commands c WHERE c.project_id=?`, []any{project}, `ORDER BY c.order_no, c.id`,
		summaryColumns(), scanSummary, p, "list commands")
}

// AllByProject returns every command of a project in order.
func (r *CommandRepo) AllByProject(ctx context.Context, project domain.ID) ([]domain.Command, error) {
	return queryAll(ctx, r.q, `SELECT `+commandColumns+` FROM commands WHERE project_id=? ORDER BY order_no, id`,
		scanCommand, "list commands", project)
}

// FindByText returns the commands whose text contains needle, ignoring case.
// A tag rename uses it to report the commands that mention the old tag.
func (r *CommandRepo) FindByText(ctx context.Context, needle string) ([]domain.Command, error) {
	return queryAll(ctx, r.q, `SELECT `+commandColumns+` FROM commands WHERE instr(lower(command_code), lower(?)) > 0 ORDER BY id`,
		scanCommand, "find commands", needle)
}

// Delete removes a command with its jobs, closes the gap in the order, and
// returns the result files and export archive to remove afterwards.
func (r *CommandRepo) Delete(ctx context.Context, id domain.ID) ([]string, error) {
	var files []string
	err := r.atomically(ctx, func(q dbtx) error {
		inTx := &CommandRepo{base: base{q: q, clock: r.clock}}
		inTx.jobs = &JobRepo{base: inTx.base}
		cmd, err := inTx.Get(ctx, id)
		if err != nil {
			return err
		}
		jobFiles, err := inTx.jobs.deleteWhere(ctx, `command_id=?`, id)
		if err != nil {
			return err
		}
		if _, err := q.ExecContext(ctx, `DELETE FROM commands WHERE id=?`, id); err != nil {
			return storeFailure(err, "delete command")
		}
		if _, err := q.ExecContext(ctx, `UPDATE commands SET order_no = order_no - 1 WHERE project_id=? AND order_no > ?`,
			cmd.ProjectID, cmd.Order); err != nil {
			return storeFailure(err, "delete command")
		}
		files = compact(append(jobFiles, cmd.DownloadFile))
		return nil
	})
	if err != nil {
		return nil, err
	}
	return files, nil
}

// deleteByProject removes the commands of a project, whose jobs are already
// gone, and returns their export archives.
func (r *CommandRepo) deleteByProject(ctx context.Context, project domain.ID) ([]string, error) {
	cmds, err := r.AllByProject(ctx, project)
	if err != nil {
		return nil, err
	}
	if _, err := r.q.ExecContext(ctx, `DELETE FROM commands WHERE project_id=?`, project); err != nil {
		return nil, storeFailure(err, "delete commands")
	}
	var files []string
	for _, c := range cmds {
		files = append(files, c.DownloadFile)
	}
	return compact(files), nil
}

// Move puts a command at a new position and shifts the commands between the old
// and the new place. A position beyond either end is clamped; the same position
// changes nothing. It returns the position the command ended up at.
func (r *CommandRepo) Move(ctx context.Context, id domain.ID, order int) (int, error) {
	var final int
	err := r.atomically(ctx, func(q dbtx) error {
		inTx := &CommandRepo{base: base{q: q, clock: r.clock}}
		cmd, err := inTx.Get(ctx, id)
		if err != nil {
			return err
		}
		var last int
		if err := q.QueryRowContext(ctx, `SELECT MAX(order_no) FROM commands WHERE project_id=?`, cmd.ProjectID).Scan(&last); err != nil {
			return storeFailure(err, "move command")
		}
		final = min(max(order, 1), last)
		if final == cmd.Order {
			return nil
		}
		shift := `UPDATE commands SET order_no = order_no + 1 WHERE project_id=? AND order_no >= ? AND order_no < ?`
		lo, hi := final, cmd.Order
		if final > cmd.Order {
			shift = `UPDATE commands SET order_no = order_no - 1 WHERE project_id=? AND order_no > ? AND order_no <= ?`
			lo, hi = cmd.Order, final
		}
		if _, err := q.ExecContext(ctx, shift, cmd.ProjectID, lo, hi); err != nil {
			return storeFailure(err, "move command")
		}
		if _, err := q.ExecContext(ctx, `UPDATE commands SET order_no=?, updated_at=? WHERE id=?`, final, formatTime(r.now()), id); err != nil {
			return storeFailure(err, "move command")
		}
		return nil
	})
	return final, err
}

// Renumber reassigns the orders of a project's commands to 1…N, keeping their
// current sequence.
func (r *CommandRepo) Renumber(ctx context.Context, project domain.ID) error {
	return r.atomically(ctx, func(q dbtx) error {
		cmds, err := (&CommandRepo{base: base{q: q, clock: r.clock}}).AllByProject(ctx, project)
		if err != nil {
			return err
		}
		for i, c := range cmds {
			if c.Order == i+1 {
				continue
			}
			if _, err := q.ExecContext(ctx, `UPDATE commands SET order_no=? WHERE id=?`, i+1, c.ID); err != nil {
				return storeFailure(err, "renumber commands")
			}
		}
		return nil
	})
}
