package service

import (
	"reflect"
	"testing"

	"github.com/rm4n0s/bos/internal/domain"
)

func TestTagListIsSeparatePerScope(t *testing.T) {
	e := newEnv(t)
	g := e.group(t, "g", "character", "anime")
	e.group(t, "h", "character")
	e.item(t, g.ID, "a", "a", "combat", "calm")
	e.item(t, g.ID, "b", "b", "combat")

	groupTags, err := e.Tags.List(ctx(t), domain.TagScopeGroup, "")
	check(t, err)
	if want := []domain.TagCount{{Tag: "anime", Count: 1}, {Tag: "character", Count: 2}}; !reflect.DeepEqual(groupTags, want) {
		t.Errorf("group tags = %+v", groupTags)
	}
	itemTags, err := e.Tags.List(ctx(t), domain.TagScopeItem, "C")
	check(t, err)
	if want := []domain.TagCount{{Tag: "calm", Count: 1}, {Tag: "combat", Count: 2}}; !reflect.DeepEqual(itemTags, want) {
		t.Errorf("item tags with prefix = %+v", itemTags)
	}

	_, err = e.Tags.List(ctx(t), "command", "")
	wantRoute(t, err, "TagService.List", "InvalidRequest")
}

func TestTagRename(t *testing.T) {
	e := newEnv(t)
	g := e.group(t, "g", "old", "keep")
	e.group(t, "h", "old", "new")
	a := e.item(t, g.ID, "a", "a", "combat", "calm")
	e.item(t, g.ID, "b", "b", "calm")
	p := e.project(t, "p")

	// Group tags: a merge keeps one tag on a group that had both.
	res, err := e.Tags.Rename(ctx(t), domain.TagScopeGroup, "OLD", "New")
	check(t, err)
	if res.Renamed != 2 || len(res.AffectedCommands) != 0 {
		t.Errorf("group rename = %+v", res)
	}
	got, _ := e.Groups.Get(ctx(t), g.ID)
	if !reflect.DeepEqual(got.Tags, []string{"keep", "new"}) {
		t.Errorf("tags = %v", got.Tags)
	}

	// Item tags: the commands that mention the old tag are reported, not
	// rewritten, and only when the whole tag matches.
	mention := e.commandRaw(t, p.ID, "mentions", "gpu1 -$ anime: g(#combat)")
	e.commandRaw(t, p.ID, "longer tag", "gpu1 -$ anime: g(#combatant)")
	e.commandRaw(t, p.ID, "other", "gpu1 -$ anime: g(a)")
	upper := e.commandRaw(t, p.ID, "upper case and where", "gpu1 -$ anime: g where #COMBAT")
	res, err = e.Tags.Rename(ctx(t), domain.TagScopeItem, "combat", "fight")
	check(t, err)
	var ids []int64
	for _, c := range res.AffectedCommands {
		ids = append(ids, c.ID)
	}
	if res.Renamed != 1 || !reflect.DeepEqual(ids, []int64{mention.ID, upper.ID}) {
		t.Errorf("rename = %+v, affected %v, want the commands %d and %d", res, ids, mention.ID, upper.ID)
	}
	after, _ := e.Items.Get(ctx(t), a.ID)
	if !reflect.DeepEqual(after.Tags, []string{"calm", "fight"}) {
		t.Errorf("item tags = %v", after.Tags)
	}
	if c, _ := e.Commands.Get(ctx(t), mention.ID); c.CommandCode != "gpu1 -$ anime: g(#combat)" {
		t.Errorf("a command text was rewritten: %q", c.CommandCode)
	}
}

func TestTagRenameFailures(t *testing.T) {
	e := newEnv(t)
	e.group(t, "g", "one")
	tests := []struct {
		name     string
		scope    domain.TagScope
		from, to string
		tag      string
	}{
		{"no such tag", domain.TagScopeGroup, "ghost", "x", "TagNotFound"},
		{"the tag is of the other scope", domain.TagScopeItem, "one", "x", "TagNotFound"},
		{"bad old tag", domain.TagScopeGroup, "bad tag", "x", "TagInvalid"},
		{"bad new tag", domain.TagScopeGroup, "one", "Not OK", "TagInvalid"},
		{"reserved new tag", domain.TagScopeGroup, "one", "where", "TagInvalid"},
		{"bad scope", "command", "one", "x", "InvalidRequest"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := e.Tags.Rename(ctx(t), tt.scope, tt.from, tt.to)
			wantRoute(t, err, "TagService.Rename", tt.tag)
		})
	}
}
