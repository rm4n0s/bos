package ui

import (
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/rm4n0s/bos/internal/domain"
)

func TestGeneratorScreen(t *testing.T) {
	u := newTestUI(t)
	wf := example(t, "creators", "illustrious_xl.json")
	mustContain(t, "page", u.get("/generators").Body, "No generators yet", "@get('/ui/generators/new')")

	// The dialog for a new generator.
	r := u.datastar("GET", "/ui/generators/new", struct{}{})
	mustContain(t, "new", r.patches(), `<div id="generator-form-body">`, `name="workflow"`, "Add generator")
	if r.signals()["generators"].(map[string]any)["open"] != true {
		t.Errorf("signals = %v", r.signals())
	}

	// The workflow is analysed by itself when it is pasted, edited or loaded
	// from a file: there is no button to press.
	form := r.patches()
	mustContain(t, "automatic analysis", form,
		`data-on:change="@post('/ui/generators/analyze', {contentType: 'form'})"`,
		`data-on:paste__debounce.400ms="@post('/ui/generators/analyze', {contentType: 'form'})"`,
		`dispatchEvent(new Event('change'`)
	mustNotContain(t, "automatic analysis", form, "Analyze workflow")
	// Datastar does not send an invalid form, and the analysis is sent before
	// the name is typed: the browser must not demand fields. The service does.
	mustNotContain(t, "automatic analysis", form, " required")

	// Analyze fills the fields from the workflow and offers its titles.
	r = u.submit("POST", "/ui/generators/analyze", field("id", "0"), field("name", "Anime"), field("code_name", "anime"), field("workflow", wf), field("output_type", "image"))
	p := r.patches()
	mustContain(t, "analyze", p, "selector #generator-form-body", `value="Anime"`, "Found: image output", "<datalist id=\"prompt-titles\">", "<option value=")
	mustNotContain(t, "analyze", p, `value="anime"></label>
</div>
<label>Workflow`, "toast error")

	// The prompt nodes that start with Positive and Negative are filled in.
	mustContain(t, "analyze", p, `name="positive_prompt_title" list="prompt-titles" value="Positive"`, `name="negative_prompt_title" list="prompt-titles" value="Negative"`)
	// What was typed there stays.
	r = u.submit("POST", "/ui/generators/analyze", field("workflow", wf), field("positive_prompt_title", "Negative"))
	mustContain(t, "analyze keeps a typed title", r.patches(), `name="positive_prompt_title" list="prompt-titles" value="Negative"`)

	// A generator needs a positive prompt node: with no node starting with the word, it is refused in the dialog.
	renamed := strings.ReplaceAll(wf, `"Positive"`, `"Wanted text"`)
	r = u.submit("POST", "/ui/generators", field("id", "0"), field("name", "Anime"), field("code_name", "anime"), field("workflow", renamed))
	mustContain(t, "no positive node", r.patches(), "selector #generators-error", "positive prompt node")

	// A workflow that is not one is an error in the dialog.
	r = u.submit("POST", "/ui/generators/analyze", field("workflow", "not json"))
	mustContain(t, "bad workflow", r.patches(), "selector #generators-error")

	// Create.
	r = u.submit("POST", "/ui/generators", field("id", "0"), field("name", "Anime"), field("code_name", "anime"), field("workflow", wf))
	mustContain(t, "create", r.patches(), "id=\"generators-table\"", "Anime", "<code>anime</code>", "Generator saved.")
	list, err := u.env.Services.Generators.List(t.Context(), domain.Page{})
	check(t, err)
	if list.Total != 1 || list.Items[0].OutputType != domain.OutputImage || list.Items[0].OutputNodeClassType == "" {
		t.Fatalf("generators = %+v", list.Items)
	}
	if r.signals()["generators"].(map[string]any)["open"] != false {
		t.Error("the dialog must close")
	}

	// Edit shows what is stored, including the attributes the analysis found.
	r = u.datastar("GET", "/ui/generators/1/edit", struct{}{})
	mustContain(t, "edit", r.patches(), `value="1"`, `value="Anime"`, "Edit generator", `"width"`)

	// Update, with attributes written by hand; bad JSON is refused.
	r = u.submit("POST", "/ui/generators", field("id", "1"), field("name", "Anime 2"), field("code_name", "anime"), field("workflow", wf),
		field("output_type", "image"), field("output_attributes", `{"width": 640, "sampler_name": "euler"}`))
	mustContain(t, "update", r.patches(), "Anime 2")
	got, _ := u.env.Services.Generators.Get(t.Context(), 1)
	if got.OutputAttributes["width"].Int != 640 {
		t.Errorf("attributes = %v", got.OutputAttributes)
	}
	r = u.submit("POST", "/ui/generators", field("id", "1"), field("name", "x"), field("code_name", "anime"), field("workflow", wf), field("output_attributes", `{oops`))
	mustContain(t, "bad attributes", r.patches(), "selector #generators-error", "not valid JSON")

	// A taken code name stays in the dialog.
	r = u.submit("POST", "/ui/generators", field("id", "0"), field("name", "Dup"), field("code_name", "anime"), field("workflow", wf))
	mustContain(t, "duplicate", r.patches(), "selector #generators-error", "already exists")

	// Delete.
	r = u.datastar("DELETE", "/ui/generators/1", struct{}{})
	mustContain(t, "delete", r.patches(), "No generators yet", "Generator deleted.")
}

func TestFixerScreen(t *testing.T) {
	u := newTestUI(t)
	wf := example(t, "fixers", "simple_upscale.json")
	form := u.datastar("GET", "/ui/fixers/new", struct{}{}).patches()
	mustContain(t, "automatic analysis", form,
		`data-on:change="@post('/ui/fixers/analyze', {contentType: 'form'})"`,
		`data-on:paste__debounce.400ms="@post('/ui/fixers/analyze', {contentType: 'form'})"`,
		`dispatchEvent(new Event('change'`)
	mustNotContain(t, "automatic analysis", form, "Analyze workflow")
	mustNotContain(t, "automatic analysis", form, " required")
	r := u.submit("POST", "/ui/fixers/analyze", field("workflow", wf))
	mustContain(t, "analyze", r.patches(), "<datalist id=\"load-titles\">", "Load Image", "Save Image", "image-input")
	r = u.submit("POST", "/ui/fixers", field("id", "0"), field("name", "Upscale"), field("code_name", "upscale"), field("workflow", wf),
		field("load_image_title", "Load Image"), field("save_image_title", "Save Image"))
	mustContain(t, "create", r.patches(), "id=\"fixers-table\"", "Upscale", "Fixer saved.")
	r = u.submit("POST", "/ui/fixers", field("id", "0"), field("name", "No titles"), field("code_name", "x"), field("workflow", wf))
	mustContain(t, "no titles", r.patches(), "selector #fixers-error")
	r = u.datastar("GET", "/ui/fixers/1/edit", struct{}{})
	mustContain(t, "edit", r.patches(), `value="Load Image"`)
	r = u.datastar("DELETE", "/ui/fixers/1", struct{}{})
	mustContain(t, "delete", r.patches(), "No fixers yet")
}

func TestProjectScreen(t *testing.T) {
	u := newTestUI(t)
	mustContain(t, "empty", u.get("/projects").Body, "No projects yet")
	r := u.submit("POST", "/ui/projects", field("id", "0"), field("name", "Demo"))
	mustContain(t, "create", r.patches(), `href="/projects/1/commands"`, "Demo", "Project saved.")
	r = u.submit("POST", "/ui/projects", field("id", "1"), field("name", "Renamed"))
	mustContain(t, "rename", r.patches(), "Renamed")
	r = u.submit("POST", "/ui/projects", field("id", "0"), field("name", ""))
	mustContain(t, "empty name", r.patches(), "selector #projects-error")
	mustContain(t, "edit", u.datastar("GET", "/ui/projects/1/edit", struct{}{}).patches(), `value="Renamed"`, "Rename project")
	mustContain(t, "delete", u.datastar("DELETE", "/ui/projects/1", struct{}{}).patches(), "No projects yet")
}

func TestGroupScreen(t *testing.T) {
	u := newTestUI(t)
	mustContain(t, "empty", u.get("/groups").Body, "No groups yet", "Import from text")

	// Create with a thumbnail and some features.
	r := u.submit("POST", "/ui/groups", field("id", "0"), field("name", "Characters"), field("code_name", "characters"), field("tags", "People, main"),
		field("use_controlnet", "on"), file("thumbnail", "c.png", png1()))
	p := r.patches()
	mustContain(t, "create", p, "Characters", `<img class="thumb" src="/files/thumbnails/`, "ControlNet", "Group saved.", "people", "main")
	thumbs := stored(t, u.env.Dirs.Thumbnails)
	if len(thumbs) != 1 {
		t.Fatalf("thumbnails = %v", thumbs)
	}
	// The thumbnail is served to the page.
	url := p[strings.Index(p, "/files/thumbnails/"):]
	url = url[:strings.Index(url, `"`)]
	if img := u.get(url); img.Status != 200 || img.Header.Get("Content-Type") != "image/png" {
		t.Errorf("thumbnail: %d %q", img.Status, img.Header.Get("Content-Type"))
	}

	// Filtering by a tag.
	_, err := u.env.Services.Groups.Create(t.Context(), domain.Group{Name: "Places", CodeName: "places", Tags: []string{"scenery"}})
	check(t, err)
	mustContain(t, "filter", u.datastar("GET", "/ui/groups/table?tag=scenery", struct{}{}).patches(), "Places", "Showing groups tagged")
	mustNotContain(t, "filter", u.datastar("GET", "/ui/groups/table?tag=scenery", struct{}{}).patches(), "Characters")
	mustContain(t, "no match", u.datastar("GET", "/ui/groups/table?tag=nothing", struct{}{}).patches(), "No group has this tag.")

	// Edit keeps the thumbnail unless asked to remove it; a new one replaces it.
	u.submit("POST", "/ui/groups", field("id", "1"), field("name", "Characters"), field("code_name", "characters"), field("tags", "people"), file("thumbnail", "", nil))
	got, _ := u.env.Services.Groups.Get(t.Context(), 1)
	if got.Thumbnail != thumbs[0] || len(got.Tags) != 1 || got.UseControlnet {
		t.Errorf("group = %+v", got)
	}
	u.submit("POST", "/ui/groups", field("id", "1"), field("name", "Characters"), field("code_name", "characters"), file("thumbnail", "n.png", png1()))
	got, _ = u.env.Services.Groups.Get(t.Context(), 1)
	if got.Thumbnail == thumbs[0] || len(stored(t, u.env.Dirs.Thumbnails)) != 1 {
		t.Errorf("the old thumbnail was kept: %v", stored(t, u.env.Dirs.Thumbnails))
	}
	u.submit("POST", "/ui/groups", field("id", "1"), field("name", "Characters"), field("code_name", "characters"), field("clear_thumbnail", "on"))
	got, _ = u.env.Services.Groups.Get(t.Context(), 1)
	if got.Thumbnail != "" || len(stored(t, u.env.Dirs.Thumbnails)) != 0 {
		t.Errorf("the thumbnail was not removed: %q %v", got.Thumbnail, stored(t, u.env.Dirs.Thumbnails))
	}

	// A refused upload leaves nothing behind and names the problem.
	r = u.submit("POST", "/ui/groups", field("id", "0"), field("name", "Bad"), field("code_name", "bad"), file("thumbnail", "x.png", []byte("<html>")))
	mustContain(t, "bad type", r.patches(), "selector #groups-error", "not of an accepted type")
	r = u.submit("POST", "/ui/groups", field("id", "0"), field("name", "Dup"), field("code_name", "characters"), file("thumbnail", "x.png", png1()))
	mustContain(t, "duplicate", r.patches(), "selector #groups-error", "already exists")
	if len(stored(t, u.env.Dirs.Thumbnails)) != 0 {
		t.Errorf("a failed save left %v", stored(t, u.env.Dirs.Thumbnails))
	}

	// Edit dialog.
	mustContain(t, "edit", u.datastar("GET", "/ui/groups/1/edit", struct{}{}).patches(), `value="Characters"`, "Edit group")

	// Delete.
	r = u.datastar("DELETE", "/ui/groups/1", struct{}{})
	mustContain(t, "delete", r.patches(), "Group deleted.")
	mustNotContain(t, "delete", r.patches(), "Characters")
}

func TestGroupImportFromText(t *testing.T) {
	u := newTestUI(t)
	r := u.submit("POST", "/ui/groups/import", field("name", "Animals"), field("code_name", "animals"), field("tags", "nature"), field("text", "a red fox\na blue owl\n\na green frog"))
	mustContain(t, "import", r.patches(), "Animals", "Group created from the text.")
	items, err := u.env.Services.Items.ListByGroup(t.Context(), 1, domain.Page{})
	check(t, err)
	if items.Total != 3 || items.Items[1].PositivePrompt != "a blue owl" {
		t.Errorf("items = %+v", items.Items)
	}
	// A text file works when nothing was pasted.
	r = u.submit("POST", "/ui/groups/import", field("name", "From file"), field("code_name", "fromfile"), file("textfile", "x.txt", []byte("one\ntwo")))
	mustContain(t, "import file", r.patches(), "From file")
	items, _ = u.env.Services.Items.ListByGroup(t.Context(), 2, domain.Page{})
	if items.Total != 2 {
		t.Errorf("items from the file = %d", items.Total)
	}
	r = u.submit("POST", "/ui/groups/import", field("name", "Dup"), field("code_name", "animals"), field("text", "x"))
	mustContain(t, "duplicate", r.patches(), "selector #groups-import-error", "already exists")
}

// newGroup creates a group with the given features and returns its id.
func (u *UI) newTestGroup(t *testing.T, g domain.Group) int64 {
	t.Helper()
	out, err := u.Services.Groups.Create(t.Context(), g)
	check(t, err)
	return out.ID
}

// A morph keeps what the user typed into a field, so a dialog must replace its
// body when it opens; otherwise "Add item" shows the item added before.
func TestOpeningADialogReplacesItsBody(t *testing.T) {
	u := newTestUI(t)
	g := u.ui.newTestGroup(t, domain.Group{Name: "Plain", CodeName: "plain"})
	tests := []struct{ name, path, body string }{
		{"new item", "/ui/groups/" + itoa(g) + "/items/new", `<div id="item-form-body">`},
		{"new group", "/ui/groups/new", `<div id="group-form-body">`},
		{"new project", "/ui/projects/new", `<div id="project-form-body">`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var mode, body bool
			for _, e := range parseEvents(u.datastar("GET", tt.path, struct{}{}).Body) {
				if e.Type != "datastar-patch-elements" {
					continue
				}
				mode = mode || strings.Contains(e.Data, "mode replace")
				body = body || strings.Contains(e.Data, tt.body)
			}
			if !mode || !body {
				t.Errorf("want the body %s replaced, not morphed (replace=%v, body=%v)", tt.body, mode, body)
			}
		})
	}
}

func TestItemScreenShowsOnlyTheSectionsOfTheGroup(t *testing.T) {
	u := newTestUI(t)
	plain := u.ui.newTestGroup(t, domain.Group{Name: "Plain", CodeName: "plain"})
	full := u.ui.newTestGroup(t, domain.Group{Name: "Full", CodeName: "full", UseControlnet: true, UseIPAdapter: true, UseMaskRegion: true, UseCoordinatesRegion: true, UseTypeAttributes: true})

	page := u.datastar("GET", "/ui/groups/"+itoa(plain)+"/items/new", struct{}{}).patches()
	// Every item can carry LoRAs, whatever its group offers.
	mustContain(t, "plain form", page, `name="code_name"`, `name="positive_prompt"`, "<legend>LoRAs", `id="lora-search"`, `id="lora-rows"`, `id="lora-row-template"`)
	mustNotContain(t, "plain form", page, "<legend>ControlNet", "<legend>IP-Adapter", "<legend>Mask regions", "<legend>Box regions", "<legend>Output settings")

	page = u.datastar("GET", "/ui/groups/"+itoa(full)+"/items/new", struct{}{}).patches()
	mustContain(t, "full form", page, "<legend>LoRAs", "<legend>ControlNet", "<legend>IP-Adapter", "<legend>Mask regions", "<legend>Box regions", "<legend>Output settings",
		`name="cn_2_type"`, `<option value="openpose"`)
	mustNotContain(t, "full form", page, `name="cn_3_type"`)

	if r := u.get("/groups/" + itoa(plain) + "/items"); r.Status != 200 {
		t.Errorf("items page: %d", r.Status)
	} else {
		mustContain(t, "items page", r.Body, "No items yet", "Plain")
	}
	if r := u.get("/groups/99/items"); r.Status != 404 {
		t.Errorf("missing group: %d", r.Status)
	}
	if r := u.get("/groups/x/items"); r.Status != 404 {
		t.Errorf("bad id: %d", r.Status)
	}
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

func TestItemScreen(t *testing.T) {
	u := newTestUI(t)
	gid := u.ui.newTestGroup(t, domain.Group{Name: "Full", CodeName: "full", UseControlnet: true, UseIPAdapter: true, UseMaskRegion: true, UseCoordinatesRegion: true, UseTypeAttributes: true})
	path := "/ui/groups/" + itoa(gid) + "/items"

	// Create with everything.
	r := u.submit("POST", path,
		field("id", "0"), field("code_name", "alice"), field("name", "Alice"), field("positive_prompt", "alice, smiling"), field("negative_prompt", "blurry"), field("tags", "Combat, hero"),
		file("thumbnail", "t.png", png1()),
		field("lora_name", "dance.safetensors"), field("lora_model", "0.8"), field("lora_clip", "0.7"),
		field("lora_name", "fox.safetensors"), field("lora_model", ""), field("lora_clip", ""),
		field("ip_enabled", "on"), field("ip_weight", "0.6"), field("ip_weight_type", "linear"), field("ip_start_at", "0"), field("ip_end_at", "1"),
		field("ip_clip_vision_model", "clip.safetensors"), field("ip_model_name", "ipa.safetensors"), file("ip_adapter_image", "ip.png", png1()),
		field("cn_0_type", ""), field("cn_1_type", "openpose"), field("cn_1_strength", "0.9"), field("cn_1_reference", "on"), file("cn_1_image", "pose.png", png1()),
		field("cn_2_type", "canny"), field("cn_2_strength", "1"), file("cn_2_image", "canny.png", png1()),
		file("mask_reference", "regions.png", twoColours()),
		field("coords", "left 512 1024 0 0\nright 512 1024 512 0"),
		field("output_type", "image"), field("output_attributes", `{"width": 768, "height": 1152}`),
	)
	if !strings.Contains(r.patches(), "Item saved.") {
		t.Fatalf("not saved:\n%s", r.patches())
	}
	items, err := u.env.Services.Items.ListByGroup(t.Context(), gid, domain.Page{})
	check(t, err)
	if items.Total != 1 {
		t.Fatalf("items = %d", items.Total)
	}
	it := items.Items[0]
	if it.PositivePrompt != "alice, smiling" || len(it.Tags) != 2 || it.Tags[0] != "combat" {
		t.Errorf("item = %+v", it)
	}
	if len(it.LoRAs) != 2 || it.LoRAs[0].StrengthClip != 0.7 || it.LoRAs[1].StrengthModel != 1 {
		t.Errorf("loras = %+v", it.LoRAs)
	}
	if it.IPAdapter == nil || it.IPAdapter.Weight != 0.6 || it.IPAdapter.ImageFile == "" || it.IPAdapter.ModelName != "ipa.safetensors" {
		t.Errorf("ip adapter = %+v", it.IPAdapter)
	}
	// The empty first slot is skipped, and the files follow their slots.
	if len(it.ControlNets) != 2 || it.ControlNets[0].Type != "openpose" || !it.ControlNets[0].IsReference ||
		!strings.Contains(it.ControlNets[0].ImagePath, "pose.png") || it.ControlNets[1].Type != "canny" || !strings.Contains(it.ControlNets[1].ImagePath, "canny.png") {
		t.Errorf("controlnets = %+v", it.ControlNets)
	}
	if it.MaskRegion == nil || len(it.MaskRegion.MaskFiles) != 3 {
		t.Errorf("mask region = %+v", it.MaskRegion)
	}
	if len(it.CoordinatedRegions) != 2 || it.CoordinatedRegions[1].X != 512 {
		t.Errorf("regions = %+v", it.CoordinatedRegions)
	}
	if it.OutputType != domain.OutputImage || it.OutputTypeAttributes["width"].Int != 768 {
		t.Errorf("output = %v %v", it.OutputType, it.OutputTypeAttributes)
	}

	// The edit dialog shows all of it, and the mask keywords found.
	edit := u.datastar("GET", "/ui/items/1/edit", struct{}{}).patches()
	mustContain(t, "edit", edit, `value="alice"`, `<input type="hidden" name="lora_name" value="dance.safetensors">`, `name="lora_model" value="0.8"`, `name="lora_clip" value="0.7"`,
		`name="lora_model" value="1"`, `data-lora-remove`, `value="ipa.safetensors"`, `<option value="openpose" selected>`,
		"Regions found:", `<span class="chip">red</span>`, `<span class="chip">blue</span>`, `<span class="chip">background</span>`, "left 512 1024 0 0", `"width"`,
		`<img class="thumb" src="/files/thumbnails/`, `<img class="thumb" src="/files/ipadapter/`)

	// Saving it back unchanged keeps every file (no upload, hidden paths).
	before := len(stored(t, u.env.Root))
	r = u.submit("POST", path,
		field("id", "1"), field("code_name", "alice"), field("name", "Alice"), field("positive_prompt", "alice"), field("tags", "combat"),
		field("lora_name", "dance.safetensors"), field("lora_model", "0.8"), field("lora_clip", "0.7"),
		field("ip_enabled", "on"), field("ip_weight", "0.6"), field("ip_weight_type", "linear"), field("ip_start_at", "0"), field("ip_end_at", "1"),
		field("ip_clip_vision_model", "clip.safetensors"), field("ip_model_name", "ipa.safetensors"),
		field("cn_0_type", "openpose"), field("cn_0_strength", "0.9"), field("cn_0_reference", "on"), field("cn_0_keep", it.ControlNets[0].ImagePath),
		field("cn_1_type", "canny"), field("cn_1_strength", "1"), field("cn_1_keep", it.ControlNets[1].ImagePath),
		field("coords", ""), field("output_type", ""), field("output_attributes", ""),
	)
	if !strings.Contains(r.patches(), "Item saved.") {
		t.Fatalf("not saved:\n%s", r.patches())
	}
	got, _ := u.env.Services.Items.Get(t.Context(), 1)
	if got.PositivePrompt != "alice" || got.Thumbnail != it.Thumbnail || got.IPAdapter.ImageFile != it.IPAdapter.ImageFile ||
		got.ControlNets[0].ImagePath != it.ControlNets[0].ImagePath || got.MaskRegion == nil || len(got.MaskRegion.MaskFiles) != 3 {
		t.Errorf("an unchanged save lost something: %+v", got)
	}
	if len(got.CoordinatedRegions) != 0 || got.OutputType != "" || len(got.LoRAs) != 1 {
		t.Errorf("cleared fields stayed: %+v", got)
	}
	if after := len(stored(t, u.env.Root)); after != before {
		t.Errorf("files before = %d, after = %d", before, after)
	}

	// Dropping the first ControlNet removes its file and keeps the other.
	r = u.submit("POST", path,
		field("id", "1"), field("code_name", "alice"), field("name", "Alice"),
		field("cn_1_type", "canny"), field("cn_1_strength", "1"), field("cn_1_keep", it.ControlNets[1].ImagePath),
		field("ip_enabled", "on"), field("ip_weight", "0.6"), field("clear_mask", "on"),
	)
	got, _ = u.env.Services.Items.Get(t.Context(), 1)
	if len(got.ControlNets) != 1 || got.ControlNets[0].ImagePath != it.ControlNets[1].ImagePath || got.MaskRegion != nil {
		t.Fatalf("item = %+v\n%s", got, r.patches())
	}
	if _, err := os.Stat(it.ControlNets[0].ImagePath); !os.IsNotExist(err) {
		t.Error("the dropped ControlNet's image was kept")
	}
	if len(stored(t, u.env.Dirs.Regions)) != 0 {
		t.Errorf("the mask files were kept: %v", stored(t, u.env.Dirs.Regions))
	}

	// A client cannot name a file through a hidden field.
	r = u.submit("POST", path, field("id", "1"), field("code_name", "alice"), field("name", "Alice"), field("cn_0_type", "canny"), field("cn_0_keep", "/etc/passwd"))
	mustContain(t, "named file", r.patches(), "selector #items-error", "file paths cannot be set")

	// Failures name the problem.
	for name, parts := range map[string][]part{
		"bad LoRA strength":  {field("code_name", "x"), field("name", "x"), field("lora_name", "a.safetensors"), field("lora_model", "high"), field("lora_clip", "1")},
		"infinite strength":  {field("code_name", "x"), field("name", "x"), field("lora_name", "a.safetensors"), field("lora_model", "1"), field("lora_clip", "Inf")},
		"LoRA without name":  {field("code_name", "x"), field("name", "x"), field("lora_name", " "), field("lora_model", "1"), field("lora_clip", "1")},
		"bad region":         {field("code_name", "x"), field("name", "x"), field("coords", "left 512")},
		"bad attributes":     {field("code_name", "x"), field("name", "x"), field("output_attributes", "{")},
		"bad number":         {field("code_name", "x"), field("name", "x"), field("ip_enabled", "on"), field("ip_weight", "heavy")},
		"duplicate code":     {field("code_name", "alice"), field("name", "x")},
		"mask is a jpeg":     {field("code_name", "x"), field("name", "x"), file("mask_reference", "m.jpg", []byte("\xff\xd8\xff\xe0\x00\x10JFIF\x00"))},
		"mask has no region": {field("code_name", "x"), field("name", "x"), file("mask_reference", "m.png", png1())},
	} {
		r := u.submit("POST", path, append([]part{field("id", "0")}, parts...)...)
		if !strings.Contains(r.patches(), "selector #items-error") {
			t.Errorf("%s: no error in the dialog:\n%s", name, r.patches())
		}
	}
	if list, _ := u.env.Services.Items.ListByGroup(t.Context(), gid, domain.Page{}); list.Total != 1 {
		t.Errorf("a refused save created an item: %d", list.Total)
	}

	// An item of another group cannot be saved through this one.
	other := u.ui.newTestGroup(t, domain.Group{Name: "Other", CodeName: "other"})
	r = u.submit("POST", "/ui/groups/"+itoa(other)+"/items", field("id", "1"), field("code_name", "alice"), field("name", "Alice"))
	mustContain(t, "other group", r.patches(), "belongs to another group")

	// Delete removes its files.
	r = u.datastar("DELETE", "/ui/items/1", struct{}{})
	mustContain(t, "delete", r.patches(), "Item deleted.", "No items yet")
	if files := stored(t, u.env.Root); len(files) != 0 {
		t.Errorf("files left after the delete: %v", files)
	}
}

func TestTagPanel(t *testing.T) {
	u := newTestUI(t)
	gid := u.ui.newTestGroup(t, domain.Group{Name: "G", CodeName: "g", Tags: []string{"people"}})
	for _, tag := range []string{"calm", "combat"} {
		_, err := u.env.Services.Items.Create(t.Context(), gid, domain.Item{CodeName: tag, Name: tag, Tags: []string{tag}})
		check(t, err)
	}
	project, _ := u.env.Services.Projects.Create(t.Context(), domain.Project{Name: "p"})
	_, err := u.env.Services.Servers.Create(t.Context(), domain.Server{Name: "s", Host: "http://s:1", CodeName: "gpu1", MaxConcurrentJobs: 1})
	check(t, err)
	_, err = u.env.Services.Generators.Create(t.Context(), domain.Generator{Name: "a", CodeName: "anime", Workflow: []byte(example(t, "creators", "illustrious_xl.json"))})
	check(t, err)
	_, err = u.env.Services.Commands.Add(t.Context(), project.ID, serviceCommand("gpu1 -$ anime: g where #calm"))
	check(t, err)

	page := u.get("/groups").Body
	mustContain(t, "panel", page, `data-tag="calm"`, `data-tag="combat"`, `data-tag="people"`)
	mustNotContain(t, "panel", page, `from='calm'`) // the tag is data, not part of an expression

	r := u.datastar("POST", "/ui/tags/rename", map[string]any{"tags": map[string]any{"scope": "item", "from": "calm", "to": "quiet"}})
	mustContain(t, "rename", r.patches(), `data-tag="quiet"`, "These commands mention", "where #calm", "Tag renamed.")
	mustNotContain(t, "rename", r.patches(), `data-tag="calm"`)
	r = u.datastar("POST", "/ui/tags/rename", map[string]any{"tags": map[string]any{"scope": "item", "from": "ghost", "to": "x"}})
	mustContain(t, "unknown tag", r.patches(), "selector #tags-error")
	r = u.datastar("POST", "/ui/tags/rename", map[string]any{"tags": map[string]any{"scope": "group", "from": "people", "to": "folk"}})
	mustContain(t, "group tag", r.patches(), `data-tag="folk"`)
}
