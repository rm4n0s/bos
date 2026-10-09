package store

import (
	"context"

	"github.com/rm4n0s/bos/internal/domain"
	"github.com/rm4n0s/errors"
)

// FixerRepo stores fixer workflows (SPEC §3.3).
type FixerRepo struct{ base }

func (r *FixerRepo) taken(err error, code string) error {
	if isUnique(err) {
		return errors.New("FixerCodeNameTaken", "a fixer with this code name already exists", "code_name", code)
	}
	return storeFailure(err, "write fixer")
}

// Create stores a new fixer and sets its ID and timestamps.
func (r *FixerRepo) Create(ctx context.Context, v *domain.Fixer) error {
	now := r.now()
	rec := fixerToRecord(v)
	rec.CreatedAt, rec.UpdatedAt = now, now
	if err := r.conn(ctx).Create(&rec).Error; err != nil {
		return r.taken(err, v.CodeName)
	}
	v.ID, v.CreatedAt, v.UpdatedAt = rec.ID, now, now
	return nil
}

// Update replaces the editable fields of a fixer.
func (r *FixerRepo) Update(ctx context.Context, v *domain.Fixer) error {
	now := r.now()
	rec := fixerToRecord(v)
	rec.UpdatedAt = now
	res := updateRow(r.conn(ctx), &rec, v.ID)
	if res.Error != nil {
		return r.taken(res.Error, v.CodeName)
	}
	if err := affected(res, "write fixer", "FixerNotFound", "fixer", v.ID); err != nil {
		return err
	}
	v.UpdatedAt = now
	return nil
}

// Delete removes a fixer.
func (r *FixerRepo) Delete(ctx context.Context, id domain.ID) error {
	return affected(r.conn(ctx).Delete(&fixerRecord{}, "id = ?", id), "delete fixer", "FixerNotFound", "fixer", id)
}

// Get returns one fixer.
func (r *FixerRepo) Get(ctx context.Context, id domain.ID) (*domain.Fixer, error) {
	rec, err := find[fixerRecord](r.conn(ctx).Where("id = ?", id), "FixerNotFound", "fixer", id, "read fixer")
	if err != nil {
		return nil, err
	}
	v := rec.domain()
	return &v, nil
}

// GetByCode returns the fixer with a code name.
func (r *FixerRepo) GetByCode(ctx context.Context, code string) (*domain.Fixer, error) {
	var rec fixerRecord
	if err := r.conn(ctx).Where("code_name = ?", code).Take(&rec).Error; err != nil {
		if isMissing(err) {
			return nil, errors.New("FixerNotFound", "fixer not found", "code_name", code)
		}
		return nil, storeFailure(err, "read fixer")
	}
	v := rec.domain()
	return &v, nil
}

// List returns one page of fixers in creation order.
func (r *FixerRepo) List(ctx context.Context, p domain.Page) (domain.Paged[domain.Fixer], error) {
	return pageOf(r.conn(ctx), "id", p, fixerRecord.domain, "list fixers")
}
