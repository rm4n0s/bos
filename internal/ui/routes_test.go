package ui

import (
	"strings"
	"testing"

	"github.com/rm4n0s/bos/internal/domain"
)

// TestEveryStateChangingRouteChecksForgery covers all the routes the interface
// registers (SPEC §16.6, acceptance 30): without the Datastar header, or from
// another origin, a write is refused with 403 before any handler runs.
func TestEveryStateChangingRouteChecksForgery(t *testing.T) {
	u := newTestUI(t)
	checked := 0
	for _, pattern := range u.ui.Patterns() {
		method, path, _ := strings.Cut(pattern, " ")
		if method == "GET" {
			continue
		}
		path = strings.NewReplacer("{id}", "1", "{name...}", "x", "{kind}", "x").Replace(path)
		checked++
		if r := u.send(method, path, strings.NewReader("{}"), nil); r.Status != 403 && path != "/login" && path != "/logout" {
			t.Errorf("%s without the header: status = %d, want 403", pattern, r.Status)
		}
		if r := u.send(method, path, strings.NewReader("{}"), map[string]string{"Datastar-Request": "true", "Origin": "https://evil.example"}); r.Status != 403 {
			t.Errorf("%s from another origin: status = %d, want 403", pattern, r.Status)
		}
		if r := u.send(method, path, strings.NewReader("{}"), map[string]string{"Datastar-Request": "true", "Sec-Fetch-Site": "cross-site"}); r.Status != 403 {
			t.Errorf("%s cross-site: status = %d, want 403", pattern, r.Status)
		}
	}
	if checked < 10 {
		t.Errorf("only %d write routes were found", checked)
	}
	// Nothing was changed by any of them.
	if list, _ := u.env.Services.Projects.List(t.Context(), domain.Page{}); list.Total != 0 {
		t.Errorf("a refused request created %d projects", list.Total)
	}
}

// TestHostileTextIsInertEverywhere puts markup into every kind of text a user
// can enter and checks every page and fragment that shows it (acceptance 31).
func TestHostileTextIsInertEverywhere(t *testing.T) {
	u := newTestUI(t)
	evil := `<script>alert(1)</script><img src=x onerror=alert(2)>'"`
	svc := u.env.Services
	ctx := t.Context()
	_, err := svc.Servers.Create(ctx, domain.Server{Name: evil, Host: "http://h:1", CodeName: "srv", MaxConcurrentJobs: 1})
	check(t, err)
	_, err = svc.Generators.Create(ctx, domain.Generator{Name: evil, CodeName: "gen", Workflow: []byte(example(t, "creators", "illustrious_xl.json"))})
	check(t, err)
	_, err = svc.Fixers.Create(ctx, domain.Fixer{Name: evil, CodeName: "fix", Workflow: []byte(example(t, "fixers", "simple_upscale.json")), LoadImageTitle: "Load Image", SaveImageTitle: "Save Image", PositivePrompt: evil})
	check(t, err)
	g, err := svc.Groups.Create(ctx, domain.Group{Name: evil, CodeName: "grp", Description: evil, Tags: []string{"tag"}})
	check(t, err)
	_, err = svc.Items.Create(ctx, g.ID, domain.Item{CodeName: "it", Name: evil, PositivePrompt: evil, NegativePrompt: evil, Tags: []string{"t"}})
	check(t, err)
	_, err = svc.Projects.Create(ctx, domain.Project{Name: evil})
	check(t, err)

	surfaces := map[string]string{
		"servers page":     u.get("/servers").Body,
		"generators page":  u.get("/generators").Body,
		"fixers page":      u.get("/fixers").Body,
		"groups page":      u.get("/groups").Body,
		"items page":       u.get("/groups/1/items").Body,
		"projects page":    u.get("/projects").Body,
		"generator edit":   u.datastar("GET", "/ui/generators/1/edit", struct{}{}).patches(),
		"fixer edit":       u.datastar("GET", "/ui/fixers/1/edit", struct{}{}).patches(),
		"group edit":       u.datastar("GET", "/ui/groups/1/edit", struct{}{}).patches(),
		"item edit":        u.datastar("GET", "/ui/items/1/edit", struct{}{}).patches(),
		"project edit":     u.datastar("GET", "/ui/projects/1/edit", struct{}{}).patches(),
		"items table":      u.datastar("GET", "/ui/groups/1/items/table", struct{}{}).patches(),
		"error in dialog":  u.submit("POST", "/ui/groups", field("id", "0"), field("name", evil), field("code_name", "grp")).patches(),
		"save with markup": u.submit("POST", "/ui/projects", field("id", "0"), field("name", evil)).patches(),
	}
	for name, body := range surfaces {
		if strings.TrimSpace(body) == "" {
			t.Errorf("%s: empty", name)
			continue
		}
		mustNotContain(t, name, body, "<script>alert", "<img src=x")
	}
	for _, name := range []string{"servers page", "projects page", "groups page", "items page", "generators page", "fixers page"} {
		mustContain(t, name, surfaces[name], "&lt;script&gt;")
	}
}

// TestDialogFormsAreMultipart: Datastar sends the fields of a form as
// multipart only when the form says so; otherwise the files are dropped.
func TestDialogFormsAreMultipart(t *testing.T) {
	u := newTestUI(t)
	_, err := u.env.Services.Groups.Create(t.Context(), domain.Group{Name: "g", CodeName: "g"})
	check(t, err)
	forms := 0
	for _, path := range []string{"/generators", "/fixers", "/groups", "/groups/1/items", "/projects"} {
		body := u.get(path).Body
		for _, f := range strings.Split(body, "<form")[1:] {
			tag := f[:strings.Index(f, ">")]
			if strings.Contains(tag, "contentType: 'form'") {
				forms++
				if !strings.Contains(tag, `enctype="multipart/form-data"`) {
					t.Errorf("%s: a form submits as 'form' without enctype multipart: <form%s>", path, tag)
				}
			}
		}
	}
	if forms < 5 {
		t.Errorf("only %d dialog forms found", forms)
	}
}
