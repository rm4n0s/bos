package cli

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/bos/internal/api"
	"github.com/rm4n0s/bos/internal/apptest"
)

// realServer is the real REST API over an in-memory core.
type realServer struct {
	t   *testing.T
	api *api.API
	srv *httptest.Server
	env *apptest.Env
}

func newRealServer(t *testing.T, token string) *realServer {
	t.Helper()
	env := apptest.New(t)
	a := api.New(api.Deps{
		Services: env.Services, Events: env.Events, Files: env.Files, Dirs: env.Dirs, AuthToken: token,
		UploadLimit: 1 << 20, Logger: env.Logger, KeepAlive: 50 * time.Millisecond,
	})
	srv := httptest.NewServer(a.Handler())
	t.Cleanup(srv.Close)
	t.Cleanup(a.Stop)
	return &realServer{t: t, api: a, srv: srv, env: env}
}

func (r *realServer) run(args ...string) (int, string, string) {
	r.t.Helper()
	var out, errOut bytes.Buffer
	code := Run(r.t.Context(), args, Env{
		Stdout: &out, Stderr: &errOut, Client: r.srv.Client(), Poll: time.Millisecond,
		Getenv: func(k string) string {
			if k == "BOS_SERVER" {
				return r.srv.URL
			}
			return ""
		},
	})
	return code, out.String(), errOut.String()
}

// must runs a command that has to succeed and returns what it printed as JSON.
func (r *realServer) must(args ...string) map[string]any {
	r.t.Helper()
	code, out, errOut := r.run(append([]string{"--json"}, args...)...)
	if code != 0 {
		r.t.Fatalf("bosctl %s: exit %d\n%s%s", strings.Join(args, " "), code, out, errOut)
	}
	var m map[string]any
	if json.Unmarshal([]byte(out), &m) != nil {
		return nil
	}
	return m
}

func png() []byte {
	return []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x01\x00\x00\x00\x01\x08\x02\x00\x00\x00")
}

// TestCLIAgainstTheRealAPI drives a whole session with bosctl and the real REST
// API behind it: what the stand-in server of the other tests cannot check is
// that the two agree.
func TestCLIAgainstTheRealAPI(t *testing.T) {
	r := newRealServer(t, "")
	dir := t.TempDir()
	wf := filepath.Join("..", "..", "examples", "workflows", "creators", "illustrious_xl.json")
	fixerWf := filepath.Join("..", "..", "examples", "workflows", "fixers", "simple_upscale.json")
	img := filepath.Join(dir, "t.png")
	if err := os.WriteFile(img, png(), 0o600); err != nil {
		t.Fatal(err)
	}

	// The catalogue.
	if s := r.must("server", "create", "--name", "GPU", "--host", "http://gpu1:8188", "--code-name", "gpu1"); s["code_name"] != "gpu1" || s["online"] != true {
		t.Errorf("server = %v", s)
	}
	r.must("server", "update", "1", "--comfyui-path", "/home/me/ComfyUI")
	r.must("server", "update", "1", "--max-jobs", "3")
	// An update changes only the flags given: the folder set before stays.
	if s := r.must("server", "show", "1"); s["max_concurrent_jobs"] != float64(3) || s["name"] != "GPU" || s["comfyui_path"] != "/home/me/ComfyUI" {
		t.Errorf("updated server = %v", s)
	}
	if loras := r.must("server", "loras", "1")["loras"].([]any); len(loras) != 2 {
		t.Errorf("loras = %v", loras)
	}
	if a := r.must("generator", "analyze", "--workflow", "@"+wf); a["output_type"] != "image" {
		t.Errorf("analysis = %v", a)
	}
	r.must("generator", "create", "--name", "Anime", "--code-name", "anime", "--workflow", "@"+wf)
	r.must("fixer", "create", "--name", "Up", "--code-name", "upscale", "--workflow", "@"+fixerWf, "--load-title", "Load Image", "--save-title", "Save Image")
	r.must("group", "create", "--name", "Characters", "--code-name", "characters", "--tags", "People", "--use-controlnet", "--thumbnail", img)
	if g := r.must("group", "show", "1"); g["thumbnail"] == "" || g["use_controlnet"] != true {
		t.Errorf("group = %v", g)
	}
	r.must("group", "update", "1", "--description", "everyone")
	if g := r.must("group", "show", "1"); g["description"] != "everyone" || g["use_controlnet"] != true || len(g["tags"].([]any)) != 1 || g["thumbnail"] == "" {
		t.Errorf("updated group = %v", g)
	}
	for _, c := range []string{"alice", "bob", "carol"} {
		r.must("item", "create", "1", "--code-name", c, "--name", c, "--positive-prompt", "portrait of "+c, "--tags", "person")
	}
	r.must("item", "create", "1", "--code-name", "dave", "--name", "Dave", "--controlnets", `[{"type":"openpose","strength":1}]`, "--controlnet-image", "0="+img)
	if it := r.must("item", "show", "4"); len(it["controlnets"].([]any)) != 1 || it["controlnets"].([]any)[0].(map[string]any)["image_path"] == "" {
		t.Errorf("item = %v", it)
	}
	r.must("item", "update", "1", "--positive-prompt", "alice, smiling")
	if it := r.must("item", "show", "1"); it["positive_prompt"] != "alice, smiling" || it["name"] != "alice" {
		t.Errorf("updated item = %v", it)
	}
	r.must("group", "import-text", "--name", "Places", "--code-name", "places", "--text", "a beach\na forest")
	if tags := r.must("tag", "list", "--scope", "item", "--prefix", "per")["tags"].([]any); len(tags) != 1 {
		t.Errorf("tags = %v", tags)
	}
	if res := r.must("tag", "rename", "--scope", "item", "--from", "person", "--to", "people"); res["renamed"] != float64(3) {
		t.Errorf("rename = %v", res)
	}

	// A project and its commands.
	r.must("project", "create", "--name", "Demo")
	code, out, _ := r.run("command", "preview", "gpu1 -$ anime: characters > upscale")
	if code != 0 || !strings.Contains(out, "The command is valid.") || !strings.Contains(out, "jobs:         4") {
		t.Errorf("preview: %d\n%s", code, out)
	}
	code, out, errOut := r.run("command", "add", "1", "gpu1 -$ anime: nowhere")
	if code != 1 || !strings.Contains(errOut, "CommandValidationFailed") || !strings.Contains(errOut, "nowhere") {
		t.Errorf("an invalid command: %d %q %q", code, out, errOut)
	}
	r.must("command", "add", "1", "gpu1 -$ anime: characters > upscale", "--name", "all")
	r.must("command", "add", "1", "gpu1 -$ anime: places", "--name", "places")
	if c := r.must("command", "move", "2", "--to", "1"); c["order"] != float64(1) {
		t.Errorf("moved = %v", c)
	}
	r.must("command", "edit", "1", "--name", "all of them")
	if c := r.must("command", "show", "1"); c["name"] != "all of them" || c["command_code"] != "gpu1 -$ anime: characters > upscale" {
		t.Errorf("edited command = %v", c)
	}
	code, out, _ = r.run("command", "list", "1")
	if code != 0 || !strings.Contains(out, "all of them") || !strings.Contains(out, "0/4") {
		t.Errorf("command list:\n%s", out)
	}
	r.must("command", "renumber", "1")

	// Jobs: a command makes its jobs when it is added.
	code, out, _ = r.run("job", "list", "1", "--status", "idle")
	if code != 0 || strings.Count(out, "idle") != 4 {
		t.Errorf("job list:\n%s", out)
	}
	r.must("job", "edit", "1", "--positive", "a better prompt")
	if j := r.must("job", "show", "1"); j["positive_prompt"] != "a better prompt" {
		t.Errorf("job = %v", j)
	}
	if j := r.must("job", "run", "1"); j["status"] != "queued" {
		t.Errorf("job run = %v", j)
	}
	if j := r.must("job", "stop", "1"); j["status"] != "idle" {
		t.Errorf("job stop = %v", j)
	}
	code, _, errOut = r.run("job", "stop", "1")
	if code != 1 || !strings.Contains(errOut, "JobNotRunnable") {
		t.Errorf("stopping an idle job: %d %q", code, errOut)
	}
	if c := r.must("command", "run", "1"); c["status"] != "queued" {
		t.Errorf("run = %v", c)
	}
	if c := r.must("command", "stop", "1"); c["status"] != "idle" {
		t.Errorf("stop = %v", c)
	}
	r.must("command", "recreate", "1")

	// Download refused, then done once the command is finished.
	code, _, errOut = r.run("command", "download", "1", "--output", filepath.Join(dir, "x.zip"))
	if code != 1 || !strings.Contains(errOut, "CommandNotFinished") {
		t.Errorf("download: %d %q", code, errOut)
	}

	// REPL and the contract.
	if j := r.must("repl", "run", "--on", "gpu1", "--generator", "anime", "--positive", "a fox", "--items", "characters(alice)"); j["status"] != "queued" || j["project_id"] != float64(-1) {
		t.Errorf("repl = %v", j)
	}
	if j := r.must("repl", "last"); !strings.Contains(j["positive_prompt"].(string), "a fox") {
		t.Errorf("repl last = %v", j)
	}
	if spec := r.must("openapi"); spec["openapi"] != "3.1.0" {
		t.Errorf("openapi = %v", spec["openapi"])
	}
	if code, out, _ := r.run("events"); code != 0 {
		t.Errorf("events: %d %q", code, out)
	}

	// Files and deletion.
	g := r.must("group", "show", "1")
	rel := strings.TrimPrefix(g["thumbnail"].(string), r.env.Dirs.Thumbnails+"/")
	saved := filepath.Join(dir, "got.png")
	if code, _, errOut := r.run("file", "get", "thumbnails", rel, "--output", saved); code != 0 {
		t.Fatalf("file get: %d %s", code, errOut)
	}
	if b, _ := os.ReadFile(saved); !bytes.Equal(b, png()) {
		t.Error("the downloaded thumbnail differs")
	}
	for _, args := range [][]string{{"item", "delete", "4"}, {"group", "delete", "1"}, {"command", "delete", "1"}, {"project", "delete", "1"}, {"fixer", "delete", "1"}, {"generator", "delete", "1"}, {"server", "delete", "1"}} {
		if code, _, errOut := r.run(args...); code != 0 {
			t.Errorf("bosctl %s: %d %s", strings.Join(args, " "), code, errOut)
		}
	}
	if code, _, _ := r.run("server", "show", "1"); code != 1 {
		t.Error("the server was not deleted")
	}
}

func TestCLIWithTheToken(t *testing.T) {
	r := newRealServer(t, "s3cret")
	if code, _, errOut := r.run("server", "list"); code != 1 || !strings.Contains(errOut, "Unauthorized") {
		t.Errorf("without a token: %d %q", code, errOut)
	}
	if code, _, errOut := r.run("server", "list", "--token", "s3cret"); code != 0 {
		t.Errorf("with the token: %d %q", code, errOut)
	}
}

// TestCLIRunWaitAgainstTheRealAPI follows a command while another goroutine
// finishes its jobs, as the scheduler would.
func TestCLIRunWaitAgainstTheRealAPI(t *testing.T) {
	r := newRealServer(t, "")
	wf := filepath.Join("..", "..", "examples", "workflows", "creators", "illustrious_xl.json")
	r.must("server", "create", "--name", "GPU", "--host", "http://gpu1:8188", "--code-name", "gpu1")
	r.must("generator", "create", "--name", "A", "--code-name", "anime", "--workflow", "@"+wf)
	r.must("group", "create", "--name", "C", "--code-name", "characters")
	for _, c := range []string{"alice", "bob"} {
		r.must("item", "create", "1", "--code-name", c, "--name", c)
	}
	r.must("project", "create", "--name", "p")
	r.must("command", "add", "1", "gpu1 -$ anime: characters")

	go func() {
		time.Sleep(60 * time.Millisecond)
		jobs, _ := r.env.DB.Repos().Jobs.AllByCommand(t.Context(), 1)
		for _, j := range jobs {
			j.Status = "finished"
			_ = r.env.DB.Repos().Jobs.Update(t.Context(), &j)
		}
		_ = r.env.DB.Repos().Commands.SetStatus(t.Context(), 1, "finished")
	}()
	code, out, errOut := r.run("command", "run", "1", "--wait", "--timeout", "20s")
	if code != 0 || !strings.Contains(out, "finished: 2 finished of 2 jobs") {
		t.Errorf("exit %d\n%s%s", code, out, errOut)
	}
}
