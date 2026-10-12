package api

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/rm4n0s/bos/internal/civitai"
	"github.com/rm4n0s/bos/internal/service"
	"github.com/rm4n0s/errors"
)

// stubCivitai answers for models 7 and 8 and refuses the rest.
type stubCivitai struct{}

func (stubCivitai) Configured() bool { return true }
func (stubCivitai) Download(_ context.Context, id, version int, _ string) (civitai.Model, error) {
	switch id {
	case 7:
		return civitai.Model{ID: 7, Requested: version, Name: "Dancing Fox", TriggerWords: []string{"dancing fox"}}, nil
	case 8:
		return civitai.Model{ID: 8, Requested: version, Name: "Sleepy Owl", Skipped: true}, nil
	}
	return civitai.Model{}, errors.New("CivitaiModelNotFound", "there is no such model", "model_id", id)
}
func (stubCivitai) Install(_ context.Context, m civitai.Model, _ civitai.Target, sub string) (string, error) {
	return civitai.ModelName(m.ID, m.Requested, sub), nil
}

// withCivitai is an API whose Civitai is a stub. The second result is the code
// name of the server the imports upload to; the test adds that server.
func withCivitai(t *testing.T) (*testAPI, string) {
	t.Helper()
	a := newTestAPIWith(t, []func(*service.Deps){func(d *service.Deps) {
		d.Civitai, d.Dirs.CivitaiCache, d.Pause = stubCivitai{}, filepath.Join(t.TempDir(), "cache"), time.Millisecond
	}})
	return a, "gpu1"
}

func addServer(a *testAPI, code string) {
	a.t.Helper()
	a.expect(a.do("POST", "/api/v1/servers", map[string]any{"name": code, "host": "http://" + code + ":8188", "code_name": code}), 201, "")
}

func TestCivitaiRoutes(t *testing.T) {
	a, server := withCivitai(t)
	addServer(a, server)

	r := a.expect(a.do("POST", "/api/v1/groups/import-civitai", map[string]any{
		"name": "Foxes", "code_name": "foxes", "tags": []string{"Animals"}, "server": server, "lora_subfolder": "civitai",
		"models": []map[string]any{{"model_id": 7, "model_strength": 0.7}, {"model_id": 8, "model_strength": 1, "model_clip": 0.5}, {"model_id": 99}},
	}), 202, "")
	if r.Header.Get("Location") != "/api/v1/groups/1" || r.json()["use_lora"] != true || r.json()["code_name"] != "foxes" {
		t.Errorf("accepted = %v, Location = %q", r.json(), r.Header.Get("Location"))
	}
	a.api.Services.Civitai.Wait()

	items := a.expect(a.do("GET", "/api/v1/groups/1/items", nil), 200, "").json()
	if items["total"] != float64(2) {
		t.Fatalf("items = %v", items)
	}
	lora := items["items"].([]any)[0].(map[string]any)["loras"].([]any)[0].(map[string]any)
	if lora["name"] != "civitai/7.safetensors" || lora["strength_model"] != 0.7 || lora["strength_clip"] != float64(1) {
		t.Errorf("lora = %v (a missing strength is 1)", lora)
	}

	// The progress is in the event log, with the group and the message.
	ev := a.expect(a.do("GET", "/api/v1/events", nil), 200, "").json()["events"].([]any)
	var types []string
	for _, e := range ev {
		m := e.(map[string]any)
		types = append(types, m["type"].(string))
		if m["group_id"] != float64(1) || m["message"] == "" {
			t.Errorf("event = %v", m)
		}
	}
	if len(types) != 4 || types[0] != "import_progress" || types[2] != "import_error" || types[3] != "import_finished" {
		t.Errorf("event types = %v", types)
	}

	// One more model into the group.
	r = a.expect(a.do("POST", "/api/v1/groups/1/items/civitai", map[string]any{"model_id": 7, "server": server, "lora_subfolder": "other"}), 202, "")
	if r.json()["id"] != float64(1) {
		t.Errorf("accepted = %v", r.json())
	}
	a.api.Services.Civitai.Wait()

	// A chosen version is its own item and its own file.
	a.expect(a.do("POST", "/api/v1/groups/1/items/civitai", map[string]any{"model_id": 7, "version_id": 71, "server": server, "lora_subfolder": "other"}), 202, "")
	a.api.Services.Civitai.Wait()
	list := a.expect(a.do("GET", "/api/v1/groups/1/items", nil), 200, "").json()["items"].([]any)
	last := list[len(list)-1].(map[string]any)
	if last["code_name"] != "7_71" || last["loras"].([]any)[0].(map[string]any)["name"] != "other/7-71.safetensors" {
		t.Errorf("item of a version = %v", last)
	}
}

func TestCivitaiRouteFailures(t *testing.T) {
	a, server := withCivitai(t)
	addServer(a, server)
	good := func(m map[string]any) map[string]any {
		out := map[string]any{"name": "G", "code_name": "g", "server": server, "models": []map[string]any{{"model_id": 7}}}
		for k, v := range m {
			out[k] = v
		}
		return out
	}
	tests := []struct {
		name   string
		path   string
		body   any
		status int
		tag    string
	}{
		{"no models", "/api/v1/groups/import-civitai", good(map[string]any{"models": []any{}}), 400, "InvalidRequest"},
		{"bad subfolder", "/api/v1/groups/import-civitai", good(map[string]any{"lora_subfolder": ".."}), 400, "InvalidRequest"},
		{"no server", "/api/v1/groups/import-civitai", good(map[string]any{"server": ""}), 400, "InvalidRequest"},
		{"unknown server", "/api/v1/groups/import-civitai", good(map[string]any{"server": "nope"}), 404, "ServerNotFound"},
		{"the folder of a ComfyUI is not a field any more", "/api/v1/groups/import-civitai", good(map[string]any{"comfyui_path": "/home/me/ComfyUI"}), 400, "InvalidRequest"},
		{"bad group name", "/api/v1/groups/import-civitai", good(map[string]any{"code_name": "bad name"}), 422, "CodeNameInvalid"},
		{"unknown field", "/api/v1/groups/import-civitai", good(map[string]any{"colour": 1}), 400, "InvalidRequest"},
		{"unknown group", "/api/v1/groups/99/items/civitai", map[string]any{"model_id": 7, "server": server}, 404, "GroupNotFound"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) { a.expect(a.do("POST", tc.path, tc.body), tc.status, tc.tag) })
	}
	if groups := a.do("GET", "/api/v1/groups", nil).json(); groups["total"] != float64(0) {
		t.Errorf("a refused import created a group: %v", groups)
	}

	// Not configured: 409, and nothing is created.
	plain := newTestAPI(t)
	plain.expect(plain.do("POST", "/api/v1/groups/import-civitai", good(nil)), 409, "CivitaiNotConfigured")
	plain.expect(plain.do("POST", "/api/v1/groups", map[string]any{"name": "G", "code_name": "g"}), 201, "")
	plain.expect(plain.do("POST", "/api/v1/groups/1/items/civitai", map[string]any{"model_id": 7, "server": server}), 409, "CivitaiNotConfigured")
}
