package main

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/bos/internal/comfy/comfytest"
)

// program is a running instance of the whole program, driven over HTTP.
type program struct {
	t      *testing.T
	base   string
	cancel context.CancelFunc
	done   chan error
}

// startProgram runs serve on the configuration of env, as `bos` does.
func startProgram(t *testing.T, env func(string) string) *program {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	p := &program{t: t, cancel: cancel, done: make(chan error, 1)}
	ready := make(chan net.Addr, 1)
	go func() { p.done <- serve(ctx, "", env, testLogger(t), func(a net.Addr) { ready <- a }, quick) }()
	select {
	case a := <-ready:
		p.base = "http://" + a.String()
	case err := <-p.done:
		t.Fatalf("serve returned early: %v", err)
	case <-time.After(10 * time.Second):
		t.Fatal("the program did not start")
	}
	t.Cleanup(func() { cancel(); <-p.done })
	return p
}

// stop sends the program the signal to end and waits for it.
func (p *program) stop() {
	p.t.Helper()
	p.cancel()
	select {
	case err := <-p.done:
		if err != nil {
			p.t.Fatalf("the program ended with %v", err)
		}
	case <-time.After(15 * time.Second):
		p.t.Fatal("the program did not stop")
	}
	p.done <- nil // for the cleanup that waits again
}

// call sends a JSON request to the REST API and decodes the answer.
func (p *program) call(method, path string, body any) (int, map[string]any) {
	p.t.Helper()
	var r io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			p.t.Fatal(err)
		}
		r = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(context.Background(), method, p.base+"/api/v1"+path, r)
	if err != nil {
		p.t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		p.t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }() // the answer was read
	b, err := io.ReadAll(res.Body)
	if err != nil {
		p.t.Fatal(err)
	}
	var out map[string]any
	_ = json.Unmarshal(b, &out) // a download or a 204 has no JSON object
	if res.Header.Get("Content-Type") == "application/zip" {
		out = map[string]any{"zip": b}
	}
	return res.StatusCode, out
}

func (p *program) must(status int, method, path string, body any) map[string]any {
	p.t.Helper()
	got, out := p.call(method, path, body)
	if got != status {
		p.t.Fatalf("%s %s: status = %d, want %d: %v", method, path, got, status, out)
	}
	return out
}

func readFile(t *testing.T, kind, name string) json.RawMessage {
	t.Helper()
	b, err := os.ReadFile(workflowFile(t, kind, name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// setup creates a server, a generator, a fixer, a group of three items and a
// project, all through the REST API.
func (p *program) setup(fakeURL string) {
	p.must(201, "POST", "/servers", map[string]any{"name": "Fake", "host": fakeURL, "code_name": "gpu1"})
	p.must(201, "POST", "/generators", map[string]any{"name": "Anime", "code_name": "anime", "workflow": readFile(p.t, "creators", "illustrious_xl.json")})
	p.must(201, "POST", "/fixers", map[string]any{
		"name": "Upscale", "code_name": "upscale", "workflow": readFile(p.t, "fixers", "simple_upscale.json"),
		"load_image_title": "Load Image", "save_image_title": "Save Image",
	})
	p.must(201, "POST", "/groups", map[string]any{"name": "Characters", "code_name": "characters"})
	for _, c := range []string{"alice", "bob", "carol"} {
		p.must(201, "POST", "/groups/1/items", map[string]any{"code_name": c, "name": c, "positive_prompt": "portrait of " + c})
	}
	p.must(201, "POST", "/projects", map[string]any{"name": "Demo"})
}

// waitFor polls until fn is true.
func (p *program) waitFor(what string, timeout time.Duration, fn func() bool) {
	p.t.Helper()
	end := time.Now().Add(timeout)
	for time.Now().Before(end) {
		if fn() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	p.t.Fatalf("timed out waiting for %s", what)
}

func (p *program) commandStatus(id int) string {
	_, c := p.call("GET", "/commands/"+strconv.Itoa(id), nil)
	s, _ := c["status"].(string)
	return s
}

func (p *program) jobs(command int) []map[string]any {
	_, out := p.call("GET", "/commands/"+strconv.Itoa(command)+"/jobs?page_size=200", nil)
	var jobs []map[string]any
	for _, j := range out["items"].([]any) {
		jobs = append(jobs, j.(map[string]any))
	}
	return jobs
}

// TestAcceptanceWholeProgram runs the program as a user would, with the
// stand-in ComfyUI: acceptance 8 (a job with a fixer), 10 (download), 24
// (preview before add), 25 (a command ends with its last job), 13 (the events
// come in order) and 26/27 on the way.
func TestAcceptanceWholeProgram(t *testing.T) {
	fake := comfytest.New(40 * time.Millisecond)
	t.Cleanup(fake.Close)
	env := testEnv(t, nil)
	p := startProgram(t, env)
	p.setup(fake.URL)

	// 24: the preview counts what Add then makes, and persists nothing.
	code := "gpu1 -$ anime: characters > upscale"
	pv := p.must(200, "POST", "/commands/preview", map[string]any{"code": code})
	if pv["valid"] != true || pv["jobs"] != float64(3) {
		t.Fatalf("preview = %v", pv)
	}
	if _, list := p.call("GET", "/projects/1/commands", nil); list["total"] != float64(0) {
		t.Fatalf("the preview stored something: %v", list)
	}
	cmd := p.must(201, "POST", "/projects/1/commands", map[string]any{"name": "all", "code": code})
	if len(p.jobs(1)) != 3 || cmd["status"] != "idle" {
		t.Fatalf("add made %d jobs, status %v", len(p.jobs(1)), cmd["status"])
	}

	// 10: an unfinished command cannot be downloaded.
	if status, out := p.call("GET", "/commands/1/download", nil); status != 409 || out["error"].(map[string]any)["tag"] != "CommandNotFinished" {
		t.Fatalf("download before the end: %d %v", status, out)
	}

	p.must(202, "POST", "/commands/1/run", nil)
	// 25: the command is not finished while a job is not.
	p.waitFor("the command to finish", 30*time.Second, func() bool {
		jobs := p.jobs(1)
		finished := 0
		for _, j := range jobs {
			if j["status"] == "finished" {
				finished++
			}
		}
		if finished < len(jobs) && p.commandStatus(1) == "finished" {
			t.Fatalf("the command is finished with %d of %d jobs", finished, len(jobs))
		}
		return p.commandStatus(1) == "finished"
	})

	// 8: every job did its generation and its fixer step.
	for _, j := range p.jobs(1) {
		if j["status"] != "finished" || j["finished_fixers"] != float64(1) || j["is_generated"] != true || j["is_fixed"] != true || j["attempts"] != float64(0) {
			t.Errorf("job = %v", j)
		}
	}
	if n := len(fake.Prompts()); n != 6 {
		t.Errorf("the server got %d prompts, want 3 generations and 3 fixer steps", n)
	}

	// 8 and 13: for every job the events are processing (generation), processing
	// (fixer), finished, in this order, and the ids rise.
	_, ev := p.call("GET", "/events", nil)
	perJob := map[float64][]string{}
	last := float64(0)
	for _, e := range ev["events"].([]any) {
		m := e.(map[string]any)
		if m["id"].(float64) <= last {
			t.Errorf("event ids do not rise: %v", ev["events"])
		}
		last = m["id"].(float64)
		if m["job_id"].(float64) > 0 {
			perJob[m["job_id"].(float64)] = append(perJob[m["job_id"].(float64)], m["type"].(string))
		}
	}
	for id, types := range perJob {
		if strings.Join(types, ",") != "processing,processing,finished" {
			t.Errorf("job %v events = %v", id, types)
		}
	}

	// 10: the archive has every result and the prompt summary.
	_, dl := p.call("GET", "/commands/1/download", nil)
	raw, _ := dl["zip"].([]byte)
	zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		t.Fatalf("the download is not a ZIP: %v", err)
	}
	names := map[string]bool{}
	for _, f := range zr.File {
		names[f.Name] = true
	}
	if len(names) != 4 || !names["summary.json"] {
		t.Errorf("archive = %v", names)
	}

	// The results are readable through the file route.
	rel := strings.TrimPrefix(p.p(jobs0(p)["result_path"]), env("BOS_RESULTS_DIR")+"/")
	res, err := http.Get(p.base + "/api/v1/files/results/" + rel) //nolint:noctx // a plain read in a test
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(res.Body)
	_ = res.Body.Close() // read to the end
	if res.StatusCode != 200 || !strings.HasPrefix(string(body), "png:fake-") {
		t.Errorf("result file: %d %q", res.StatusCode, body)
	}
}

func jobs0(p *program) map[string]any { return p.jobs(1)[0] }

// p reads a string out of a decoded JSON value.
func (p *program) p(v any) string {
	s, _ := v.(string)
	return s
}

// TestAcceptanceRestartMidBatch is acceptance 28: the program is stopped while
// jobs run, nothing is lost, and after the next start no job is left
// `processing` and the batch finishes.
func TestAcceptanceRestartMidBatch(t *testing.T) {
	fake := comfytest.New(600 * time.Millisecond)
	t.Cleanup(fake.Close)
	root := t.TempDir()
	envOf := func() func(string) string {
		return func(k string) string {
			switch k {
			case "BOS_DATABASE":
				return filepath.Join(root, "bos.db")
			case "BOS_RESULTS_DIR":
				return filepath.Join(root, "results")
			case "BOS_CONTROLNET_DIR":
				return filepath.Join(root, "controlnet")
			case "BOS_IPADAPTER_DIR":
				return filepath.Join(root, "ipadapter")
			case "BOS_REGIONS_DIR":
				return filepath.Join(root, "regions")
			case "BOS_THUMBNAILS_DIR":
				return filepath.Join(root, "thumbnails")
			case "BOS_DOWNLOADS_DIR":
				return filepath.Join(root, "downloads")
			case "BOS_LISTEN_ADDR":
				return "127.0.0.1:0"
			}
			return ""
		}
	}

	first := startProgram(t, envOf())
	first.setup(fake.URL)
	first.must(201, "POST", "/projects/1/commands", map[string]any{"name": "all", "code": "gpu1 -$ anime: characters"})
	first.must(202, "POST", "/commands/1/run", nil)
	first.waitFor("a job to be running", 15*time.Second, func() bool {
		for _, j := range first.jobs(1) {
			if j["status"] == "processing" {
				return true
			}
		}
		return false
	})
	first.stop() // SIGTERM while a job runs: in-flight requests end, the workers stop

	second := startProgram(t, envOf())
	second.waitFor("the batch to finish after the restart", 30*time.Second, func() bool { return second.commandStatus(1) == "finished" })
	for _, j := range second.jobs(1) {
		if j["status"] != "finished" {
			t.Errorf("job after the restart = %v", j)
		}
	}
	if res, err := http.Get(second.base + "/healthz"); err != nil || res.StatusCode != 200 { //nolint:noctx // a plain read in a test
		t.Errorf("healthz: %v", err)
	} else {
		_ = res.Body.Close() // nothing to read
	}
}
