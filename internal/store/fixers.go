package store

import (
	"context"
	"database/sql"

	"github.com/rm4n0s/bos/internal/domain"
	"github.com/rm4n0s/errors"
)

// FixerRepo stores fixer workflows (SPEC §3.3).
type FixerRepo struct{ base }

const fixerColumns = `id, name, code_name, workflow, load_image_title, save_image_title, positive_prompt,
	negative_prompt, positive_prompt_title, negative_prompt_title, created_at, updated_at`

func scanFixer(s scanner) (*domain.Fixer, error) {
	var v domain.Fixer
	var workflow, created, updated string
	if err := s.Scan(&v.ID, &v.Name, &v.CodeName, &workflow, &v.LoadImageTitle, &v.SaveImageTitle, &v.PositivePrompt,
		&v.NegativePrompt, &v.PositivePromptTitle, &v.NegativePromptTitle, &created, &updated); err != nil {
		return nil, err
	}
	v.Workflow = []byte(workflow)
	var err error
	if v.CreatedAt, err = parseTime(created); err != nil {
		return nil, err
	}
	if v.UpdatedAt, err = parseTime(updated); err != nil {
		return nil, err
	}
	return &v, nil
}

func (r *FixerRepo) taken(err error, code string) error {
	if isUnique(err) {
		return errors.New("FixerCodeNameTaken", "a fixer with this code name already exists", "code_name", code)
	}
	return storeFailure(err, "write fixer")
}

// Create stores a new fixer and sets its ID and timestamps.
func (r *FixerRepo) Create(ctx context.Context, v *domain.Fixer) error {
	now := r.now()
	res, err := r.q.ExecContext(ctx,
		`INSERT INTO fixers (name, code_name, workflow, load_image_title, save_image_title, positive_prompt,
		   negative_prompt, positive_prompt_title, negative_prompt_title, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		v.Name, v.CodeName, string(v.Workflow), v.LoadImageTitle, v.SaveImageTitle, v.PositivePrompt,
		v.NegativePrompt, v.PositivePromptTitle, v.NegativePromptTitle, formatTime(now), formatTime(now))
	if err != nil {
		return r.taken(err, v.CodeName)
	}
	if v.ID, err = res.LastInsertId(); err != nil {
		return storeFailure(err, "fixer id")
	}
	v.CreatedAt, v.UpdatedAt = now, now
	return nil
}

// Update replaces the editable fields of a fixer.
func (r *FixerRepo) Update(ctx context.Context, v *domain.Fixer) error {
	now := r.now()
	res, err := r.q.ExecContext(ctx,
		`UPDATE fixers SET name=?, code_name=?, workflow=?, load_image_title=?, save_image_title=?, positive_prompt=?,
		   negative_prompt=?, positive_prompt_title=?, negative_prompt_title=?, updated_at=? WHERE id=?`,
		v.Name, v.CodeName, string(v.Workflow), v.LoadImageTitle, v.SaveImageTitle, v.PositivePrompt,
		v.NegativePrompt, v.PositivePromptTitle, v.NegativePromptTitle, formatTime(now), v.ID)
	if err != nil {
		return r.taken(err, v.CodeName)
	}
	if n, err := res.RowsAffected(); err != nil {
		return storeFailure(err, "fixer rows")
	} else if n == 0 {
		return notFound("FixerNotFound", "fixer", v.ID)
	}
	v.UpdatedAt = now
	return nil
}

// Delete removes a fixer.
func (r *FixerRepo) Delete(ctx context.Context, id domain.ID) error {
	res, err := r.q.ExecContext(ctx, `DELETE FROM fixers WHERE id=?`, id)
	if err != nil {
		return storeFailure(err, "delete fixer")
	}
	if n, err := res.RowsAffected(); err != nil {
		return storeFailure(err, "fixer rows")
	} else if n == 0 {
		return notFound("FixerNotFound", "fixer", id)
	}
	return nil
}

// Get returns one fixer.
func (r *FixerRepo) Get(ctx context.Context, id domain.ID) (*domain.Fixer, error) {
	v, err := scanFixer(r.q.QueryRowContext(ctx, `SELECT `+fixerColumns+` FROM fixers WHERE id=?`, id))
	return one(v, err, "FixerNotFound", "fixer", id, "read fixer")
}

// GetByCode returns the fixer with a code name.
func (r *FixerRepo) GetByCode(ctx context.Context, code string) (*domain.Fixer, error) {
	v, err := scanFixer(r.q.QueryRowContext(ctx, `SELECT `+fixerColumns+` FROM fixers WHERE code_name=?`, code))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errors.New("FixerNotFound", "fixer not found", "code_name", code)
	}
	if err != nil {
		return nil, asStoreError(err, "read fixer")
	}
	return v, nil
}

// List returns one page of fixers in creation order.
func (r *FixerRepo) List(ctx context.Context, p domain.Page) (domain.Paged[domain.Fixer], error) {
	return queryPage(ctx, r.q, `FROM fixers`, nil, `ORDER BY id`, fixerColumns, scanFixer, p, "list fixers")
}
