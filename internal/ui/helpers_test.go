package ui

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/bos/internal/apptest"
)

func check(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// testUI is the interface over a real in-memory core.
type testUI struct {
	t   *testing.T
	env *apptest.Env
	ui  *UI
	srv *httptest.Server
	// client does not follow redirects and keeps no cookies unless asked.
	client *http.Client
}

type option func(*Deps)

func newTestUI(t *testing.T, opts ...option) *testUI {
	t.Helper()
	env := apptest.New(t)
	deps := Deps{
		Services: env.Services, Events: env.Events, Files: env.Files, Dirs: env.Dirs,
		Logger: env.Logger, KeepAlive: 50 * time.Millisecond, UploadLimit: 1 << 20,
	}
	for _, o := range opts {
		o(&deps)
	}
	u, err := New(deps)
	check(t, err)
	srv := httptest.NewServer(u.Handler())
	t.Cleanup(srv.Close)
	t.Cleanup(u.Stop)
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return &testUI{t: t, env: env, ui: u, srv: srv, client: client}
}

// reply is a response read to the end.
type reply struct {
	Status int
	Header http.Header
	Body   string
}

// send sends a request; headers may be nil.
func (u *testUI) send(method, path string, body io.Reader, headers map[string]string) reply {
	u.t.Helper()
	req, err := http.NewRequestWithContext(u.t.Context(), method, u.srv.URL+path, body)
	check(u.t, err)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	res, err := u.client.Do(req)
	check(u.t, err)
	defer func() { check(u.t, res.Body.Close()) }()
	b, err := io.ReadAll(res.Body)
	check(u.t, err)
	return reply{Status: res.StatusCode, Header: res.Header, Body: string(b)}
}

// get fetches a page.
func (u *testUI) get(path string) reply { return u.send("GET", path, nil, nil) }

// datastar sends a request the way Datastar does: the signals as JSON in the
// body, or in the datastar query parameter for a GET.
func (u *testUI) datastar(method, path string, signals any) reply {
	u.t.Helper()
	b, err := json.Marshal(signals)
	check(u.t, err)
	headers := map[string]string{"Datastar-Request": "true", "Accept": "text/event-stream"}
	if method == "GET" || method == "DELETE" {
		sep := "?"
		if strings.Contains(path, "?") {
			sep = "&"
		}
		return u.send(method, path+sep+"datastar="+url.QueryEscape(string(b)), nil, headers)
	}
	headers["Content-Type"] = "application/json"
	return u.send(method, path, bytes.NewReader(b), headers)
}

// sseEvent is one server-sent event.
type sseEvent struct {
	Type string
	ID   string
	Data string
}

// events parses a server-sent event body.
func parseEvents(body string) []sseEvent {
	var out []sseEvent
	var cur sseEvent
	var data []string
	flush := func() {
		if cur.Type != "" || len(data) > 0 {
			cur.Data = strings.Join(data, "\n")
			out = append(out, cur)
		}
		cur, data = sseEvent{}, nil
	}
	sc := bufio.NewScanner(strings.NewReader(body))
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		line := sc.Text()
		switch {
		case line == "":
			flush()
		case strings.HasPrefix(line, "event: "):
			cur.Type = strings.TrimPrefix(line, "event: ")
		case strings.HasPrefix(line, "id: "):
			cur.ID = strings.TrimPrefix(line, "id: ")
		case strings.HasPrefix(line, "data: "):
			data = append(data, strings.TrimPrefix(line, "data: "))
		}
	}
	flush()
	return out
}

// patches returns the HTML of every element patch in a response.
func (r reply) patches() string {
	var b strings.Builder
	for _, e := range parseEvents(r.Body) {
		if e.Type == "datastar-patch-elements" {
			b.WriteString(e.Data)
			b.WriteString("\n")
		}
	}
	return b.String()
}

// signals returns the merged JSON of every signal patch in a response.
func (r reply) signals() map[string]any {
	out := map[string]any{}
	for _, e := range parseEvents(r.Body) {
		if e.Type != "datastar-patch-signals" {
			continue
		}
		data := strings.TrimPrefix(e.Data, "signals ")
		var m map[string]any
		if json.Unmarshal([]byte(data), &m) == nil {
			for k, v := range m {
				out[k] = v
			}
		}
	}
	return out
}

func mustContain(t *testing.T, what, text string, wants ...string) {
	t.Helper()
	for _, want := range wants {
		if !strings.Contains(text, want) {
			t.Errorf("%s lacks %q:\n%s", what, want, text)
		}
	}
}

func mustNotContain(t *testing.T, what, text string, unwanted ...string) {
	t.Helper()
	for _, bad := range unwanted {
		if strings.Contains(text, bad) {
			t.Errorf("%s contains %q:\n%s", what, bad, text)
		}
	}
}

func jsonUnmarshal(s string, v any) error { return json.Unmarshal([]byte(s), v) }
