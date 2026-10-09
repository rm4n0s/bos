package service

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/rm4n0s/bos/internal/domain"
)

func TestItems(t *testing.T) {
	e := newEnv(t)
	g := e.group(t, "poses")
	it, err := e.Items.Create(ctx(t), g.ID, domain.Item{
		CodeName: "jump", Name: "Jumping", PositivePrompt: "jumping", Tags: []string{"Action", "acrobatic"},
		LoRAs:              []domain.LoRA{{Name: "l", StrengthModel: 1, StrengthClip: 1}},
		ControlNets:        []domain.ControlNet{{Type: "openpose", ImagePath: "x.png", Strength: 1}},
		CoordinatedRegions: []domain.CoordinatedRegion{{Keyword: "left", Width: 10, Height: 20, X: 0, Y: 5}},
	})
	check(t, err)
	if it.GroupID != g.ID || !reflect.DeepEqual(it.Tags, []string{"acrobatic", "action"}) {
		t.Errorf("created = %+v", it)
	}
	got, err := e.Items.Get(ctx(t), it.ID)
	check(t, err)
	if !reflect.DeepEqual(got, it) {
		t.Errorf("Get = %+v\nwant  %+v", got, it)
	}

	e.item(t, g.ID, "sit", "sitting")
	page, err := e.Items.ListByGroup(ctx(t), g.ID, domain.Page{})
	check(t, err)
	if page.Total != 2 || page.Items[0].CodeName != "jump" {
		t.Errorf("page = %+v", page)
	}

	// The group of an item does not change when it is edited.
	other := e.group(t, "other")
	upd, err := e.Items.Update(ctx(t), it.ID, domain.Item{GroupID: other.ID, CodeName: "leap", PositivePrompt: "leaping"})
	check(t, err)
	if upd.GroupID != g.ID || upd.CodeName != "leap" || len(upd.Tags) != 0 {
		t.Errorf("updated = %+v", upd)
	}

	check(t, e.Items.Delete(ctx(t), it.ID))
	_, err = e.Items.Get(ctx(t), it.ID)
	wantRoute(t, err, "ItemService.Get", "ItemNotFound")
}

func TestItemFailures(t *testing.T) {
	e := newEnv(t)
	g := e.group(t, "poses")
	it := e.item(t, g.ID, "jump", "jumping")
	good := domain.Item{CodeName: "other"}
	tests := []struct {
		name   string
		change func(*domain.Item)
		tag    string
	}{
		{"long name", func(v *domain.Item) { v.Name = strings.Repeat("n", 301) }, "NameInvalid"},
		{"bad code", func(v *domain.Item) { v.CodeName = "a b" }, "CodeNameInvalid"},
		{"reserved code", func(v *domain.Item) { v.CodeName = "not" }, "CodeNameReserved"},
		{"bad tag", func(v *domain.Item) { v.Tags = []string{"No Good"} }, "TagInvalid"},
		{"bad output type", func(v *domain.Item) { v.OutputType = "audio" }, "OutputTypeInvalid"},
		{"unknown controlnet", func(v *domain.Item) { v.ControlNets = []domain.ControlNet{{Type: "magic"}} }, "ItemControlNetInvalid"},
		{"empty keyword", func(v *domain.Item) {
			v.CoordinatedRegions = []domain.CoordinatedRegion{{Keyword: "", Width: 1, Height: 1}}
		}, "ItemRegionInvalid"},
		{"reserved keyword", func(v *domain.Item) {
			v.CoordinatedRegions = []domain.CoordinatedRegion{{Keyword: "where", Width: 1, Height: 1}}
		}, "ItemRegionInvalid"},
		{"keyword with a space", func(v *domain.Item) {
			v.CoordinatedRegions = []domain.CoordinatedRegion{{Keyword: "a b", Width: 1, Height: 1}}
		}, "ItemRegionInvalid"},
		{"keyword twice", func(v *domain.Item) {
			v.CoordinatedRegions = []domain.CoordinatedRegion{{Keyword: "a", Width: 1, Height: 1}, {Keyword: "a", Width: 1, Height: 1}}
		}, "ItemRegionInvalid"},
		{"keyword by mask and by coordinates", func(v *domain.Item) {
			v.MaskRegion = &domain.MaskRegion{MaskFiles: map[string]string{"a": "m.png"}}
			v.CoordinatedRegions = []domain.CoordinatedRegion{{Keyword: "a", Width: 1, Height: 1}}
		}, "ItemRegionInvalid"},
		{"empty rectangle", func(v *domain.Item) {
			v.CoordinatedRegions = []domain.CoordinatedRegion{{Keyword: "a", Width: 0, Height: 1}}
		}, "ItemRegionInvalid"},
		{"negative position", func(v *domain.Item) {
			v.CoordinatedRegions = []domain.CoordinatedRegion{{Keyword: "a", Width: 1, Height: 1, X: -1}}
		}, "ItemRegionInvalid"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := good
			tt.change(&in)
			_, err := e.Items.Create(ctx(t), g.ID, in)
			wantRoute(t, err, "ItemService.Create", tt.tag)
			_, err = e.Items.Update(ctx(t), it.ID, in)
			wantRoute(t, err, "ItemService.Update", tt.tag)
		})
	}
	if page, _ := e.Items.ListByGroup(ctx(t), g.ID, domain.Page{}); page.Total != 1 {
		t.Errorf("%d items, a rejected create stored something", page.Total)
	}

	_, err := e.Items.Create(ctx(t), g.ID, domain.Item{CodeName: "jump"})
	wantRoute(t, err, "ItemService.Create", "ItemCodeNameTaken")
	_, err = e.Items.Create(ctx(t), 999, domain.Item{CodeName: "x"})
	wantRoute(t, err, "ItemService.Create", "GroupNotFound")
	_, err = e.Items.Update(ctx(t), 999, good)
	wantRoute(t, err, "ItemService.Update", "ItemNotFound")
	wantRoute(t, e.Items.Delete(ctx(t), 999), "ItemService.Delete", "ItemNotFound")
	_, err = e.Items.ListByGroup(ctx(t), 999, domain.Page{})
	wantRoute(t, err, "ItemService.ListByGroup", "GroupNotFound")
}

func TestUpdateRemovesFilesTheItemNoLongerUses(t *testing.T) {
	e := newEnv(t)
	g := e.group(t, "g")
	thumb := e.write(t, "thumbs/a.png", "x")
	cnKept := e.write(t, "controlnet/kept.png", "x")
	cnDropped := e.write(t, "controlnet/dropped.png", "x")
	ip := e.write(t, "ipadapter/a.png", "x")
	it, err := e.Items.Create(ctx(t), g.ID, domain.Item{
		CodeName: "i", Thumbnail: thumb, IPAdapter: &domain.IPAdapter{ImageFile: ip},
		ControlNets: []domain.ControlNet{{Type: "canny", ImagePath: cnKept}, {Type: "canny", ImagePath: cnDropped}},
	})
	check(t, err)

	_, err = e.Items.Update(ctx(t), it.ID, domain.Item{
		CodeName: "i", Thumbnail: thumb, ControlNets: []domain.ControlNet{{Type: "canny", ImagePath: cnKept}},
	})
	check(t, err)
	if !exists(thumb) || !exists(cnKept) {
		t.Error("a file that is still used was removed")
	}
	if exists(cnDropped) || exists(ip) {
		t.Error("a file that is no longer used was kept")
	}
}

func pngFixture(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "mask", "testdata", "color-mask.png"))
	check(t, err)
	return b
}

func TestMasksAreGeneratedFromTheReferenceImage(t *testing.T) {
	e := newEnv(t)
	g := e.group(t, "scenes")
	ref := e.write(t, "regions/ref.png", string(pngFixture(t)))

	it, err := e.Items.Create(ctx(t), g.ID, domain.Item{CodeName: "duo", MaskRegion: &domain.MaskRegion{ReferencePath: ref}})
	check(t, err)
	m := it.MaskRegion
	if m.FolderPath == "" || !strings.HasPrefix(m.FolderPath, e.dirs.Regions) {
		t.Fatalf("mask folder = %q, want one under %s", m.FolderPath, e.dirs.Regions)
	}
	for _, kw := range []string{"red", "blue", "background"} {
		f, ok := m.MaskFiles[kw]
		if !ok || !exists(f) || !strings.HasPrefix(f, m.FolderPath) {
			t.Errorf("mask %q = %q (exists %v)", kw, f, exists(f))
		}
	}

	// The same reference image keeps its masks.
	same, err := e.Items.Update(ctx(t), it.ID, domain.Item{CodeName: "duo", MaskRegion: &domain.MaskRegion{ReferencePath: ref}})
	check(t, err)
	if same.MaskRegion.FolderPath != m.FolderPath || !reflect.DeepEqual(same.MaskRegion.MaskFiles, m.MaskFiles) {
		t.Errorf("masks were made again: %+v", same.MaskRegion)
	}

	// Another reference image gets new masks, and the old ones go.
	ref2 := e.write(t, "regions/ref2.png", string(pngFixture(t)))
	moved, err := e.Items.Update(ctx(t), it.ID, domain.Item{CodeName: "duo", MaskRegion: &domain.MaskRegion{ReferencePath: ref2}})
	check(t, err)
	if moved.MaskRegion.FolderPath == m.FolderPath || exists(m.FolderPath) || exists(ref) || !exists(moved.MaskRegion.FolderPath) {
		t.Errorf("old folder exists = %v, old ref exists = %v, new = %+v", exists(m.FolderPath), exists(ref), moved.MaskRegion)
	}

	// Deleting the item removes the reference image and the masks.
	check(t, e.Items.Delete(ctx(t), it.ID))
	if exists(ref2) || exists(moved.MaskRegion.FolderPath) {
		t.Error("the reference image or the masks survived the deletion")
	}
}

func TestMaskFailures(t *testing.T) {
	e := newEnv(t)
	g := e.group(t, "scenes")

	notPNG := e.write(t, "regions/photo.jpg", "not a png")
	_, err := e.Items.Create(ctx(t), g.ID, domain.Item{CodeName: "a", MaskRegion: &domain.MaskRegion{ReferencePath: notPNG}})
	wantRoute(t, err, "ItemService.Create", "MaskGenerationFailed")

	_, err = e.Items.Create(ctx(t), g.ID, domain.Item{CodeName: "b", MaskRegion: &domain.MaskRegion{ReferencePath: e.root + "/regions/missing.png"}})
	wantRoute(t, err, "ItemService.Create", "FileNotFound")

	_, err = e.Items.Create(ctx(t), g.ID, domain.Item{CodeName: "c", MaskRegion: &domain.MaskRegion{ReferencePath: filepath.Join(t.TempDir(), "x.png")}})
	wantRoute(t, err, "ItemService.Create", "FileOutsideStorage")

	// A mask keyword that clashes with a coordinate keyword is found after the
	// masks exist; the masks that were made are removed again.
	ref := e.write(t, "regions/ref.png", string(pngFixture(t)))
	_, err = e.Items.Create(ctx(t), g.ID, domain.Item{
		CodeName: "d", MaskRegion: &domain.MaskRegion{ReferencePath: ref},
		CoordinatedRegions: []domain.CoordinatedRegion{{Keyword: "red", Width: 1, Height: 1}},
	})
	wantRoute(t, err, "ItemService.Create", "ItemRegionInvalid")
	entries, _ := os.ReadDir(e.dirs.Regions)
	for _, entry := range entries {
		if entry.IsDir() {
			t.Errorf("mask folder %s left behind", entry.Name())
		}
	}

	// Cancelled while waiting to start.
	cctx, cancel := cancellable(t)
	cancel()
	_, err = e.Items.Create(cctx, g.ID, domain.Item{CodeName: "e", MaskRegion: &domain.MaskRegion{ReferencePath: ref}})
	wantRoute(t, err, "ItemService.Create", "MaskGenerationFailed")

	if page, _ := e.Items.ListByGroup(ctx(t), g.ID, domain.Page{}); page.Total != 0 {
		t.Errorf("%d items stored by failed creates", page.Total)
	}
}
