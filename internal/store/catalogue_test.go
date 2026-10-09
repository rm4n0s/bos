package store

import (
	"reflect"
	"strconv"
	"testing"

	"github.com/rm4n0s/bos/internal/domain"
)

func TestServers(t *testing.T) {
	r := newDB(t).Repos()
	a, b := newServer("gpu1"), newServer("gpu2")
	b.IsLocal, b.MaxConcurrentJobs = true, 3
	check(t, r.Servers.Create(ctx(t), a))
	check(t, r.Servers.Create(ctx(t), b))
	if a.ID == 0 || b.ID <= a.ID || a.CreatedAt.IsZero() || !a.CreatedAt.Equal(a.UpdatedAt) {
		t.Fatalf("ids/timestamps not set: %+v %+v", a, b)
	}

	got, err := r.Servers.Get(ctx(t), b.ID)
	check(t, err)
	if !reflect.DeepEqual(got, b) {
		t.Errorf("Get = %+v\nwant  %+v", got, b)
	}
	byCode, err := r.Servers.GetByCode(ctx(t), "gpu1")
	check(t, err)
	if byCode.ID != a.ID {
		t.Errorf("GetByCode = %+v", byCode)
	}

	a.Name, a.Host, a.MaxConcurrentJobs = "Renamed", "http://other:1", 2
	check(t, r.Servers.Update(ctx(t), a))
	got, err = r.Servers.Get(ctx(t), a.ID)
	check(t, err)
	if got.Name != "Renamed" || got.MaxConcurrentJobs != 2 || !got.UpdatedAt.After(got.CreatedAt) || got.CreatedAt != a.CreatedAt {
		t.Errorf("after update: %+v", got)
	}

	all, err := r.Servers.All(ctx(t))
	check(t, err)
	if len(all) != 2 || all[0].CodeName != "gpu1" {
		t.Errorf("All = %+v", all)
	}
	check(t, r.Servers.Delete(ctx(t), a.ID))
	if all, _ = r.Servers.All(ctx(t)); len(all) != 1 {
		t.Errorf("%d servers after delete", len(all))
	}
}

func TestServerFailures(t *testing.T) {
	r := newDB(t).Repos()
	a := newServer("gpu1")
	check(t, r.Servers.Create(ctx(t), a))

	wantRoute(t, r.Servers.Create(ctx(t), newServer("gpu1")), "ServerRepo.Create", "ServerCodeNameTaken")
	b := newServer("gpu2")
	check(t, r.Servers.Create(ctx(t), b))
	b.CodeName = "gpu1"
	wantRoute(t, r.Servers.Update(ctx(t), b), "ServerRepo.Update", "ServerCodeNameTaken")

	missing := &domain.Server{ID: 999, Name: "x", Host: "h", CodeName: "x", MaxConcurrentJobs: 1}
	wantRoute(t, r.Servers.Update(ctx(t), missing), "ServerRepo.Update", "ServerNotFound")
	wantRoute(t, r.Servers.Delete(ctx(t), 999), "ServerRepo.Delete", "ServerNotFound")
	_, err := r.Servers.Get(ctx(t), 999)
	wantRoute(t, err, "ServerRepo.Get", "ServerNotFound")
	_, err = r.Servers.GetByCode(ctx(t), "nope")
	wantRoute(t, err, "ServerRepo.GetByCode", "ServerNotFound")

	bad := newServer("gpu3")
	bad.MaxConcurrentJobs = 0 // violates the CHECK constraint
	wantRoute(t, r.Servers.Create(ctx(t), bad), "ServerRepo.Create", "StoreFailure")
}

func TestPaginationOfLists(t *testing.T) {
	r := newDB(t).Repos()
	for i := range 45 {
		check(t, r.Servers.Create(ctx(t), newServer("s"+strconv.Itoa(i))))
	}
	page, err := r.Servers.List(ctx(t), domain.Page{})
	check(t, err)
	if page.Total != 45 || len(page.Items) != 20 || page.Page != 1 || page.PageSize != 20 || page.TotalPages != 3 {
		t.Errorf("first page = %+v", page)
	}
	page, err = r.Servers.List(ctx(t), domain.Page{Number: 3, Size: 20})
	check(t, err)
	if len(page.Items) != 5 || page.Items[0].CodeName != "s40" {
		t.Errorf("last page = %d items, first %s", len(page.Items), page.Items[0].CodeName)
	}
	page, err = r.Servers.List(ctx(t), domain.Page{Number: 9, Size: 20})
	check(t, err)
	if len(page.Items) != 0 || page.Items == nil || page.Total != 45 {
		t.Errorf("page past the end = %+v", page)
	}
	page, err = r.Servers.List(ctx(t), domain.Page{Number: 1, Size: 1000})
	check(t, err)
	if page.PageSize != 200 || len(page.Items) != 45 {
		t.Errorf("page size not capped: %d, %d items", page.PageSize, len(page.Items))
	}
}

func TestGenerators(t *testing.T) {
	r := newDB(t).Repos()
	g := &domain.Generator{
		Name: "Anime", CodeName: "anime", Workflow: []byte(`{"3":{"class_type":"KSampler","inputs":{}}}`),
		PositivePromptTitle: "Positive", NegativePromptTitle: "Negative", OutputType: domain.OutputImage,
		OutputAttributes: domain.OutputAttributes{
			"width": {Kind: domain.AttrInt, Int: 512}, "cfg": {Kind: domain.AttrDecimal, Float: 7},
			"seed": {Kind: domain.AttrInt, Int: 18446744073709551615}, "sampler_name": {Kind: domain.AttrText, Text: "euler"},
		},
		OutputNodeClassType: "SaveImage", OutputNodeTitle: "Save", HasRandomSeed: true,
	}
	check(t, r.Generators.Create(ctx(t), g))
	got, err := r.Generators.Get(ctx(t), g.ID)
	check(t, err)
	if !reflect.DeepEqual(got, g) {
		t.Errorf("Get = %+v\nwant  %+v", got, g)
	}
	byCode, err := r.Generators.GetByCode(ctx(t), "anime")
	check(t, err)
	if byCode.ID != g.ID {
		t.Error("GetByCode")
	}

	g.Name, g.HasRandomSeed, g.OutputAttributes = "Anime 2", false, nil
	check(t, r.Generators.Update(ctx(t), g))
	got, _ = r.Generators.Get(ctx(t), g.ID)
	if got.Name != "Anime 2" || got.HasRandomSeed || len(got.OutputAttributes) != 0 {
		t.Errorf("after update: %+v", got)
	}

	page, err := r.Generators.List(ctx(t), domain.Page{})
	check(t, err)
	if page.Total != 1 {
		t.Errorf("page = %+v", page)
	}
	other := &domain.Generator{Name: "x", CodeName: "anime", Workflow: []byte(`{}`), OutputType: domain.OutputImage}
	wantRoute(t, r.Generators.Create(ctx(t), other), "GeneratorRepo.Create", "GeneratorCodeNameTaken")
	other.CodeName = "other"
	check(t, r.Generators.Create(ctx(t), other))
	other.CodeName = "anime"
	wantRoute(t, r.Generators.Update(ctx(t), other), "GeneratorRepo.Update", "GeneratorCodeNameTaken")
	wantRoute(t, r.Generators.Update(ctx(t), &domain.Generator{ID: 99, CodeName: "z", OutputType: domain.OutputImage}), "GeneratorRepo.Update", "GeneratorNotFound")
	wantRoute(t, r.Generators.Delete(ctx(t), 99), "GeneratorRepo.Delete", "GeneratorNotFound")
	_, err = r.Generators.Get(ctx(t), 99)
	wantRoute(t, err, "GeneratorRepo.Get", "GeneratorNotFound")
	_, err = r.Generators.GetByCode(ctx(t), "nope")
	wantRoute(t, err, "GeneratorRepo.GetByCode", "GeneratorNotFound")
	check(t, r.Generators.Delete(ctx(t), g.ID))
}

func TestFixers(t *testing.T) {
	r := newDB(t).Repos()
	f := &domain.Fixer{
		Name: "Upscale", CodeName: "upscale", Workflow: []byte(`{}`), LoadImageTitle: "Load", SaveImageTitle: "Save",
		PositivePrompt: "sharp", NegativePrompt: "blurry", PositivePromptTitle: "P", NegativePromptTitle: "N",
	}
	check(t, r.Fixers.Create(ctx(t), f))
	got, err := r.Fixers.Get(ctx(t), f.ID)
	check(t, err)
	if !reflect.DeepEqual(got, f) {
		t.Errorf("Get = %+v\nwant  %+v", got, f)
	}
	byCode, err := r.Fixers.GetByCode(ctx(t), "upscale")
	check(t, err)
	if byCode.ID != f.ID {
		t.Error("GetByCode")
	}
	f.Name = "Upscale 2"
	check(t, r.Fixers.Update(ctx(t), f))
	if got, _ = r.Fixers.Get(ctx(t), f.ID); got.Name != "Upscale 2" {
		t.Errorf("after update: %+v", got)
	}
	page, err := r.Fixers.List(ctx(t), domain.Page{})
	check(t, err)
	if page.Total != 1 {
		t.Errorf("page = %+v", page)
	}

	dup := &domain.Fixer{Name: "x", CodeName: "upscale", Workflow: []byte(`{}`)}
	wantRoute(t, r.Fixers.Create(ctx(t), dup), "FixerRepo.Create", "FixerCodeNameTaken")
	dup.CodeName = "face"
	check(t, r.Fixers.Create(ctx(t), dup))
	dup.CodeName = "upscale"
	wantRoute(t, r.Fixers.Update(ctx(t), dup), "FixerRepo.Update", "FixerCodeNameTaken")
	wantRoute(t, r.Fixers.Update(ctx(t), &domain.Fixer{ID: 99, CodeName: "z"}), "FixerRepo.Update", "FixerNotFound")
	wantRoute(t, r.Fixers.Delete(ctx(t), 99), "FixerRepo.Delete", "FixerNotFound")
	_, err = r.Fixers.Get(ctx(t), 99)
	wantRoute(t, err, "FixerRepo.Get", "FixerNotFound")
	_, err = r.Fixers.GetByCode(ctx(t), "nope")
	wantRoute(t, err, "FixerRepo.GetByCode", "FixerNotFound")
	check(t, r.Fixers.Delete(ctx(t), f.ID))
}

func TestGroupsAndTags(t *testing.T) {
	r := newDB(t).Repos()
	g := mustGroup(t, r, "characters", "wip", "anime", "wip") // duplicates collapse, order is sorted
	if !reflect.DeepEqual(g.Tags, []string{"anime", "wip"}) {
		t.Errorf("tags on create = %v", g.Tags)
	}
	g.UseLora, g.UseMaskRegion, g.Thumbnail, g.Description = true, true, "thumbs/c.png", "people"
	check(t, r.Groups.Update(ctx(t), g))
	got, err := r.Groups.Get(ctx(t), g.ID)
	check(t, err)
	if !reflect.DeepEqual(got, g) {
		t.Errorf("Get = %+v\nwant  %+v", got, g)
	}

	// Replacing the tags.
	g.Tags = []string{"character"}
	check(t, r.Groups.Update(ctx(t), g))
	got, _ = r.Groups.GetByCode(ctx(t), "characters")
	if !reflect.DeepEqual(got.Tags, []string{"character"}) {
		t.Errorf("tags after update = %v", got.Tags)
	}
	untagged := mustGroup(t, r, "poses")
	if got, _ = r.Groups.Get(ctx(t), untagged.ID); got.Tags == nil || len(got.Tags) != 0 {
		t.Errorf("tags of an untagged group = %#v, want an empty list", got.Tags)
	}

	// Acceptance test 41: the tag filter.
	mustGroup(t, r, "scenes", "character", "wip")
	page, err := r.Groups.List(ctx(t), "character", domain.Page{})
	check(t, err)
	if page.Total != 2 || page.Items[0].CodeName != "characters" || page.Items[1].CodeName != "scenes" {
		t.Errorf("filtered = %+v", page)
	}
	if !reflect.DeepEqual(page.Items[1].Tags, []string{"character", "wip"}) {
		t.Errorf("tags in a list = %v", page.Items[1].Tags)
	}
	page, err = r.Groups.List(ctx(t), "", domain.Page{})
	check(t, err)
	if page.Total != 3 {
		t.Errorf("unfiltered total = %d", page.Total)
	}
	page, err = r.Groups.List(ctx(t), "nosuch", domain.Page{})
	check(t, err)
	if page.Total != 0 || page.Items == nil {
		t.Errorf("unknown tag = %+v", page)
	}

	// Failures.
	dup := &domain.Group{Name: "x", CodeName: "characters"}
	wantRoute(t, r.Groups.Create(ctx(t), dup), "GroupRepo.Create", "GroupCodeNameTaken")
	dup.ID, dup.CodeName = untagged.ID, "characters"
	wantRoute(t, r.Groups.Update(ctx(t), dup), "GroupRepo.Update", "GroupCodeNameTaken")
	wantRoute(t, r.Groups.Update(ctx(t), &domain.Group{ID: 99, CodeName: "z"}), "GroupRepo.Update", "GroupNotFound")
	_, err = r.Groups.Get(ctx(t), 99)
	wantRoute(t, err, "GroupRepo.Get", "GroupNotFound")
	_, err = r.Groups.GetByCode(ctx(t), "nope")
	wantRoute(t, err, "GroupRepo.GetByCode", "GroupNotFound")
	_, err = r.Groups.Delete(ctx(t), 99)
	wantRoute(t, err, "GroupRepo.Delete", "GroupNotFound")
}

func TestItems(t *testing.T) {
	r := newDB(t).Repos()
	g := mustGroup(t, r, "characters")
	it := &domain.Item{
		GroupID: g.ID, CodeName: "alice", Name: "Alice", PositivePrompt: "alice, red hair", NegativePrompt: "ugly",
		LoRAs:                []domain.LoRA{{Name: "a.safetensors", StrengthModel: 0.8, StrengthClip: 0.6}},
		IPAdapter:            &domain.IPAdapter{ImageFile: "ip/a.png", Weight: 0.7, WeightType: "linear", StartAt: 0, EndAt: 1, ClipVisionModel: "cv", ModelName: "ipa"},
		ControlNets:          []domain.ControlNet{{Type: "openpose", ImagePath: "cn/a.png", IsReference: true, Strength: 1}},
		MaskRegion:           &domain.MaskRegion{ReferencePath: "regions/a.png", FolderPath: "regions/a", MaskFiles: map[string]string{"red": "regions/a/red.png"}},
		CoordinatedRegions:   []domain.CoordinatedRegion{{Keyword: "left", Width: 10, Height: 20, X: 1, Y: 2}},
		OutputType:           domain.OutputImage,
		OutputTypeAttributes: domain.OutputAttributes{"steps": {Kind: domain.AttrInt, Int: 40}},
		Tags:                 []string{"b", "a"},
		Thumbnail:            "thumbs/a.png",
	}
	check(t, r.Items.Create(ctx(t), it))
	got, err := r.Items.Get(ctx(t), it.ID)
	check(t, err)
	if !reflect.DeepEqual(got, it) {
		t.Errorf("Get = %+v\nwant  %+v", got, it)
	}

	// An item with nothing optional reads back with empty lists and no nulls.
	plain := mustItem(t, r, g.ID, "bob")
	got, err = r.Items.Get(ctx(t), plain.ID)
	check(t, err)
	if got.IPAdapter != nil || got.MaskRegion != nil || len(got.LoRAs) != 0 || len(got.Tags) != 0 || got.Tags == nil {
		t.Errorf("plain item = %+v", got)
	}

	it.Name, it.Tags, it.IPAdapter, it.MaskRegion = "Alice 2", []string{"c"}, nil, nil
	check(t, r.Items.Update(ctx(t), it))
	got, _ = r.Items.Get(ctx(t), it.ID)
	if got.Name != "Alice 2" || !reflect.DeepEqual(got.Tags, []string{"c"}) || got.IPAdapter != nil || got.MaskRegion != nil {
		t.Errorf("after update: %+v", got)
	}

	// The same code name may exist in another group (SPEC §3.6), not twice in one.
	other := mustGroup(t, r, "poses")
	mustItem(t, r, other.ID, "alice")
	dup := &domain.Item{GroupID: g.ID, CodeName: "alice"}
	wantRoute(t, r.Items.Create(ctx(t), dup), "ItemRepo.Create", "ItemCodeNameTaken")
	dup.GroupID = 999
	wantRoute(t, r.Items.Create(ctx(t), dup), "ItemRepo.failure", "GroupNotFound")
	dup.ID, dup.GroupID, dup.CodeName = plain.ID, g.ID, "alice"
	wantRoute(t, r.Items.Update(ctx(t), dup), "ItemRepo.Update", "ItemCodeNameTaken")
	wantRoute(t, r.Items.Update(ctx(t), &domain.Item{ID: 999, GroupID: g.ID, CodeName: "q"}), "ItemRepo.Update.func1", "ItemNotFound")
	_, err = r.Items.Get(ctx(t), 999)
	wantRoute(t, err, "ItemRepo.Get", "ItemNotFound")
	_, err = r.Items.Delete(ctx(t), 999)
	wantRoute(t, err, "ItemRepo.Delete", "ItemNotFound")
}

// Items come back in creation order, which is the item order of SPEC §6.4.
func TestItemListing(t *testing.T) {
	r := newDB(t).Repos()
	g := mustGroup(t, r, "g")
	other := mustGroup(t, r, "h")
	for _, c := range []string{"zed", "alpha", "mid"} {
		mustItem(t, r, g.ID, c, "t")
	}
	mustItem(t, r, other.ID, "elsewhere")
	all, err := r.Items.AllByGroup(ctx(t), g.ID)
	check(t, err)
	var codes []string
	for _, it := range all {
		codes = append(codes, it.CodeName)
		if len(it.Tags) != 1 {
			t.Errorf("tags of %s = %v", it.CodeName, it.Tags)
		}
	}
	if !reflect.DeepEqual(codes, []string{"zed", "alpha", "mid"}) {
		t.Errorf("order = %v", codes)
	}
	page, err := r.Items.ListByGroup(ctx(t), g.ID, domain.Page{Number: 2, Size: 2})
	check(t, err)
	if page.Total != 3 || len(page.Items) != 1 || page.Items[0].CodeName != "mid" || page.TotalPages != 2 {
		t.Errorf("page = %+v", page)
	}
}

func TestTagListAndRename(t *testing.T) {
	r := newDB(t).Repos()
	g1 := mustGroup(t, r, "g1", "character", "wip")
	g2 := mustGroup(t, r, "g2", "character", "style")
	mustItem(t, r, g1.ID, "a", "combat", "cool", "100%_sure")
	mustItem(t, r, g1.ID, "b", "combat", "calm")
	mustItem(t, r, g2.ID, "c", "cool")

	// Acceptance test 39: item tags with a prefix; group tags are not included.
	got, err := r.Tags.List(ctx(t), domain.TagScopeItem, "co")
	check(t, err)
	if want := []domain.TagCount{{Tag: "combat", Count: 2}, {Tag: "cool", Count: 2}}; !reflect.DeepEqual(got, want) {
		t.Errorf("item tags with prefix co = %+v, want %+v", got, want)
	}
	got, err = r.Tags.List(ctx(t), domain.TagScopeGroup, "")
	check(t, err)
	if want := []domain.TagCount{{Tag: "character", Count: 2}, {Tag: "style", Count: 1}, {Tag: "wip", Count: 1}}; !reflect.DeepEqual(got, want) {
		t.Errorf("group tags = %+v", got)
	}
	// % and _ in a prefix are not wildcards.
	if got, _ = r.Tags.List(ctx(t), domain.TagScopeItem, "100%_"); len(got) != 1 {
		t.Errorf("prefix with wildcards = %+v", got)
	}
	if got, _ = r.Tags.List(ctx(t), domain.TagScopeItem, "_"); len(got) != 0 {
		t.Errorf("underscore matched as a wildcard: %+v", got)
	}
	if got, err = r.Tags.List(ctx(t), domain.TagScopeItem, "zzz"); err != nil || got == nil || len(got) != 0 {
		t.Errorf("no match = %#v, %v", got, err)
	}

	// Acceptance test 40: renaming to an existing tag merges the two.
	n, err := r.Tags.Rename(ctx(t), domain.TagScopeItem, "combat", "cool")
	check(t, err)
	if n != 2 {
		t.Errorf("renamed on %d items, want 2", n)
	}
	items, _ := r.Items.AllByGroup(ctx(t), g1.ID)
	if !reflect.DeepEqual(items[0].Tags, []string{"100%_sure", "cool"}) || !reflect.DeepEqual(items[1].Tags, []string{"calm", "cool"}) {
		t.Errorf("tags after merge: %v / %v", items[0].Tags, items[1].Tags)
	}
	got, _ = r.Tags.List(ctx(t), domain.TagScopeItem, "co")
	if want := []domain.TagCount{{Tag: "cool", Count: 3}}; !reflect.DeepEqual(got, want) {
		t.Errorf("after merge = %+v", got)
	}
	// A plain rename, a rename of an unknown tag, and a rename to itself.
	if n, err = r.Tags.Rename(ctx(t), domain.TagScopeGroup, "wip", "draft"); err != nil || n != 1 {
		t.Errorf("rename = %d, %v", n, err)
	}
	if n, err = r.Tags.Rename(ctx(t), domain.TagScopeGroup, "nosuch", "x"); err != nil || n != 0 {
		t.Errorf("rename of an unknown tag = %d, %v", n, err)
	}
	if n, err = r.Tags.Rename(ctx(t), domain.TagScopeGroup, "draft", "draft"); err != nil || n != 1 {
		t.Errorf("rename to itself = %d, %v", n, err)
	}
	// The two vocabularies are separate: renaming a group tag leaves item tags.
	if got, _ = r.Tags.List(ctx(t), domain.TagScopeItem, "draft"); len(got) != 0 {
		t.Errorf("group rename leaked into items: %+v", got)
	}

	_, err = r.Tags.List(ctx(t), "bogus", "")
	wantRoute(t, err, "TagRepo.List", "StoreFailure")
	_, err = r.Tags.Rename(ctx(t), "bogus", "a", "b")
	wantRoute(t, err, "TagRepo.Rename", "StoreFailure")
}

// Deleting a group deletes its items and tags (SPEC §7.4) and reports the files.
func TestDeleteGroupCascades(t *testing.T) {
	db := newDB(t)
	r := db.Repos()
	g := mustGroup(t, r, "g", "t")
	g.Thumbnail = "thumbs/g.png"
	check(t, r.Groups.Update(ctx(t), g))
	mustItem(t, r, g.ID, "plain", "x")
	rich := &domain.Item{
		GroupID: g.ID, CodeName: "rich", Thumbnail: "thumbs/rich.png",
		IPAdapter:   &domain.IPAdapter{ImageFile: "ip/rich.png"},
		ControlNets: []domain.ControlNet{{Type: "canny", ImagePath: "cn/1.png"}, {Type: "depth", ImagePath: "cn/1.png"}},
		MaskRegion:  &domain.MaskRegion{ReferencePath: "regions/rich.png", FolderPath: "regions/rich"},
	}
	check(t, r.Items.Create(ctx(t), rich))
	keep := mustGroup(t, r, "keep", "t")
	keepItem := mustItem(t, r, keep.ID, "k", "x")

	files, err := r.Groups.Delete(ctx(t), g.ID)
	check(t, err)
	want := []string{"thumbs/g.png", "thumbs/rich.png", "ip/rich.png", "regions/rich.png", "regions/rich", "cn/1.png"}
	if !reflect.DeepEqual(files, want) {
		t.Errorf("files = %v\nwant   %v", files, want)
	}
	for table, wantRows := range map[string]int{"groups": 1, "items": 1, "group_tags": 1, "item_tags": 1} {
		var n int
		check(t, db.sql.QueryRowContext(ctx(t), `SELECT COUNT(*) FROM `+table).Scan(&n))
		if n != wantRows {
			t.Errorf("%s has %d rows after the delete, want %d", table, n, wantRows)
		}
	}
	if _, err := r.Items.Get(ctx(t), keepItem.ID); err != nil {
		t.Errorf("an item of another group was deleted: %v", err)
	}

	// Deleting one item reports its files.
	rich2 := &domain.Item{GroupID: keep.ID, CodeName: "rich", IPAdapter: &domain.IPAdapter{ImageFile: "ip/r2.png"}}
	check(t, r.Items.Create(ctx(t), rich2))
	files, err = r.Items.Delete(ctx(t), rich2.ID)
	check(t, err)
	if !reflect.DeepEqual(files, []string{"ip/r2.png"}) {
		t.Errorf("item files = %v", files)
	}
}

// The foreign keys are the safety net behind the explicit cascades.
func TestForeignKeysProtectParents(t *testing.T) {
	db := newDB(t)
	r := db.Repos()
	g := mustGroup(t, r, "g")
	mustItem(t, r, g.ID, "a", "t")
	if _, err := db.sql.ExecContext(ctx(t), `DELETE FROM groups WHERE id=?`, g.ID); err == nil {
		t.Error("a group with items was deleted by plain SQL")
	} else if !isForeignKey(err) {
		t.Errorf("unexpected error %v", err)
	}
	p := mustProject(t, r, "p")
	mustCommand(t, r, p.ID, "c", 0)
	if _, err := db.sql.ExecContext(ctx(t), `DELETE FROM projects WHERE id=?`, p.ID); err == nil {
		t.Error("a project with commands was deleted by plain SQL")
	}
}
