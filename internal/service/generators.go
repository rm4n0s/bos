package service

import (
	"context"

	"github.com/rm4n0s/bos/internal/domain"
	"github.com/rm4n0s/bos/internal/workflow"
	"github.com/rm4n0s/errors"
)

// GeneratorService manages the generator workflows (SPEC §3.2).
type GeneratorService struct{ base }

// Analyze infers the output type, default attributes, output node and the
// candidate prompt-node titles of a workflow, and persists nothing
// (SPEC §15.2).
func (s *GeneratorService) Analyze(workflowJSON []byte) (*workflow.Analysis, error) {
	g, err := workflow.Parse(workflowJSON)
	if err != nil {
		return nil, err
	}
	return workflow.Analyze(g)
}

// complete checks a generator and fills in what the analysis can supply: an
// empty output type, attributes, output node class or title are taken from the
// workflow.
func (s *GeneratorService) complete(v *domain.Generator) error {
	if err := checkName("generator", v.Name, maxName); err != nil {
		return err
	}
	if err := checkCodeName("generator", v.CodeName); err != nil {
		return err
	}
	g, err := workflow.Parse(v.Workflow)
	if err != nil {
		return err
	}
	if v.OutputType != "" && !v.OutputType.Valid() {
		return errors.New("OutputTypeInvalid", "the output type must be image, video, 3d or unknown", "output_type", string(v.OutputType))
	}
	if v.OutputType == "" || v.OutputNodeTitle == "" || v.OutputNodeClassType == "" || v.OutputAttributes == nil {
		a, err := workflow.Analyze(g)
		if err != nil {
			return err
		}
		if v.OutputType == "" {
			v.OutputType = a.OutputType
		}
		if v.OutputAttributes == nil {
			v.OutputAttributes = a.Attributes
		}
		if v.OutputNodeClassType == "" && v.OutputNodeTitle == "" {
			v.OutputNodeClassType, v.OutputNodeTitle = a.OutputNodeClassType, a.OutputNodeTitle
		}
	}
	// Every title the application addresses must name exactly one node.
	return workflow.CheckTitles(g, v.PositivePromptTitle, v.NegativePromptTitle, v.OutputNodeTitle)
}

// Create adds a generator. The workflow is parsed and its node titles are
// checked; fields left empty are inferred from it.
func (s *GeneratorService) Create(ctx context.Context, in domain.Generator) (*domain.Generator, error) {
	if err := s.complete(&in); err != nil {
		return nil, err
	}
	in.ID = 0
	if err := s.Store.Repos().Generators().Create(ctx, &in); err != nil {
		return nil, err
	}
	return &in, nil
}

// Update replaces the editable fields of a generator.
func (s *GeneratorService) Update(ctx context.Context, id domain.ID, in domain.Generator) (*domain.Generator, error) {
	if err := s.complete(&in); err != nil {
		return nil, err
	}
	in.ID = id
	generators := s.Store.Repos().Generators()
	if err := generators.Update(ctx, &in); err != nil {
		return nil, err
	}
	return generators.Get(ctx, id)
}

// Delete removes a generator. Commands that name it fail validation the next
// time they are edited; their jobs are snapshots and stay (SPEC §3).
func (s *GeneratorService) Delete(ctx context.Context, id domain.ID) error {
	return s.Store.Repos().Generators().Delete(ctx, id)
}

// Get returns one generator.
func (s *GeneratorService) Get(ctx context.Context, id domain.ID) (*domain.Generator, error) {
	return s.Store.Repos().Generators().Get(ctx, id)
}

// List returns one page of generators.
func (s *GeneratorService) List(ctx context.Context, p domain.Page) (domain.Paged[domain.Generator], error) {
	return s.Store.Repos().Generators().List(ctx, p)
}
