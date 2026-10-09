package store

import (
	"context"

	"github.com/rm4n0s/bos/internal/domain"
	"github.com/rm4n0s/errors"
)

// GroupRepo stores groups with their group tags (SPEC §3.5).
type GroupRepo struct{ base }

func (r *GroupRepo) taken(err error, code string) error {
	if isUnique(err) {
		return errors.New("GroupCodeNameTaken", "a group with this code name already exists", "code_name", code)
	}
	return storeFailure(err, "write group")
}

// groupsOf converts the records and fills in their tags.
func (r *GroupRepo) groupsOf(ctx context.Context, recs []groupRecord) ([]domain.Group, error) {
	ids := make([]int64, len(recs))
	for i, rec := range recs {
		ids[i] = rec.ID
	}
	tags, err := loadTags(r.conn(ctx), groupTagTable, ids)
	if err != nil {
		return nil, err
	}
	out := make([]domain.Group, len(recs))
	for i, rec := range recs {
		out[i] = rec.domain()
		if t := tags[rec.ID]; t != nil {
			out[i].Tags = t
		}
	}
	return out, nil
}

// Create stores a new group with its tags and sets its ID and timestamps.
func (r *GroupRepo) Create(ctx context.Context, v *domain.Group) error {
	now := r.now()
	return r.atomically(ctx, func(tx *Repos) error {
		rec := groupToRecord(v)
		rec.CreatedAt, rec.UpdatedAt = now, now
		if err := tx.conn(ctx).Create(&rec).Error; err != nil {
			return r.taken(err, v.CodeName)
		}
		if err := writeTags(tx.conn(ctx), groupTagTable, rec.ID, v.Tags); err != nil {
			return err
		}
		v.ID, v.CreatedAt, v.UpdatedAt = rec.ID, now, now
		v.Tags = cleanTags(v.Tags)
		return nil
	})
}

// Update replaces the editable fields of a group, including its tags.
func (r *GroupRepo) Update(ctx context.Context, v *domain.Group) error {
	now := r.now()
	return r.atomically(ctx, func(tx *Repos) error {
		rec := groupToRecord(v)
		rec.UpdatedAt = now
		res := updateRow(tx.conn(ctx), &rec, v.ID)
		if res.Error != nil {
			return r.taken(res.Error, v.CodeName)
		}
		if err := affected(res, "write group", "GroupNotFound", "group", v.ID); err != nil {
			return err
		}
		if err := writeTags(tx.conn(ctx), groupTagTable, v.ID, v.Tags); err != nil {
			return err
		}
		v.UpdatedAt = now
		v.Tags = cleanTags(v.Tags)
		return nil
	})
}

// Get returns one group with its tags.
func (r *GroupRepo) Get(ctx context.Context, id domain.ID) (*domain.Group, error) {
	rec, err := find[groupRecord](r.conn(ctx).Where("id = ?", id), "GroupNotFound", "group", id, "read group")
	if err != nil {
		return nil, err
	}
	return r.single(ctx, *rec)
}

// GetByCode returns the group with a code name.
func (r *GroupRepo) GetByCode(ctx context.Context, code string) (*domain.Group, error) {
	var rec groupRecord
	if err := r.conn(ctx).Where("code_name = ?", code).Take(&rec).Error; err != nil {
		if isMissing(err) {
			return nil, errors.New("GroupNotFound", "group not found", "code_name", code)
		}
		return nil, storeFailure(err, "read group")
	}
	return r.single(ctx, rec)
}

func (r *GroupRepo) single(ctx context.Context, rec groupRecord) (*domain.Group, error) {
	groups, err := r.groupsOf(ctx, []groupRecord{rec})
	if err != nil {
		return nil, err
	}
	return &groups[0], nil
}

// List returns one page of groups in creation order. A non-empty tag keeps only
// the groups that carry that group tag.
func (r *GroupRepo) List(ctx context.Context, tag string, p domain.Page) (domain.Paged[domain.Group], error) {
	q := r.conn(ctx)
	if tag != "" {
		carriers := r.conn(ctx).Model(&groupTagRecord{}).Select("group_id").Where("tag = ?", tag)
		q = q.Where("id IN (?)", carriers)
	}
	page, err := pageOf(q, "id", p, func(rec groupRecord) groupRecord { return rec }, "list groups")
	if err != nil {
		return domain.Paged[domain.Group]{}, err
	}
	groups, err := r.groupsOf(ctx, page.Items)
	if err != nil {
		return domain.Paged[domain.Group]{}, err
	}
	return domain.NewPaged(groups, page.Total, p), nil
}

// Delete removes a group together with its items (SPEC §7.4). It returns the
// files and folders that belonged to them, for the caller to remove once the
// transaction has committed.
func (r *GroupRepo) Delete(ctx context.Context, id domain.ID) ([]string, error) {
	var files []string
	err := r.atomically(ctx, func(tx *Repos) error {
		rec, err := find[groupRecord](tx.conn(ctx).Where("id = ?", id), "GroupNotFound", "group", id, "delete group")
		if err != nil {
			return err
		}
		itemFiles, err := tx.Items.deleteByGroup(ctx, id)
		if err != nil {
			return err
		}
		if err := writeTags(tx.conn(ctx), groupTagTable, id, nil); err != nil {
			return err
		}
		if err := tx.conn(ctx).Delete(&groupRecord{}, "id = ?", id).Error; err != nil {
			return storeFailure(err, "delete group")
		}
		files = compact(append([]string{rec.Thumbnail}, itemFiles...))
		return nil
	})
	if err != nil {
		return nil, err
	}
	return files, nil
}
