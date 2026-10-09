package store

import (
	"context"

	"github.com/rm4n0s/bos/internal/domain"
	"github.com/rm4n0s/errors"
)

// GeneratorRepo stores generator workflows (SPEC §3.2).
type GeneratorRepo struct{ base }

func (r *GeneratorRepo) taken(err error, code string) error {
	if isUnique(err) {
		return errors.New("GeneratorCodeNameTaken", "a generator with this code name already exists", "code_name", code)
	}
	return storeFailure(err, "write generator")
}

// Create stores a new generator and sets its ID and timestamps.
func (r *GeneratorRepo) Create(ctx context.Context, v *domain.Generator) error {
	now := r.now()
	rec := generatorToRecord(v)
	rec.CreatedAt, rec.UpdatedAt = now, now
	if err := r.conn(ctx).Create(&rec).Error; err != nil {
		return r.taken(err, v.CodeName)
	}
	v.ID, v.CreatedAt, v.UpdatedAt = rec.ID, now, now
	return nil
}

// Update replaces the editable fields of a generator.
func (r *GeneratorRepo) Update(ctx context.Context, v *domain.Generator) error {
	now := r.now()
	rec := generatorToRecord(v)
	rec.UpdatedAt = now
	res := updateRow(r.conn(ctx), &rec, v.ID)
	if res.Error != nil {
		return r.taken(res.Error, v.CodeName)
	}
	if err := affected(res, "write generator", "GeneratorNotFound", "generator", v.ID); err != nil {
		return err
	}
	v.UpdatedAt = now
	return nil
}

// Delete removes a generator.
func (r *GeneratorRepo) Delete(ctx context.Context, id domain.ID) error {
	return affected(r.conn(ctx).Delete(&generatorRecord{}, "id = ?", id), "delete generator", "GeneratorNotFound", "generator", id)
}

// Get returns one generator.
func (r *GeneratorRepo) Get(ctx context.Context, id domain.ID) (*domain.Generator, error) {
	rec, err := find[generatorRecord](r.conn(ctx).Where("id = ?", id), "GeneratorNotFound", "generator", id, "read generator")
	if err != nil {
		return nil, err
	}
	v := rec.domain()
	return &v, nil
}

// GetByCode returns the generator with a code name.
func (r *GeneratorRepo) GetByCode(ctx context.Context, code string) (*domain.Generator, error) {
	var rec generatorRecord
	if err := r.conn(ctx).Where("code_name = ?", code).Take(&rec).Error; err != nil {
		if isMissing(err) {
			return nil, errors.New("GeneratorNotFound", "generator not found", "code_name", code)
		}
		return nil, storeFailure(err, "read generator")
	}
	v := rec.domain()
	return &v, nil
}

// List returns one page of generators in creation order.
func (r *GeneratorRepo) List(ctx context.Context, p domain.Page) (domain.Paged[domain.Generator], error) {
	return pageOf(r.conn(ctx), "id", p, generatorRecord.domain, "list generators")
}
