package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// recorded is a request the stand-in server saw.
type recorded struct {
	Method, Path, Query, ContentType, Auth string
	Body                                   []byte
}

// stub is a server that records requests and answers from a table.
type stub struct {
	t       *testing.T
	srv     *httptest.Server
	mu      sync.Mutex
	seen    []recorded
	answers map[string]answer // "METHOD /path" → answer
}

type answer struct {
	status int
	body   string
	header map[string]string
}

func newStub(t *testing.T) *stub {
	t.Helper()
	s := &stub{t: t, answers: map[string]answer{}}
	s.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		s.mu.Lock()
		s.seen = append(s.seen, recorded{r.Method, r.URL.Path, r.URL.RawQuery, r.Header.Get("Content-Type"), r.Header.Get("Authorization"), b})
		a, ok := s.answers[r.Method+" "+r.URL.Path]
		s.mu.Unlock()
		if !ok {
			a = answer{status: 200, body: `{"id":1}`}
		}
		for k, v := range a.header {
			w.Header().Set(k, v)
		}
		w.WriteHeader(a.status)
		_, _ = w.Write([]byte(a.body))
	}))
	t.Cleanup(s.srv.Close)
	return s
}

func (s *stub) last() recorded {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.seen) == 0 {
		s.t.Fatal("no request reached the server")
	}
	return s.seen[len(s.seen)-1]
}

// bosctl runs the command line against the stub.
func (s *stub) bosctl(args ...string) (code int, stdout, stderr string) {
	return s.bosctlIn("", args...)
}

func (s *stub) bosctlIn(stdin string, args ...string) (code int, stdout, stderr string) {
	s.t.Helper()
	var out, errOut bytes.Buffer
	env := Env{
		Stdin: strings.NewReader(stdin), Stdout: &out, Stderr: &errOut, Client: s.srv.Client(), Poll: time.Millisecond,
		Getenv: func(k string) string {
			if k == "BOS_SERVER" {
				return s.srv.URL
			}
			return ""
		},
	}
	code = Run(s.t.Context(), args, env)
	return code, out.String(), errOut.String()
}

func tmpFile(t *testing.T, name, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func jsonOf(t *testing.T, b []byte) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("not a JSON object: %v\n%s", err, b)
	}
	return m
}

func TestEveryCommandSendsTheRightRequest(t *testing.T) {
	wf := tmpFile(t, "wf.json", `{"3":{"class_type":"KSampler"}}`)
	prompt := tmpFile(t, "prompt.txt", "a long prompt")
	tests := []struct {
		name   string
		args   []string
		method string
		path   string
		query  string
		body   map[string]any // nil: no body; checked key by key
	}{
		{"server list", []string{"server", "list"}, "GET", "/api/v1/servers", "", nil},
		{"server list paged", []string{"server", "list", "--page", "2", "--page-size", "5"}, "GET", "/api/v1/servers", "page=2&page_size=5", nil},
		{"server show", []string{"server", "show", "3"}, "GET", "/api/v1/servers/3", "", nil},
		{"server create", []string{"server", "create", "--name", "GPU", "--host", "http://h:8188", "--code-name", "gpu1", "--local", "--max-jobs", "2"}, "POST", "/api/v1/servers", "",
			map[string]any{"name": "GPU", "host": "http://h:8188", "code_name": "gpu1", "is_local": true, "max_concurrent_jobs": float64(2)}},
		{"server delete", []string{"server", "delete", "4"}, "DELETE", "/api/v1/servers/4", "", nil},
		{"server loras", []string{"server", "loras", "4"}, "GET", "/api/v1/servers/4/loras", "", nil},
		{"lora list", []string{"lora", "list", "--q", "fox", "--page", "2"}, "GET", "/api/v1/loras", "page=2&q=fox", nil},
		{"lora show", []string{"lora", "show", "--name", "civitai/12.safetensors"}, "GET", "/api/v1/loras/detail", "name=civitai%2F12.safetensors", nil},
		{"lora delete", []string{"lora", "delete", "--name", "civitai/12.safetensors"}, "DELETE", "/api/v1/loras", "name=civitai%2F12.safetensors", nil},
		{"generator analyze", []string{"generator", "analyze", "--workflow", "@" + wf}, "POST", "/api/v1/generators/analyze", "", map[string]any{"workflow": map[string]any{"3": map[string]any{"class_type": "KSampler"}}}},
		{"generator create", []string{"generator", "create", "--name", "A", "--code-name", "a", "--workflow", "@" + wf, "--random-seed", "--attributes", `{"width":512}`}, "POST", "/api/v1/generators", "",
			map[string]any{"name": "A", "code_name": "a", "has_random_seed": true, "output_attributes": map[string]any{"width": float64(512)}}},
		{"generator delete", []string{"generator", "delete", "2"}, "DELETE", "/api/v1/generators/2", "", nil},
		{"fixer create", []string{"fixer", "create", "--name", "U", "--code-name", "u", "--workflow", "@" + wf, "--load-title", "Load", "--save-title", "Save"}, "POST", "/api/v1/fixers", "",
			map[string]any{"load_image_title": "Load", "save_image_title": "Save"}},
		{"fixer list", []string{"fixer", "list"}, "GET", "/api/v1/fixers", "", nil},
		{"group list by tag", []string{"group", "list", "--tag", "people"}, "GET", "/api/v1/groups", "tag=people", nil},
		{"group create", []string{"group", "create", "--name", "C", "--code-name", "c", "--tags", "a, b", "--use-controlnet"}, "POST", "/api/v1/groups", "",
			map[string]any{"name": "C", "code_name": "c", "tags": []any{"a", "b"}, "use_controlnet": true}},
		{"group import-text", []string{"group", "import-text", "--name", "P", "--code-name", "p", "--text", "@" + prompt}, "POST", "/api/v1/groups/import-text", "",
			map[string]any{"name": "P", "text": "a long prompt"}},
		{"group import-civitai", []string{"group", "import-civitai", "--name", "F", "--code-name", "f", "--model", "7/71:0.8:0.5", "--model", "8", "--upload-to", "gpu1", "--lora-subfolder", "civ"}, "POST", "/api/v1/groups/import-civitai", "",
			map[string]any{"server": "gpu1", "lora_subfolder": "civ", "models": []any{map[string]any{"model_id": float64(7), "version_id": float64(71), "model_strength": 0.8, "model_clip": 0.5}, map[string]any{"model_id": float64(8)}}}},
		{"item list", []string{"item", "list", "5"}, "GET", "/api/v1/groups/5/items", "", nil},
		{"item create", []string{"item", "create", "5", "--code-name", "alice", "--name", "Alice", "--positive-prompt", "@" + prompt, "--loras", "dance.safetensors:0.8,fox.safetensors"}, "POST", "/api/v1/groups/5/items", "",
			map[string]any{"code_name": "alice", "positive_prompt": "a long prompt", "loras": []any{
				map[string]any{"name": "dance.safetensors", "strength_model": 0.8, "strength_clip": float64(1)},
				map[string]any{"name": "fox.safetensors", "strength_model": float64(1), "strength_clip": float64(1)}}}},
		{"item add-civitai", []string{"item", "add-civitai", "5", "--model", "7", "--upload-to", "gpu1"}, "POST", "/api/v1/groups/5/items/civitai", "", map[string]any{"model_id": float64(7), "server": "gpu1"}},
		{"item add-civitai version", []string{"item", "add-civitai", "5", "--model", "7", "--version", "71", "--upload-to", "gpu1"}, "POST", "/api/v1/groups/5/items/civitai", "", map[string]any{"model_id": float64(7), "version_id": float64(71), "server": "gpu1"}},
		{"item delete", []string{"item", "delete", "9"}, "DELETE", "/api/v1/items/9", "", nil},
		{"tag list", []string{"tag", "list", "--scope", "item", "--prefix", "co"}, "GET", "/api/v1/tags", "prefix=co&scope=item", nil},
		{"tag rename", []string{"tag", "rename", "--scope", "item", "--from", "calm", "--to", "quiet"}, "POST", "/api/v1/tags/rename", "", map[string]any{"scope": "item", "from": "calm", "to": "quiet"}},
		{"project create", []string{"project", "create", "--name", "Demo"}, "POST", "/api/v1/projects", "", map[string]any{"name": "Demo"}},
		{"project delete", []string{"project", "delete", "1"}, "DELETE", "/api/v1/projects/1", "", nil},
		{"command list", []string{"command", "list", "1"}, "GET", "/api/v1/projects/1/commands", "", nil},
		{"command preview argument", []string{"command", "preview", "gpu1 -$ g: a"}, "POST", "/api/v1/commands/preview", "", map[string]any{"code": "gpu1 -$ g: a"}},
		{"command preview flag", []string{"command", "preview", "--code", "gpu1 -$ g: b"}, "POST", "/api/v1/commands/preview", "", map[string]any{"code": "gpu1 -$ g: b"}},
		{"command add", []string{"command", "add", "1", "gpu1 -$ g: a", "--name", "first", "--at", "2"}, "POST", "/api/v1/projects/1/commands", "", map[string]any{"name": "first", "code": "gpu1 -$ g: a", "insert_at": float64(2)}},
		{"command move", []string{"command", "move", "3", "--to", "1"}, "POST", "/api/v1/commands/3/move", "", map[string]any{"order": float64(1)}},
		{"command renumber", []string{"command", "renumber", "1"}, "POST", "/api/v1/projects/1/commands/renumber", "", nil},
		{"command run", []string{"command", "run", "3"}, "POST", "/api/v1/commands/3/run", "", nil},
		{"command stop", []string{"command", "stop", "3"}, "POST", "/api/v1/commands/3/stop", "", nil},
		{"command recreate", []string{"command", "recreate", "3"}, "POST", "/api/v1/commands/3/recreate", "", nil},
		{"command delete", []string{"command", "delete", "3"}, "DELETE", "/api/v1/commands/3", "", nil},
		{"job list", []string{"job", "list", "3", "--status", "error"}, "GET", "/api/v1/commands/3/jobs", "status=error", nil},
		{"job show", []string{"job", "show", "8"}, "GET", "/api/v1/jobs/8", "", nil},
		{"job run", []string{"job", "run", "8"}, "POST", "/api/v1/jobs/8/run", "", nil},
		{"job stop", []string{"job", "stop", "8"}, "POST", "/api/v1/jobs/8/stop", "", nil},
		{"repl run", []string{"repl", "run", "--on", "gpu1", "--generator", "anime", "--positive", "a fox", "--items", "characters(a)", "--loras", "x.safetensors:0.5"}, "POST", "/api/v1/repl/run", "",
			map[string]any{"server": "gpu1", "generator": "anime", "positive": "a fox", "items": "characters(a)"}},
		{"repl last", []string{"repl", "last"}, "GET", "/api/v1/repl/last", "", nil},
		{"events", []string{"events", "--after", "5"}, "GET", "/api/v1/events", "after=5", nil},
		{"openapi", []string{"openapi"}, "GET", "/api/v1/openapi.json", "", nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := newStub(t)
			s.answers["GET /api/v1/events"] = answer{200, `{"events":[],"last_id":0,"gap":false}`, nil}
			code, out, errOut := s.bosctl(tc.args...)
			if code != 0 {
				t.Fatalf("exit %d\n%s%s", code, out, errOut)
			}
			got := s.last()
			if got.Method != tc.method || got.Path != tc.path || got.Query != tc.query {
				t.Fatalf("request = %s %s?%s, want %s %s?%s", got.Method, got.Path, got.Query, tc.method, tc.path, tc.query)
			}
			if tc.body == nil {
				if len(got.Body) != 0 {
					t.Errorf("a body was sent: %s", got.Body)
				}
				return
			}
			if got.ContentType != "application/json" {
				t.Errorf("Content-Type = %q", got.ContentType)
			}
			body := jsonOf(t, got.Body)
			for k, want := range tc.body {
				if wantJSON, _ := json.Marshal(want); func() string { g, _ := json.Marshal(body[k]); return string(g) }() != string(wantJSON) {
					t.Errorf("body[%s] = %v, want %v", k, body[k], want)
				}
			}
		})
	}
}

func TestTheTokenAndTheServerComeFromFlagsOrTheEnvironment(t *testing.T) {
	s := newStub(t)
	if code, _, e := s.bosctl("server", "list", "--token", "s3cret"); code != 0 || s.last().Auth != "Bearer s3cret" {
		t.Errorf("flag token: %d %q %s", code, s.last().Auth, e)
	}
	// The flag wins over the environment, before and after the command.
	other := newStub(t)
	var out, errOut bytes.Buffer
	code := Run(t.Context(), []string{"--server", other.srv.URL, "server", "list"}, Env{Stdout: &out, Stderr: &errOut, Client: s.srv.Client(), Getenv: func(k string) string {
		if k == "BOS_SERVER" {
			return s.srv.URL
		}
		return "env-token"
	}})
	if code != 0 || other.last().Auth != "Bearer env-token" {
		t.Errorf("environment token: %d %q", code, other.last().Auth)
	}
	if code, _, _ := s.bosctl("server", "list", "--server", other.srv.URL); code != 0 || other.last().Path != "/api/v1/servers" {
		t.Error("--server after the command was ignored")
	}
}

func TestMergingUpdatesChangeOnlyTheFlagsGiven(t *testing.T) {
	s := newStub(t)
	s.answers["GET /api/v1/servers/2"] = answer{200, `{"id":2,"name":"GPU","host":"http://h:1","code_name":"gpu1","is_local":false,"max_concurrent_jobs":1,"online":true,"created_at":"x"}`, nil}
	code, _, e := s.bosctl("server", "update", "2", "--max-jobs", "4")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, e)
	}
	got := s.last()
	if got.Method != "PUT" || got.Path != "/api/v1/servers/2" {
		t.Fatalf("request = %s %s", got.Method, got.Path)
	}
	body := jsonOf(t, got.Body)
	if body["max_concurrent_jobs"] != float64(4) || body["name"] != "GPU" || body["host"] != "http://h:1" || body["code_name"] != "gpu1" {
		t.Errorf("body = %v", body)
	}
	for _, readOnly := range []string{"id", "online", "created_at"} {
		if _, ok := body[readOnly]; ok {
			t.Errorf("the read-only field %s was sent back", readOnly)
		}
	}

	// A boolean can be turned off.
	s.answers["GET /api/v1/groups/3"] = answer{200, `{"id":3,"name":"G","code_name":"g","description":"","tags":["x"],"use_controlnet":true,"thumbnail":"/p"}`, nil}
	if code, _, e := s.bosctl("group", "update", "3", "--use-controlnet=false"); code != 0 {
		t.Fatalf("exit %d: %s", code, e)
	}
	body = jsonOf(t, s.last().Body)
	if body["use_controlnet"] != false || body["name"] != "G" || len(body["tags"].([]any)) != 1 {
		t.Errorf("body = %v", body)
	}

	// The command's text comes back from command_code and goes out as code.
	s.answers["GET /api/v1/commands/5"] = answer{200, `{"id":5,"name":"n","description":"d","command_code":"gpu1 -$ g: a","command_json":{}}`, nil}
	if code, _, e := s.bosctl("command", "edit", "5", "--name", "renamed"); code != 0 {
		t.Fatalf("exit %d: %s", code, e)
	}
	body = jsonOf(t, s.last().Body)
	if body["name"] != "renamed" || body["code"] != "gpu1 -$ g: a" || body["description"] != "d" {
		t.Errorf("body = %v", body)
	}
	if _, ok := body["command_json"]; ok {
		t.Error("command_json was sent back")
	}

	// A file replaces what the server has, and flags still win.
	file := tmpFile(t, "s.json", `{"name":"From file","host":"http://f:1","code_name":"ff"}`)
	n := len(s.seen)
	if code, _, e := s.bosctl("server", "update", "2", "--file", file, "--name", "Flag"); code != 0 {
		t.Fatalf("exit %d: %s", code, e)
	}
	if len(s.seen) != n+1 {
		t.Error("the resource was read although a file was given")
	}
	if body := jsonOf(t, s.last().Body); body["name"] != "Flag" || body["host"] != "http://f:1" {
		t.Errorf("body = %v", body)
	}
}

func TestBodyFromStandardInput(t *testing.T) {
	s := newStub(t)
	if code, _, e := s.bosctlIn(`{"name":"Piped"}`, "project", "create", "--file", "-"); code != 0 {
		t.Fatalf("exit %d: %s", code, e)
	}
	if body := jsonOf(t, s.last().Body); body["name"] != "Piped" {
		t.Errorf("body = %v", body)
	}
	if code, _, e := s.bosctlIn("a long prompt from stdin", "command", "preview", "--code", "@-"); code != 0 {
		t.Fatalf("exit %d: %s", code, e)
	}
	if body := jsonOf(t, s.last().Body); body["code"] != "a long prompt from stdin" {
		t.Errorf("body = %v", body)
	}
}

func TestUploadsAreMultipart(t *testing.T) {
	s := newStub(t)
	png := tmpFile(t, "t.png", "PNGDATA")
	cn := tmpFile(t, "pose.png", "POSE")
	code, _, e := s.bosctl("item", "create", "5", "--code-name", "a", "--name", "A", "--thumbnail", png, "--controlnet-image", "0="+cn,
		"--controlnets", `[{"type":"openpose","strength":1}]`)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, e)
	}
	got := s.last()
	media, params, err := mime.ParseMediaType(got.ContentType)
	if err != nil || media != "multipart/form-data" {
		t.Fatalf("Content-Type = %q", got.ContentType)
	}
	mr := multipart.NewReader(bytes.NewReader(got.Body), params["boundary"])
	parts := map[string]string{}
	for {
		p, err := mr.NextPart()
		if err != nil {
			break
		}
		b, _ := io.ReadAll(p)
		parts[p.FormName()] = string(b)
	}
	if parts["thumbnail"] != "PNGDATA" || parts["controlnet_0"] != "POSE" {
		t.Errorf("parts = %v", parts)
	}
	data := jsonOf(t, []byte(parts["data"]))
	if data["code_name"] != "a" || len(data["controlnets"].([]any)) != 1 {
		t.Errorf("data = %v", data)
	}
	if code, _, e := s.bosctl("group", "create", "--name", "G", "--code-name", "g", "--thumbnail", png); code != 0 || !strings.HasPrefix(s.last().ContentType, "multipart/form-data") {
		t.Errorf("group thumbnail: %d %s %s", code, s.last().ContentType, e)
	}
}

func TestListsAreTables(t *testing.T) {
	s := newStub(t)
	s.answers["GET /api/v1/servers"] = answer{200, `{"items":[{"id":1,"code_name":"gpu1","name":"GPU one","host":"http://h:1","max_concurrent_jobs":2,"online":true},{"id":2,"code_name":"gpu2","name":"GPU two","host":"http://g:1","max_concurrent_jobs":1,"online":false}],"total":42,"page":1,"page_size":2,"total_pages":21}`, nil}
	code, out, errOut := s.bosctl("server", "list")
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 3 || !strings.HasPrefix(lines[0], "ID") || !strings.Contains(lines[1], "gpu1") || !strings.Contains(lines[1], "yes") || !strings.Contains(lines[2], "no") {
		t.Errorf("table:\n%s", out)
	}
	if !strings.Contains(errOut, "page 1 of 21 (42 in all)") {
		t.Errorf("stderr = %q", errOut)
	}

	s.answers["GET /api/v1/projects/1/commands"] = answer{200, `{"items":[{"order":1,"id":4,"name":"first","status":"queued","finished_jobs":2,"total_jobs":9,"command_code":"gpu1 -$ g: characters"}],"total":1,"page":1,"page_size":20,"total_pages":1}`, nil}
	_, out, _ = s.bosctl("command", "list", "1")
	if !strings.Contains(out, "2/9") || !strings.Contains(out, "queued") {
		t.Errorf("command table:\n%s", out)
	}
	s.answers["GET /api/v1/groups"] = answer{200, `{"items":[],"total":0,"page":1,"page_size":20,"total_pages":0}`, nil}
	if _, out, _ := s.bosctl("group", "list"); strings.TrimSpace(out) != "(nothing)" {
		t.Errorf("empty list = %q", out)
	}
}

func TestOneResourceAndJSONOutput(t *testing.T) {
	s := newStub(t)
	long := strings.Repeat("x", 300)
	s.answers["GET /api/v1/generators/1"] = answer{200, `{"id":1,"name":"Anime","workflow":{"a":"` + long + `"},"has_random_seed":true,"output_attributes":{"width":512}}`, nil}
	_, out, _ := s.bosctl("generator", "show", "1")
	if !strings.HasPrefix(out, "id:") || !strings.Contains(out, "Anime") || !strings.Contains(out, "use --json for all of it") || strings.Contains(out, long) {
		t.Errorf("show:\n%s", out)
	}
	_, out, _ = s.bosctl("generator", "show", "1", "--json")
	if !strings.Contains(out, long) || !json.Valid([]byte(out)) {
		t.Errorf("--json did not print the whole answer:\n%s", out)
	}
	_, out, _ = s.bosctl("--json", "generator", "show", "1")
	if !json.Valid([]byte(out)) {
		t.Errorf("--json before the command: %s", out)
	}
	s.answers["DELETE /api/v1/servers/1"] = answer{204, "", nil}
	if code, out, errOut := s.bosctl("server", "delete", "1"); code != 0 || out != "" || !strings.Contains(errOut, "done") {
		t.Errorf("delete: %d %q %q", code, out, errOut)
	}
}

func TestPreviewIsReadable(t *testing.T) {
	s := newStub(t)
	s.answers["POST /api/v1/commands/preview"] = answer{200, `{"valid":true,"errors":[],"warnings":[{"message":"gpu2 is offline"}],"combinations":9,"after_where":9,"kept":9,"jobs":9,"dynamic":false,"per_server":[{"server":"gpu1","jobs":5},{"server":"gpu2","jobs":4}]}`, nil}
	code, out, _ := s.bosctl("command", "preview", "x")
	for _, want := range []string{"The command is valid.", "jobs:         9", "gpu1: 5 jobs", "warning: gpu2 is offline"} {
		if code != 0 || !strings.Contains(out, want) {
			t.Errorf("preview lacks %q:\n%s", want, out)
		}
	}
	s.answers["POST /api/v1/commands/preview"] = answer{200, `{"valid":false,"errors":[{"rule":"S1","message":"&& and || cannot be mixed","position":{"offset":34,"length":2}}],"warnings":[]}`, nil}
	_, out, _ = s.bosctl("command", "preview", "x")
	if !strings.Contains(out, "not valid") || !strings.Contains(out, "S1 at 34: && and || cannot be mixed") {
		t.Errorf("invalid preview:\n%s", out)
	}
}

func TestErrorsAreShownWithTheirTagAndExitOne(t *testing.T) {
	s := newStub(t)
	s.answers["GET /api/v1/servers/9"] = answer{404, `{"error":{"tag":"ServerNotFound","message":"server not found","metadata":{"id":9}}}`, nil}
	code, out, errOut := s.bosctl("server", "show", "9")
	if code != ExitServerError || out != "" || strings.TrimSpace(errOut) != "ServerNotFound: server not found" {
		t.Errorf("not found: %d %q %q", code, out, errOut)
	}
	s.answers["POST /api/v1/projects/1/commands"] = answer{422, `{"error":{"tag":"CommandValidationFailed","message":"command is not valid","metadata":{"errors":[{"rule":"SEM","message":"no such group nowhere","position":{"offset":15,"length":7}}]}}}`, nil}
	code, _, errOut = s.bosctl("command", "add", "1", "gpu1 -$ g: nowhere")
	if code != 1 || !strings.Contains(errOut, "CommandValidationFailed: command is not valid") || !strings.Contains(errOut, "  SEM at 15: no such group nowhere") {
		t.Errorf("validation: %d\n%s", code, errOut)
	}
	// --json prints the envelope as it came.
	code, out, _ = s.bosctl("command", "add", "1", "gpu1 -$ g: nowhere", "--json")
	if code != 1 || !strings.Contains(out, `"CommandValidationFailed"`) {
		t.Errorf("json error: %d %q", code, out)
	}
	// A server that is not bos.
	s.answers["GET /api/v1/jobs/1"] = answer{502, "<html>bad gateway</html>", nil}
	if code, _, errOut := s.bosctl("job", "show", "1"); code != 1 || !strings.Contains(errOut, "answered 502") {
		t.Errorf("other error: %d %q", code, errOut)
	}
	// The error of the read behind an update is reported the same way.
	s.answers["GET /api/v1/servers/7"] = answer{404, `{"error":{"tag":"ServerNotFound","message":"server not found"}}`, nil}
	if code, _, errOut := s.bosctl("server", "update", "7", "--name", "x"); code != 1 || !strings.Contains(errOut, "ServerNotFound") {
		t.Errorf("update of a missing server: %d %q", code, errOut)
	}
}

func TestAnUnreachableServerIsExitThree(t *testing.T) {
	s := newStub(t)
	url := s.srv.URL
	s.srv.Close()
	var out, errOut bytes.Buffer
	code := Run(t.Context(), []string{"--server", url, "server", "list"}, Env{Stdout: &out, Stderr: &errOut, Getenv: func(string) string { return "" }})
	if code != ExitUnreachable || !strings.Contains(errOut.String(), "cannot reach "+url) {
		t.Errorf("exit %d: %s", code, errOut.String())
	}
	if code := Run(t.Context(), []string{"--server", url, "server", "update", "1", "--name", "x"}, Env{Stdout: &out, Stderr: &errOut, Getenv: func(string) string { return "" }}); code != ExitUnreachable {
		t.Errorf("update against a dead server: exit %d", code)
	}
}

func TestUsageMistakesAreExitTwo(t *testing.T) {
	s := newStub(t)
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"unknown resource", []string{"nothing", "list"}, "there is no command"},
		{"unknown verb", []string{"server", "explode"}, "there is no command"},
		{"missing argument", []string{"server", "show"}, "takes the arguments ID"},
		{"too many arguments", []string{"server", "show", "1", "2"}, "takes the arguments ID"},
		{"bad id", []string{"server", "show", "abc"}, "positive number"},
		{"zero id", []string{"server", "show", "0"}, "positive number"},
		{"missing flags", []string{"server", "create", "--name", "x"}, "needs --host, --code-name"},
		{"unknown flag", []string{"server", "list", "--colour", "red"}, "flag provided but not defined"},
		{"bad number", []string{"server", "create", "--max-jobs", "many"}, "invalid value"},
		{"bad JSON", []string{"generator", "analyze", "--workflow", "{nope"}, "not valid JSON"},
		{"missing file", []string{"generator", "analyze", "--workflow", "@/no/such/file"}, "cannot read"},
		{"bad lora", []string{"item", "create", "1", "--code-name", "a", "--name", "a", "--loras", ":0.5"}, "name[:model strength"},
		{"bad model", []string{"group", "import-civitai", "--name", "a", "--code-name", "a", "--model", "x", "--upload-to", "gpu1"}, "id[/version][:model strength"},
		{"bad model version", []string{"group", "import-civitai", "--name", "a", "--code-name", "a", "--model", "7/x", "--upload-to", "gpu1"}, "id[/version][:model strength"},
		{"bad controlnet image", []string{"item", "create", "1", "--code-name", "a", "--name", "a", "--controlnet-image", "pose.png"}, "N=path"},
		{"bad timeout", []string{"command", "run", "1", "--wait", "--timeout", "soon"}, "duration"},
		{"file is not an object", []string{"server", "create", "--file", "-"}, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			code, _, errOut := s.bosctlIn("[1]", tc.args...)
			if code != ExitUsage || !strings.Contains(errOut, tc.want) {
				t.Errorf("exit %d, stderr:\n%s\nwant %q", code, errOut, tc.want)
			}
		})
	}
	if n := len(s.seen); n != 0 {
		t.Errorf("a mistake in the call reached the server (%d requests)", n)
	}
}

func TestHelp(t *testing.T) {
	s := newStub(t)
	code, out, _ := s.bosctl("help")
	if code != 0 || !strings.Contains(out, "Resources:") || !strings.Contains(out, "server") || !strings.Contains(out, "list, show, create") {
		t.Errorf("help:\n%s", out)
	}
	if code, out, _ := s.bosctl(); code != 0 || !strings.Contains(out, "Usage:") {
		t.Errorf("no arguments: %d\n%s", code, out)
	}
	code, out, _ = s.bosctl("server", "help")
	if code != 0 || !strings.Contains(out, "bosctl server delete") {
		t.Errorf("resource help:\n%s", out)
	}
	code, out, _ = s.bosctl("help", "server", "create")
	if code != 0 || !strings.Contains(out, "--code-name") || !strings.Contains(out, "bosctl server create [flags]") {
		t.Errorf("command help:\n%s", out)
	}
	if code, _, errOut := s.bosctl("nothing", "help"); code != ExitUsage || !strings.Contains(errOut, "no resource") {
		t.Errorf("help of nothing: %d %q", code, errOut)
	}
	if len(s.seen) != 0 {
		t.Error("help reached the server")
	}
}

func TestDownloadsAreSaved(t *testing.T) {
	s := newStub(t)
	s.answers["GET /api/v1/commands/4/download"] = answer{200, "ZIPBYTES", map[string]string{"Content-Type": "application/zip", "Content-Disposition": `attachment; filename="command-4.zip"`}}
	dir := t.TempDir()
	out := filepath.Join(dir, "mine.zip")
	code, _, errOut := s.bosctl("command", "download", "4", "--output", out)
	if b, err := os.ReadFile(out); code != 0 || err != nil || string(b) != "ZIPBYTES" || !strings.Contains(errOut, "saved "+out+" (8 bytes)") {
		t.Errorf("download: %d %v %q", code, err, errOut)
	}
	// To standard output.
	if code, stdout, _ := s.bosctl("command", "download", "4", "--output", "-"); code != 0 || stdout != "ZIPBYTES" {
		t.Errorf("stdout download: %d %q", code, stdout)
	}
	// By default, the name the server suggests, in the current folder.
	cwd, _ := os.Getwd()
	t.Chdir(dir)
	defer func() { _ = os.Chdir(cwd) }() // back for the other tests
	if code, _, _ := s.bosctl("command", "download", "4"); code != 0 {
		t.Fatal("download failed")
	}
	if b, err := os.ReadFile(filepath.Join(dir, "command-4.zip")); err != nil || string(b) != "ZIPBYTES" {
		t.Errorf("default name: %v", err)
	}
	// A file route keeps the name of the file.
	s.answers["GET /api/v1/files/results/12/fox.png"] = answer{200, "PNG", nil}
	if code, _, _ := s.bosctl("file", "get", "results", "12/fox.png"); code != 0 {
		t.Fatal("file get failed")
	}
	if b, err := os.ReadFile(filepath.Join(dir, "fox.png")); err != nil || string(b) != "PNG" {
		t.Errorf("file get: %v", err)
	}
	// A refused download writes nothing.
	s.answers["GET /api/v1/commands/5/download"] = answer{409, `{"error":{"tag":"CommandNoResults","message":"only a finished command can be downloaded"}}`, nil}
	code, _, errOut = s.bosctl("command", "download", "5", "--output", filepath.Join(dir, "no.zip"))
	if code != 1 || !strings.Contains(errOut, "CommandNoResults") {
		t.Errorf("refused download: %d %q", code, errOut)
	}
	if _, err := os.Stat(filepath.Join(dir, "no.zip")); err == nil {
		t.Error("a refused download wrote a file")
	}
}

func TestRunWaitFollowsTheCommandToTheEnd(t *testing.T) {
	var mu sync.Mutex
	round := 0
	statuses := []string{"queued", "processing", "processing", "finished"}
	jobs := []string{
		`{"items":[{"status":"queued"},{"status":"queued"}],"total_pages":1}`,
		`{"items":[{"status":"processing"},{"status":"queued"}],"total_pages":1}`,
		`{"items":[{"status":"processing"},{"status":"queued"}],"total_pages":1}`,
		`{"items":[{"status":"finished"},{"status":"finished"}],"total_pages":1}`,
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch {
		case r.Method == "POST" && r.URL.Path == "/api/v1/commands/3/run":
			w.WriteHeader(202)
			_, _ = w.Write([]byte(`{"id":3,"status":"queued"}`))
		case r.URL.Path == "/api/v1/commands/3":
			_, _ = w.Write([]byte(`{"id":3,"status":"` + statuses[min(round, 3)] + `"}`))
		case r.URL.Path == "/api/v1/commands/3/jobs":
			_, _ = w.Write([]byte(jobs[min(round, 3)]))
			round++
		}
	}))
	defer srv.Close()
	var out, errOut bytes.Buffer
	code := Run(t.Context(), []string{"--server", srv.URL, "command", "run", "3", "--wait"}, Env{Stdout: &out, Stderr: &errOut, Client: srv.Client(), Poll: time.Millisecond, Getenv: func(string) string { return "" }})
	if code != 0 {
		t.Fatalf("exit %d\n%s%s", code, out.String(), errOut.String())
	}
	for _, want := range []string{"id:", "queued: 2 queued of 2 jobs", "processing: 1 queued, 1 processing of 2 jobs", "finished: 2 finished of 2 jobs"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output lacks %q:\n%s", want, out.String())
		}
	}
	if n := strings.Count(out.String(), "processing: 1 queued, 1 processing"); n != 1 {
		t.Errorf("an unchanged state was printed %d times", n)
	}
}

func TestRunWaitEndsWithAFailureWhenAJobFails(t *testing.T) {
	for status, wantErr := range map[string]string{"error": "ended with errors", "idle": "is idle"} {
		t.Run(status, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/v1/commands/3/run":
					w.WriteHeader(202)
					_, _ = w.Write([]byte(`{"id":3}`))
				case "/api/v1/commands/3":
					_, _ = w.Write([]byte(`{"id":3,"status":"` + status + `"}`))
				default:
					_, _ = w.Write([]byte(`{"items":[{"status":"error"}],"total_pages":1}`))
				}
			}))
			defer srv.Close()
			var out, errOut bytes.Buffer
			code := Run(t.Context(), []string{"--server", srv.URL, "command", "run", "3", "--wait"}, Env{Stdout: &out, Stderr: &errOut, Client: srv.Client(), Poll: time.Millisecond, Getenv: func(string) string { return "" }})
			if code != ExitServerError || !strings.Contains(errOut.String(), wantErr) {
				t.Errorf("exit %d: %s", code, errOut.String())
			}
		})
	}
}

func TestRunWaitHonoursItsTimeoutAndCancellation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/commands/3/run":
			w.WriteHeader(202)
			_, _ = w.Write([]byte(`{"id":3}`))
		case "/api/v1/commands/3":
			_, _ = w.Write([]byte(`{"id":3,"status":"processing"}`))
		default:
			_, _ = w.Write([]byte(`{"items":[{"status":"processing"}],"total_pages":1}`))
		}
	}))
	defer srv.Close()
	run := func(ctx context.Context, extra ...string) (int, string) {
		var out, errOut bytes.Buffer
		code := Run(ctx, append([]string{"--server", srv.URL, "command", "run", "3", "--wait"}, extra...), Env{Stdout: &out, Stderr: &errOut, Client: srv.Client(), Poll: time.Millisecond, Getenv: func(string) string { return "" }})
		return code, errOut.String()
	}
	if code, e := run(t.Context(), "--timeout", "50ms"); code != ExitServerError || !strings.Contains(e, "stopped waiting") {
		t.Errorf("timeout: %d %q", code, e)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	if code, e := run(ctx); code != ExitServerError || !strings.Contains(e, "stopped waiting") {
		t.Errorf("cancel: %d %q", code, e)
	}
}

func TestEventsFollowReconnectsWhereItStopped(t *testing.T) {
	var mu sync.Mutex
	var headers []string
	connections := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		connections++
		n := connections
		headers = append(headers, r.Header.Get("Last-Event-ID"))
		mu.Unlock()
		if r.Header.Get("Accept") != "text/event-stream" {
			t.Errorf("Accept = %q", r.Header.Get("Accept"))
		}
		w.Header().Set("Content-Type", "text/event-stream")
		switch n {
		case 1:
			_, _ = w.Write([]byte(": keep-alive\n\nid: 1\nevent: processing\ndata: {\"job_id\":7,\"command_id\":3,\"project_id\":1,\"server\":\"gpu1\"}\n\nid: 2\nevent: finished\ndata: {\"job_id\":7,\"command_id\":3,\"project_id\":1,\"server\":\"gpu1\"}\n\n"))
		default:
			_, _ = w.Write([]byte("event: gap\ndata: {\"gap\":true}\n\nid: 3\nevent: import_progress\ndata: {\"group_id\":2,\"message\":\"Imported Fox (1/1).\"}\n\n"))
			w.(http.Flusher).Flush()
			<-r.Context().Done()
		}
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(t.Context())
	var out, errOut safeBuffer
	done := make(chan int, 1)
	go func() {
		done <- Run(ctx, []string{"--server", srv.URL, "events", "--follow"}, Env{Stdout: &out, Stderr: &errOut, Client: srv.Client(), Getenv: func(string) string { return "" }})
	}()
	deadline := time.After(10 * time.Second)
	for !strings.Contains(out.String(), "Imported Fox") {
		select {
		case <-deadline:
			t.Fatalf("no second connection:\n%s%s", out.String(), errOut.String())
		case <-time.After(10 * time.Millisecond):
		}
	}
	cancel()
	if code := <-done; code != 0 {
		t.Errorf("exit %d", code)
	}
	want := []string{"1 processing job=7 command=3 project=1 server=gpu1", "2 finished job=7 command=3 project=1 server=gpu1", "3 import_progress group=2 Imported Fox (1/1)."}
	for _, w := range want {
		if !strings.Contains(out.String(), w) {
			t.Errorf("output lacks %q:\n%s", w, out.String())
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if len(headers) < 2 || headers[0] != "" || headers[1] != "2" {
		t.Errorf("Last-Event-ID of the connections = %q: a reconnect must continue after the last event seen", headers)
	}
	if !strings.Contains(errOut.String(), "events were missed") {
		t.Errorf("the gap was not reported: %q", errOut.String())
	}
}

type safeBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *safeBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}
func (s *safeBuffer) String() string { s.mu.Lock(); defer s.mu.Unlock(); return s.b.String() }

func TestEventsFollowRefusedByTheServer(t *testing.T) {
	s := newStub(t)
	s.answers["GET /api/v1/events"] = answer{401, `{"error":{"tag":"Unauthorized","message":"a valid bearer token is required"}}`, nil}
	code, _, errOut := s.bosctl("events", "--follow")
	if code != 1 || !strings.Contains(errOut, "Unauthorized") {
		t.Errorf("exit %d: %q", code, errOut)
	}
}

func TestEventsAsATable(t *testing.T) {
	s := newStub(t)
	s.answers["GET /api/v1/events"] = answer{200, `{"events":[{"id":1,"type":"finished","job_id":4,"command_id":2,"project_id":1,"server":"gpu1"},{"id":2,"type":"import_finished","group_id":3,"message":"2 of 2 models imported.","job_id":0,"command_id":0,"project_id":0,"server":""}],"last_id":2,"gap":true}`, nil}
	code, out, errOut := s.bosctl("events")
	if code != 0 || !strings.Contains(out, "1 finished job=4 command=2 project=1 server=gpu1") || !strings.Contains(out, "2 import_finished group=3 2 of 2 models imported.") {
		t.Errorf("events:\n%s", out)
	}
	if !strings.Contains(errOut, "events were missed") {
		t.Errorf("gap = %q", errOut)
	}
	s.answers["GET /api/v1/events"] = answer{200, `{"events":[],"last_id":0,"gap":false}`, nil}
	if _, out, _ := s.bosctl("events"); !strings.Contains(out, "(no events)") {
		t.Errorf("empty = %q", out)
	}
}

// TestEveryCommandBuilds makes sure no command defines a flag that another one
// or a global flag already has (the flag package panics on that).
func TestEveryCommandBuilds(t *testing.T) {
	for _, c := range commands {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("%s %s: %v", c.Resource, c.Verb, r)
				}
			}()
			newFlags(c, Env{Getenv: func(string) string { return "" }}, globals{})
		}()
	}
}

var update = flag.Bool("update", false, "rewrite docs/cli.md")

// TestReferenceIsUpToDate keeps docs/cli.md in step with the command table.
func TestReferenceIsUpToDate(t *testing.T) {
	var buf bytes.Buffer
	Reference(&buf)
	file := filepath.Join("..", "..", "docs", "cli.md")
	if *update {
		if err := os.WriteFile(file, buf.Bytes(), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("%v (run go test ./internal/cli -update)", err)
	}
	if string(want) != buf.String() {
		t.Error("docs/cli.md is out of date; run go test ./internal/cli -update")
	}
}
