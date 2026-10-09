package api

import (
	"os"
	"strings"
	"testing"
)

func TestUploadRefusals(t *testing.T) {
	a := seeded(t)
	group := map[string]any{"name": "G", "code_name": "g"}
	item := map[string]any{"code_name": "n", "name": "N"}
	textFile := []byte("this is not an image at all")
	gif := []byte("GIF89a\x01\x00\x01\x00\x00\x00\x00;")
	jpeg := append([]byte{0xff, 0xd8, 0xff, 0xe0, 0, 0x10}, []byte("JFIF\x00")...)

	tests := []struct {
		name         string
		method, path string
		data         any
		parts        []part
		status       int
		tag          string
	}{
		{"text as thumbnail", "POST", "/api/v1/groups", group, []part{{"thumbnail", "a.png", "image/png", textFile}}, 400, "InvalidRequest"},
		{"unknown part", "POST", "/api/v1/groups", group, []part{{"avatar", "a.png", "image/png", pngBytes}}, 400, "InvalidRequest"},
		{"same part twice", "POST", "/api/v1/groups", group, []part{{"thumbnail", "a.png", "image/png", pngBytes}, {"thumbnail", "b.png", "image/png", pngBytes}}, 400, "InvalidRequest"},
		{"bad data part", "POST", "/api/v1/groups", map[string]any{"name": "G", "code_name": "g", "colour": 1}, nil, 400, "InvalidRequest"},
		{"too large", "POST", "/api/v1/groups", group, []part{{"thumbnail", "big.png", "image/png", append(append([]byte{}, pngBytes...), make([]byte, 1<<20)...)}}, 413, "UploadTooLarge"},
		{"ip adapter file without ip_adapter", "POST", "/api/v1/groups/1/items", item, []part{{"ip_adapter_image", "a.png", "image/png", pngBytes}}, 400, "InvalidRequest"},
		{"mask file without a region", "POST", "/api/v1/groups/1/items", item, []part{{"mask_reference", "a.png", "image/png", pngBytes}}, 400, "InvalidRequest"},
		{"controlnet file out of range", "POST", "/api/v1/groups/1/items", item, []part{{"controlnet_0", "a.png", "image/png", pngBytes}}, 400, "InvalidRequest"},
		{"controlnet name malformed", "POST", "/api/v1/groups/1/items", item, []part{{"controlnet_01", "a.png", "image/png", pngBytes}}, 400, "InvalidRequest"},
		{"gif as mask reference", "POST", "/api/v1/groups/1/items", map[string]any{"code_name": "m", "name": "M", "mask_region_images": map[string]any{}}, []part{{"mask_reference", "a.gif", "image/gif", gif}}, 400, "InvalidRequest"},
		{"jpeg as thumbnail is fine", "POST", "/api/v1/groups", map[string]any{"name": "J", "code_name": "j"}, []part{{"thumbnail", "a.jpg", "image/jpeg", jpeg}}, 201, ""},
		{"webp is not a thumbnail", "POST", "/api/v1/groups", map[string]any{"name": "W", "code_name": "w"}, []part{{"thumbnail", "a.webp", "image/webp", []byte("RIFF\x00\x00\x00\x00WEBPVP8 ")}}, 400, "InvalidRequest"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			before := len(storedFiles(t, a.root))
			r := a.form(tc.method, tc.path, tc.data, tc.parts...)
			a.expect(r, tc.status, tc.tag)
			if tc.status >= 400 {
				if after := len(storedFiles(t, a.root)); after != before {
					t.Errorf("a refused upload left %d new files behind", after-before)
				}
			}
		})
	}
}

func TestFailedCreateRemovesItsUploads(t *testing.T) {
	a := seeded(t)
	before := len(storedFiles(t, a.root))
	// The code name is taken, so the service refuses after the file was stored.
	r := a.form("POST", "/api/v1/groups", map[string]any{"name": "Dup", "code_name": "characters"}, part{"thumbnail", "a.png", "image/png", pngBytes})
	a.expect(r, 409, "GroupCodeNameTaken")
	if after := len(storedFiles(t, a.root)); after != before {
		t.Errorf("%d files were left behind", after-before)
	}
}

func TestClientsCannotNameFiles(t *testing.T) {
	a := seeded(t)
	secret := a.root + "/results/other.png"
	check(t, os.MkdirAll(a.dirs.Results, 0o750))
	check(t, os.WriteFile(secret, pngBytes, 0o600))

	for name, body := range map[string]map[string]any{
		"thumbnail":  {"code_name": "x", "name": "X", "thumbnail": secret},
		"ip adapter": {"code_name": "x", "name": "X", "ip_adapter": map[string]any{"image_file": secret, "weight": 1}},
		"controlnet": {"code_name": "x", "name": "X", "controlnets": []map[string]any{{"type": "canny", "image_path": secret, "strength": 1}}},
		"mask":       {"code_name": "x", "name": "X", "mask_region_images": map[string]any{"reference_path": secret}},
	} {
		t.Run(name, func(t *testing.T) {
			a.expect(a.do("POST", "/api/v1/groups/1/items", body), 400, "InvalidRequest")
		})
	}
	a.expect(a.do("POST", "/api/v1/groups", map[string]any{"name": "X", "code_name": "x", "thumbnail": secret}), 201, "")
	g := a.do("GET", "/api/v1/groups/2", nil).json()
	if g["thumbnail"] != "" {
		t.Errorf("a client-named thumbnail was kept: %v", g["thumbnail"])
	}
	a.expect(a.do("PUT", "/api/v1/groups/1", map[string]any{"name": "C", "code_name": "characters", "thumbnail": secret}), 400, "InvalidRequest")
	if _, err := os.Stat(secret); err != nil {
		t.Errorf("the file was touched: %v", err)
	}
}

func TestUpdateKeepsReplacesAndRemovesFiles(t *testing.T) {
	a := seeded(t)
	created := a.expect(a.form("POST", "/api/v1/groups/1/items", map[string]any{"code_name": "t", "name": "T"},
		part{"thumbnail", "one.png", "image/png", pngBytes}), 201, "").json()
	first := created["thumbnail"].(string)
	id := jsonNumber(created["id"].(float64))
	if _, err := os.Stat(first); err != nil {
		t.Fatalf("the thumbnail is missing: %v", err)
	}

	// Sent back unchanged: kept.
	created["code_name"], created["name"] = "t", "T"
	kept := a.expect(a.do("PUT", "/api/v1/items/"+id, mustItem(created)), 200, "").json()
	if kept["thumbnail"] != first {
		t.Errorf("thumbnail = %v, want it kept", kept["thumbnail"])
	}
	// A new file: replaced, the old one removed.
	replaced := a.expect(a.form("PUT", "/api/v1/items/"+id, map[string]any{"code_name": "t", "name": "T"},
		part{"thumbnail", "two.png", "image/png", pngBytes}), 200, "").json()
	second := replaced["thumbnail"].(string)
	if second == first || !strings.HasSuffix(second, "-two.png") {
		t.Errorf("thumbnail = %v", second)
	}
	if _, err := os.Stat(first); !os.IsNotExist(err) {
		t.Error("the replaced thumbnail was kept")
	}
	// Cleared: removed.
	a.expect(a.do("PUT", "/api/v1/items/"+id, map[string]any{"code_name": "t", "name": "T", "thumbnail": ""}), 200, "")
	if _, err := os.Stat(second); !os.IsNotExist(err) {
		t.Error("the cleared thumbnail was kept")
	}
	// Deleting the item removes what it owns.
	again := a.expect(a.form("PUT", "/api/v1/items/"+id, map[string]any{"code_name": "t", "name": "T"},
		part{"thumbnail", "three.png", "image/png", pngBytes}), 200, "").json()["thumbnail"].(string)
	a.expect(a.do("DELETE", "/api/v1/items/"+id, nil), 204, "")
	if _, err := os.Stat(again); !os.IsNotExist(err) {
		t.Error("the deleted item's thumbnail was kept")
	}
}

func mustItem(m map[string]any) map[string]any { return m }

func TestMaskReferenceGeneratesMasks(t *testing.T) {
	a := seeded(t)
	// Two flat regions on white: left red, right blue.
	img := twoColourPNG(t)
	item := a.expect(a.form("POST", "/api/v1/groups/1/items", map[string]any{
		"code_name": "regions", "name": "Regions", "mask_region_images": map[string]any{"folder_path": "/etc"},
	}, part{"mask_reference", "regions.png", "image/png", img}), 201, "").json()
	mask := item["mask_region_images"].(map[string]any)
	files, _ := mask["mask_files"].(map[string]any)
	if len(files) != 3 || files["red"] == nil || files["blue"] == nil || files["background"] == nil {
		t.Fatalf("mask_files = %v", mask["mask_files"])
	}
	if folder := mask["folder_path"].(string); !strings.HasPrefix(folder, a.dirs.Regions) {
		t.Errorf("the folder is %q: a client-named folder must be ignored", folder)
	}
	// The masks can be fetched through the files route.
	red := files["red"].(string)
	rel := strings.TrimPrefix(red, a.dirs.Regions+"/")
	a.expect(a.do("GET", "/api/v1/files/regions/"+rel, nil), 200, "")
	// A reference with no region fails with MaskGenerationFailed.
	a.expect(a.form("POST", "/api/v1/groups/1/items", map[string]any{"code_name": "white", "name": "White", "mask_region_images": map[string]any{}},
		part{"mask_reference", "w.png", "image/png", pngBytes}), 422, "MaskGenerationFailed")
}
