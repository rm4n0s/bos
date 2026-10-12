package ui

import (
	"strings"
	"testing"

	"github.com/rm4n0s/bos/internal/domain"
)

// lorasUI has the server gpu1, which holds dance and fox, and an item that
// carries dance.
func lorasUI(t *testing.T) (*testUI, domain.ID) {
	t.Helper()
	u := newTestUI(t)
	srv, err := u.env.Services.Servers.Create(t.Context(), domain.Server{Name: "GPU One", Host: "http://gpu1:8188", CodeName: "gpu1"})
	check(t, err)
	gid := u.ui.newTestGroup(t, domain.Group{Name: "Looks", CodeName: "looks"})
	_, err = u.env.Services.Items.Create(t.Context(), gid, domain.Item{CodeName: "alice", Name: "Alice", LoRAs: []domain.LoRA{{Name: "dance", StrengthModel: 0.8, StrengthClip: 0.7}}})
	check(t, err)
	return u, srv.ID
}

func TestLoRAPage(t *testing.T) {
	u, _ := lorasUI(t)
	page := u.get("/loras")
	if page.Status != 200 {
		t.Fatalf("status = %d", page.Status)
	}
	mustContain(t, "page", page.Body, "<h1>LoRAs</h1>", `aria-current="page">LoRAs</a>`, "dance.safetensors", "fox.safetensors", `<span class="chip">gpu1</span>`, `id="lora-detail"`)
	mustContain(t, "nav", u.get("/servers").Body, `<a href="/loras">LoRAs</a>`)

	// Search and paging keep the text.
	table := u.datastar("GET", "/ui/loras/table?q=FOX", struct{}{}).patches()
	mustContain(t, "search", table, "fox.safetensors")
	mustNotContain(t, "search", table, "dance.safetensors")
	mustContain(t, "no match", u.datastar("GET", "/ui/loras/table?q=zzz", struct{}{}).patches(), "No LoRA has this text in its name.")
	if r := u.datastar("GET", "/ui/loras/table?page=0", struct{}{}); !strings.Contains(r.patches(), "toast error") {
		t.Errorf("a bad page was accepted:\n%s", r.patches())
	}
}

func TestLoRAScriptIsServedAndLoaded(t *testing.T) {
	u, _ := lorasUI(t)
	mustContain(t, "script", u.get("/static/loras.js").Body, "lora-row-template", "data-lora-remove")
	mustContain(t, "layout", u.get("/loras").Body, `src="/static/loras.js"`)
}

func TestLoRAPageNamesTheServersItCouldNotAsk(t *testing.T) {
	u, id := lorasUI(t)
	u.env.Fleet.SetDown(id, true)
	mustContain(t, "offline", u.get("/loras").Body, "could not be asked", "<code>gpu1</code>", "dance.safetensors")
}

func TestLoRADetail(t *testing.T) {
	u, id := lorasUI(t)
	r := u.datastar("GET", "/ui/loras/detail?name=dance&q=da", struct{}{})
	mustContain(t, "detail", r.patches(), "selector #lora-detail", "<code>dance.safetensors</code>", "GPU One", "installed", "<code>looks</code>", "Alice", "0.8", "0.7",
		"Delete everywhere", "deletes 1 item with their files", "q=da")
	if r.signals()["loras"].(map[string]any)["open"] != true {
		t.Errorf("the dialog must open: %v", r.signals())
	}

	u.env.Fleet.SetDown(id, true)
	mustContain(t, "offline", u.datastar("GET", "/ui/loras/detail?name=dance", struct{}{}).patches(), "not asked: offline")
	u.env.Fleet.SetDown(id, false)

	mustContain(t, "unknown", u.datastar("GET", "/ui/loras/detail?name=nothing", struct{}{}).patches(), "toast error", "no item carries this LoRA")
}

func TestLoRADelete(t *testing.T) {
	u, id := lorasUI(t)
	// A server that cannot be asked stops the deletion and says which.
	u.env.Fleet.SetDown(id, true)
	r := u.datastar("DELETE", "/ui/loras?name=dance", struct{}{})
	mustContain(t, "down", r.patches(), "selector #lora-error", "gpu1")
	if items, _ := u.env.Services.Items.ListByGroup(t.Context(), 1, domain.Page{}); items.Total != 1 {
		t.Fatalf("an item was deleted although a server could not be asked")
	}
	u.env.Fleet.SetDown(id, false)

	r = u.datastar("DELETE", "/ui/loras?name=dance&q=", struct{}{})
	mustContain(t, "deleted", r.patches(), "dance.safetensors: deleted from 1 server, 1 item deleted.", "fox.safetensors")
	if r.signals()["loras"].(map[string]any)["open"] != false {
		t.Errorf("the dialog must close: %v", r.signals())
	}
	if items, _ := u.env.Services.Items.ListByGroup(t.Context(), 1, domain.Page{}); items.Total != 0 {
		t.Errorf("the item that carried the LoRA is still there")
	}
}

func TestLoRASuggestions(t *testing.T) {
	u, _ := lorasUI(t)
	r := u.datastar("GET", "/ui/loras/suggest?q=fo", struct{}{})
	mustContain(t, "suggest", r.patches(), "selector #lora-suggest", `data-name="fox.safetensors"`)
	mustNotContain(t, "suggest", r.patches(), "dance.safetensors")
	mustContain(t, "all", u.datastar("GET", "/ui/loras/suggest?q=", struct{}{}).patches(), `data-name="dance.safetensors"`, `data-name="fox.safetensors"`)
}

func TestItemDialogShowsTheLoRATable(t *testing.T) {
	u, _ := lorasUI(t)
	edit := u.datastar("GET", "/ui/items/1/edit", struct{}{}).patches()
	mustContain(t, "edit", edit, `<input type="hidden" name="lora_name" value="dance">`, `name="lora_model" value="0.8"`, `name="lora_clip" value="0.7"`, "data-lora-remove", `/ui/loras/suggest?q=`)
	mustNotContain(t, "edit", edit, "This item carries no LoRA.</p>\n  <label>Add a LoRA", "<table class=\"lora-table\" hidden")
	blank := u.datastar("GET", "/ui/groups/1/items/new", struct{}{}).patches()
	mustContain(t, "new", blank, `<table class="lora-table" hidden>`, "This item carries no LoRA.", `<template id="lora-row-template">`)
}

func TestReadLoRAs(t *testing.T) {
	f := &form{values: map[string][]string{
		"lora_name": {" a ", "b.safetensors", "c"}, "lora_model": {"0.5", "", "2"}, "lora_clip": {"0.25"},
	}}
	got, err := readLoRAs(f)
	check(t, err)
	want := []domain.LoRA{{Name: "a", StrengthModel: 0.5, StrengthClip: 0.25}, {Name: "b.safetensors", StrengthModel: 1, StrengthClip: 1}, {Name: "c", StrengthModel: 2, StrengthClip: 1}}
	if len(got) != 3 || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Errorf("loras = %+v, want %+v", got, want)
	}
	if got, err := readLoRAs(&form{values: map[string][]string{}}); err != nil || got != nil {
		t.Errorf("no rows = %v, %v", got, err)
	}
	for name, bad := range map[string]map[string][]string{
		"no name":  {"lora_name": {"  "}},
		"text":     {"lora_name": {"a"}, "lora_model": {"loud"}},
		"nan":      {"lora_name": {"a"}, "lora_clip": {"NaN"}},
		"infinite": {"lora_name": {"a"}, "lora_model": {"-Inf"}},
	} {
		if _, err := readLoRAs(&form{values: bad}); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
}
