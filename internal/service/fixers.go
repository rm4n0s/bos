package service

import (
	"context"

	"github.com/rm4n0s/bos/internal/domain"
	"github.com/rm4n0s/bos/internal/workflow"
	"github.com/rm4n0s/errors"
)

// FixerService manages the fixer workflows (SPEC §3.3).
type FixerService struct{ base }

func (s *FixerService) check(v *domain.Fixer) error {
	if err := checkName("fixer", v.Name, maxName); err != nil {
		return err
	}
	if err := checkCodeName("fixer", v.CodeName); err != nil {
		return err
	}
	g, err := workflow.Parse(v.Workflow)
	if err != nil {
		return err
	}
	for _, f := range []struct{ field, title string }{
		{"load_image_title", v.LoadImageTitle}, {"save_image_title", v.SaveImageTitle},
	} {
		if f.title == "" {
			return errors.New("FixerTitleRequired", "a fixer needs the title of its image input and output nodes", "field", f.field)
		}
	}
	return workflow.CheckTitles(g, v.LoadImageTitle, v.SaveImageTitle, v.PositivePromptTitle, v.NegativePromptTitle)
}

// Create adds a fixer; its workflow and node titles are checked.
func (s *FixerService) Create(ctx context.Context, in domain.Fixer) (*domain.Fixer, error) {
	if err := s.check(&in); err != nil {
		return nil, err
	}
	in.ID = 0
	if err := s.Store.Repos().Fixers().Create(ctx, &in); err != nil {
		return nil, err
	}
	return &in, nil
}

// Update replaces the editable fields of a fixer.
func (s *FixerService) Update(ctx context.Context, id domain.ID, in domain.Fixer) (*domain.Fixer, error) {
	if err := s.check(&in); err != nil {
		return nil, err
	}
	in.ID = id
	fixers := s.Store.Repos().Fixers()
	if err := fixers.Update(ctx, &in); err != nil {
		return nil, err
	}
	return fixers.Get(ctx, id)
}

// Delete removes a fixer.
func (s *FixerService) Delete(ctx context.Context, id domain.ID) error {
	return s.Store.Repos().Fixers().Delete(ctx, id)
}

// Get returns one fixer.
func (s *FixerService) Get(ctx context.Context, id domain.ID) (*domain.Fixer, error) {
	return s.Store.Repos().Fixers().Get(ctx, id)
}

// List returns one page of fixers.
func (s *FixerService) List(ctx context.Context, p domain.Page) (domain.Paged[domain.Fixer], error) {
	return s.Store.Repos().Fixers().List(ctx, p)
}
