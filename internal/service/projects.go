package service

import (
	"context"

	"github.com/rm4n0s/bos/internal/domain"
	"github.com/rm4n0s/errors"
)

// ProjectService manages projects (SPEC §3.7).
type ProjectService struct{ base }

// Create adds a project.
func (s *ProjectService) Create(ctx context.Context, in domain.Project) (*domain.Project, error) {
	if err := checkName("project", in.Name, maxName); err != nil {
		return nil, err
	}
	in.ID = 0
	if err := s.Store.Repos().Projects().Create(ctx, &in); err != nil {
		return nil, err
	}
	return &in, nil
}

// Update renames a project.
func (s *ProjectService) Update(ctx context.Context, id domain.ID, in domain.Project) (*domain.Project, error) {
	if err := checkName("project", in.Name, maxName); err != nil {
		return nil, err
	}
	in.ID = id
	projects := s.Store.Repos().Projects()
	if err := projects.Update(ctx, &in); err != nil {
		return nil, err
	}
	return projects.Get(ctx, id)
}

// Get returns one project.
func (s *ProjectService) Get(ctx context.Context, id domain.ID) (*domain.Project, error) {
	return s.Store.Repos().Projects().Get(ctx, id)
}

// List returns one page of projects.
func (s *ProjectService) List(ctx context.Context, p domain.Page) (domain.Paged[domain.Project], error) {
	return s.Store.Repos().Projects().List(ctx, p)
}

// Delete removes a project with its commands, jobs and result files
// (SPEC §7.4). It refuses while one of its commands is queued or running.
func (s *ProjectService) Delete(ctx context.Context, id domain.ID) error {
	r := s.Store.Repos()
	if _, err := r.Projects().Get(ctx, id); err != nil {
		return err
	}
	cmds, err := r.Commands().AllByProject(ctx, id)
	if err != nil {
		return err
	}
	for _, c := range cmds {
		if busy(c.Status) {
			return errors.New("CommandBusy", "a command of the project is queued or running; stop it first", "command_id", c.ID, "status", string(c.Status))
		}
	}
	files, err := r.Projects().Delete(ctx, id)
	if err != nil {
		return err
	}
	s.removeFiles(files)
	return nil
}

// busy reports whether a command is waiting for the scheduler or being run by
// it, so its jobs must not be replaced.
func busy(s domain.Status) bool { return s == domain.StatusQueued || s == domain.StatusProcessing }
