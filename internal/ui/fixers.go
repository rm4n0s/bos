package ui

import (
	"net/http"

	"github.com/rm4n0s/bos/internal/domain"
)

// fixerForm is what the fixer dialog shows.
type fixerForm struct {
	ID            int64
	Name          string
	CodeName      string
	Workflow      string
	LoadTitle     string
	SaveTitle     string
	PositiveTitle string
	NegativeTitle string
	Positive      string
	Negative      string
	LoadTitles    []string
	SaveTitles    []string
	PromptTitles  []string
	Analysed      bool
}

type fixersTable struct {
	Items []domain.Fixer
	Pager pagerData
	Blank fixerForm
}

func fixerFormOf(x *domain.Fixer) fixerForm {
	return fixerForm{
		ID: x.ID, Name: x.Name, CodeName: x.CodeName, Workflow: string(x.Workflow), LoadTitle: x.LoadImageTitle, SaveTitle: x.SaveImageTitle,
		PositiveTitle: x.PositivePromptTitle, NegativeTitle: x.NegativePromptTitle, Positive: x.PositivePrompt, Negative: x.NegativePrompt,
	}
}

func fixerFromForm(f *form) (domain.Fixer, error) {
	id, err := f.id()
	if err != nil {
		return domain.Fixer{}, err
	}
	return domain.Fixer{
		ID: id, Name: f.str("name"), CodeName: f.str("code_name"), Workflow: []byte(f.raw("workflow")),
		LoadImageTitle: f.str("load_image_title"), SaveImageTitle: f.str("save_image_title"),
		PositivePromptTitle: f.str("positive_prompt_title"), NegativePromptTitle: f.str("negative_prompt_title"),
		PositivePrompt: f.str("positive_prompt"), NegativePrompt: f.str("negative_prompt"),
	}, nil
}

func (u *UI) fixersTable(r *http.Request) (fixersTable, error) {
	p, err := pageParam(r)
	if err != nil {
		return fixersTable{}, err
	}
	out, err := u.Services.Fixers.List(r.Context(), p)
	if err != nil {
		return fixersTable{}, err
	}
	return fixersTable{Items: out.Items, Pager: pagerOf("/ui/fixers/table", out)}, nil
}

func (u *UI) fixerRoutes() {
	u.handle("GET /fixers", func(w http.ResponseWriter, r *http.Request) {
		t, err := u.fixersTable(r)
		if err != nil {
			u.failPage(w, r, err)
			return
		}
		u.renderPage(w, r, "fixers", page{Title: "Fixers", Nav: "fixers", Data: t})
	})
	u.handle("GET /ui/fixers/table", view(u, func(r *http.Request, _ *struct{}, s *stream) error {
		t, err := u.fixersTable(r)
		if err != nil {
			return err
		}
		return s.render("fixers_table", t)
	}))
	u.handle("GET /ui/fixers/new", view(u, func(_ *http.Request, _ *struct{}, s *stream) error {
		if err := s.into("fixer-form-body", "fixer_form", fixerForm{}); err != nil {
			return err
		}
		return s.set(map[string]any{"fixers": map[string]any{"open": true}})
	}))
	u.handle("GET /ui/fixers/{id}/edit", view(u, func(r *http.Request, _ *struct{}, s *stream) error {
		id, err := pathID(r, "id")
		if err != nil {
			return err
		}
		x, err := u.Services.Fixers.Get(r.Context(), id)
		if err != nil {
			return err
		}
		if err := s.into("fixer-form-body", "fixer_form", fixerFormOf(x)); err != nil {
			return err
		}
		return s.set(map[string]any{"fixers": map[string]any{"open": true}})
	}))
	u.handle("POST /ui/fixers/analyze", u.actForm("fixers-error", func(r *http.Request, f *form, s *stream) error {
		x, err := fixerFromForm(f)
		if err != nil {
			return err
		}
		an, err := u.Services.Generators.Analyze(x.Workflow)
		if err != nil {
			return err
		}
		shown := fixerFormOf(&x)
		shown.LoadTitles, shown.SaveTitles, shown.PromptTitles, shown.Analysed = an.LoadImageTitles, an.SaveImageTitles, an.PromptTitles, true
		if shown.LoadTitle == "" && len(an.LoadImageTitles) == 1 {
			shown.LoadTitle = an.LoadImageTitles[0]
		}
		if shown.SaveTitle == "" && len(an.SaveImageTitles) == 1 {
			shown.SaveTitle = an.SaveImageTitles[0]
		}
		return s.into("fixer-form-body", "fixer_form", shown)
	}))
	u.handle("POST /ui/fixers", u.actForm("fixers-error", func(r *http.Request, f *form, s *stream) error {
		x, err := fixerFromForm(f)
		if err != nil {
			return err
		}
		if x.ID > 0 {
			_, err = u.Services.Fixers.Update(r.Context(), x.ID, x)
		} else {
			_, err = u.Services.Fixers.Create(r.Context(), x)
		}
		if err != nil {
			return err
		}
		return u.refreshFixers(r, s, "Fixer saved.")
	}))
	u.handle("DELETE /ui/fixers/{id}", act(u, "", func(r *http.Request, _ *struct{}, s *stream) error {
		id, err := pathID(r, "id")
		if err != nil {
			return err
		}
		if err := u.Services.Fixers.Delete(r.Context(), id); err != nil {
			return err
		}
		return u.refreshFixers(r, s, "Fixer deleted.")
	}))
}

func (u *UI) refreshFixers(r *http.Request, s *stream, message string) error {
	t, err := u.fixersTable(r)
	if err != nil {
		return err
	}
	if err := s.render("fixers_table", t); err != nil {
		return err
	}
	if err := s.set(map[string]any{"fixers": map[string]any{"open": false}, "_armed": 0}); err != nil {
		return err
	}
	if err := s.into("fixers-error", "error_text", ""); err != nil {
		return err
	}
	return s.toast("ok", message)
}
