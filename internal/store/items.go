package store

import (
	"context"

	"github.com/rm4n0s/bos/internal/domain"
	"github.com/rm4n0s/errors"
)

// ItemRepo stores items with their item tags (SPEC §3.6).
type ItemRepo struct{ base }

func (r *ItemRepo) failure(err error, code string, group int64) error {
	switch {
	case isUnique(err):
		return errors.New("ItemCodeNameTaken", "an item with this code name already exists in the group", "code_name", code, "group_id", group)
	case isForeignKey(err):
		return notFound("GroupNotFound", "group", group)
	}
	return storeFailure(err, "write item")
}

// itemsOf converts the records and fills in their tags.
func (r *ItemRepo) itemsOf(ctx context.Context, recs []itemRecord) ([]domain.Item, error) {
	ids := make([]int64, len(recs))
	for i, rec := range recs {
		ids[i] = rec.ID
	}
	tags, err := loadTags(r.conn(ctx), itemTagTable, ids)
	if err != nil {
		return nil, err
	}
	out := make([]domain.Item, len(recs))
	for i, rec := range recs {
		out[i] = rec.domain()
		if t := tags[rec.ID]; t != nil {
			out[i].Tags = t
		}
	}
	return out, nil
}

// Create stores a new item with its tags and sets its ID and timestamps.
func (r *ItemRepo) Create(ctx context.Context, v *domain.Item) error {
	now := r.now()
	return r.atomically(ctx, func(tx *Repos) error {
		rec := itemToRecord(v)
		rec.CreatedAt, rec.UpdatedAt = now, now
		if err := tx.conn(ctx).Create(&rec).Error; err != nil {
			return r.failure(err, v.CodeName, v.GroupID)
		}
		if err := writeTags(tx.conn(ctx), itemTagTable, rec.ID, v.Tags); err != nil {
			return err
		}
		v.ID, v.CreatedAt, v.UpdatedAt = rec.ID, now, now
		v.Tags = cleanTags(v.Tags)
		return nil
	})
}

// Update replaces the editable fields of an item, including its tags. The item
// stays in its group.
func (r *ItemRepo) Update(ctx context.Context, v *domain.Item) error {
	now := r.now()
	return r.atomically(ctx, func(tx *Repos) error {
		rec := itemToRecord(v)
		rec.UpdatedAt = now
		res := tx.conn(ctx).Model(&rec).Where("id = ?", v.ID).Select("*").Omit("id", "group_id", "created_at").Updates(&rec)
		if res.Error != nil {
			return r.failure(res.Error, v.CodeName, v.GroupID)
		}
		if err := affected(res, "write item", "ItemNotFound", "item", v.ID); err != nil {
			return err
		}
		if err := writeTags(tx.conn(ctx), itemTagTable, v.ID, v.Tags); err != nil {
			return err
		}
		v.UpdatedAt = now
		v.Tags = cleanTags(v.Tags)
		return nil
	})
}

// Get returns one item with its tags.
func (r *ItemRepo) Get(ctx context.Context, id domain.ID) (*domain.Item, error) {
	rec, err := find[itemRecord](r.conn(ctx).Where("id = ?", id), "ItemNotFound", "item", id, "read item")
	if err != nil {
		return nil, err
	}
	items, err := r.itemsOf(ctx, []itemRecord{*rec})
	if err != nil {
		return nil, err
	}
	return &items[0], nil
}

// ListByGroup returns one page of a group's items in creation order.
func (r *ItemRepo) ListByGroup(ctx context.Context, group domain.ID, p domain.Page) (domain.Paged[domain.Item], error) {
	page, err := pageOf(r.conn(ctx).Where("group_id = ?", group), "id", p, func(rec itemRecord) itemRecord { return rec }, "list items")
	if err != nil {
		return domain.Paged[domain.Item]{}, err
	}
	items, err := r.itemsOf(ctx, page.Items)
	if err != nil {
		return domain.Paged[domain.Item]{}, err
	}
	return domain.NewPaged(items, page.Total, p), nil
}

// AllByGroup returns every item of a group in creation order, which is the item
// order of the command language (SPEC §6.4).
func (r *ItemRepo) AllByGroup(ctx context.Context, group domain.ID) ([]domain.Item, error) {
	recs, err := all[itemRecord](r.conn(ctx).Where("group_id = ?", group).Order("id"), "list items")
	if err != nil {
		return nil, err
	}
	return r.itemsOf(ctx, recs)
}

// LoRAUsage returns, in creation order, every item that carries a LoRA with
// the LoRAs it carries. It reads only the columns it needs.
func (r *ItemRepo) LoRAUsage(ctx context.Context) ([]domain.LoRAUse, error) {
	recs, err := all[itemRecord](r.conn(ctx).Select("id", "group_id", "code_name", "name", "loras").Order("id"), "list item LoRAs")
	if err != nil {
		return nil, err
	}
	groups, err := all[groupRecord](r.conn(ctx).Select("id", "code_name"), "list item LoRAs")
	if err != nil {
		return nil, err
	}
	codes := make(map[int64]string, len(groups))
	for _, g := range groups {
		codes[g.ID] = g.CodeName
	}
	var out []domain.LoRAUse
	for _, rec := range recs {
		if len(rec.LoRAs.V) == 0 {
			continue
		}
		out = append(out, domain.LoRAUse{ItemID: rec.ID, ItemCode: rec.CodeName, ItemName: rec.Name, GroupID: rec.GroupID, GroupCode: codes[rec.GroupID], LoRAs: rec.LoRAs.V})
	}
	return out, nil
}

// Delete removes an item and returns the files and folders that belonged to it
// (SPEC §7.4) for the caller to remove after the transaction has committed.
func (r *ItemRepo) Delete(ctx context.Context, id domain.ID) ([]string, error) {
	it, err := r.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	err = r.atomically(ctx, func(tx *Repos) error {
		if err := writeTags(tx.conn(ctx), itemTagTable, id, nil); err != nil {
			return err
		}
		if err := tx.conn(ctx).Delete(&itemRecord{}, "id = ?", id).Error; err != nil {
			return storeFailure(err, "delete item")
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return ItemFiles(it), nil
}

// deleteByGroup removes every item of a group and returns their files.
func (r *ItemRepo) deleteByGroup(ctx context.Context, group domain.ID) ([]string, error) {
	items, err := r.AllByGroup(ctx, group)
	if err != nil {
		return nil, err
	}
	db := r.conn(ctx)
	owned := db.Model(&itemRecord{}).Select("id").Where("group_id = ?", group)
	if err := db.Where("item_id IN (?)", owned).Delete(&itemTagRecord{}).Error; err != nil {
		return nil, storeFailure(err, "delete items")
	}
	if err := db.Delete(&itemRecord{}, "group_id = ?", group).Error; err != nil {
		return nil, storeFailure(err, "delete items")
	}
	var files []string
	for i := range items {
		files = append(files, ItemFiles(&items[i])...)
	}
	return compact(files), nil
}

// ItemFiles lists the files and folders an item owns (SPEC §7.4).
func ItemFiles(it *domain.Item) []string { return it.Files() }
