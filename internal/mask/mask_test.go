package mask

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/rm4n0s/errors"
)

func encodePNG(t *testing.T, img image.Image) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// flat paints rects of colours on white.
func flat(w, h int, rects map[color.RGBA]image.Rectangle) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for i := range img.Pix {
		img.Pix[i] = 255
	}
	for c, r := range rects {
		for y := r.Min.Y; y < r.Max.Y; y++ {
			for x := r.Min.X; x < r.Max.X; x++ {
				img.SetRGBA(x, y, c)
			}
		}
	}
	return img
}

func count(g *image.Gray) int {
	n := 0
	for _, p := range g.Pix {
		if p == 255 {
			n++
		}
	}
	return n
}

func keywords(masks []Mask) []string {
	out := make([]string, len(masks))
	for i, m := range masks {
		out[i] = m.Keyword
	}
	return out
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestGenerateFromFixture(t *testing.T) {
	data, err := os.ReadFile("testdata/color-mask.png")
	if err != nil {
		t.Fatal(err)
	}
	masks, err := Generate(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	got := keywords(masks)
	if len(got) != 3 || got[2] != Background {
		t.Fatalf("keywords = %v", got)
	}
	has := map[string]bool{got[0]: true, got[1]: true}
	if !has["red"] || !has["blue"] || has["white"] {
		t.Errorf("keywords = %v, want red, blue and background", got)
	}
	// Every pixel belongs to exactly one mask.
	total := 0
	for _, m := range masks {
		if m.Image.Bounds().Dx() != 751 || m.Image.Bounds().Dy() != 586 {
			t.Errorf("mask %s has size %v", m.Keyword, m.Image.Bounds())
		}
		total += count(m.Image)
	}
	if total != 751*586 {
		t.Errorf("masks cover %d pixels, want %d", total, 751*586)
	}
}

func TestGenerateNamesAndOrder(t *testing.T) {
	img := flat(10, 10, map[color.RGBA]image.Rectangle{
		{R: 255, A: 255}:              image.Rect(0, 0, 3, 3),
		{R: 0, G: 128, B: 0, A: 255}:  image.Rect(5, 0, 8, 3),
		{R: 12, G: 34, B: 56, A: 255}: image.Rect(0, 5, 4, 8),
		{A: 255}:                      image.Rect(6, 6, 9, 9), // black has no name
	})
	masks, err := Generate(bytes.NewReader(encodePNG(t, img)))
	if err != nil {
		t.Fatal(err)
	}
	// Colours are listed in the order they first appear, scanning rows from the top.
	want := []string{"red", "dark_green", "color_12_34_56", "color_0_0_0", "background"}
	if got := keywords(masks); !equal(got, want) {
		t.Errorf("keywords = %v, want %v", got, want)
	}
	if n := count(masks[0].Image); n != 9 {
		t.Errorf("red mask has %d pixels, want 9", n)
	}
	if n := count(masks[4].Image); n != 100-9-9-12-9 { // red, dark green, 12/34/56 and black
		t.Errorf("background has %d pixels", n)
	}
	for _, m := range masks {
		for _, p := range m.Image.Pix {
			if p != 0 && p != 255 {
				t.Fatalf("mask %s is not binary: %d", m.Keyword, p)
			}
		}
	}
}

func TestNameTable(t *testing.T) {
	tests := map[[3]uint8]string{
		{255, 0, 0}: "red", {0, 255, 0}: "green", {0, 0, 255}: "blue", {255, 255, 0}: "yellow", {255, 0, 255}: "magenta",
		{0, 255, 255}: "cyan", {128, 0, 0}: "maroon", {0, 128, 0}: "dark_green", {0, 0, 128}: "navy", {128, 128, 0}: "olive",
		{255, 165, 0}: "orange", {128, 0, 128}: "purple", {255, 192, 203}: "pink", {165, 42, 42}: "brown", {128, 128, 128}: "gray",
		{1, 2, 3}: "color_1_2_3",
	}
	for c, want := range tests {
		if got := Name(c[0], c[1], c[2]); got != want {
			t.Errorf("Name(%v) = %s, want %s", c, got, want)
		}
	}
}

func TestTransparentPixelsAreBackground(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 4, 1))
	img.SetNRGBA(0, 0, color.NRGBA{R: 255, A: 255})
	img.SetNRGBA(1, 0, color.NRGBA{R: 10, G: 20, B: 30, A: 0}) // fully transparent
	img.SetNRGBA(2, 0, color.NRGBA{A: 0})
	img.SetNRGBA(3, 0, color.NRGBA{R: 255, G: 255, B: 255, A: 255})
	masks, err := Generate(bytes.NewReader(encodePNG(t, img)))
	if err != nil {
		t.Fatal(err)
	}
	if got := keywords(masks); !equal(got, []string{"red", Background}) {
		t.Fatalf("keywords = %v", got)
	}
	if count(masks[1].Image) != 3 {
		t.Errorf("background has %d pixels, want 3", count(masks[1].Image))
	}
}

func TestPalettedImage(t *testing.T) {
	pal := color.Palette{color.White, color.RGBA{R: 255, A: 255}}
	img := image.NewPaletted(image.Rect(0, 0, 2, 1), pal)
	img.SetColorIndex(1, 0, 1)
	masks, err := Generate(bytes.NewReader(encodePNG(t, img)))
	if err != nil {
		t.Fatal(err)
	}
	if got := keywords(masks); !equal(got, []string{"red", Background}) {
		t.Errorf("keywords = %v", got)
	}
}

func TestGenerateFailures(t *testing.T) {
	var jpg bytes.Buffer
	if err := jpeg.Encode(&jpg, flat(8, 8, map[color.RGBA]image.Rectangle{{R: 255, A: 255}: image.Rect(0, 0, 4, 4)}), nil); err != nil {
		t.Fatal(err)
	}
	rects := map[color.RGBA]image.Rectangle{}
	for i := range MaxColours + 1 {
		rects[color.RGBA{R: uint8(10 * (i + 1)), A: 255}] = image.Rect(i, 0, i+1, 1)
	}
	tenColours := map[color.RGBA]image.Rectangle{}
	for i := range MaxColours {
		tenColours[color.RGBA{R: uint8(10 * (i + 1)), A: 255}] = image.Rect(i, 0, i+1, 1)
	}
	// Anti-aliasing: a gradient edge produces many colours.
	soft := image.NewRGBA(image.Rect(0, 0, 40, 1))
	for x := range 40 {
		soft.SetRGBA(x, 0, color.RGBA{R: 255, G: uint8(x * 6), B: uint8(x * 6), A: 255})
	}

	tests := []struct {
		name string
		data []byte
		ok   bool
		want string // part of the reason
	}{
		{"jpeg", jpg.Bytes(), false, "not a PNG"},
		{"garbage", []byte("hello"), false, "not a PNG"},
		{"empty", nil, false, "not a PNG"},
		{"truncated", encodePNG(t, flat(8, 8, nil))[:30], false, "cannot be decoded"},
		{"all white", encodePNG(t, flat(8, 8, nil)), false, "no colour besides white"},
		{"eleven colours", encodePNG(t, flat(20, 3, rects)), false, "more than 10 colours"},
		{"anti-aliased", encodePNG(t, soft), false, "more than 10 colours"},
		{"exactly ten colours", encodePNG(t, flat(20, 3, tenColours)), true, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			masks, err := Generate(bytes.NewReader(tt.data))
			if tt.ok {
				if err != nil || len(masks) != MaxColours+1 {
					t.Fatalf("masks = %d, err = %v", len(masks), err)
				}
				return
			}
			appErr, isApp := errors.FromError(err)
			if !isApp || appErr.Tag != "MaskGenerationFailed" || !appErr.HasRoute("Generate") {
				t.Fatalf("error = %v", err)
			}
			reason, _ := appErr.Metadata["reason"].(string)
			if reason == "" || !contains(reason+" "+appErr.Message, tt.want) {
				t.Errorf("reason = %q (%s), want it to mention %q", reason, appErr.Message, tt.want)
			}
		})
	}
}

func contains(s, sub string) bool { return bytes.Contains([]byte(s), []byte(sub)) }

func TestSave(t *testing.T) {
	img := flat(6, 4, map[color.RGBA]image.Rectangle{{R: 255, A: 255}: image.Rect(0, 0, 2, 2), {B: 255, A: 255}: image.Rect(4, 2, 6, 4)})
	masks, err := Generate(bytes.NewReader(encodePNG(t, img)))
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(t.TempDir(), "nested", "masks")
	paths, err := Save(dir, masks)
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 3 || paths["red"] != filepath.Join(dir, "mask_red.png") {
		t.Fatalf("paths = %v", paths)
	}
	for kw, p := range paths {
		f, err := os.Open(p)
		if err != nil {
			t.Fatal(err)
		}
		decoded, err := png.Decode(f)
		if closeErr := f.Close(); closeErr != nil {
			t.Fatal(closeErr)
		}
		if err != nil {
			t.Fatalf("%s: %v", kw, err)
		}
		g, ok := decoded.(*image.Gray)
		if !ok || count(g) == 0 {
			t.Errorf("%s: decoded %T with %d set pixels", kw, decoded, count(g))
		}
	}

	// A directory that cannot be created, and a file that cannot be written.
	blocker := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocker, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Save(filepath.Join(blocker, "sub"), masks); !hasRoute(err, "Save", "FileStoreFailure") {
		t.Errorf("mkdir failure = %v", err)
	}
	ro := t.TempDir()
	if err := os.Chmod(ro, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chmod(ro, 0o700); err != nil {
			t.Error(err)
		}
	})
	if os.Geteuid() != 0 {
		if _, err := Save(ro, masks); !hasRoute(err, "Save", "FileStoreFailure") {
			t.Errorf("write failure = %v", err)
		}
	}
}

func hasRoute(err error, entry, tag string) bool {
	appErr, ok := errors.FromError(err)
	return ok && appErr.Tag == tag && appErr.HasRoute(entry)
}
