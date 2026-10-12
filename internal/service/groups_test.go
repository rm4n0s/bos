package service

import (
	"reflect"
	"strings"
	"testing"

	"github.com/rm4n0s/bos/internal/domain"
)

func TestGroups(t *testing.T) {
	e := newEnv(t)
	g, err := e.Groups.Create(ctx(t), domain.Group{Name: "Characters", CodeName: "characters", Description: "people", Tags: []string{"Anime", "wip", "ANIME"}, UseControlnet: true})
	check(t, err)
	if !reflect.DeepEqual(g.Tags, []string{"anime", "wip"}) {
		t.Errorf("tags = %v, want them lower-cased, sorted and without repeats", g.Tags)
	}
	got, err := e.Groups.Get(ctx(t), g.ID)
	check(t, err)
	if !reflect.DeepEqual(got, g) {
		t.Errorf("Get = %+v\nwant  %+v", got, g)
	}

	e.group(t, "poses", "style")
	e.group(t, "emotions")
	page, err := e.Groups.List(ctx(t), "", domain.Page{})
	check(t, err)
	if page.Total != 3 {
		t.Errorf("total = %d", page.Total)
	}
	page, err = e.Groups.List(ctx(t), "ANIME", domain.Page{})
	check(t, err)
	if page.Total != 1 || page.Items[0].CodeName != "characters" {
		t.Errorf("filter by tag = %+v", page)
	}

	upd, err := e.Groups.Update(ctx(t), g.ID, domain.Group{Name: "People", CodeName: "characters", Tags: []string{"done"}})
	check(t, err)
	if upd.Name != "People" || !reflect.DeepEqual(upd.Tags, []string{"done"}) || upd.UseControlnet {
		t.Errorf("updated = %+v", upd)
	}
	check(t, e.Groups.Delete(ctx(t), g.ID))
	_, err = e.Groups.Get(ctx(t), g.ID)
	wantRoute(t, err, "GroupService.Get", "GroupNotFound")
}

func TestGroupFailures(t *testing.T) {
	e := newEnv(t)
	g := e.group(t, "characters")
	good := domain.Group{Name: "n", CodeName: "other"}
	tests := []struct {
		name   string
		change func(*domain.Group)
		tag    string
	}{
		{"blank name", func(v *domain.Group) { v.Name = "" }, "NameInvalid"},
		{"long name", func(v *domain.Group) { v.Name = strings.Repeat("n", 301) }, "NameInvalid"},
		{"bad code", func(v *domain.Group) { v.CodeName = "a-b" }, "CodeNameInvalid"},
		{"reserved code", func(v *domain.Group) { v.CodeName = "sample" }, "CodeNameReserved"},
		{"tag with a space", func(v *domain.Group) { v.Tags = []string{"two words"} }, "TagInvalid"},
		{"empty tag", func(v *domain.Group) { v.Tags = []string{""} }, "TagInvalid"},
		{"reserved tag", func(v *domain.Group) { v.Tags = []string{"and"} }, "TagInvalid"},
		{"long tag", func(v *domain.Group) { v.Tags = []string{strings.Repeat("t", 51)} }, "TagInvalid"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := good
			tt.change(&in)
			_, err := e.Groups.Create(ctx(t), in)
			wantRoute(t, err, "GroupService.Create", tt.tag)
			_, err = e.Groups.Update(ctx(t), g.ID, in)
			wantRoute(t, err, "GroupService.Update", tt.tag)
			_, err = e.Groups.ImportText(ctx(t), in, "a")
			wantRoute(t, err, "GroupService.ImportText", tt.tag)
		})
	}
	if page, _ := e.Groups.List(ctx(t), "", domain.Page{}); page.Total != 1 {
		t.Errorf("%d groups, a rejected create stored something", page.Total)
	}

	_, err := e.Groups.Create(ctx(t), domain.Group{Name: "n", CodeName: "characters"})
	wantRoute(t, err, "GroupService.Create", "GroupCodeNameTaken")
	_, err = e.Groups.Update(ctx(t), 999, good)
	wantRoute(t, err, "GroupService.Update", "GroupNotFound")
	wantRoute(t, e.Groups.Delete(ctx(t), 999), "GroupService.Delete", "GroupNotFound")
}

func TestGroupThumbnailIsRemovedWhenReplaced(t *testing.T) {
	e := newEnv(t)
	old := e.write(t, "thumbs/old.png", "x")
	newer := e.write(t, "thumbs/new.png", "x")
	g, err := e.Groups.Create(ctx(t), domain.Group{Name: "g", CodeName: "g", Thumbnail: old})
	check(t, err)

	_, err = e.Groups.Update(ctx(t), g.ID, domain.Group{Name: "g", CodeName: "g", Thumbnail: old})
	check(t, err)
	if !exists(old) {
		t.Fatal("the thumbnail was removed although the edit kept it")
	}
	_, err = e.Groups.Update(ctx(t), g.ID, domain.Group{Name: "g", CodeName: "g", Thumbnail: newer})
	check(t, err)
	if exists(old) || !exists(newer) {
		t.Errorf("old exists = %v, new exists = %v", exists(old), exists(newer))
	}
}

func TestDeleteGroupRemovesItsFiles(t *testing.T) {
	e := newEnv(t)
	thumb := e.write(t, "thumbs/g.png", "x")
	itemThumb := e.write(t, "thumbs/i.png", "x")
	cn := e.write(t, "controlnet/c.png", "x")
	folder := e.write(t, "regions/f/mask_red.png", "x")
	keep := e.write(t, "thumbs/keep.png", "x")
	g, err := e.Groups.Create(ctx(t), domain.Group{Name: "g", CodeName: "g", Thumbnail: thumb})
	check(t, err)
	_, err = e.Items.Create(ctx(t), g.ID, domain.Item{
		CodeName: "i", Thumbnail: itemThumb, ControlNets: []domain.ControlNet{{Type: "canny", ImagePath: cn}},
		MaskRegion: &domain.MaskRegion{FolderPath: folder[:strings.LastIndex(folder, "/")], MaskFiles: map[string]string{"red": folder}},
	})
	check(t, err)
	other := e.group(t, "other")
	_, err = e.Items.Create(ctx(t), other.ID, domain.Item{CodeName: "k", Thumbnail: keep})
	check(t, err)

	check(t, e.Groups.Delete(ctx(t), g.ID))
	for _, p := range []string{thumb, itemThumb, cn, folder} {
		if exists(p) {
			t.Errorf("%s was not removed", p)
		}
	}
	if !exists(keep) {
		t.Error("a file of another group was removed")
	}
}

// A file that cannot be removed does not undo a deletion that happened.
func TestFileRemovalFailureIsLoggedNotReturned(t *testing.T) {
	e := newEnv(t)
	outside := t.TempDir() + "/elsewhere.png"
	g, err := e.Groups.Create(ctx(t), domain.Group{Name: "g", CodeName: "g", Thumbnail: outside})
	check(t, err)
	check(t, e.Groups.Delete(ctx(t), g.ID))
	if !strings.Contains(e.logs.String(), "could not remove a file") || !strings.Contains(e.logs.String(), "FileOutsideStorage") {
		t.Errorf("log = %q", e.logs.String())
	}
}

func TestImportText(t *testing.T) {
	e := newEnv(t)
	text := "red hair, <tmpl:eyes> and <tmpl:mouth>\r\n\r\nblue hair\n   \n<tmpl:hat> hat\n"
	g, err := e.Groups.ImportText(ctx(t), domain.Group{Name: "Looks", CodeName: "looks", Tags: []string{"Imported"}}, text)
	check(t, err)
	if !reflect.DeepEqual(g.Tags, []string{"imported"}) {
		t.Errorf("tags = %v", g.Tags)
	}
	items, err := e.repos.Items().AllByGroup(ctx(t), g.ID)
	check(t, err)
	type row struct{ code, name, positive, negative string }
	var got []row
	for _, it := range items {
		got = append(got, row{it.CodeName, it.Name, it.PositivePrompt, it.NegativePrompt})
	}
	want := []row{
		{"0", "0", "red hair, <tmpl:eyes> and <tmpl:mouth>", "<tmpl:eyes>,<tmpl:mouth>"},
		{"2", "2", "blue hair", ""},
		{"4", "4", "<tmpl:hat> hat", "<tmpl:hat>"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("items = %+v\nwant   %+v", got, want)
	}
}

func TestImportTextIsAtomic(t *testing.T) {
	e := newEnv(t)
	e.group(t, "looks")
	_, err := e.Groups.ImportText(ctx(t), domain.Group{Name: "n", CodeName: "looks"}, "a\nb")
	wantRoute(t, err, "GroupService.ImportText", "GroupCodeNameTaken")
	if page, _ := e.Groups.List(ctx(t), "", domain.Page{}); page.Total != 1 {
		t.Errorf("%d groups", page.Total)
	}
}
