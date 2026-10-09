package store

import (
	"context"

	"github.com/rm4n0s/bos/internal/domain"
)

// ProjectRepo stores projects (SPEC §3.7).
type ProjectRepo struct{ base }

// Create stores a new project and sets its ID and timestamps.
func (r *ProjectRepo) Create(ctx context.Context, v *domain.Project) error {
	now := r.now()
	rec := projectToRecord(v)
	rec.CreatedAt, rec.UpdatedAt = now, now
	if err := r.conn(ctx).Create(&rec).Error; err != nil {
		return storeFailure(err, "write project")
	}
	v.ID, v.CreatedAt, v.UpdatedAt = rec.ID, now, now
	return nil
}

// Update renames a project.
func (r *ProjectRepo) Update(ctx context.Context, v *domain.Project) error {
	now := r.now()
	rec := projectToRecord(v)
	rec.UpdatedAt = now
	if err := affected(updateRow(r.conn(ctx), &rec, v.ID), "write project", "ProjectNotFound", "project", v.ID); err != nil {
		return err
	}
	v.UpdatedAt = now
	return nil
}

// Get returns one project.
func (r *ProjectRepo) Get(ctx context.Context, id domain.ID) (*domain.Project, error) {
	rec, err := find[projectRecord](r.conn(ctx).Where("id = ?", id), "ProjectNotFound", "project", id, "read project")
	if err != nil {
		return nil, err
	}
	v := rec.domain()
	return &v, nil
}

// List returns one page of projects in creation order.
func (r *ProjectRepo) List(ctx context.Context, p domain.Page) (domain.Paged[domain.Project], error) {
	return pageOf(r.conn(ctx), "id", p, projectRecord.domain, "list projects")
}

// Delete removes a project with all its jobs and commands (SPEC §7.4) and
// returns the result files and export archives to remove after the transaction
// has committed.
func (r *ProjectRepo) Delete(ctx context.Context, id domain.ID) ([]string, error) {
	var files []string
	err := r.atomically(ctx, func(tx *Repos) error {
		if _, err := tx.Projects.Get(ctx, id); err != nil {
			return err
		}
		jobFiles, err := tx.Jobs.deleteWhere(ctx, "project_id = ?", id)
		if err != nil {
			return err
		}
		cmdFiles, err := tx.Commands.deleteByProject(ctx, id)
		if err != nil {
			return err
		}
		if err := tx.conn(ctx).Delete(&projectRecord{}, "id = ?", id).Error; err != nil {
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
