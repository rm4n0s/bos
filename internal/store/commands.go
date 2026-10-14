package store

import (
	"context"

	"github.com/rm4n0s/bos/internal/domain"
	"gorm.io/gorm"
)

// CommandRepo stores commands and keeps their order contiguous within a
// project (SPEC §3.8, §7.2).
type CommandRepo struct{ base }

// lastOrder returns the highest order in a project, or 0 for an empty one.
func lastOrder(db *gorm.DB, project domain.ID) (int, error) {
	var last int
	err := db.Model(&commandRecord{}).Where("project_id = ?", project).Select("COALESCE(MAX(order_no), 0)").Scan(&last).Error
	if err != nil {
		return 0, err
	}
	return last, nil
}

// shiftOrders moves the commands of a project whose order matches cond by
// delta.
func shiftOrders(db *gorm.DB, project domain.ID, delta int, cond string, args ...any) error {
	return db.Model(&commandRecord{}).Where("project_id = ?", project).Where(cond, args...).
		UpdateColumn("order_no", gorm.Expr("order_no + ?", delta)).Error
}

// Create stores a new command in its project. insertAt is the 1-based position;
// a value below 1 or beyond the end appends. Later commands move up by one.
func (r *CommandRepo) Create(ctx context.Context, v *domain.Command, insertAt int) error {
	now := r.now()
	return r.atomically(ctx, func(tx *Repos) error {
		db := tx.conn(ctx)
		last, err := lastOrder(db, v.ProjectID)
		if err != nil {
			return storeFailure(err, "write command")
		}
		order := last + 1
		if insertAt >= 1 && insertAt <= last {
			order = insertAt
			if err := shiftOrders(db, v.ProjectID, 1, "order_no >= ?", order); err != nil {
				return storeFailure(err, "write command")
			}
		}
		rec := commandToRecord(v)
		rec.OrderNo = order
		if rec.Status == "" {
			rec.Status = string(domain.StatusIdle)
		}
		if rec.CommandJSON == "" {
			rec.CommandJSON = "{}"
		}
		rec.CreatedAt, rec.UpdatedAt = now, now
		if err := db.Create(&rec).Error; err != nil {
			if isForeignKey(err) {
				return notFound("ProjectNotFound", "project", v.ProjectID)
			}
			return storeFailure(err, "write command")
		}
		v.ID, v.Order, v.Status, v.CreatedAt, v.UpdatedAt = rec.ID, order, domain.Status(rec.Status), now, now
		if v.CommandJSON == nil {
			v.CommandJSON = []byte(rec.CommandJSON)
		}
		return nil
	})
}

// Update replaces the editable fields of a command: name, description, text,
// parsed form, status and export path. Its position changes only through Move.
func (r *CommandRepo) Update(ctx context.Context, v *domain.Command) error {
	now := r.now()
	rec := commandToRecord(v)
	rec.UpdatedAt = now
	res := r.conn(ctx).Model(&rec).Where("id = ?", v.ID).Select("*").
		Omit("id", "project_id", "order_no", "created_at").Updates(&rec)
	if err := affected(res, "write command", "CommandNotFound", "command", v.ID); err != nil {
		return err
	}
	v.UpdatedAt = now
	return nil
}

// SetStatus changes the status of a command.
func (r *CommandRepo) SetStatus(ctx context.Context, id domain.ID, status domain.Status) error {
	res := r.conn(ctx).Model(&commandRecord{}).Where("id = ?", id).
		Updates(map[string]any{"status": string(status), "updated_at": r.now()})
	return affected(res, "write command", "CommandNotFound", "command", id)
}

// SetDownloadFile remembers the path of a command's export archive. It writes
// that column only, so a command that is running keeps the status the scheduler
// gives it.
func (r *CommandRepo) SetDownloadFile(ctx context.Context, id domain.ID, path string) error {
	res := r.conn(ctx).Model(&commandRecord{}).Where("id = ?", id).
		Updates(map[string]any{"download_file": path, "updated_at": r.now()})
	return affected(res, "write command", "CommandNotFound", "command", id)
}

// Get returns one command.
func (r *CommandRepo) Get(ctx context.Context, id domain.ID) (*domain.Command, error) {
	rec, err := find[commandRecord](r.conn(ctx).Where("id = ?", id), "CommandNotFound", "command", id, "read command")
	if err != nil {
		return nil, err
	}
	v := rec.domain()
	return &v, nil
}

// jobCounts returns, per command, how many of its jobs match the filter.
func jobCounts(db *gorm.DB, commands []int64, status domain.Status) (map[int64]int, error) {
	q := db.Model(&jobRecord{}).Select("command_id, COUNT(*) AS n").Where("command_id IN ?", commands)
	if status != "" {
		q = q.Where("status = ?", string(status))
	}
	var rows []struct {
		CommandID int64
		N         int
	}
	if err := q.Group("command_id").Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make(map[int64]int, len(rows))
	for _, row := range rows {
		out[row.CommandID] = row.N
	}
	return out, nil
}

// ListByProject returns one page of a project's commands in order, with the
// progress of their jobs.
func (r *CommandRepo) ListByProject(ctx context.Context, project domain.ID, p domain.Page) (domain.Paged[domain.CommandSummary], error) {
	if _, err := find[projectRecord](r.conn(ctx).Where("id = ?", project), "ProjectNotFound", "project", project, "list commands"); err != nil {
		return domain.Paged[domain.CommandSummary]{}, err
	}
	page, err := pageOf(r.conn(ctx).Where("project_id = ?", project), "order_no, id", p,
		func(rec commandRecord) commandRecord { return rec }, "list commands")
	if err != nil {
		return domain.Paged[domain.CommandSummary]{}, err
	}
	ids := make([]int64, len(page.Items))
	for i, rec := range page.Items {
		ids[i] = rec.ID
	}
	var total, finished map[int64]int
	if len(ids) > 0 {
		db := r.conn(ctx)
		if total, err = jobCounts(db, ids, ""); err != nil {
			return domain.Paged[domain.CommandSummary]{}, storeFailure(err, "list commands")
		}
		if finished, err = jobCounts(db, ids, domain.StatusFinished); err != nil {
			return domain.Paged[domain.CommandSummary]{}, storeFailure(err, "list commands")
		}
	}
	out := make([]domain.CommandSummary, len(page.Items))
	for i, rec := range page.Items {
		out[i] = domain.CommandSummary{Command: rec.domain(), FinishedJobs: finished[rec.ID], TotalJobs: total[rec.ID]}
	}
	return domain.NewPaged(out, page.Total, p), nil
}

// ListByStatus returns the commands that are in one of the statuses, by id. The
// scheduler uses it to repair the status of commands after a restart.
func (r *CommandRepo) ListByStatus(ctx context.Context, statuses ...domain.Status) ([]domain.Command, error) {
	names := make([]string, len(statuses))
	for i, st := range statuses {
		names[i] = string(st)
	}
	recs, err := all[commandRecord](r.conn(ctx).Where("status IN ?", names).Order("id"), "list commands")
	if err != nil {
		return nil, err
	}
	return mapAll(recs, commandRecord.domain), nil
}

// AllByProject returns every command of a project in order.
func (r *CommandRepo) AllByProject(ctx context.Context, project domain.ID) ([]domain.Command, error) {
	recs, err := all[commandRecord](r.conn(ctx).Where("project_id = ?", project).Order("order_no, id"), "list commands")
	if err != nil {
		return nil, err
	}
	return mapAll(recs, commandRecord.domain), nil
}

// FindByText returns the commands whose text contains needle, ignoring case.
// A tag rename uses it to report the commands that mention the old tag.
func (r *CommandRepo) FindByText(ctx context.Context, needle string) ([]domain.Command, error) {
	recs, err := all[commandRecord](r.conn(ctx).Where("instr(lower(command_code), lower(?)) > 0", needle).Order("id"), "find commands")
	if err != nil {
		return nil, err
	}
	return mapAll(recs, commandRecord.domain), nil
}

// Delete removes a command with its jobs, closes the gap in the order, and
// returns the result files and export archive to remove afterwards.
func (r *CommandRepo) Delete(ctx context.Context, id domain.ID) ([]string, error) {
	var files []string
	err := r.atomically(ctx, func(tx *Repos) error {
		cmd, err := tx.Commands.Get(ctx, id)
		if err != nil {
			return err
		}
		jobFiles, err := tx.Jobs.deleteWhere(ctx, "command_id = ?", id)
		if err != nil {
			return err
		}
		db := tx.conn(ctx)
		if err := db.Delete(&commandRecord{}, "id = ?", id).Error; err != nil {
			return storeFailure(err, "delete command")
		}
		if err := shiftOrders(db, cmd.ProjectID, -1, "order_no > ?", cmd.Order); err != nil {
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
	if err := r.conn(ctx).Delete(&commandRecord{}, "project_id = ?", project).Error; err != nil {
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
	err := r.atomically(ctx, func(tx *Repos) error {
		cmd, err := tx.Commands.Get(ctx, id)
		if err != nil {
			return err
		}
		db := tx.conn(ctx)
		last, err := lastOrder(db, cmd.ProjectID)
		if err != nil {
			return storeFailure(err, "move command")
		}
		final = min(max(order, 1), last)
		if final == cmd.Order {
			return nil
		}
		if final < cmd.Order {
			err = shiftOrders(db, cmd.ProjectID, 1, "order_no >= ? AND order_no < ?", final, cmd.Order)
		} else {
			err = shiftOrders(db, cmd.ProjectID, -1, "order_no > ? AND order_no <= ?", cmd.Order, final)
		}
		if err != nil {
			return storeFailure(err, "move command")
		}
		moved := db.Model(&commandRecord{}).Where("id = ?", id).Updates(map[string]any{"order_no": final, "updated_at": r.now()})
		if moved.Error != nil {
			return storeFailure(moved.Error, "move command")
		}
		return nil
	})
	return final, err
}

// Renumber reassigns the orders of a project's commands to 1…N, keeping their
// current sequence.
func (r *CommandRepo) Renumber(ctx context.Context, project domain.ID) error {
	return r.atomically(ctx, func(tx *Repos) error {
		cmds, err := tx.Commands.AllByProject(ctx, project)
		if err != nil {
			return err
		}
		for i, c := range cmds {
			if c.Order == i+1 {
				continue
			}
			if err := tx.conn(ctx).Model(&commandRecord{}).Where("id = ?", c.ID).UpdateColumn("order_no", i+1).Error; err != nil {
				return storeFailure(err, "renumber commands")
			}
		}
		return nil
	})
}
