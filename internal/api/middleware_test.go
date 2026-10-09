package api

import (
	"bufio"
	"flag"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestAuthToken(t *testing.T) {
	a := newTestAPI(t, func(d *Deps) { d.AuthToken = "s3cret" })
	tests := []struct {
		name    string
		path    string
		headers map[string]string
		status  int
	}{
		{"healthz is open", "/healthz", nil, 200},
		{"no token", "/api/v1/servers", nil, 401},
		{"wrong token", "/api/v1/servers", map[string]string{"Authorization": "Bearer nope"}, 401},
		{"wrong scheme", "/api/v1/servers", map[string]string{"Authorization": "Basic s3cret"}, 401},
		{"token", "/api/v1/servers", map[string]string{"Authorization": "Bearer s3cret"}, 200},
		{"openapi needs it too", "/api/v1/openapi.json", nil, 401},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := a.request("GET", tc.path, nil, tc.headers)
			if r.Status != tc.status {
				t.Fatalf("status = %d, want %d\n%s", r.Status, tc.status, r.Body)
			}
			if tc.status == 401 && (r.tag() != "Unauthorized" || r.Header.Get("WWW-Authenticate") != "Bearer") {
				t.Errorf("tag = %q, header = %q", r.tag(), r.Header.Get("WWW-Authenticate"))
			}
		})
	}
}

func TestCORS(t *testing.T) {
	off := newTestAPI(t)
	r := off.request("GET", "/api/v1/servers", nil, map[string]string{"Origin": "https://app.example"})
	if r.Header.Get("Access-Control-Allow-Origin") != "" {
		t.Error("CORS must be off without an allow-list")
	}

	on := newTestAPI(t, func(d *Deps) { d.CORSAllowList = []string{"https://app.example"} })
	r = on.request("GET", "/api/v1/servers", nil, map[string]string{"Origin": "https://app.example"})
	if r.Header.Get("Access-Control-Allow-Origin") != "https://app.example" || r.Header.Get("Vary") != "Origin" {
		t.Errorf("headers = %v", r.Header)
	}
	r = on.request("GET", "/api/v1/servers", nil, map[string]string{"Origin": "https://evil.example"})
	if r.Header.Get("Access-Control-Allow-Origin") != "" {
		t.Error("an origin outside the list was allowed")
	}
	r = on.request("OPTIONS", "/api/v1/servers", nil, map[string]string{"Origin": "https://app.example"})
	if r.Status != 204 || !strings.Contains(r.Header.Get("Access-Control-Allow-Headers"), "Authorization") {
		t.Errorf("preflight = %d %v", r.Status, r.Header)
	}
}

func TestPreflightNeedsNoToken(t *testing.T) {
	a := newTestAPI(t, func(d *Deps) { d.AuthToken = "t"; d.CORSAllowList = []string{"https://app.example"} })
	r := a.request("OPTIONS", "/api/v1/servers", nil, map[string]string{"Origin": "https://app.example"})
	if r.Status != 204 {
		t.Errorf("preflight status = %d", r.Status)
	}
}

func TestPagination(t *testing.T) {
	a := newTestAPI(t)
	for i := range 5 {
		a.expect(a.do("POST", "/api/v1/projects", map[string]any{"name": "p" + strconv.Itoa(i)}), 201, "")
	}
	p := a.expect(a.do("GET", "/api/v1/projects?page=2&page_size=2", nil), 200, "").json()
	if p["total"] != float64(5) || p["page"] != float64(2) || p["page_size"] != float64(2) || p["total_pages"] != float64(3) || len(p["items"].([]any)) != 2 {
		t.Errorf("page = %v", p)
	}
	capped := a.expect(a.do("GET", "/api/v1/projects?page_size=5000", nil), 200, "").json()
	if capped["page_size"] != float64(200) {
		t.Errorf("page_size = %v, want it cut to 200", capped["page_size"])
	}
	beyond := a.expect(a.do("GET", "/api/v1/projects?page=9", nil), 200, "").json()
	if items, ok := beyond["items"].([]any); !ok || len(items) != 0 {
		t.Errorf("a page past the end must be an empty list, got %v", beyond["items"])
	}
}

func TestBodyLimit(t *testing.T) {
	a := newTestAPI(t)
	big := `{"name":"` + strings.Repeat("x", maxJSONBody+10) + `"}`
	r := a.do("POST", "/api/v1/projects", big)
	if r.Status != 413 || r.tag() != "UploadTooLarge" {
		t.Errorf("status = %d, tag = %q", r.Status, r.tag())
	}
}

func TestFiles(t *testing.T) {
	a := newTestAPI(t)
	write := func(rel, content string) {
		p := filepath.Join(a.dirs.Results, rel)
		check(t, os.MkdirAll(filepath.Dir(p), 0o750))
		check(t, os.WriteFile(p, []byte(content), 0o600))
	}
	write("7/fox.png", "0123456789")
	write("top.mp4", "video")
	check(t, os.WriteFile(filepath.Join(a.root, "outside.txt"), []byte("secret"), 0o600))

	a.expect(a.do("GET", "/api/v1/files/results/7/fox.png", nil), 200, "")
	if got := a.do("GET", "/api/v1/files/results/top.mp4", nil); got.Header.Get("Content-Type") != "video/mp4" {
		t.Errorf("type = %q", got.Header.Get("Content-Type"))
	}
	ranged := a.request("GET", "/api/v1/files/results/7/fox.png", nil, map[string]string{"Range": "bytes=2-4"})
	if ranged.Status != 206 || string(ranged.Body) != "234" {
		t.Errorf("range = %d %q", ranged.Status, ranged.Body)
	}
	for _, path := range []string{
		"/api/v1/files/results/..%2foutside.txt", "/api/v1/files/results/7/../../outside.txt", "/api/v1/files/results/%2e%2e/outside.txt",
		"/api/v1/files/results/7%5c..%5cfox.png", "/api/v1/files/results/7/", "/api/v1/files/results/7", "/api/v1/files/results/",
		"/api/v1/files/results/%00", "/api/v1/files/nope/top.mp4",
	} {
		r := a.do("GET", path, nil)
		if r.Status != 404 && r.Status != 301 && r.Status != 307 && r.Status != 308 || strings.Contains(string(r.Body), "secret") {
			t.Errorf("%s answered %d %s", path, r.Status, r.Body)
		}
	}
}

func TestCleanName(t *testing.T) {
	tests := map[string]bool{
		"a.png": true, "7/a.png": true, "item-1f/red.png": true,
		"": false, ".": false, "..": false, "../a": false, "a/..": false, "a//b": false, "/a": false, "a/": false, `a\b`: false, "a\x00": false, "a\nb": false,
	}
	for name, want := range tests {
		if got := cleanName(name); got != want {
			t.Errorf("cleanName(%q) = %v, want %v", name, got, want)
		}
	}
}

// ---- events ----

// sse reads server-sent events from a stream.
type sse struct {
	t    *testing.T
	body *bufio.Scanner
	stop func()
}

func (a *testAPI) stream(headers map[string]string, query string) *sse {
	a.t.Helper()
	req, err := http.NewRequestWithContext(a.t.Context(), "GET", a.srv.URL+"/api/v1/events"+query, nil)
	check(a.t, err)
	req.Header.Set("Accept", "text/event-stream")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	res, err := http.DefaultClient.Do(req) //nolint:bodyclose // the returned sse closes it in stop
	check(a.t, err)
	if res.StatusCode != 200 || !strings.HasPrefix(res.Header.Get("Content-Type"), "text/event-stream") {
		a.t.Fatalf("stream status = %d, type = %q", res.StatusCode, res.Header.Get("Content-Type"))
	}
	return &sse{t: a.t, body: bufio.NewScanner(res.Body), stop: func() { _ = res.Body.Close() }}
}

// next returns the next block of lines, up to a blank line.
func (s *sse) next() string {
	s.t.Helper()
	var lines []string
	for s.body.Scan() {
		line := s.body.Text()
		if line == "" {
			return strings.Join(lines, "\n")
		}
		lines = append(lines, line)
	}
	s.t.Fatalf("the stream ended: %v", s.body.Err())
	return ""
}

func TestEventStream(t *testing.T) {
	a := newTestAPI(t)
	a.events.Append(eventOf("finished")) // before the client connects

	s := a.stream(nil, "")
	defer s.stop()
	// A client without a cursor sees only what happens from now on.
	if got := s.next(); got != ": keep-alive" {
		t.Fatalf("first block = %q, want a keep-alive", got)
	}
	e := a.events.Append(eventOf("processing"))
	got := s.next()
	for _, want := range []string{"id: " + strconv.FormatInt(e.ID, 10), "event: processing", `"job_id":1`, `"server":"gpu1"`} {
		if !strings.Contains(got, want) {
			t.Errorf("event %q lacks %q", got, want)
		}
	}
}

func TestEventStreamResumes(t *testing.T) {
	a := newTestAPI(t)
	for _, kind := range []string{"processing", "finished", "error"} {
		a.events.Append(eventOf(kind))
	}
	s := a.stream(map[string]string{"Last-Event-ID": "1"}, "")
	defer s.stop()
	if first := s.next(); !strings.HasPrefix(first, "id: 2\nevent: finished") {
		t.Errorf("first event = %q", first)
	}
	if second := s.next(); !strings.HasPrefix(second, "id: 3\nevent: error") {
		t.Errorf("second event = %q", second)
	}

	// The after parameter does the same.
	q := a.stream(nil, "?after=2")
	defer q.stop()
	if first := q.next(); !strings.HasPrefix(first, "id: 3") {
		t.Errorf("after=2 gave %q", first)
	}
}

func TestEventGap(t *testing.T) {
	a := newTestAPI(t, func(d *Deps) { d.Events = nil })
	_ = a
	small := newTestAPI(t)
	for range 150 { // the log keeps the last 100
		small.events.Append(eventOf("processing"))
	}
	ev := small.expect(small.do("GET", "/api/v1/events?after=3", nil), 200, "").json()
	if ev["gap"] != true || len(ev["events"].([]any)) != 100 {
		t.Errorf("gap = %v, %d events", ev["gap"], len(ev["events"].([]any)))
	}
	s := small.stream(map[string]string{"Last-Event-ID": "3"}, "")
	defer s.stop()
	if first := s.next(); first != "event: gap\ndata: {\"gap\":true}" {
		t.Errorf("first block = %q", first)
	}
	if second := s.next(); !strings.HasPrefix(second, "id: 51\n") {
		t.Errorf("second block = %q", second)
	}
}

func TestEventCursorInvalid(t *testing.T) {
	a := newTestAPI(t)
	a.expect(a.do("GET", "/api/v1/events?after=-1", nil), 400, "InvalidRequest")
	a.expect(a.do("GET", "/api/v1/events?after=x", nil), 400, "InvalidRequest")
}

func TestStopEndsTheStream(t *testing.T) {
	a := newTestAPI(t)
	s := a.stream(nil, "")
	defer s.stop()
	s.next() // a keep-alive: the stream is live
	a.api.Stop()
	done := make(chan struct{})
	go func() {
		for s.body.Scan() {
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the stream did not end after Stop")
	}
}

// ---- OpenAPI ----

var update = flag.Bool("update", false, "rewrite docs/openapi.json")

// TestOpenAPIMatchesTheFile keeps the committed contract in step with the
// routes (SPEC §15.5): a route added without regenerating the file fails here.
func TestOpenAPIMatchesTheFile(t *testing.T) {
	a := newTestAPI(t)
	got := a.expect(a.do("GET", "/api/v1/openapi.json", nil), 200, "").Body
	file := filepath.Join("..", "..", "docs", "openapi.json")
	if *update {
		check(t, os.WriteFile(file, got, 0o600))
	}
	want, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("%v (run go test ./internal/api -update)", err)
	}
	if string(want) != string(got) {
		t.Error("docs/openapi.json is out of date; run go test ./internal/api -update")
	}
}

func TestOpenAPIDescribesEveryRoute(t *testing.T) {
	a := newTestAPI(t)
	spec := a.expect(a.do("GET", "/api/v1/openapi.json", nil), 200, "").json()
	paths := spec["paths"].(map[string]any)
	seen := map[string]bool{}
	for _, r := range a.api.Routes() {
		item, ok := paths[r.Path].(map[string]any)
		if !ok {
			t.Errorf("%s is not in the document", r.pattern())
			continue
		}
		op, ok := item[strings.ToLower(r.Method)].(map[string]any)
		if !ok {
			t.Errorf("%s has no %s operation", r.Path, r.Method)
			continue
		}
		id := op["operationId"].(string)
		if seen[id] {
			t.Errorf("operationId %s is used twice", id)
		}
		seen[id] = true
		if _, ok := op["responses"].(map[string]any)[strconv.Itoa(r.Success)]; !ok {
			t.Errorf("%s does not document its success code %d", r.pattern(), r.Success)
		}
	}
}
