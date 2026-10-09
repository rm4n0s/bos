package store

import (
	"context"
	"database/sql"

	"github.com/rm4n0s/bos/internal/domain"
	"github.com/rm4n0s/errors"
)

// ItemRepo stores items with their item tags (SPEC §3.6).
type ItemRepo struct{ base }

const itemColumns = `id, group_id, code_name, name, positive_prompt, negative_prompt, loras, ip_adapter, controlnets,
	mask_region, coordinated_regions, output_type, output_type_attributes, thumbnail, created_at, updated_at`

// itemJSON holds the encoded JSON columns of an item.
type itemJSON struct {
	loras, ipAdapter, controlNets, maskRegion, regions, attrs any
}

func encodeItem(v *domain.Item) (itemJSON, error) {
	var j itemJSON
	var err error
	if j.loras, err = encode(v.LoRAs, "[]"); err != nil {
		return j, err
	}
	if j.controlNets, err = encode(v.ControlNets, "[]"); err != nil {
		return j, err
	}
	if j.regions, err = encode(v.CoordinatedRegions, "[]"); err != nil {
		return j, err
	}
	if j.attrs, err = encode(v.OutputTypeAttributes, "{}"); err != nil {
		return j, err
	}
	if v.IPAdapter != nil {
		if j.ipAdapter, err = encode(v.IPAdapter, "null"); err != nil {
			return j, err
		}
	}
	if v.MaskRegion != nil {
		if j.maskRegion, err = encode(v.MaskRegion, "null"); err != nil {
			return j, err
		}
	}
	return j, nil
}

func scanItem(s scanner) (*domain.Item, error) {
	var v domain.Item
	var loras, controlNets, regions, attrs, created, updated, outputType string
	var ip, mask sql.NullString
	if err := s.Scan(&v.ID, &v.GroupID, &v.CodeName, &v.Name, &v.PositivePrompt, &v.NegativePrompt, &loras, &ip,
		&controlNets, &mask, &regions, &outputType, &attrs, &v.Thumbnail, &created, &updated); err != nil {
		return nil, err
	}
	v.OutputType = domain.OutputType(outputType)
	// A slice, not a map: several columns often hold the same text, "[]".
	for _, col := range []struct {
		text string
		dst  any
	}{{loras, &v.LoRAs}, {controlNets, &v.ControlNets}, {regions, &v.CoordinatedRegions}, {attrs, &v.OutputTypeAttributes}} {
		if err := decode(col.text, col.dst); err != nil {
			return nil, err
		}
	}
	if ip.Valid {
		if err := decode(ip.String, &v.IPAdapter); err != nil {
			return nil, err
		}
	}
	if mask.Valid {
		if err := decode(mask.String, &v.MaskRegion); err != nil {
			return nil, err
		}
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

func (r *ItemRepo) failure(err error, code string, group int64) error {
	switch {
	case isUnique(err):
		return errors.New("ItemCodeNameTaken", "an item with this code name already exists in the group", "code_name", code, "group_id", group)
	case isForeignKey(err):
		return notFound("GroupNotFound", "group", group)
	}
	return storeFailure(err, "write item")
}

func (r *ItemRepo) withTags(ctx context.Context, items []domain.Item) error {
	ids := make([]int64, len(items))
	for i, it := range items {
		ids[i] = it.ID
	}
	tags, err := loadTags(ctx, r.q, itemTagTable, ids)
	if err != nil {
		return err
	}
	for i := range items {
		items[i].Tags = tags[items[i].ID]
		if items[i].Tags == nil {
			items[i].Tags = []string{}
		}
	}
	return nil
}

// Create stores a new item with its tags and sets its ID and timestamps.
func (r *ItemRepo) Create(ctx context.Context, v *domain.Item) error {
	j, err := encodeItem(v)
	if err != nil {
		return err
	}
	now := r.now()
	return r.atomically(ctx, func(q dbtx) error {
		res, err := q.ExecContext(ctx,
			`INSERT INTO items (group_id, code_name, name, positive_prompt, negative_prompt, loras, ip_adapter, controlnets,
			   mask_region, coordinated_regions, output_type, output_type_attributes, thumbnail, created_at, updated_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			v.GroupID, v.CodeName, v.Name, v.PositivePrompt, v.NegativePrompt, j.loras, j.ipAdapter, j.controlNets,
			j.maskRegion, j.regions, string(v.OutputType), j.attrs, v.Thumbnail, formatTime(now), formatTime(now))
		if err != nil {
			return r.failure(err, v.CodeName, v.GroupID)
		}
		id, err := res.LastInsertId()
		if err != nil {
			return storeFailure(err, "item id")
		}
		if err := writeTags(ctx, q, itemTagTable, id, v.Tags); err != nil {
			return err
		}
		v.ID, v.CreatedAt, v.UpdatedAt = id, now, now
		v.Tags = cleanTags(v.Tags)
		return nil
	})
}

// Update replaces the editable fields of an item, including its tags. The item
// stays in its group.
func (r *ItemRepo) Update(ctx context.Context, v *domain.Item) error {
	j, err := encodeItem(v)
	if err != nil {
		return err
	}
	now := r.now()
	return r.atomically(ctx, func(q dbtx) error {
		res, err := q.ExecContext(ctx,
			`UPDATE items SET code_name=?, name=?, positive_prompt=?, negative_prompt=?, loras=?, ip_adapter=?,
			   controlnets=?, mask_region=?, coordinated_regions=?, output_type=?, output_type_attributes=?, thumbnail=?,
			   updated_at=? WHERE id=?`,
			v.CodeName, v.Name, v.PositivePrompt, v.NegativePrompt, j.loras, j.ipAdapter, j.controlNets, j.maskRegion,
			j.regions, string(v.OutputType), j.attrs, v.Thumbnail, formatTime(now), v.ID)
		if err != nil {
			return r.failure(err, v.CodeName, v.GroupID)
		}
		if n, err := res.RowsAffected(); err != nil {
			return storeFailure(err, "item rows")
		} else if n == 0 {
			return notFound("ItemNotFound", "item", v.ID)
		}
		if err := writeTags(ctx, q, itemTagTable, v.ID, v.Tags); err != nil {
			return err
		}
		v.UpdatedAt = now
		v.Tags = cleanTags(v.Tags)
		return nil
	})
}

// Get returns one item with its tags.
func (r *ItemRepo) Get(ctx context.Context, id domain.ID) (*domain.Item, error) {
	v, err := scanItem(r.q.QueryRowContext(ctx, `SELECT `+itemColumns+` FROM items WHERE id=?`, id))
	v, err = one(v, err, "ItemNotFound", "item", id, "read item")
	if err != nil {
		return nil, err
	}
	list := []domain.Item{*v}
	if err := r.withTags(ctx, list); err != nil {
		return nil, err
	}
	return &list[0], nil
}

// ListByGroup returns one page of a group's items in creation order.
func (r *ItemRepo) ListByGroup(ctx context.Context, group domain.ID, p domain.Page) (domain.Paged[domain.Item], error) {
	page, err := queryPage(ctx, r.q, `FROM items WHERE group_id=?`, []any{group}, `ORDER BY id`, itemColumns, scanItem, p, "list items")
	if err != nil {
		return page, err
	}
	return page, r.withTags(ctx, page.Items)
}

// AllByGroup returns every item of a group in creation order, which is the item
// order of the command language (SPEC §6.4).
func (r *ItemRepo) AllByGroup(ctx context.Context, group domain.ID) ([]domain.Item, error) {
	items, err := queryAll(ctx, r.q, `SELECT `+itemColumns+` FROM items WHERE group_id=? ORDER BY id`, scanItem, "list items", group)
	if err != nil {
		return nil, err
	}
	if err := r.withTags(ctx, items); err != nil {
		return nil, err
	}
	return items, nil
}

// Delete removes an item and returns the files and folders that belonged to it
// (SPEC §7.4) for the caller to remove after the transaction has committed.
func (r *ItemRepo) Delete(ctx context.Context, id domain.ID) ([]string, error) {
	it, err := r.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	err = r.atomically(ctx, func(q dbtx) error {
		if _, err := q.ExecContext(ctx, `DELETE FROM item_tags WHERE item_id=?`, id); err != nil {
			return storeFailure(err, "delete item")
		}
		if _, err := q.ExecContext(ctx, `DELETE FROM items WHERE id=?`, id); err != nil {
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
	if _, err := r.q.ExecContext(ctx, `DELETE FROM item_tags WHERE item_id IN (SELECT id FROM items WHERE group_id=?)`, group); err != nil {
		return nil, storeFailure(err, "delete items")
	}
	if _, err := r.q.ExecContext(ctx, `DELETE FROM items WHERE group_id=?`, group); err != nil {
		return nil, storeFailure(err, "delete items")
	}
	var files []string
	for i := range items {
		files = append(files, ItemFiles(&items[i])...)
	}
	return compact(files), nil
}

// ItemFiles lists the files and folders an item owns: its thumbnail, IP-Adapter
// image, mask reference image and mask folder, and ControlNet images (SPEC §7.4).
func ItemFiles(it *domain.Item) []string {
	files := []string{it.Thumbnail}
	if it.IPAdapter != nil {
		files = append(files, it.IPAdapter.ImageFile)
	}
	if it.MaskRegion != nil {
		files = append(files, it.MaskRegion.ReferencePath, it.MaskRegion.FolderPath)
	}
	for _, c := range it.ControlNets {
		files = append(files, c.ImagePath)
	}
	return compact(files)
}
