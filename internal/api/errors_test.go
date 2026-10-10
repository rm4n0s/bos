package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rm4n0s/errors"
)

// seeded is an API with a server, a generator, a fixer, groups with items, a
// project and a command.
func seeded(t *testing.T) *testAPI {
	t.Helper()
	a := newTestAPI(t)
	ok := func(r reply) { t.Helper(); a.expect(r, 201, "") }
	ok(a.do("POST", "/api/v1/servers", map[string]any{"name": "GPU 1", "host": "http://gpu1:8188", "code_name": "gpu1"}))
	ok(a.do("POST", "/api/v1/generators", map[string]any{"name": "Anime", "code_name": "anime", "workflow": example(t, "creators", "illustrious_xl.json")}))
	ok(a.do("POST", "/api/v1/fixers", map[string]any{
		"name": "Upscale", "code_name": "upscale", "workflow": example(t, "fixers", "simple_upscale.json"),
		"load_image_title": "Load Image", "save_image_title": "Save Image",
	}))
	ok(a.do("POST", "/api/v1/groups", map[string]any{"name": "Characters", "code_name": "characters"}))
	ok(a.do("POST", "/api/v1/groups/1/items", map[string]any{"code_name": "a", "name": "Alice", "positive_prompt": "alice"}))
	ok(a.do("POST", "/api/v1/projects", map[string]any{"name": "Demo"}))
	ok(a.do("POST", "/api/v1/projects/1/commands", map[string]any{"name": "c", "code": "gpu1 -$ anime: characters"}))
	return a
}

// TestErrorEnvelope drives the failure of each notable tag of SPEC §15.2 and
// asserts its status and the envelope (acceptance 26).
func TestErrorEnvelope(t *testing.T) {
	a := seeded(t)
	noPositive := json.RawMessage(strings.ReplaceAll(string(example(t, "creators", "illustrious_xl.json")), `"Positive"`, `"Wanted text"`))
	badWorkflow := map[string]any{"name": "Bad", "code_name": "bad", "workflow": map[string]any{"1": map[string]any{"class_type": "KSampler"}}}

	tests := []struct {
		name         string
		method, path string
		body         any
		status       int
		tag          string
	}{
		{"bad id", "GET", "/api/v1/servers/abc", nil, 400, "InvalidRequest"},
		{"zero id", "GET", "/api/v1/servers/0", nil, 400, "InvalidRequest"},
		{"bad page", "GET", "/api/v1/servers?page=0", nil, 400, "InvalidRequest"},
		{"bad page size", "GET", "/api/v1/servers?page_size=x", nil, 400, "InvalidRequest"},
		{"unknown field", "POST", "/api/v1/projects", `{"name":"x","colour":"red"}`, 400, "InvalidRequest"},
		{"not json", "POST", "/api/v1/projects", `{"name":`, 400, "InvalidRequest"},
		{"empty body", "POST", "/api/v1/projects", ``, 400, "InvalidRequest"},
		{"trailing data", "POST", "/api/v1/projects", `{"name":"x"} {"name":"y"}`, 400, "InvalidRequest"},
		{"bad tag scope", "GET", "/api/v1/tags?scope=nope", nil, 400, "InvalidRequest"},
		{"bad job status", "GET", "/api/v1/commands/1/jobs?status=nope", nil, 400, "InvalidRequest"},

		{"server not found", "GET", "/api/v1/servers/99", nil, 404, "ServerNotFound"},
		{"generator not found", "GET", "/api/v1/generators/99", nil, 404, "GeneratorNotFound"},
		{"fixer not found", "GET", "/api/v1/fixers/99", nil, 404, "FixerNotFound"},
		{"group not found", "GET", "/api/v1/groups/99", nil, 404, "GroupNotFound"},
		{"items of a missing group", "GET", "/api/v1/groups/99/items", nil, 404, "GroupNotFound"},
		{"item not found", "GET", "/api/v1/items/99", nil, 404, "ItemNotFound"},
		{"project not found", "GET", "/api/v1/projects/99", nil, 404, "ProjectNotFound"},
		{"commands of a missing project", "GET", "/api/v1/projects/99/commands", nil, 404, "ProjectNotFound"},
		{"command not found", "GET", "/api/v1/commands/99", nil, 404, "CommandNotFound"},
		{"job not found", "GET", "/api/v1/jobs/99", nil, 404, "JobNotFound"},
		{"no previous REPL job", "GET", "/api/v1/repl/last", nil, 404, "JobNotFound"},
		{"file not found", "GET", "/api/v1/files/results/nope.png", nil, 404, "FileNotFound"},
		{"unknown file kind", "GET", "/api/v1/files/secrets/x", nil, 404, "FileNotFound"},
		{"tag not found", "POST", "/api/v1/tags/rename", map[string]any{"scope": "item", "from": "nope", "to": "x"}, 404, "TagNotFound"},

		{"code name invalid", "POST", "/api/v1/servers", map[string]any{"name": "x", "host": "http://x:1", "code_name": "bad name"}, 422, "CodeNameInvalid"},
		{"code name reserved", "POST", "/api/v1/groups", map[string]any{"name": "x", "code_name": "where"}, 422, "CodeNameReserved"},
		{"tag invalid", "POST", "/api/v1/tags/rename", map[string]any{"scope": "item", "from": "a", "to": "bad tag!"}, 422, "TagInvalid"},
		{"host invalid", "POST", "/api/v1/servers", map[string]any{"name": "x", "host": "ftp://x", "code_name": "x"}, 422, "ServerHostInvalid"},
		{"comfyui folder invalid", "POST", "/api/v1/servers", map[string]any{"name": "x", "host": "http://x:1", "code_name": "x", "comfyui_path": "/a\nb"}, 422, "ServerComfyPathInvalid"},
		{"name invalid", "POST", "/api/v1/projects", map[string]any{"name": ""}, 422, "NameInvalid"},

		{"server taken", "POST", "/api/v1/servers", map[string]any{"name": "x", "host": "http://x:1", "code_name": "gpu1"}, 409, "ServerCodeNameTaken"},
		{"generator taken", "POST", "/api/v1/generators", map[string]any{"name": "x", "code_name": "anime", "workflow": example(t, "creators", "illustrious_xl.json")}, 409, "GeneratorCodeNameTaken"},
		{"fixer taken", "POST", "/api/v1/fixers", map[string]any{"name": "x", "code_name": "upscale", "workflow": example(t, "fixers", "simple_upscale.json"), "load_image_title": "Load Image", "save_image_title": "Save Image"}, 409, "FixerCodeNameTaken"},
		{"group taken", "POST", "/api/v1/groups", map[string]any{"name": "x", "code_name": "characters"}, 409, "GroupCodeNameTaken"},
		{"item taken", "POST", "/api/v1/groups/1/items", map[string]any{"name": "x", "code_name": "a"}, 409, "ItemCodeNameTaken"},

		{"workflow invalid", "POST", "/api/v1/generators/analyze", map[string]any{"workflow": "not a graph"}, 422, "WorkflowInvalid"},
		{"generator workflow without an output", "POST", "/api/v1/generators", badWorkflow, 422, "OutputNodeNotFound"},
		{"generator without a positive prompt node", "POST", "/api/v1/generators", map[string]any{"name": "x", "code_name": "nopos", "workflow": noPositive}, 422, "GeneratorTitleRequired"},
		{"fixer without titles", "POST", "/api/v1/fixers", map[string]any{"name": "x", "code_name": "x", "workflow": example(t, "fixers", "simple_upscale.json")}, 422, "FixerTitleRequired"},

		{"lex error", "POST", "/api/v1/projects/1/commands", map[string]any{"name": "x", "code": "gpu1 -$ anime: characters @"}, 422, "CommandLexError"},
		{"syntax error", "POST", "/api/v1/projects/1/commands", map[string]any{"name": "x", "code": "gpu1 -$"}, 422, "CommandSyntaxError"},
		{"validation failed", "POST", "/api/v1/projects/1/commands", map[string]any{"name": "x", "code": "gpu1 -$ anime: nowhere"}, 422, "CommandValidationFailed"},

		{"repl invalid", "POST", "/api/v1/repl/run", map[string]any{"server": "gpu1", "generator": "anime", "items": "not valid"}, 422, "ReplInvalid"},
		{"repl unknown server", "POST", "/api/v1/repl/run", map[string]any{"server": "nope", "generator": "anime"}, 404, "ServerNotFound"},

		{"command not finished", "GET", "/api/v1/commands/1/download", nil, 409, "CommandNotFinished"},
		{"job not runnable", "POST", "/api/v1/jobs/1/stop", nil, 409, "JobNotRunnable"},
		{"loras of a missing server", "GET", "/api/v1/servers/99/loras", nil, 404, "ServerNotFound"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := a.do(tc.method, tc.path, tc.body)
			if r.Status != tc.status {
				t.Fatalf("status = %d, want %d\n%s", r.Status, tc.status, r.Body)
			}
			if tc.tag == "" {
				return
			}
			if r.tag() != tc.tag {
				t.Fatalf("tag = %q, want %q\n%s", r.tag(), tc.tag, r.Body)
			}
			if got, want := r.Header.Get("Content-Type"), "application/json; charset=utf-8"; got != want {
				t.Errorf("Content-Type = %q", got)
			}
			e := r.json()["error"].(map[string]any)
			if e["message"] == "" {
				t.Error("the envelope has no message")
			}
			if strings.Contains(string(r.Body), "stack") || strings.Contains(string(r.Body), "goexit") {
				t.Errorf("the envelope leaks the stack: %s", r.Body)
			}
		})
	}
}

// TestPreviewAndCreateDisagree is acceptance 27: the same invalid text is a 200
// with valid:false in the preview and a 422 when added.
func TestPreviewAndCreateDisagree(t *testing.T) {
	a := seeded(t)
	code := "gpu1 -$ anime: nowhere"
	pv := a.expect(a.do("POST", "/api/v1/commands/preview", map[string]any{"code": code}), 200, "").json()
	problems, _ := pv["errors"].([]any)
	if pv["valid"] != false || len(problems) == 0 {
		t.Fatalf("preview = %v", pv)
	}
	p := problems[0].(map[string]any)
	if p["rule"] == "" || p["message"] == "" || p["position"] == nil {
		t.Errorf("problem = %v", p)
	}
	r := a.expect(a.do("POST", "/api/v1/projects/1/commands", map[string]any{"name": "x", "code": code}), 422, "CommandValidationFailed")
	meta := r.json()["error"].(map[string]any)["metadata"].(map[string]any)
	if list, _ := meta["errors"].([]any); len(list) == 0 {
		t.Errorf("metadata = %v", meta)
	}
}

// TestInternalErrorsAreWithheld checks that a failure of the server's own
// storage reaches the client without its details.
func TestInternalErrorsAreWithheld(t *testing.T) {
	a := seeded(t)
	check(t, a.db.Close()) // every query now fails
	r := a.do("GET", "/api/v1/servers", nil)
	if r.Status != 500 || r.tag() != "StoreFailure" {
		t.Fatalf("status = %d, tag = %q\n%s", r.Status, r.tag(), r.Body)
	}
	e := r.json()["error"].(map[string]any)
	if e["message"] != "internal error" || e["metadata"] != nil {
		t.Errorf("envelope = %v", e)
	}
	if !strings.Contains(a.logs.String(), "StoreFailure") {
		t.Errorf("the failure was not logged:\n%s", a.logs.String())
	}
}

// TestNonApplicationErrors checks that an error of another kind is a 500 with
// a generic body, and that a panic does not escape.
func TestNonApplicationErrors(t *testing.T) {
	a := newTestAPI(t)
	r := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), "GET", "/", nil)
	a.api.writeError(r, req, http.ErrNotSupported)
	if r.Code != 500 || !strings.Contains(r.Body.String(), `"InternalError"`) || strings.Contains(r.Body.String(), "not supported") {
		t.Errorf("response = %d %s", r.Code, r.Body)
	}

	panicking := a.api.recoverer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic("boom") }))
	r = httptest.NewRecorder()
	panicking.ServeHTTP(r, req)
	if r.Code != 500 || strings.Contains(r.Body.String(), "boom") {
		t.Errorf("panic response = %d %s", r.Code, r.Body)
	}
	if !strings.Contains(a.logs.String(), "boom") {
		t.Error("the panic was not logged")
	}
}

func TestPublicMetadata(t *testing.T) {
	err := errors.New("FileNotFound", "x", "path", "/secret/place", "kind", "results", "bad", make(chan int))
	appErr, _ := errors.FromError(err)
	got := publicMetadata(appErr.Metadata)
	if _, leaked := got["path"]; leaked || got["kind"] != "results" {
		t.Errorf("metadata = %v", got)
	}
	if _, ok := got["bad"]; ok {
		t.Error("a value that is not JSON was kept")
	}
	if publicMetadata(nil) != nil {
		t.Error("no metadata must stay nil")
	}
}
