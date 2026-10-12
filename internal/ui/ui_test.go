package ui

import (
	"strings"
	"testing"

	"github.com/rm4n0s/bos/internal/domain"
	"github.com/rm4n0s/errors"
)

func serverForm(id int, name, host, code string) map[string]any {
	return map[string]any{"servers": map[string]any{"form": map[string]any{
		"id": id, "name": name, "host": host, "code_name": code, "is_local": false, "max_concurrent_jobs": 1,
	}}}
}

// The server dialog has no folder of ComfyUI: the program reaches the output
// and models folders through the comfyui-model-crud extension.
func TestServerDialogNamesTheExtension(t *testing.T) {
	u := newTestUI(t)
	got := u.get("/servers").Body
	mustContain(t, "dialog", got, "comfyui-model-crud")
	mustNotContain(t, "dialog", got, "comfyui_path", "ComfyUI folder")
	r := u.datastar("POST", "/ui/servers", serverForm(0, "GPU One", "http://gpu1:8188", "gpu1"))
	mustContain(t, "table", r.patches(), `id="servers-table"`, "GPU One")
	form := u.datastar("GET", "/ui/servers/1/edit", struct{}{}).signals()["servers"].(map[string]any)["form"].(map[string]any)
	if _, there := form["comfyui_path"]; there {
		t.Errorf("edit signals = %v", form)
	}
}

func TestPagesRender(t *testing.T) {
	u := newTestUI(t)
	for _, tc := range []struct{ path, want string }{
		{"/", "Bowl of Scenes"},
		{"/servers", "No servers yet — add one."},
	} {
		r := u.get(tc.path)
		if r.Status != 200 || !strings.HasPrefix(r.Header.Get("Content-Type"), "text/html") {
			t.Errorf("%s: status = %d, type = %q", tc.path, r.Status, r.Header.Get("Content-Type"))
		}
		mustContain(t, tc.path, r.Body, tc.want, `<script type="module" src="/static/datastar.js">`, `id="toasts"`)
		if r.Header.Get("Cache-Control") != "no-store" {
			t.Errorf("%s: Cache-Control = %q", tc.path, r.Header.Get("Cache-Control"))
		}
	}
	if r := u.get("/nope"); r.Status != 404 {
		t.Errorf("/nope: status = %d", r.Status)
	}
}

func TestStaticAssetsAreEmbedded(t *testing.T) {
	u := newTestUI(t)
	js := u.get("/static/datastar.js")
	if js.Status != 200 || !strings.Contains(js.Body, "Datastar v1.0.4") {
		t.Errorf("datastar.js: status = %d", js.Status)
	}
	if css := u.get("/static/app.css"); css.Status != 200 || css.Header.Get("Content-Type") != "text/css; charset=utf-8" {
		t.Errorf("app.css: status = %d, type = %q", css.Status, css.Header.Get("Content-Type"))
	}
	// Nothing is loaded from elsewhere (SPEC §16.1, rule 2).
	page := u.get("/servers").Body
	mustNotContain(t, "the page", page, "https://cdn", "//cdn.")
	for _, ref := range []string{`src="`, `href="`} {
		for _, part := range strings.Split(page, ref)[1:] {
			if v := part[:strings.Index(part, `"`)]; !strings.HasPrefix(v, "/") {
				t.Errorf("the page refers to %q, which is not served by this program", v)
			}
		}
	}
}

func TestSecurityHeaders(t *testing.T) {
	u := newTestUI(t)
	h := u.get("/servers").Header
	csp := h.Get("Content-Security-Policy")
	mustContain(t, "CSP", csp, "script-src 'self' 'unsafe-eval'", "default-src 'none'", "frame-ancestors 'none'", "connect-src 'self'")
	mustNotContain(t, "CSP", csp, "http:", "https:", "'unsafe-inline'")
	if h.Get("X-Content-Type-Options") != "nosniff" || h.Get("X-Frame-Options") != "DENY" {
		t.Errorf("headers = %v", h)
	}
}

// TestCSRF is acceptance 30.
func TestCSRF(t *testing.T) {
	u := newTestUI(t)
	body := func() *strings.Reader {
		return strings.NewReader(`{"servers":{"form":{"id":0,"name":"x","host":"http://x:1","code_name":"x","max_concurrent_jobs":1}}}`)
	}
	tests := []struct {
		name    string
		headers map[string]string
		status  int
	}{
		{"no Datastar header", map[string]string{"Content-Type": "application/json"}, 403},
		{"Datastar header false", map[string]string{"Datastar-Request": "false"}, 403},
		{"another origin", map[string]string{"Datastar-Request": "true", "Origin": "https://evil.example"}, 403},
		{"cross-site fetch metadata", map[string]string{"Datastar-Request": "true", "Sec-Fetch-Site": "cross-site"}, 403},
		{"same-site fetch metadata", map[string]string{"Datastar-Request": "true", "Sec-Fetch-Site": "same-site"}, 403},
		{"same origin", map[string]string{"Datastar-Request": "true", "Sec-Fetch-Site": "same-origin"}, 200},
		{"no browser headers", map[string]string{"Datastar-Request": "true"}, 200},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// A new code name each time: only the status matters here.
			r := u.send("POST", "/ui/servers", body(), tc.headers)
			if r.Status != tc.status {
				t.Fatalf("status = %d, want %d\n%s", r.Status, tc.status, r.Body)
			}
		})
	}
	// Nothing was created by the refused requests: only the first accepted one
	// made a server, the second hit its code name.
	list, err := u.env.Services.Servers.List(t.Context(), domain.Page{})
	check(t, err)
	if list.Total != 1 {
		t.Errorf("%d servers", list.Total)
	}
	// The same-origin Origin of the browser is accepted.
	same := map[string]string{"Datastar-Request": "true", "Origin": u.srv.URL}
	if r := u.send("DELETE", "/ui/servers/1", nil, same); r.Status != 200 {
		t.Errorf("same Origin: status = %d", r.Status)
	}
	// Reading never needs the header.
	if r := u.get("/ui/servers/table"); r.Status != 200 {
		t.Errorf("GET fragment: status = %d", r.Status)
	}
}

func TestServerScreen(t *testing.T) {
	u := newTestUI(t)

	// Create.
	r := u.datastar("POST", "/ui/servers", serverForm(0, "GPU One", "http://gpu1:8188", "gpu1"))
	if r.Status != 200 || !strings.HasPrefix(r.Header.Get("Content-Type"), "text/event-stream") {
		t.Fatalf("status = %d, type = %q", r.Status, r.Header.Get("Content-Type"))
	}
	mustContain(t, "patches", r.patches(), `id="servers-table"`, "GPU One", "gpu1", "Server saved.", `class="badge online"`)
	if open := r.signals()["servers"].(map[string]any)["open"]; open != false {
		t.Errorf("the dialog must close: %v", r.signals())
	}

	// The page lists it.
	mustContain(t, "page", u.get("/servers").Body, "GPU One", `id="server-1"`)

	// Edit: the server fills the form signals.
	r = u.datastar("GET", "/ui/servers/1/edit", struct{}{})
	form := r.signals()["servers"].(map[string]any)["form"].(map[string]any)
	if form["name"] != "GPU One" || form["code_name"] != "gpu1" || form["id"] != float64(1) || r.signals()["servers"].(map[string]any)["open"] != true {
		t.Errorf("edit signals = %v", r.signals())
	}

	// Update.
	r = u.datastar("POST", "/ui/servers", serverForm(1, "GPU Renamed", "http://gpu1:8188", "gpu1"))
	mustContain(t, "patches", r.patches(), "GPU Renamed")
	mustNotContain(t, "patches", r.patches(), "GPU One")

	// A business failure is a 200 with the message in the form, dialog open.
	r = u.datastar("POST", "/ui/servers", serverForm(0, "Dup", "http://dup:1", "gpu1"))
	if r.Status != 200 {
		t.Fatalf("status = %d", r.Status)
	}
	mustContain(t, "patches", r.patches(), "selector #servers-error", "a server with this code name already exists")
	if len(r.signals()) != 0 {
		t.Errorf("the dialog must stay open, got signals %v", r.signals())
	}
	if list, _ := u.env.Services.Servers.List(t.Context(), domain.Page{}); list.Total != 1 {
		t.Errorf("a refused save created a server: %d", list.Total)
	}
	// Field errors name the problem.
	r = u.datastar("POST", "/ui/servers", serverForm(0, "Bad", "ftp://x", "bad"))
	mustContain(t, "patches", r.patches(), "http or https")

	// Test connection.
	r = u.datastar("POST", "/ui/servers/1/test", struct{}{})
	mustContain(t, "patches", r.patches(), "The server answers.", `class="toast ok"`)
	u.env.Fleet.SetDown(1, true)
	r = u.datastar("POST", "/ui/servers/1/test", struct{}{})
	mustContain(t, "patches", r.patches(), `class="toast error"`, "does not answer")
	mustContain(t, "table", u.get("/ui/servers/table").Body, `class="badge offline"`)

	// Delete.
	r = u.datastar("DELETE", "/ui/servers/1", struct{}{})
	mustContain(t, "patches", r.patches(), "No servers yet — add one.", "Server deleted.")
	r = u.datastar("DELETE", "/ui/servers/1", struct{}{})
	mustContain(t, "patches", r.patches(), `class="toast error"`, "not found")
}

func TestServerTablePaging(t *testing.T) {
	u := newTestUI(t)
	for i := range 25 {
		code := "s" + string(rune('a'+i))
		_, err := u.env.Services.Servers.Create(t.Context(), domain.Server{Name: "Server " + code, Host: "http://" + code + ":1", CodeName: code, MaxConcurrentJobs: 1})
		check(t, err)
	}
	first := u.get("/servers").Body
	mustContain(t, "page 1", first, "Page 1 of 2", "25 in all", "Server sa")
	mustNotContain(t, "page 1", first, "Server sy")
	r := u.datastar("GET", "/ui/servers/table?page=2", struct{}{})
	mustContain(t, "page 2", r.patches(), "Page 2 of 2", "Server sy")
	mustNotContain(t, "page 2", r.patches(), "Server sa")
	r = u.datastar("GET", "/ui/servers/table?page=x", struct{}{})
	mustContain(t, "bad page", r.patches(), `class="toast error"`)
}

// The pager is above the table and below it, with first, previous, the page
// numbers, next and last.
func TestPagerIsAboveAndBelowTheTable(t *testing.T) {
	u := newTestUI(t)
	// The address sits in a script string of the attribute, where the template
	// engine writes its slashes escaped.
	get := func(page int) string { return `@get('\/ui\/servers\/table?page=` + itoa(int64(page)) + `')` }
	for i := range 65 { // four pages of 20
		code := "s" + string(rune('a'+i%26)) + string(rune('a'+i/26))
		_, err := u.env.Services.Servers.Create(t.Context(), domain.Server{Name: "Server " + code, Host: "http://" + code + ":1", CodeName: code, MaxConcurrentJobs: 1})
		check(t, err)
	}
	for name, body := range map[string]string{
		"page":     u.get("/servers?page=2").Body,
		"fragment": u.datastar("GET", "/ui/servers/table?page=2", struct{}{}).patches(),
	} {
		if n := strings.Count(body, `<nav class="pager"`); n != 2 {
			t.Fatalf("%s: %d pagers, want 2 (above and below)", name, n)
		}
		if top, table, bottom := strings.Index(body, `<nav class="pager"`), strings.Index(body, "<table"), strings.LastIndex(body, `<nav class="pager"`); top >= table || table >= bottom {
			t.Errorf("%s: the pagers are at %d and %d, the table at %d", name, top, bottom, table)
		}
		mustContain(t, name, body, get(1), get(3), get(4),
			">« First<", ">‹ Previous<", ">Next ›<", ">Last »<", `aria-current="page" disabled>2</button>`, "Page 2 of 4 · 65 in all")
	}
	// On the first page First and Previous are off; on the last, Next and Last.
	first := u.get("/servers").Body
	mustContain(t, "first page", first, `title="First page" data-on:click="`+get(1)+`" disabled>`,
		`title="Previous page" data-on:click="`+get(0)+`" disabled>`)
	last := u.get("/servers?page=4").Body
	mustContain(t, "last page", last, `title="Last page" data-on:click="`+get(4)+`" disabled>`)
	// One page needs no pager.
	v := newTestUI(t)
	mustNotContain(t, "one page", v.get("/servers").Body, `<nav class="pager"`)
}

// TestHostileTextIsInert is acceptance 31 for the surfaces that exist so far.
func TestHostileTextIsInert(t *testing.T) {
	u := newTestUI(t)
	evil := `<script>alert(1)</script>"><img src=x onerror=alert(2)>`
	_, err := u.env.Services.Servers.Create(t.Context(), domain.Server{Name: evil, Host: "http://h:1", CodeName: "evil", MaxConcurrentJobs: 1})
	check(t, err)

	for name, body := range map[string]string{
		"page":  u.get("/servers").Body,
		"table": u.datastar("GET", "/ui/servers/table", struct{}{}).patches(),
		"save":  u.datastar("POST", "/ui/servers", serverForm(0, evil, "http://h2:1", "evil2")).patches(),
	} {
		mustNotContain(t, name, body, "<script>alert", "<img")
		mustContain(t, name, body, "&lt;script&gt;", "&lt;img")
	}
	// A validation message that quotes user text is escaped too.
	r := u.datastar("POST", "/ui/servers", serverForm(0, "x", evil, "evil3"))
	mustNotContain(t, "error", r.patches(), "<script>alert")
}

func TestUnexpectedFailureIsAGenericToast(t *testing.T) {
	u := newTestUI(t)
	check(t, u.env.DB.Close())
	r := u.datastar("POST", "/ui/servers", serverForm(0, "x", "http://x:1", "x"))
	if r.Status != 200 {
		t.Fatalf("status = %d", r.Status)
	}
	mustContain(t, "patches", r.patches(), `class="toast error"`, "Something went wrong on the server")
	mustNotContain(t, "patches", r.patches(), "sql", "StoreFailure", "database")
	mustContain(t, "log", u.env.Logs.String(), "StoreFailure")
}

func TestActionsRefuseUnreadableSignals(t *testing.T) {
	u := newTestUI(t)
	r := u.send("POST", "/ui/servers", strings.NewReader("{not json"), map[string]string{"Datastar-Request": "true"})
	if r.Status != 400 {
		t.Errorf("status = %d", r.Status)
	}
}

func TestFlexNumbers(t *testing.T) {
	var s serverSignals
	check(t, jsonUnmarshal(`{"servers":{"form":{"id":"3","max_concurrent_jobs":""}}}`, &s))
	if s.Servers.Form.ID != 3 || s.Servers.Form.MaxConcurrentJobs != 0 {
		t.Errorf("form = %+v", s.Servers.Form)
	}
	check(t, jsonUnmarshal(`{"servers":{"form":{"id":2.0,"max_concurrent_jobs":4}}}`, &s))
	if s.Servers.Form.ID != 2 || s.Servers.Form.MaxConcurrentJobs != 4 {
		t.Errorf("form = %+v", s.Servers.Form)
	}
	var f flexFloat
	check(t, f.UnmarshalJSON([]byte(`"0.5"`)))
	if f != 0.5 {
		t.Errorf("f = %v", f)
	}
	if err := f.UnmarshalJSON([]byte(`"x"`)); err == nil {
		t.Error("garbage was accepted")
	}
}

func TestTemplatesThatDoNotParseAreReported(t *testing.T) {
	_, err := parseTemplates(nil) // the real templates call functions the empty map lacks
	appErr, ok := errors.FromError(err)
	if !ok || !appErr.HasRoute("parseTemplates.TemplateInvalid") {
		t.Fatalf("err = %v", err)
	}
}

// Nothing the interface sends is kept by the browser: not the pages, not the
// scripts and styles (a changed style sheet would meet a new script), and not
// the pictures, whose names are reused (the REPL result always has the same
// one).
func TestNothingIsCached(t *testing.T) {
	u := newTestUI(t)
	check(t, u.env.Files.Write(u.env.Dirs.Results+"/1/alice.png", strings.NewReader("png")))
	for _, path := range []string{"/", "/servers", "/static/app.css", "/static/datastar.js", "/static/viewer.js", "/static/complete.js", "/files/results/1/alice.png"} {
		r := u.get(path)
		if r.Status != 200 {
			t.Errorf("%s: status = %d", path, r.Status)
		}
		if got := r.Header.Get("Cache-Control"); got != "no-store" {
			t.Errorf("%s: Cache-Control = %q, want no-store", path, got)
		}
	}
}
