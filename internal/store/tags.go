package store

import (
	"context"
	"sort"
	"strings"

	"github.com/rm4n0s/bos/internal/domain"
)

// tagTable names the two tag tables. Group tags and item tags are separate
// vocabularies (SPEC §3.4).
type tagTable struct{ table, owner string }

var (
	groupTagTable = tagTable{table: "group_tags", owner: "group_id"}
	itemTagTable  = tagTable{table: "item_tags", owner: "item_id"}
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
func writeTags(ctx context.Context, q dbtx, t tagTable, id int64, tags []string) error {
	if _, err := q.ExecContext(ctx, `DELETE FROM `+t.table+` WHERE `+t.owner+`=?`, id); err != nil {
		return storeFailure(err, "write tags")
	}
	for _, tag := range cleanTags(tags) {
		if _, err := q.ExecContext(ctx, `INSERT INTO `+t.table+` (`+t.owner+`, tag) VALUES (?, ?)`, id, tag); err != nil {
			return storeFailure(err, "write tags")
		}
	}
	return nil
}

// loadTags returns the tags of the given owners, sorted.
func loadTags(ctx context.Context, q dbtx, t tagTable, ids []int64) (map[int64][]string, error) {
	out := make(map[int64][]string, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	rows, err := q.QueryContext(ctx,
		`SELECT `+t.owner+`, tag FROM `+t.table+` WHERE `+t.owner+` IN (`+placeholders(len(ids))+`) ORDER BY tag`, args...)
	if err != nil {
		return nil, storeFailure(err, "read tags")
	}
	for rows.Next() {
		var id int64
		var tag string
		if err := rows.Scan(&id, &tag); err != nil {
			return nil, closeRows(rows, storeFailure(err, "read tags"))
		}
		out[id] = append(out[id], tag)
	}
	if err := rows.Err(); err != nil {
		return nil, closeRows(rows, storeFailure(err, "read tags"))
	}
	if err := rows.Close(); err != nil {
		return nil, storeFailure(err, "read tags")
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
	rows, err := r.q.QueryContext(ctx,
		`SELECT tag, COUNT(*) FROM `+t.table+` WHERE tag LIKE ? ESCAPE '\' GROUP BY tag ORDER BY tag`, esc+"%")
	if err != nil {
		return nil, storeFailure(err, "list tags")
	}
	out := []domain.TagCount{}
	for rows.Next() {
		var c domain.TagCount
		if err := rows.Scan(&c.Tag, &c.Count); err != nil {
			return nil, closeRows(rows, storeFailure(err, "list tags"))
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, closeRows(rows, storeFailure(err, "list tags"))
	}
	if err := rows.Close(); err != nil {
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
	var affected int
	err := r.atomically(ctx, func(q dbtx) error {
		if err := q.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+t.table+` WHERE tag=?`, from).Scan(&affected); err != nil {
			return storeFailure(err, "rename tag")
		}
		if affected == 0 || from == to {
			return nil
		}
		// Owners that carry `to` already would collide: drop their `from` first.
		if _, err := q.ExecContext(ctx,
			`DELETE FROM `+t.table+` WHERE tag=? AND `+t.owner+` IN (SELECT `+t.owner+` FROM `+t.table+` WHERE tag=?)`,
			from, to); err != nil {
			return storeFailure(err, "rename tag")
		}
		if _, err := q.ExecContext(ctx, `UPDATE `+t.table+` SET tag=? WHERE tag=?`, to, from); err != nil {
			return storeFailure(err, "rename tag")
		}
		return nil
	})
	return affected, err
}
