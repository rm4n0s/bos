package store

import (
	"context"
	"database/sql"

	"github.com/rm4n0s/bos/internal/domain"
	"github.com/rm4n0s/errors"
)

var errUnknownScope = errors.New("StoreFailure", "unknown tag scope")

// GroupRepo stores groups with their group tags (SPEC §3.5).
type GroupRepo struct {
	base
	items *ItemRepo
}

const groupColumns = `id, name, description, code_name, thumbnail, use_lora, use_controlnet, use_ip_adapter,
	use_mask_region, use_coordinates_region, use_type_attributes, created_at, updated_at`

func scanGroup(s scanner) (*domain.Group, error) {
	var v domain.Group
	var lora, cn, ip, mask, coord, attrs int
	var created, updated string
	if err := s.Scan(&v.ID, &v.Name, &v.Description, &v.CodeName, &v.Thumbnail, &lora, &cn, &ip, &mask, &coord, &attrs,
		&created, &updated); err != nil {
		return nil, err
	}
	v.UseLora, v.UseControlnet, v.UseIPAdapter = lora != 0, cn != 0, ip != 0
	v.UseMaskRegion, v.UseCoordinatesRegion, v.UseTypeAttributes = mask != 0, coord != 0, attrs != 0
	var err error
	if v.CreatedAt, err = parseTime(created); err != nil {
		return nil, err
	}
	if v.UpdatedAt, err = parseTime(updated); err != nil {
		return nil, err
	}
	return &v, nil
}

func (r *GroupRepo) taken(err error, code string) error {
	if isUnique(err) {
		return errors.New("GroupCodeNameTaken", "a group with this code name already exists", "code_name", code)
	}
	return storeFailure(err, "write group")
}

// withTags fills in the tags of the groups.
func (r *GroupRepo) withTags(ctx context.Context, groups []domain.Group) error {
	ids := make([]int64, len(groups))
	for i, g := range groups {
		ids[i] = g.ID
	}
	tags, err := loadTags(ctx, r.q, groupTagTable, ids)
	if err != nil {
		return err
	}
	for i := range groups {
		groups[i].Tags = tags[groups[i].ID]
		if groups[i].Tags == nil {
			groups[i].Tags = []string{}
		}
	}
	return nil
}

// Create stores a new group with its tags and sets its ID and timestamps.
func (r *GroupRepo) Create(ctx context.Context, v *domain.Group) error {
	now := r.now()
	return r.atomically(ctx, func(q dbtx) error {
		res, err := q.ExecContext(ctx,
			`INSERT INTO groups (name, description, code_name, thumbnail, use_lora, use_controlnet, use_ip_adapter,
			   use_mask_region, use_coordinates_region, use_type_attributes, created_at, updated_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			v.Name, v.Description, v.CodeName, v.Thumbnail, boolInt(v.UseLora), boolInt(v.UseControlnet),
			boolInt(v.UseIPAdapter), boolInt(v.UseMaskRegion), boolInt(v.UseCoordinatesRegion),
			boolInt(v.UseTypeAttributes), formatTime(now), formatTime(now))
		if err != nil {
			return r.taken(err, v.CodeName)
		}
		id, err := res.LastInsertId()
		if err != nil {
			return storeFailure(err, "group id")
		}
		if err := writeTags(ctx, q, groupTagTable, id, v.Tags); err != nil {
			return err
		}
		v.ID, v.CreatedAt, v.UpdatedAt = id, now, now
		v.Tags = cleanTags(v.Tags)
		return nil
	})
}

// Update replaces the editable fields of a group, including its tags.
func (r *GroupRepo) Update(ctx context.Context, v *domain.Group) error {
	now := r.now()
	return r.atomically(ctx, func(q dbtx) error {
		res, err := q.ExecContext(ctx,
			`UPDATE groups SET name=?, description=?, code_name=?, thumbnail=?, use_lora=?, use_controlnet=?,
			   use_ip_adapter=?, use_mask_region=?, use_coordinates_region=?, use_type_attributes=?, updated_at=?
			 WHERE id=?`,
			v.Name, v.Description, v.CodeName, v.Thumbnail, boolInt(v.UseLora), boolInt(v.UseControlnet),
			boolInt(v.UseIPAdapter), boolInt(v.UseMaskRegion), boolInt(v.UseCoordinatesRegion),
			boolInt(v.UseTypeAttributes), formatTime(now), v.ID)
		if err != nil {
			return r.taken(err, v.CodeName)
		}
		if n, err := res.RowsAffected(); err != nil {
			return storeFailure(err, "group rows")
		} else if n == 0 {
			return notFound("GroupNotFound", "group", v.ID)
		}
		if err := writeTags(ctx, q, groupTagTable, v.ID, v.Tags); err != nil {
			return err
		}
		v.UpdatedAt = now
		v.Tags = cleanTags(v.Tags)
		return nil
	})
}

// Get returns one group with its tags.
func (r *GroupRepo) Get(ctx context.Context, id domain.ID) (*domain.Group, error) {
	v, err := scanGroup(r.q.QueryRowContext(ctx, `SELECT `+groupColumns+` FROM groups WHERE id=?`, id))
	v, err = one(v, err, "GroupNotFound", "group", id, "read group")
	if err != nil {
		return nil, err
	}
	return r.single(ctx, v)
}

// GetByCode returns the group with a code name.
func (r *GroupRepo) GetByCode(ctx context.Context, code string) (*domain.Group, error) {
	v, err := scanGroup(r.q.QueryRowContext(ctx, `SELECT `+groupColumns+` FROM groups WHERE code_name=?`, code))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errors.New("GroupNotFound", "group not found", "code_name", code)
	}
	if err != nil {
		return nil, asStoreError(err, "read group")
	}
	return r.single(ctx, v)
}

func (r *GroupRepo) single(ctx context.Context, v *domain.Group) (*domain.Group, error) {
	list := []domain.Group{*v}
	if err := r.withTags(ctx, list); err != nil {
		return nil, err
	}
	return &list[0], nil
}

// List returns one page of groups in creation order. A non-empty tag keeps only
// the groups that carry that group tag.
func (r *GroupRepo) List(ctx context.Context, tag string, p domain.Page) (domain.Paged[domain.Group], error) {
	from, args := `FROM groups`, []any(nil)
	if tag != "" {
		from += ` WHERE id IN (SELECT group_id FROM group_tags WHERE tag=?)`
		args = []any{tag}
	}
	page, err := queryPage(ctx, r.q, from, args, `ORDER BY id`, groupColumns, scanGroup, p, "list groups")
	if err != nil {
		return page, err
	}
	return page, r.withTags(ctx, page.Items)
}

// Delete removes a group together with its items (SPEC §7.4). It returns the
// files and folders that belonged to them, for the caller to remove once the
// transaction has committed.
func (r *GroupRepo) Delete(ctx context.Context, id domain.ID) ([]string, error) {
	var files []string
	err := r.atomically(ctx, func(q dbtx) error {
		var thumb string
		if err := q.QueryRowContext(ctx, `SELECT thumbnail FROM groups WHERE id=?`, id).Scan(&thumb); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return notFound("GroupNotFound", "group", id)
			}
			return storeFailure(err, "delete group")
		}
		itemFiles, err := (&ItemRepo{base: base{q: q, clock: r.clock}}).deleteByGroup(ctx, id)
		if err != nil {
			return err
		}
		if _, err := q.ExecContext(ctx, `DELETE FROM group_tags WHERE group_id=?`, id); err != nil {
			return storeFailure(err, "delete group")
		}
		if _, err := q.ExecContext(ctx, `DELETE FROM groups WHERE id=?`, id); err != nil {
			return storeFailure(err, "delete group")
		}
		files = compact(append([]string{thumb}, itemFiles...))
		return nil
	})
	if err != nil {
		return nil, err
	}
	return files, nil
}

// compact drops empty and repeated paths, keeping the first of each.
func compact(paths []string) []string {
	seen := make(map[string]bool, len(paths))
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		if p != "" && !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	return out
}
