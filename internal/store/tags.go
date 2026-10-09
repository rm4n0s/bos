package store

import (
	"context"
	"sort"
	"strings"

	"github.com/rm4n0s/bos/internal/domain"
	"github.com/rm4n0s/errors"
	"gorm.io/gorm"
)

var errUnknownScope = errors.New("StoreFailure", "unknown tag scope")

// tagTable describes one of the two tag tables. Group tags and item tags are
// separate vocabularies (SPEC §3.4).
type tagTable struct {
	owner string                            // the column that names the group or item
	model func() any                        // a new pointer to a record of the table
	rows  func(id int64, tags []string) any // the records of one owner's tags
}

var (
	groupTagTable = tagTable{owner: "group_id", model: func() any { return &groupTagRecord{} }, rows: func(id int64, tags []string) any {
		out := make([]groupTagRecord, len(tags))
		for i, t := range tags {
			out[i] = groupTagRecord{GroupID: id, Tag: t}
		}
		return &out
	}}
	itemTagTable = tagTable{owner: "item_id", model: func() any { return &itemTagRecord{} }, rows: func(id int64, tags []string) any {
		out := make([]itemTagRecord, len(tags))
		for i, t := range tags {
			out[i] = itemTagRecord{ItemID: id, Tag: t}
		}
		return &out
	}}
)

// cleanTags returns tags sorted and without duplicates. Callers have already
// validated and lower-cased them.
func cleanTags(tags []string) []string {
	seen := make(map[string]bool, len(tags))
	out := make([]string, 0, len(tags))
	for _, t := range tags {
		if !seen[t] {
			seen[t] = true
			out = append(out, t)
		}
	}
	sort.Strings(out)
	return out
}

// writeTags replaces the tags of one owner.
func writeTags(db *gorm.DB, t tagTable, id int64, tags []string) error {
	if err := db.Where(t.owner+" = ?", id).Delete(t.model()).Error; err != nil {
		return storeFailure(err, "write tags")
	}
	clean := cleanTags(tags)
	if len(clean) == 0 {
		return nil
	}
	if err := db.Create(t.rows(id, clean)).Error; err != nil {
		return storeFailure(err, "write tags")
	}
	return nil
}

// loadTags returns the tags of the given owners, sorted.
func loadTags(db *gorm.DB, t tagTable, ids []int64) (map[int64][]string, error) {
	out := make(map[int64][]string, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	var rows []struct {
		Owner int64
		Tag   string
	}
	err := db.Model(t.model()).Select(t.owner+" AS owner, tag").Where(t.owner+" IN ?", ids).Order("tag").Scan(&rows).Error
	if err != nil {
		return nil, storeFailure(err, "read tags")
	}
	for _, row := range rows {
		out[row.Owner] = append(out[row.Owner], row.Tag)
	}
	return out, nil
}

// TagRepo lists and renames tags (SPEC §3.4).
type TagRepo struct{ base }

func tableFor(scope domain.TagScope) (tagTable, bool) {
	switch scope {
	case domain.TagScopeGroup:
		return groupTagTable, true
	case domain.TagScopeItem:
		return itemTagTable, true
	}
	return tagTable{}, false
}

// List returns the tags in use in a scope with the number of groups or items
// that carry each, ordered by tag. A prefix limits the list, for autocomplete.
func (r *TagRepo) List(ctx context.Context, scope domain.TagScope, prefix string) ([]domain.TagCount, error) {
	t, ok := tableFor(scope)
	if !ok {
		return nil, storeFailure(errUnknownScope, "list tags")
	}
	// LIKE needs the escape character for % and _ inside the prefix.
	esc := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(prefix)
	out := []domain.TagCount{}
	err := r.conn(ctx).Model(t.model()).Select("tag, COUNT(*) AS count").
		Where("tag LIKE ? ESCAPE ?", esc+"%", `\`).Group("tag").Order("tag").Scan(&out).Error
	if err != nil {
		return nil, storeFailure(err, "list tags")
	}
	return out, nil
}

// Rename changes a tag on every group or item that carries it. When `to`
// already exists the two merge, and an owner that had both keeps one. It
// returns how many owners carried `from`.
func (r *TagRepo) Rename(ctx context.Context, scope domain.TagScope, from, to string) (int, error) {
	t, ok := tableFor(scope)
	if !ok {
		return 0, storeFailure(errUnknownScope, "rename tag")
	}
	var affectedOwners int
	err := r.atomically(ctx, func(tx *Repos) error {
		db := tx.conn(ctx)
		var n int64
		if err := db.Model(t.model()).Where("tag = ?", from).Count(&n).Error; err != nil {
			return storeFailure(err, "rename tag")
		}
		affectedOwners = int(n)
		if n == 0 || from == to {
			return nil
		}
		// Owners that carry `to` already would collide: drop their `from` first.
		carriers := db.Model(t.model()).Select(t.owner).Where("tag = ?", to)
		if err := db.Where("tag = ? AND "+t.owner+" IN (?)", from, carriers).Delete(t.model()).Error; err != nil {
			return storeFailure(err, "rename tag")
		}
		if err := db.Model(t.model()).Where("tag = ?", from).Update("tag", to).Error; err != nil {
			return storeFailure(err, "rename tag")
		}
		return nil
	})
	return affectedOwners, err
}
