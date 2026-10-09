package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rm4n0s/bos/internal/command"
	"github.com/rm4n0s/bos/internal/domain"
)

// commandWorld is a server, a generator, two groups and a project.
func (u *testUI) commandWorld() domain.ID {
	u.t.Helper()
	svc, ctx := u.env.Services, u.t.Context()
	_, err := svc.Servers.Create(ctx, domain.Server{Name: "GPU", Host: "http://gpu1:8188", CodeName: "gpu1", MaxConcurrentJobs: 1})
	check(u.t, err)
	_, err = svc.Generators.Create(ctx, domain.Generator{Name: "Anime", CodeName: "anime", Workflow: []byte(example(u.t, "creators", "illustrious_xl.json"))})
	check(u.t, err)
	for g, items := range map[string][]string{"characters": {"a", "b", "c"}, "poses": {"p1", "p2"}} {
		grp, err := svc.Groups.Create(ctx, domain.Group{Name: g, CodeName: g})
		check(u.t, err)
		for _, it := range items {
			_, err := svc.Items.Create(ctx, grp.ID, domain.Item{CodeName: it, Name: it, PositivePrompt: it})
			check(u.t, err)
		}
	}
	p, err := svc.Projects.Create(ctx, domain.Project{Name: "Demo"})
	check(u.t, err)
	return p.ID
}

func TestSegments(t *testing.T) {
	tests := []struct {
		name  string
		text  string
		spans []command.Pos
		want  string
	}{
		{"no spans", "abc", nil, "abc"},
		{"middle", "gpu1 -$ anime: nowhere", []command.Pos{{Offset: 15, Length: 7}}, "gpu1 -$ anime: [nowhere]"},
		{"start and end", "abcdef", []command.Pos{{Offset: 0, Length: 2}, {Offset: 4, Length: 2}}, "[ab]cd[ef]"},
		{"overlap merges", "abcdef", []command.Pos{{Offset: 1, Length: 3}, {Offset: 2, Length: 3}}, "a[bcde]f"},
		{"contained", "abcdef", []command.Pos{{Offset: 1, Length: 4}, {Offset: 2, Length: 1}}, "a[bcde]f"},
		{"a point marks one character", "abc", []command.Pos{{Offset: 1, Length: 0}}, "a[b]c"},
		{"past the end is cut", "abc", []command.Pos{{Offset: 2, Length: 9}}, "ab[c]"},
		{"outside is ignored", "abc", []command.Pos{{Offset: 7, Length: 1}, {Offset: -1, Length: 1}}, "abc"},
		{"inside a character is ignored", "né", []command.Pos{{Offset: 2, Length: 1}}, "né"},
		{"multibyte span", "né!", []command.Pos{{Offset: 1, Length: 2}}, "n[é]!"},
		{"unsorted", "abcdef", []command.Pos{{Offset: 4, Length: 1}, {Offset: 0, Length: 1}}, "[a]bcd[e]f"},
		{"empty text", "", []command.Pos{{Offset: 0, Length: 1}}, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var b strings.Builder
			for _, s := range segments(tc.text, tc.spans) {
				if s.Mark {
					b.WriteString("[" + s.Text + "]")
				} else {
					b.WriteString(s.Text)
				}
			}
			if b.String() != tc.want {
				t.Errorf("got %q, want %q", b.String(), tc.want)
			}
		})
	}
}

func TestCommandPreviewPanel(t *testing.T) {
	u := newTestUI(t)
	u.commandWorld()

	preview := func(code string) reply {
		return u.submit("POST", "/ui/commands/preview", field("code", code))
	}
	// Nothing typed.
	r := preview("  ")
	mustContain(t, "empty", r.patches(), `id="command-preview"`, "Type a command")
	if r.signals()["cmd"].(map[string]any)["valid"] != false {
		t.Errorf("signals = %v", r.signals())
	}

	// A valid command: counts, and Add may be enabled.
	r = preview("gpu1 -$ anime: characters * poses")
	mustContain(t, "valid", r.patches(), "The command is valid.", "6 combinations", "<strong>6 jobs</strong>", "Jobs go to whichever server is free.")
	if r.signals()["cmd"].(map[string]any)["valid"] != true {
		t.Errorf("signals = %v", r.signals())
	}
	r = preview("gpu1 -$ anime: characters * poses where #nothing limit 3")
	mustNotContain(t, "invalid is not valid", r.patches(), "The command is valid.")

	// An invalid one: the problems and the text with the span marked.
	r = preview("gpu1 -$ anime: nowhere")
	mustContain(t, "invalid", r.patches(), `<ul class="problems">`, `<mark class="mark">nowhere</mark>`, "nowhere")
	if r.signals()["cmd"].(map[string]any)["valid"] != false {
		t.Errorf("an invalid command must keep Add disabled: %v", r.signals())
	}
	// A syntax error too.
	r = preview("gpu1 -$")
	mustContain(t, "syntax", r.patches(), `<ul class="problems">`)

	// Per-server counts for a static spread.
	r = preview("gpu1 -$ anime: characters sample 2")
	mustContain(t, "valid", r.patches(), "2 jobs")
}

func TestCommandScreen(t *testing.T) {
	u := newTestUI(t)
	project := u.commandWorld()
	base := "/ui/projects/" + itoa(project) + "/commands"
	mustContain(t, "empty page", u.get("/projects/"+itoa(project)+"/commands").Body, "No commands yet", "Demo")
	if u.get("/projects/99/commands").Status != 404 {
		t.Error("a missing project must be a 404")
	}

	// New command dialog.
	r := u.datastar("GET", base+"/new", struct{}{})
	mustContain(t, "new", r.patches(), "selector #command-form-body", `name="code"`, `name="insert_at"`, "Type a command")

	// Add two.
	r = u.submit("POST", base, field("id", "0"), field("name", "first"), field("description", "all of them"), field("code", "gpu1 -$ anime: characters"))
	mustContain(t, "add", r.patches(), "id=\"commands-table\"", "first", "all of them", `<span class="badge idle">idle</span>`, "0/3", "Command saved.")
	u.submit("POST", base, field("id", "0"), field("name", "second"), field("code", "gpu1 -$ anime: poses"), field("insert_at", "1"))
	cmds, err := u.env.Services.Commands.List(t.Context(), project, domain.Page{})
	check(t, err)
	if cmds.Total != 2 || cmds.Items[0].Name != "second" {
		t.Fatalf("commands = %+v", cmds.Items)
	}

	// A command that does not validate stays in the dialog with every problem.
	r = u.submit("POST", base, field("id", "0"), field("name", "bad"), field("code", "gpu1 -$ anime: nowhere"))
	mustContain(t, "invalid", r.patches(), "selector #commands-error")
	r = u.submit("POST", base, field("id", "0"), field("name", ""), field("code", "gpu1 -$ anime: poses"), field("insert_at", "x"))
	mustContain(t, "bad position", r.patches(), "selector #commands-error", "whole number")

	// Edit shows the text and its preview; saving changes it.
	r = u.datastar("GET", "/ui/commands/1/edit", struct{}{})
	mustContain(t, "edit", r.patches(), `value="first"`, "gpu1 -$ anime: characters", "The command is valid.", "Edit command")
	mustNotContain(t, "edit", r.patches(), `name="insert_at"`)
	if r.signals()["cmd"].(map[string]any)["valid"] != true {
		t.Errorf("an existing command is valid: %v", r.signals())
	}
	r = u.submit("POST", base, field("id", "1"), field("name", "renamed"), field("code", "gpu1 -$ anime: characters(a)"))
	mustContain(t, "update", r.patches(), "renamed", "0/1")

	// Reorder.
	u.datastar("POST", "/ui/commands/1/up", struct{}{})
	cmds, _ = u.env.Services.Commands.List(t.Context(), project, domain.Page{})
	if cmds.Items[0].ID != 1 {
		t.Errorf("order after up = %v", cmds.Items)
	}
	u.datastar("POST", "/ui/commands/1/down", struct{}{})
	cmds, _ = u.env.Services.Commands.List(t.Context(), project, domain.Page{})
	if cmds.Items[0].ID != 2 {
		t.Errorf("order after down = %v", cmds.Items)
	}

	// Run, run again, stop, recreate.
	r = u.datastar("POST", "/ui/commands/1/run", struct{}{})
	mustContain(t, "run", r.patches(), "Queued.", `<span class="badge queued">queued</span>`)
	if u.env.Wakes.Count() != 1 {
		t.Errorf("the scheduler was woken %d times", u.env.Wakes.Count())
	}
	r = u.datastar("POST", "/ui/commands/1/run", struct{}{})
	mustContain(t, "run again", r.patches(), `class="toast error"`, "already queued")
	mustContain(t, "stop", u.datastar("POST", "/ui/commands/1/stop", struct{}{}).patches(), "Stopped what was waiting.", `badge idle`)
	mustContain(t, "recreate", u.datastar("POST", "/ui/commands/1/recreate", struct{}{}).patches(), "The jobs were made again.")

	// A finished command can be downloaded.
	if r := u.get("/commands/1/download"); r.Status != 409 || !strings.Contains(r.Body, "finished") {
		t.Errorf("download of an unfinished command: %d %q", r.Status, r.Body)
	}
	jobs, _ := u.env.DB.Repos().Jobs.AllByCommand(t.Context(), 1)
	for _, j := range jobs {
		check(t, os.MkdirAll(filepath.Dir(j.ResultPath), 0o750))
		check(t, os.WriteFile(j.ResultPath, png1(), 0o600))
		j.Status = domain.StatusFinished
		check(t, u.env.DB.Repos().Jobs.Update(t.Context(), &j))
	}
	check(t, u.env.DB.Repos().Commands.SetStatus(t.Context(), 1, domain.StatusFinished))
	page := u.get("/projects/" + itoa(project) + "/commands").Body
	mustContain(t, "finished page", page, `href="/commands/1/download"`, `badge finished`, "1/1")
	dl := u.get("/commands/1/download")
	if dl.Status != 200 || dl.Header.Get("Content-Type") != "application/zip" || !strings.HasPrefix(dl.Body, "PK") ||
		!strings.Contains(dl.Header.Get("Content-Disposition"), "command-1.zip") {
		t.Errorf("download: %d %q %q", dl.Status, dl.Header.Get("Content-Type"), dl.Header.Get("Content-Disposition"))
	}
	if u.get("/commands/99/download").Status != 404 || u.get("/commands/x/download").Status != 404 {
		t.Error("a missing command must be a 404")
	}

	// Delete.
	r = u.datastar("DELETE", "/ui/commands/1", struct{}{})
	mustContain(t, "delete", r.patches(), "Command deleted.")
	mustNotContain(t, "delete", r.patches(), "renamed")
	r = u.datastar("DELETE", "/ui/commands/1", struct{}{})
	mustContain(t, "delete again", r.patches(), `class="toast error"`, "not found")
}

func TestCommandListPagesKeepTheirPlace(t *testing.T) {
	u := newTestUI(t)
	project := u.commandWorld()
	for range 22 {
		_, err := u.env.Services.Commands.Add(t.Context(), project, serviceCommand("gpu1 -$ anime: poses"))
		check(t, err)
	}
	page := u.get("/projects/" + itoa(project) + "/commands").Body
	mustContain(t, "page 1", page, "Page 1 of 2", "@post('/ui/commands/1/up?page=1')")
	r := u.datastar("GET", "/ui/projects/"+itoa(project)+"/commands/table?page=2", struct{}{})
	mustContain(t, "page 2", r.patches(), "Page 2 of 2", "@post('/ui/commands/21/up?page=2')")
	// An action taken on page 2 redraws page 2.
	r = u.datastar("POST", "/ui/commands/21/up?page=2", struct{}{})
	mustContain(t, "action on page 2", r.patches(), "Page 2 of 2")
}
