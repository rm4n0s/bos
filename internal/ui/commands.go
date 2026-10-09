package ui

import (
	"net/http"
	"sort"
	"strconv"
	"unicode/utf8"

	"github.com/rm4n0s/bos/internal/command"
	"github.com/rm4n0s/bos/internal/domain"
	"github.com/rm4n0s/bos/internal/service"
	"github.com/rm4n0s/errors"
)

// segment is a stretch of command text, marked when a problem points at it.
type segment struct {
	Text string
	Mark bool
}

// segments cuts text at the spans: the spans are byte offsets (SPEC §15.3), so
// a span that does not fall on character boundaries is ignored rather than
// allowed to break a character.
func segments(text string, spans []command.Pos) []segment {
	type span struct{ from, to int }
	var ok []span
	for _, p := range spans {
		from, to := p.Offset, p.End()
		if p.Length <= 0 {
			to = from + 1 // a problem at a point marks the character there
		}
		if to > len(text) {
			to = len(text)
		}
		if from < 0 || from >= len(text) || !utf8.RuneStart(text[from]) || to < len(text) && !utf8.RuneStart(text[to]) {
			continue
		}
		ok = append(ok, span{from, to})
	}
	sort.Slice(ok, func(i, j int) bool { return ok[i].from < ok[j].from })
	var out []segment
	at := 0
	for _, s := range ok {
		if s.from < at {
			if s.to <= at {
				continue
			}
			s.from = at // overlapping spans are merged
		}
		if s.from > at {
			out = append(out, segment{Text: text[at:s.from]})
		}
		if n := len(out); n > 0 && out[n-1].Mark {
			out[n-1].Text += text[s.from:s.to] // adjacent marks read as one
		} else {
			out = append(out, segment{Text: text[s.from:s.to], Mark: true})
		}
		at = s.to
	}
	if at < len(text) {
		out = append(out, segment{Text: text[at:]})
	}
	if len(out) == 0 && text != "" {
		out = []segment{{Text: text}}
	}
	return out
}

// previewView is what the preview panel shows.
type previewView struct {
	Empty bool
	command.Preview
	Segments []segment
}

func (u *UI) previewOf(r *http.Request, code string) (previewView, error) {
	if code == "" || len(code) > 0 && onlySpace(code) {
		return previewView{Empty: true}, nil
	}
	pv, err := u.Services.Commands.Preview(r.Context(), code)
	if err != nil {
		return previewView{}, err
	}
	view := previewView{Preview: *pv}
	if !pv.Valid {
		spans := make([]command.Pos, len(pv.Problems))
		for i, p := range pv.Problems {
			spans[i] = p.Position
		}
		view.Segments = segments(code, spans)
	}
	return view, nil
}

func onlySpace(s string) bool {
	for _, r := range s {
		if r != ' ' && r != '\n' && r != '\t' && r != '\r' {
			return false
		}
	}
	return true
}

type commandForm struct {
	ID                      int64
	Name, Description, Code string
	Preview                 previewView
}

type commandsTable struct {
	Project domain.Project
	Items   []domain.CommandSummary
	Pager   pagerData
	Blank   commandForm
}

func (u *UI) commandsTable(r *http.Request, projectID domain.ID) (commandsTable, error) {
	project, err := u.Services.Projects.Get(r.Context(), projectID)
	if err != nil {
		return commandsTable{}, err
	}
	p, err := pageParam(r)
	if err != nil {
		return commandsTable{}, err
	}
	out, err := u.Services.Commands.List(r.Context(), projectID, p)
	if err != nil {
		return commandsTable{}, err
	}
	return commandsTable{
		Project: *project, Items: out.Items, Blank: commandForm{Preview: previewView{Empty: true}},
		Pager: pagerOf("/ui/projects/"+strconv.FormatInt(projectID, 10)+"/commands/table", out),
	}, nil
}

func (u *UI) commandRoutes() {
	u.handle("GET /projects/{id}/commands", func(w http.ResponseWriter, r *http.Request) {
		id, err := pathID(r, "id")
		if err != nil {
			http.NotFound(w, r)
			return
		}
		t, err := u.commandsTable(r, id)
		if err != nil {
			u.notFoundOrFail(w, r, err)
			return
		}
		u.renderPage(w, r, "commands", page{Title: t.Project.Name, Nav: "projects", Data: t})
	})
	u.handle("GET /ui/projects/{id}/commands/table", view(u, func(r *http.Request, _ *struct{}, s *stream) error {
		id, err := pathID(r, "id")
		if err != nil {
			return err
		}
		return u.renderCommands(r, s, id)
	}))
	u.handle("GET /ui/projects/{id}/commands/new", view(u, func(_ *http.Request, _ *struct{}, s *stream) error {
		if err := s.into("command-form-body", "command_form", commandForm{Preview: previewView{Empty: true}}); err != nil {
			return err
		}
		return s.set(map[string]any{"commands": map[string]any{"open": true}, "cmd": map[string]any{"valid": false}})
	}))
	u.handle("GET /ui/commands/{id}/edit", view(u, func(r *http.Request, _ *struct{}, s *stream) error {
		id, err := pathID(r, "id")
		if err != nil {
			return err
		}
		c, err := u.Services.Commands.Get(r.Context(), id)
		if err != nil {
			return err
		}
		pv, err := u.previewOf(r, c.CommandCode)
		if err != nil {
			return err
		}
		form := commandForm{ID: c.ID, Name: c.Name, Description: c.Description, Code: c.CommandCode, Preview: pv}
		if err := s.into("command-form-body", "command_form", form); err != nil {
			return err
		}
		return s.set(map[string]any{"commands": map[string]any{"open": true}, "cmd": map[string]any{"valid": pv.Valid}})
	}))
	u.handle("POST /ui/commands/preview", u.actForm("", func(r *http.Request, f *form, s *stream) error {
		pv, err := u.previewOf(r, f.raw("code"))
		if err != nil {
			return err
		}
		if err := s.render("command_preview", pv); err != nil {
			return err
		}
		return s.set(map[string]any{"cmd": map[string]any{"valid": pv.Valid}})
	}))
	u.handle("POST /ui/projects/{id}/commands", u.actForm("commands-error", func(r *http.Request, f *form, s *stream) error {
		project, err := pathID(r, "id")
		if err != nil {
			return err
		}
		id, err := f.id()
		if err != nil {
			return err
		}
		in := service.CommandInput{Name: f.str("name"), Description: f.str("description"), Code: f.raw("code")}
		if id > 0 {
			_, err = u.Services.Commands.Edit(r.Context(), id, in)
		} else {
			if in.InsertAt, err = atoi(f.str("insert_at")); err != nil {
				return err
			}
			_, err = u.Services.Commands.Add(r.Context(), project, in)
		}
		if err != nil {
			return err
		}
		if err := s.set(map[string]any{"commands": map[string]any{"open": false}}); err != nil {
			return err
		}
		if err := s.into("commands-error", "error_text", ""); err != nil {
			return err
		}
		if err := u.renderCommands(r, s, project); err != nil {
			return err
		}
		return s.toast("ok", "Command saved.")
	}))

	// What a command can be told to do. Each refreshes the list it is in.
	type op struct {
		path    string
		message string
		do      func(*service.CommandService, *http.Request, domain.ID) error
	}
	for _, o := range []op{
		{"POST /ui/commands/{id}/run", "Queued.", func(c *service.CommandService, r *http.Request, id domain.ID) error {
			_, err := c.Run(r.Context(), id)
			return err
		}},
		{"POST /ui/commands/{id}/stop", "Stopped what was waiting.", func(c *service.CommandService, r *http.Request, id domain.ID) error {
			_, err := c.Stop(r.Context(), id)
			return err
		}},
		{"POST /ui/commands/{id}/recreate", "The jobs were made again.", func(c *service.CommandService, r *http.Request, id domain.ID) error {
			_, err := c.Recreate(r.Context(), id)
			return err
		}},
		{"POST /ui/commands/{id}/up", "", func(c *service.CommandService, r *http.Request, id domain.ID) error {
			_, err := c.Up(r.Context(), id)
			return err
		}},
		{"POST /ui/commands/{id}/down", "", func(c *service.CommandService, r *http.Request, id domain.ID) error {
			_, err := c.Down(r.Context(), id)
			return err
		}},
	} {
		u.handle(o.path, act(u, "", func(r *http.Request, _ *struct{}, s *stream) error {
			id, err := pathID(r, "id")
			if err != nil {
				return err
			}
			cmd, err := u.Services.Commands.Get(r.Context(), id)
			if err != nil {
				return err
			}
			if err := o.do(u.Services.Commands, r, id); err != nil {
				return err
			}
			if err := u.renderCommands(r, s, cmd.ProjectID); err != nil {
				return err
			}
			if o.message != "" {
				return s.toast("ok", o.message)
			}
			return nil
		}))
	}
	u.handle("DELETE /ui/commands/{id}", act(u, "", func(r *http.Request, _ *struct{}, s *stream) error {
		id, err := pathID(r, "id")
		if err != nil {
			return err
		}
		cmd, err := u.Services.Commands.Get(r.Context(), id)
		if err != nil {
			return err
		}
		if err := u.Services.Commands.Delete(r.Context(), id); err != nil {
			return err
		}
		if err := u.renderCommands(r, s, cmd.ProjectID); err != nil {
			return err
		}
		return s.toast("ok", "Command deleted.")
	}))
	u.handle("GET /commands/{id}/download", func(w http.ResponseWriter, r *http.Request) {
		id, err := pathID(r, "id")
		if err != nil {
			http.NotFound(w, r)
			return
		}
		path, err := u.Services.Commands.Download(r.Context(), id)
		if err == nil {
			w.Header().Set("Content-Disposition", `attachment; filename="command-`+strconv.FormatInt(id, 10)+`.zip"`)
			err = u.fileServer.Send(w, r, path, "application/zip")
		}
		if err != nil {
			u.downloadFailed(w, r, err)
		}
	})
}

// downloadFailed answers a download that cannot be made: what the user could
// fix is explained, the rest is the log's business.
func (u *UI) downloadFailed(w http.ResponseWriter, r *http.Request, err error) {
	message, userFault := describe(err)
	if !userFault {
		u.failPage(w, r, err)
		return
	}
	status := http.StatusConflict
	if appErr, ok := errors.FromError(err); ok && appErr.Tag == "CommandNotFound" {
		status = http.StatusNotFound
	}
	http.Error(w, message, status)
}

func atoi(s string) (int, error) {
	if s == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, errors.New("InvalidRequest", "a whole number is needed", "value", s)
	}
	return n, nil
}

// renderCommands redraws the command list of a project.
func (u *UI) renderCommands(r *http.Request, s *stream, project domain.ID) error {
	t, err := u.commandsTable(r, project)
	if err != nil {
		return err
	}
	if err := s.render("commands_table", t); err != nil {
		return err
	}
	return s.set(map[string]any{"_armed": 0})
}
