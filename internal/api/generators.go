package api

import (
	"encoding/json"
	"net/http"

	"github.com/rm4n0s/bos/internal/domain"
	"github.com/rm4n0s/bos/internal/workflow"
)

// generatorBody is a generator on the wire: the workflow is JSON, not base64.
type generatorBody struct {
	domain.Generator
	Workflow json.RawMessage `json:"workflow"`
}

func generatorOf(g *domain.Generator) generatorBody {
	return generatorBody{Generator: *g, Workflow: g.Workflow}
}

func (b generatorBody) domain() domain.Generator {
	g := b.Generator
	g.Workflow = b.Workflow
	return g
}

// fixerBody is a fixer on the wire.
type fixerBody struct {
	domain.Fixer
	Workflow json.RawMessage `json:"workflow"`
}

func fixerOf(f *domain.Fixer) fixerBody { return fixerBody{Fixer: *f, Workflow: f.Workflow} }

func (b fixerBody) domain() domain.Fixer {
	f := b.Fixer
	f.Workflow = b.Workflow
	return f
}

// analysisBody is the answer of the workflow analysis.
type analysisBody struct {
	OutputType          domain.OutputType       `json:"output_type"`
	OutputAttributes    domain.OutputAttributes `json:"output_attributes"`
	OutputNodeClassType string                  `json:"output_node_class_type"`
	OutputNodeTitle     string                  `json:"output_node_title"`
	PromptTitles        []string                `json:"prompt_titles"`
	LoadImageTitles     []string                `json:"load_image_titles"`
	SaveImageTitles     []string                `json:"save_image_titles"`
}

func analysisOf(an *workflow.Analysis) analysisBody {
	nonNil := func(s []string) []string {
		if s == nil {
			return []string{}
		}
		return s
	}
	return analysisBody{
		OutputType: an.OutputType, OutputAttributes: an.Attributes,
		OutputNodeClassType: an.OutputNodeClassType, OutputNodeTitle: an.OutputNodeTitle,
		PromptTitles: nonNil(an.PromptTitles), LoadImageTitles: nonNil(an.LoadImageTitles), SaveImageTitles: nonNil(an.SaveImageTitles),
	}
}

func (a *API) generatorRoutes() {
	wf := []string{"WorkflowInvalid", "WorkflowTitleDuplicated", "WorkflowTitleNotFound", "OutputNodeNotFound", "NoSamplerInWorkflow"}
	a.add(route{Method: "GET", Path: "/generators", Paged: true, Summary: "List generators", Run: a.listGenerators})
	a.add(route{Method: "POST", Path: "/generators", Summary: "Create a generator", Success: http.StatusCreated, Body: "json",
		Errors: append([]string{"GeneratorCodeNameTaken", "CodeNameInvalid"}, wf...), Run: a.createGenerator})
	a.add(route{Method: "POST", Path: "/generators/analyze", Summary: "Infer the output of a workflow; persists nothing", Body: "json",
		Errors: []string{"WorkflowInvalid", "OutputNodeNotFound"}, Run: a.analyzeWorkflow})
	a.add(route{Method: "GET", Path: "/generators/{id}", Summary: "Get a generator", Errors: []string{"GeneratorNotFound"}, Run: a.getGenerator})
	a.add(route{Method: "PUT", Path: "/generators/{id}", Summary: "Replace the editable fields of a generator", Body: "json",
		Errors: append([]string{"GeneratorNotFound", "GeneratorCodeNameTaken"}, wf...), Run: a.updateGenerator})
	a.add(route{Method: "DELETE", Path: "/generators/{id}", Summary: "Delete a generator", Success: http.StatusNoContent,
		Errors: []string{"GeneratorNotFound"}, Response: "none", Run: a.deleteGenerator})
}

func (a *API) listGenerators(w http.ResponseWriter, r *http.Request) error {
	p, err := pageOf(r)
	if err != nil {
		return err
	}
	page, err := a.Services.Generators.List(r.Context(), p)
	if err != nil {
		return err
	}
	items := make([]generatorBody, len(page.Items))
	for i := range page.Items {
		items[i] = generatorOf(&page.Items[i])
	}
	a.writeJSON(w, http.StatusOK, domain.Paged[generatorBody]{Items: items, Total: page.Total, Page: page.Page, PageSize: page.PageSize, TotalPages: page.TotalPages})
	return nil
}

func (a *API) createGenerator(w http.ResponseWriter, r *http.Request) error {
	var in generatorBody
	if err := decodeJSON(w, r, &in); err != nil {
		return err
	}
	out, err := a.Services.Generators.Create(r.Context(), in.domain())
	if err != nil {
		return err
	}
	a.created(w, "/generators", out.ID, generatorOf(out))
	return nil
}

func (a *API) analyzeWorkflow(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		Workflow json.RawMessage `json:"workflow"`
	}
	if err := decodeJSON(w, r, &in); err != nil {
		return err
	}
	an, err := a.Services.Generators.Analyze(in.Workflow)
	if err != nil {
		return err
	}
	a.writeJSON(w, http.StatusOK, analysisOf(an))
	return nil
}

func (a *API) getGenerator(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "id")
	if err != nil {
		return err
	}
	out, err := a.Services.Generators.Get(r.Context(), id)
	if err != nil {
		return err
	}
	a.writeJSON(w, http.StatusOK, generatorOf(out))
	return nil
}

func (a *API) updateGenerator(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "id")
	if err != nil {
		return err
	}
	var in generatorBody
	if err := decodeJSON(w, r, &in); err != nil {
		return err
	}
	out, err := a.Services.Generators.Update(r.Context(), id, in.domain())
	if err != nil {
		return err
	}
	a.writeJSON(w, http.StatusOK, generatorOf(out))
	return nil
}

func (a *API) deleteGenerator(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "id")
	if err != nil {
		return err
	}
	if err := a.Services.Generators.Delete(r.Context(), id); err != nil {
		return err
	}
	noContent(w)
	return nil
}

func (a *API) fixerRoutes() {
	a.add(route{Method: "GET", Path: "/fixers", Paged: true, Summary: "List fixers", Run: a.listFixers})
	a.add(route{Method: "POST", Path: "/fixers", Summary: "Create a fixer", Success: http.StatusCreated, Body: "json",
		Errors: []string{"FixerCodeNameTaken", "CodeNameInvalid", "FixerTitleRequired", "WorkflowInvalid"}, Run: a.createFixer})
	a.add(route{Method: "GET", Path: "/fixers/{id}", Summary: "Get a fixer", Errors: []string{"FixerNotFound"}, Run: a.getFixer})
	a.add(route{Method: "PUT", Path: "/fixers/{id}", Summary: "Replace the editable fields of a fixer", Body: "json",
		Errors: []string{"FixerNotFound", "FixerCodeNameTaken"}, Run: a.updateFixer})
	a.add(route{Method: "DELETE", Path: "/fixers/{id}", Summary: "Delete a fixer", Success: http.StatusNoContent,
		Errors: []string{"FixerNotFound"}, Response: "none", Run: a.deleteFixer})
}

func (a *API) listFixers(w http.ResponseWriter, r *http.Request) error {
	p, err := pageOf(r)
	if err != nil {
		return err
	}
	page, err := a.Services.Fixers.List(r.Context(), p)
	if err != nil {
		return err
	}
	items := make([]fixerBody, len(page.Items))
	for i := range page.Items {
		items[i] = fixerOf(&page.Items[i])
	}
	a.writeJSON(w, http.StatusOK, domain.Paged[fixerBody]{Items: items, Total: page.Total, Page: page.Page, PageSize: page.PageSize, TotalPages: page.TotalPages})
	return nil
}

func (a *API) createFixer(w http.ResponseWriter, r *http.Request) error {
	var in fixerBody
	if err := decodeJSON(w, r, &in); err != nil {
		return err
	}
	out, err := a.Services.Fixers.Create(r.Context(), in.domain())
	if err != nil {
		return err
	}
	a.created(w, "/fixers", out.ID, fixerOf(out))
	return nil
}

func (a *API) getFixer(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "id")
	if err != nil {
		return err
	}
	out, err := a.Services.Fixers.Get(r.Context(), id)
	if err != nil {
		return err
	}
	a.writeJSON(w, http.StatusOK, fixerOf(out))
	return nil
}

func (a *API) updateFixer(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "id")
	if err != nil {
		return err
	}
	var in fixerBody
	if err := decodeJSON(w, r, &in); err != nil {
		return err
	}
	out, err := a.Services.Fixers.Update(r.Context(), id, in.domain())
	if err != nil {
		return err
	}
	a.writeJSON(w, http.StatusOK, fixerOf(out))
	return nil
}

func (a *API) deleteFixer(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "id")
	if err != nil {
		return err
	}
	if err := a.Services.Fixers.Delete(r.Context(), id); err != nil {
		return err
	}
	noContent(w)
	return nil
}
