package uploads

import (
	"strings"
	"testing"

	"github.com/rm4n0s/bos/internal/domain"
	"github.com/rm4n0s/bos/internal/files"
	"github.com/rm4n0s/bos/internal/filesrv"
	"github.com/rm4n0s/errors"
)

func newStore(t *testing.T) (*files.Store, filesrv.Dirs) {
	t.Helper()
	root := t.TempDir()
	st, err := files.NewStore([]string{root})
	if err != nil {
		t.Fatal(err)
	}
	return st, filesrv.Dirs{Thumbnails: root + "/thumbs", ControlNet: root + "/cn", IPAdapter: root + "/ip", Regions: root + "/regions"}
}

func wantTag(t *testing.T, err error, tag string) {
	t.Helper()
	appErr, ok := errors.FromError(err)
	if !ok || appErr.Tag != tag {
		t.Errorf("err = %v, want tag %s", err, tag)
	}
}

func TestKinds(t *testing.T) {
	_, d := newStore(t)
	item, group := ItemKinds(d), GroupKinds(d)
	for field, want := range map[string]string{"thumbnail": d.Thumbnails, "ip_adapter_image": d.IPAdapter, "mask_reference": d.Regions, "controlnet_0": d.ControlNet, "controlnet_12": d.ControlNet} {
		if k, ok := item(field); !ok || k.Dir != want {
			t.Errorf("item %s = %+v, %v", field, k, ok)
		}
	}
	for _, field := range []string{"avatar", "controlnet_", "controlnet_01", "controlnet_-1", "controlnet_x", "data"} {
		if _, ok := item(field); ok {
			t.Errorf("item accepted %q", field)
		}
	}
	if _, ok := group("thumbnail"); !ok {
		t.Error("group thumbnail refused")
	}
	if _, ok := group("controlnet_0"); ok {
		t.Error("group accepted a ControlNet image")
	}
	if k, _ := item("mask_reference"); len(k.MIMEs) != 1 || k.MIMEs[0] != "image/png" {
		t.Errorf("mask types = %v", k.MIMEs)
	}
}

func TestSaveChecksTheContent(t *testing.T) {
	st, d := newStore(t)
	png := "\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR"
	p, err := Save(st, "thumbnail", Kind{Dir: d.Thumbnails, MIMEs: ThumbnailTypes}, "../x.png", strings.NewReader(png), 0)
	if err != nil || !strings.HasPrefix(p, d.Thumbnails) {
		t.Fatalf("path = %q, err = %v", p, err)
	}
	_, err = Save(st, "thumbnail", Kind{Dir: d.Thumbnails, MIMEs: ThumbnailTypes}, "x.png", strings.NewReader("<html>not an image</html>"), 0)
	wantTag(t, err, "InvalidRequest")
	// The name does not decide: a text file called .png is refused, a PNG called .txt is fine.
	if _, err := Save(st, "thumbnail", Kind{Dir: d.Thumbnails, MIMEs: ThumbnailTypes}, "x.txt", strings.NewReader(png), 0); err != nil {
		t.Errorf("a PNG named .txt: %v", err)
	}
	_, err = Save(st, "x", Kind{Dir: d.Thumbnails, MIMEs: ThumbnailTypes}, "big.png", strings.NewReader(png+strings.Repeat("x", 100)), 20)
	wantTag(t, err, "UploadTooLarge")
}

func TestApplyItem(t *testing.T) {
	set := NewSet()
	set.Add("thumbnail", "/s/thumb.png")
	set.Add("controlnet_1", "/s/cn1.png")
	in := domain.Item{ControlNets: []domain.ControlNet{{Type: "canny"}, {Type: "depth"}}, MaskRegion: &domain.MaskRegion{FolderPath: "/etc", MaskFiles: map[string]string{"a": "/etc/passwd"}}}
	if err := ApplyItem(&in, set, nil); err != nil {
		t.Fatal(err)
	}
	if in.Thumbnail != "/s/thumb.png" || in.ControlNets[1].ImagePath != "/s/cn1.png" || in.ControlNets[0].ImagePath != "" {
		t.Errorf("item = %+v", in)
	}
	if in.MaskRegion.FolderPath != "" || in.MaskRegion.MaskFiles != nil {
		t.Errorf("the client's mask folder was kept: %+v", in.MaskRegion)
	}

	// Parts that need something in the item.
	for field, item := range map[string]domain.Item{
		"ip_adapter_image": {}, "mask_reference": {}, "controlnet_2": {ControlNets: []domain.ControlNet{{}}},
	} {
		s := NewSet()
		s.Add(field, "/s/x.png")
		wantTag(t, ApplyItem(&item, s, nil), "InvalidRequest")
	}

	// A path the client names is refused unless the item had it.
	named := domain.Item{Thumbnail: "/elsewhere.png"}
	wantTag(t, ApplyItem(&named, NewSet(), nil), "InvalidRequest")
	if err := ApplyItem(&named, NewSet(), []string{"/elsewhere.png"}); err != nil {
		t.Errorf("a path the item had: %v", err)
	}
}

func TestSet(t *testing.T) {
	s := NewSet()
	s.Add("b", "/2")
	s.Add("a", "/1")
	if got := strings.Join(s.Paths(), ","); got != "/1,/2" {
		t.Errorf("paths = %s", got)
	}
	if !s.Has("a") || s.Has("c") || !s.Contains("/2") || s.Contains("/3") {
		t.Error("membership is wrong")
	}
}
