package ui

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/bos/internal/apptest"
	"github.com/rm4n0s/bos/internal/domain"
	"github.com/rm4n0s/bos/internal/service"
)

// civitaiUI is a UI whose Civitai is the fake one, without any server.
func civitaiUI(t *testing.T) *testUI {
	t.Helper()
	return newTestUIWith(t, []func(*service.Deps){func(d *service.Deps) {
		d.Civitai, d.Dirs.CivitaiCache, d.Pause = apptest.FakeCivitai{}, filepath.Join(t.TempDir(), "cache"), time.Millisecond
	}}, fast)
}

// withCivitai is civitaiUI with the server gpu1, whose code name is the second
// result: the server the imports upload to.
func withCivitai(t *testing.T) (*testUI, string) {
	t.Helper()
	u := civitaiUI(t)
	_, err := u.env.Services.Servers.Create(t.Context(), domain.Server{Name: "GPU One", Host: "http://gpu1:8188", CodeName: "gpu1"})
	check(t, err)
	return u, "gpu1"
}

func TestParseModels(t *testing.T) {
	got, err := parseModels("123\n 456 , 0.8 , 0.5 \n\n789, 0.7\r\n")
	check(t, err)
	want := []service.CivitaiModel{{ModelID: 123, StrengthModel: 1, StrengthClip: 1}, {ModelID: 456, StrengthModel: 0.8, StrengthClip: 0.5}, {ModelID: 789, StrengthModel: 0.7, StrengthClip: 1}}
	if len(got) != 3 || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Errorf("models = %+v", got)
	}
	got, err = parseModels("123/456\n789 / 12 , 0.5")
	check(t, err)
	if len(got) != 2 || got[0] != (service.CivitaiModel{ModelID: 123, VersionID: 456, StrengthModel: 1, StrengthClip: 1}) ||
		got[1] != (service.CivitaiModel{ModelID: 789, VersionID: 12, StrengthModel: 0.5, StrengthClip: 1}) {
		t.Errorf("models with a version = %+v", got)
	}
	for _, bad := range []string{"abc", "0", "-3", "12, loud", "12, 1, x", "12 13", "12/", "12/x", "12/0", "12/-1", "/5", "12/3/4"} {
		if _, err := parseModels(bad); err == nil {
			t.Errorf("%q was accepted", bad)
		}
	}
}

func TestCivitaiImportFromTheGroupsPage(t *testing.T) {
	u, server := withCivitai(t)
	mustContain(t, "page", u.get("/groups").Body, "Import from Civitai", `name="server"`, `name="models"`)
	mustContain(t, "dialog", u.datastar("GET", "/ui/groups/civitai/new", struct{}{}).patches(), `<div id="group-civitai-body">`, "Import LoRAs from Civitai")

	s := u.live("/ui/stream/toasts", nil)
	defer s.stop()

	r := u.submit("POST", "/ui/groups/civitai", field("name", "Foxes"), field("code_name", "foxes"), field("tags", "animals"),
		field("models", "7, 0.7\n8"), field("server", server), field("lora_subfolder", "civitai"))
	mustContain(t, "started", r.patches(), "The import started.", "Foxes", `<span class="chip">LoRA</span>`)
	if r.signals()["groups"].(map[string]any)["civitaiOpen"] != false {
		t.Errorf("the dialog must close: %v", r.signals())
	}

	// Each model is a message, and the end is another.
	got := s.until("2 of 2 models imported.")
	mustContain(t, "progress", got, "Imported Dancing Fox (1/2).", "Imported Sleepy Owl (2/2).", `class="toast finished"`)
	u.env.Services.Civitai.Wait()
	items, err := u.env.Services.Items.ListByGroup(t.Context(), 1, domain.Page{})
	check(t, err)
	if items.Total != 2 || items.Items[0].LoRAs[0].Name != "civitai/7.safetensors" || items.Items[0].LoRAs[0].StrengthModel != 0.7 {
		t.Errorf("items = %+v", items.Items)
	}

	// Failures stay in the dialog or arrive as an error message.
	for name, parts := range map[string][]part{
		"bad line":      {field("name", "A"), field("code_name", "a"), field("models", "abc"), field("server", server)},
		"no server":     {field("name", "A"), field("code_name", "a"), field("models", "7"), field("server", "nope")},
		"bad subfolder": {field("name", "A"), field("code_name", "a"), field("models", "7"), field("server", server), field("lora_subfolder", "../x")},
		"taken group":   {field("name", "A"), field("code_name", "foxes"), field("models", "7"), field("server", server)},
		"no models":     {field("name", "A"), field("code_name", "a"), field("models", ""), field("server", server)},
	} {
		r := u.submit("POST", "/ui/groups/civitai", parts...)
		mustContain(t, name, r.patches(), "selector #groups-civitai-error")
	}
}

func TestCivitaiItemsOnTheItemsPage(t *testing.T) {
	u, server := withCivitai(t)
	gid := u.ui.newTestGroup(t, domain.Group{Name: "Loras", CodeName: "loras", UseLora: true})
	path := "/groups/" + itoa(gid) + "/items"
	mustContain(t, "page", u.get(path).Body, "Add from Civitai", `data-group="`+itoa(gid)+`"`)

	s := u.live("/ui/stream/toasts", nil)
	defer s.stop()
	r := u.submit("POST", "/ui"+path+"/civitai", field("models", "7\n99"), field("server", server), field("lora_subfolder", "x"))
	mustContain(t, "started", r.patches(), "The import started.")
	if r.signals()["items"].(map[string]any)["civitaiOpen"] != false {
		t.Errorf("signals = %v", r.signals())
	}
	// The list of the group redraws itself with the new item, and the model
	// that does not exist is an error message.
	got := s.until("there is no such model on Civitai")
	mustContain(t, "events", got, `class="toast error"`, "Model 99:")
	got = s.until(`id="items-table"`)
	mustContain(t, "list", got, "selector #items-table[data-group=", "Dancing Fox", "<code>7</code>")

	// Refusals.
	mustContain(t, "unknown group", u.submit("POST", "/ui/groups/99/items/civitai", field("models", "7"), field("server", server)).patches(), "selector #items-civitai-error")
	mustContain(t, "no models", u.submit("POST", "/ui"+path+"/civitai", field("models", ""), field("server", server)).patches(), "selector #items-civitai-error")
	if u.datastar("GET", "/ui"+path+"/civitai/new", struct{}{}).signals()["items"].(map[string]any)["civitaiOpen"] != true {
		t.Error("the dialog did not open")
	}
}

func TestCivitaiNotConfiguredInTheInterface(t *testing.T) {
	u := newTestUI(t)
	r := u.submit("POST", "/ui/groups/civitai", field("name", "A"), field("code_name", "a"), field("models", "7"), field("server", "gpu1"))
	mustContain(t, "not configured", r.patches(), "selector #groups-civitai-error", "civitai_token")
}

// The import dialogs offer the servers by name, and the models are uploaded to
// the one that is chosen. With a single server it is chosen already.
func TestCivitaiDialogsOfferTheServers(t *testing.T) {
	u := civitaiUI(t)
	gid := u.ui.newTestGroup(t, domain.Group{Name: "Loras", CodeName: "loras", UseLora: true})
	groups, items := "/ui/groups/civitai/new", "/ui/groups/"+itoa(gid)+"/items/civitai/new"
	mk := func(name, code string) {
		_, err := u.env.Services.Servers.Create(t.Context(), domain.Server{Name: name, Host: "http://" + code + ":8188", CodeName: code})
		check(t, err)
	}

	// No server: a hint says where to add one, and there is nothing to choose.
	for _, p := range []string{groups, items} {
		got := u.datastar("GET", p, struct{}{}).patches()
		mustContain(t, "none "+p, got, "Add a", `href="/servers"`)
		mustNotContain(t, "none "+p, got, "<select", `name="server"`)
	}

	mk("Zeta box", "z")
	mk("Alpha box", "a")
	for _, p := range []string{groups, items} {
		got := u.datastar("GET", p, struct{}{}).patches()
		mustContain(t, "select "+p, got, `<select name="server" required>`, `<option value="a">Alpha box (a)</option>`, `<option value="z">Zeta box (z)</option>`, "choose a server")
		// By name, and nothing is chosen for the user when there are several.
		if strings.Index(got, "Alpha box") > strings.Index(got, "Zeta box") {
			t.Errorf("%s: the servers are not in order of name", p)
		}
		mustNotContain(t, "several "+p, got, ` selected`, "comfyui_path")
	}

	// The pages carry the same select, so the dialogs are right before they are opened.
	mustContain(t, "groups page", u.get("/groups").Body, `<option value="a">`)
	mustContain(t, "items page", u.get("/groups/"+itoa(gid)+"/items").Body, `<option value="a">`, `id="items-civitai-body"`)
	mustContain(t, "items dialog", u.datastar("GET", items, struct{}{}).patches(), `<div id="items-civitai-body">`)

	// A single server needs no choosing.
	u2, _ := withCivitai(t)
	got := u2.datastar("GET", groups, struct{}{}).patches()
	mustContain(t, "one server", got, `<option value="gpu1">GPU One (gpu1)</option>`)
	mustNotContain(t, "one server", got, "choose a server")
}
