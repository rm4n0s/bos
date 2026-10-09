package store

import (
	"context"
	"database/sql"

	"github.com/rm4n0s/bos/internal/domain"
	"github.com/rm4n0s/errors"
)

// ProjectRepo stores projects (SPEC §3.7).
type ProjectRepo struct {
	base
	commands *CommandRepo
	jobs     *JobRepo
}

const projectColumns = `id, name, created_at, updated_at`

func scanProject(s scanner) (*domain.Project, error) {
	var v domain.Project
	var created, updated string
	if err := s.Scan(&v.ID, &v.Name, &created, &updated); err != nil {
		return nil, err
	}
	var err error
	if v.CreatedAt, err = parseTime(created); err != nil {
		return nil, err
	}
	if v.UpdatedAt, err = parseTime(updated); err != nil {
		return nil, err
	}
	return &v, nil
}

// Create stores a new project and sets its ID and timestamps.
func (r *ProjectRepo) Create(ctx context.Context, v *domain.Project) error {
	now := r.now()
	res, err := r.q.ExecContext(ctx, `INSERT INTO projects (name, created_at, updated_at) VALUES (?, ?, ?)`,
		v.Name, formatTime(now), formatTime(now))
	if err != nil {
		return storeFailure(err, "write project")
	}
	if v.ID, err = res.LastInsertId(); err != nil {
		return storeFailure(err, "project id")
	}
	v.CreatedAt, v.UpdatedAt = now, now
	return nil
}

// Update renames a project.
func (r *ProjectRepo) Update(ctx context.Context, v *domain.Project) error {
	now := r.now()
	res, err := r.q.ExecContext(ctx, `UPDATE projects SET name=?, updated_at=? WHERE id=?`, v.Name, formatTime(now), v.ID)
	if err != nil {
		return storeFailure(err, "write project")
	}
	if n, err := res.RowsAffected(); err != nil {
		return storeFailure(err, "project rows")
	} else if n == 0 {
		return notFound("ProjectNotFound", "project", v.ID)
	}
	v.UpdatedAt = now
	return nil
}

// Get returns one project.
func (r *ProjectRepo) Get(ctx context.Context, id domain.ID) (*domain.Project, error) {
	v, err := scanProject(r.q.QueryRowContext(ctx, `SELECT `+projectColumns+` FROM projects WHERE id=?`, id))
	return one(v, err, "ProjectNotFound", "project", id, "read project")
}

// List returns one page of projects in creation order.
func (r *ProjectRepo) List(ctx context.Context, p domain.Page) (domain.Paged[domain.Project], error) {
	return queryPage(ctx, r.q, `FROM projects`, nil, `ORDER BY id`, projectColumns, scanProject, p, "list projects")
}

// Delete removes a project with all its jobs and commands (SPEC §7.4) and
// returns the result files and export archives to remove after the transaction
// has committed.
func (r *ProjectRepo) Delete(ctx context.Context, id domain.ID) ([]string, error) {
	var files []string
	err := r.atomically(ctx, func(q dbtx) error {
		var exists int
		if err := q.QueryRowContext(ctx, `SELECT 1 FROM projects WHERE id=?`, id).Scan(&exists); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return notFound("ProjectNotFound", "project", id)
			}
			return storeFailure(err, "delete project")
		}
		inTx := base{q: q, clock: r.clock}
		jobs := &JobRepo{base: inTx}
		jobFiles, err := jobs.deleteWhere(ctx, `project_id=?`, id)
		if err != nil {
			return err
		}
		cmdFiles, err := (&CommandRepo{base: inTx, jobs: jobs}).deleteByProject(ctx, id)
		if err != nil {
			return err
		}
		if _, err := q.ExecContext(ctx, `DELETE FROM projects WHERE id=?`, id); err != nil {
			return storeFailure(err, "delete project")
		}
		files = compact(append(jobFiles, cmdFiles...))
		return nil
	})
	if err != nil {
		return nil, err
	}
	return files, nil
}
