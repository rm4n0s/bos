package store

import (
	"context"
	"database/sql"

	"github.com/rm4n0s/bos/internal/domain"
	"github.com/rm4n0s/errors"
)

// GeneratorRepo stores generator workflows (SPEC §3.2).
type GeneratorRepo struct{ base }

const generatorColumns = `id, name, code_name, workflow, positive_prompt_title, negative_prompt_title,
	output_type, output_attributes, output_node_class_type, output_node_title, has_random_seed, created_at, updated_at`

func scanGenerator(s scanner) (*domain.Generator, error) {
	var v domain.Generator
	var workflow, attrs, created, updated string
	var random int
	var outputType string
	if err := s.Scan(&v.ID, &v.Name, &v.CodeName, &workflow, &v.PositivePromptTitle, &v.NegativePromptTitle,
		&outputType, &attrs, &v.OutputNodeClassType, &v.OutputNodeTitle, &random, &created, &updated); err != nil {
		return nil, err
	}
	v.Workflow = []byte(workflow)
	v.OutputType = domain.OutputType(outputType)
	v.HasRandomSeed = random != 0
	if err := decode(attrs, &v.OutputAttributes); err != nil {
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

func (r *GeneratorRepo) taken(err error, code string) error {
	if isUnique(err) {
		return errors.New("GeneratorCodeNameTaken", "a generator with this code name already exists", "code_name", code)
	}
	return storeFailure(err, "write generator")
}

// Create stores a new generator and sets its ID and timestamps.
func (r *GeneratorRepo) Create(ctx context.Context, v *domain.Generator) error {
	attrs, err := encode(v.OutputAttributes, "{}")
	if err != nil {
		return err
	}
	now := r.now()
	res, err := r.q.ExecContext(ctx,
		`INSERT INTO generators (name, code_name, workflow, positive_prompt_title, negative_prompt_title, output_type,
		   output_attributes, output_node_class_type, output_node_title, has_random_seed, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		v.Name, v.CodeName, string(v.Workflow), v.PositivePromptTitle, v.NegativePromptTitle, string(v.OutputType),
		attrs, v.OutputNodeClassType, v.OutputNodeTitle, boolInt(v.HasRandomSeed), formatTime(now), formatTime(now))
	if err != nil {
		return r.taken(err, v.CodeName)
	}
	if v.ID, err = res.LastInsertId(); err != nil {
		return storeFailure(err, "generator id")
	}
	v.CreatedAt, v.UpdatedAt = now, now
	return nil
}

// Update replaces the editable fields of a generator.
func (r *GeneratorRepo) Update(ctx context.Context, v *domain.Generator) error {
	attrs, err := encode(v.OutputAttributes, "{}")
	if err != nil {
		return err
	}
	now := r.now()
	res, err := r.q.ExecContext(ctx,
		`UPDATE generators SET name=?, code_name=?, workflow=?, positive_prompt_title=?, negative_prompt_title=?,
		   output_type=?, output_attributes=?, output_node_class_type=?, output_node_title=?, has_random_seed=?, updated_at=?
		 WHERE id=?`,
		v.Name, v.CodeName, string(v.Workflow), v.PositivePromptTitle, v.NegativePromptTitle, string(v.OutputType),
		attrs, v.OutputNodeClassType, v.OutputNodeTitle, boolInt(v.HasRandomSeed), formatTime(now), v.ID)
	if err != nil {
		return r.taken(err, v.CodeName)
	}
	if n, err := res.RowsAffected(); err != nil {
		return storeFailure(err, "generator rows")
	} else if n == 0 {
		return notFound("GeneratorNotFound", "generator", v.ID)
	}
	v.UpdatedAt = now
	return nil
}

// Delete removes a generator.
func (r *GeneratorRepo) Delete(ctx context.Context, id domain.ID) error {
	res, err := r.q.ExecContext(ctx, `DELETE FROM generators WHERE id=?`, id)
	if err != nil {
		return storeFailure(err, "delete generator")
	}
	if n, err := res.RowsAffected(); err != nil {
		return storeFailure(err, "generator rows")
	} else if n == 0 {
		return notFound("GeneratorNotFound", "generator", id)
	}
	return nil
}

// Get returns one generator.
func (r *GeneratorRepo) Get(ctx context.Context, id domain.ID) (*domain.Generator, error) {
	v, err := scanGenerator(r.q.QueryRowContext(ctx, `SELECT `+generatorColumns+` FROM generators WHERE id=?`, id))
	return one(v, err, "GeneratorNotFound", "generator", id, "read generator")
}

// GetByCode returns the generator with a code name.
func (r *GeneratorRepo) GetByCode(ctx context.Context, code string) (*domain.Generator, error) {
	v, err := scanGenerator(r.q.QueryRowContext(ctx, `SELECT `+generatorColumns+` FROM generators WHERE code_name=?`, code))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errors.New("GeneratorNotFound", "generator not found", "code_name", code)
	}
	if err != nil {
		return nil, asStoreError(err, "read generator")
	}
	return v, nil
}

// List returns one page of generators in creation order.
func (r *GeneratorRepo) List(ctx context.Context, p domain.Page) (domain.Paged[domain.Generator], error) {
	return queryPage(ctx, r.q, `FROM generators`, nil, `ORDER BY id`, generatorColumns, scanGenerator, p, "list generators")
}
