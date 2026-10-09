package api

import (
	"net/http"

	"github.com/rm4n0s/bos/internal/domain"
)

func (a *API) tagRoutes() {
	a.add(route{Method: "GET", Path: "/tags", Summary: "List the tags in use with their counts", Query: []string{"scope", "prefix"},
		Errors: []string{"InvalidRequest"}, Run: a.listTags})
	a.add(route{Method: "POST", Path: "/tags/rename", Summary: "Rename or merge a tag", Body: "json",
		Errors: []string{"TagInvalid", "TagNotFound", "InvalidRequest"}, Run: a.renameTag})
}

func (a *API) listTags(w http.ResponseWriter, r *http.Request) error {
	q := r.URL.Query()
	tags, err := a.Services.Tags.List(r.Context(), domain.TagScope(q.Get("scope")), q.Get("prefix"))
	if err != nil {
		return err
	}
	if tags == nil {
		tags = []domain.TagCount{}
	}
	a.writeJSON(w, http.StatusOK, map[string]any{"tags": tags})
	return nil
}

func (a *API) renameTag(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		Scope domain.TagScope `json:"scope"`
		From  string          `json:"from"`
		To    string          `json:"to"`
	}
	if err := decodeJSON(w, r, &in); err != nil {
		return err
	}
	out, err := a.Services.Tags.Rename(r.Context(), in.Scope, in.From, in.To)
	if err != nil {
		return err
	}
	if out.AffectedCommands == nil {
		out.AffectedCommands = []domain.Command{}
	}
	a.writeJSON(w, http.StatusOK, out)
	return nil
}
