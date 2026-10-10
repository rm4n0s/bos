package ui

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/rm4n0s/bos/internal/domain"
	"github.com/rm4n0s/errors"
)

// generatorForm is what the generator dialog shows.
type generatorForm struct {
	ID            int64
	Name          string
	CodeName      string
	Workflow      string
	PositiveTitle string
	NegativeTitle string
	OutputType    string
	Attributes    string
	NodeClass     string
	NodeTitle     string
	RandomSeed    bool
	PromptTitles  []string
	Analysed      bool
}

type generatorsTable struct {
	Items []domain.Generator
	Pager pagerData
	Blank generatorForm
}

func generatorFormOf(g *domain.Generator) generatorForm {
	attrs := ""
	if len(g.OutputAttributes) > 0 {
		if b, err := json.MarshalIndent(g.OutputAttributes, "", "  "); err == nil {
			attrs = string(b)
		}
	}
	return generatorForm{
		ID: g.ID, Name: g.Name, CodeName: g.CodeName, Workflow: string(g.Workflow),
		PositiveTitle: g.PositivePromptTitle, NegativeTitle: g.NegativePromptTitle, OutputType: string(g.OutputType),
		Attributes: attrs, NodeClass: g.OutputNodeClassType, NodeTitle: g.OutputNodeTitle, RandomSeed: g.HasRandomSeed,
	}
}

// generatorFromForm reads the dialog. Fields the user left empty stay empty:
// the service fills them from the workflow's analysis.
func generatorFromForm(f *form) (domain.Generator, generatorForm, error) {
	id, err := f.id()
	if err != nil {
		return domain.Generator{}, generatorForm{}, err
	}
	g := domain.Generator{
		ID: id, Name: f.str("name"), CodeName: f.str("code_name"), Workflow: []byte(f.raw("workflow")),
		PositivePromptTitle: f.str("positive_prompt_title"), NegativePromptTitle: f.str("negative_prompt_title"),
		OutputType: domain.OutputType(f.str("output_type")), OutputNodeClassType: f.str("output_node_class_type"),
		OutputNodeTitle: f.str("output_node_title"), HasRandomSeed: f.checked("has_random_seed"),
	}
	if attrs := f.raw("output_attributes"); attrs != "" {
		if err := json.Unmarshal([]byte(attrs), &g.OutputAttributes); err != nil {
			return g, generatorFormOf(&g), errors.New("InvalidRequest", "the output attributes are not valid JSON: "+err.Error(), "field", "output_attributes")
		}
	}
	form := generatorFormOf(&g)
	form.Attributes = f.raw("output_attributes")
	return g, form, nil
}

func (u *UI) generatorsTable(r *http.Request) (generatorsTable, error) {
	p, err := pageParam(r)
	if err != nil {
		return generatorsTable{}, err
	}
	out, err := u.Services.Generators.List(r.Context(), p)
	if err != nil {
		return generatorsTable{}, err
	}
	return generatorsTable{Items: out.Items, Pager: pagerOf("/ui/generators/table", out), Blank: generatorForm{OutputType: "image"}}, nil
}

func (u *UI) generatorRoutes() {
	u.handle("GET /generators", func(w http.ResponseWriter, r *http.Request) {
		t, err := u.generatorsTable(r)
		if err != nil {
			u.failPage(w, r, err)
			return
		}
		u.renderPage(w, r, "generators", page{Title: "Generators", Nav: "generators", Data: t})
	})
	u.handle("GET /ui/generators/table", view(u, func(r *http.Request, _ *struct{}, s *stream) error {
		t, err := u.generatorsTable(r)
		if err != nil {
			return err
		}
		return s.render("generators_table", t)
	}))
	u.handle("GET /ui/generators/new", view(u, func(_ *http.Request, _ *struct{}, s *stream) error {
		if err := s.into("generator-form-body", "generator_form", generatorForm{OutputType: "image"}); err != nil {
			return err
		}
		return s.set(map[string]any{"generators": map[string]any{"open": true}})
	}))
	u.handle("GET /ui/generators/{id}/edit", view(u, func(r *http.Request, _ *struct{}, s *stream) error {
		id, err := pathID(r, "id")
		if err != nil {
			return err
		}
		g, err := u.Services.Generators.Get(r.Context(), id)
		if err != nil {
			return err
		}
		if err := s.into("generator-form-body", "generator_form", generatorFormOf(g)); err != nil {
			return err
		}
		return s.set(map[string]any{"generators": map[string]any{"open": true}})
	}))
	u.handle("POST /ui/generators/analyze", u.actForm("generators-error", func(r *http.Request, f *form, s *stream) error {
		g, shown, err := generatorFromForm(f)
		if err != nil {
			return err
		}
		an, err := u.Services.Generators.Analyze(g.Workflow)
		if err != nil {
			return err
		}
		// What the workflow decides replaces what was shown; the attributes
		// only fill an empty box, so the user's edits stay.
		shown.OutputType = string(an.OutputType)
		shown.NodeClass = an.OutputNodeClassType
		shown.NodeTitle = an.OutputNodeTitle
		if shown.Attributes == "" && len(an.Attributes) > 0 {
			if b, err := json.MarshalIndent(an.Attributes, "", "  "); err == nil {
				shown.Attributes = string(b)
			}
		}
		shown.PromptTitles = an.PromptTitles
		// The nodes titled "positive…" and "negative…" are the defaults; a title the user typed stays.
		if strings.TrimSpace(shown.PositiveTitle) == "" {
			shown.PositiveTitle = an.PositiveTitle
		}
		if strings.TrimSpace(shown.NegativeTitle) == "" {
			shown.NegativeTitle = an.NegativeTitle
		}
		shown.Analysed = true
		return s.into("generator-form-body", "generator_form", shown)
	}))
	u.handle("POST /ui/generators", u.actForm("generators-error", func(r *http.Request, f *form, s *stream) error {
		g, _, err := generatorFromForm(f)
		if err != nil {
			return err
		}
		if g.ID > 0 {
			_, err = u.Services.Generators.Update(r.Context(), g.ID, g)
		} else {
			_, err = u.Services.Generators.Create(r.Context(), g)
		}
		if err != nil {
			return err
		}
		return u.refreshGenerators(r, s, "Generator saved.")
	}))
	u.handle("DELETE /ui/generators/{id}", act(u, "", func(r *http.Request, _ *struct{}, s *stream) error {
		id, err := pathID(r, "id")
		if err != nil {
			return err
		}
		if err := u.Services.Generators.Delete(r.Context(), id); err != nil {
			return err
		}
		return u.refreshGenerators(r, s, "Generator deleted.")
	}))
}

func (u *UI) refreshGenerators(r *http.Request, s *stream, message string) error {
	t, err := u.generatorsTable(r)
	if err != nil {
		return err
	}
	if err := s.render("generators_table", t); err != nil {
		return err
	}
	if err := s.set(map[string]any{"generators": map[string]any{"open": false}, "_armed": 0}); err != nil {
		return err
	}
	if err := s.into("generators-error", "error_text", ""); err != nil {
		return err
	}
	return s.toast("ok", message)
}
