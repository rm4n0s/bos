package api

import (
	"net/http"
	"strings"

	"github.com/rm4n0s/bos/internal/domain"
	"github.com/rm4n0s/bos/internal/uploads"
)

func (a *API) groupRoutes() {
	a.add(route{Method: "GET", Path: "/groups", Paged: true, Summary: "List groups", Query: []string{"tag"}, Errors: []string{"TagInvalid"}, Run: a.listGroups})
	a.add(route{Method: "POST", Path: "/groups", Summary: "Create a group; the thumbnail may be uploaded", Success: http.StatusCreated,
		Body: "either", Errors: []string{"GroupCodeNameTaken", "CodeNameInvalid", "CodeNameReserved", "UploadTooLarge"}, Run: a.createGroup})
	a.add(route{Method: "POST", Path: "/groups/import-text", Summary: "Create a group with one item per line of text", Success: http.StatusCreated,
		Body: "json", Errors: []string{"GroupCodeNameTaken", "CodeNameInvalid"}, Run: a.importGroupText})
	a.add(route{Method: "GET", Path: "/groups/{id}", Summary: "Get a group", Errors: []string{"GroupNotFound"}, Run: a.getGroup})
	a.add(route{Method: "PUT", Path: "/groups/{id}", Summary: "Replace the editable fields of a group", Body: "either",
		Errors: []string{"GroupNotFound", "GroupCodeNameTaken", "UploadTooLarge"}, Run: a.updateGroup})
	a.add(route{Method: "DELETE", Path: "/groups/{id}", Summary: "Delete a group with its items and files", Success: http.StatusNoContent,
		Errors: []string{"GroupNotFound"}, Response: "none", Run: a.deleteGroup})
}

func (a *API) listGroups(w http.ResponseWriter, r *http.Request) error {
	p, err := pageOf(r)
	if err != nil {
		return err
	}
	out, err := a.Services.Groups.List(r.Context(), strings.ToLower(r.URL.Query().Get("tag")), p)
	if err != nil {
		return err
	}
	a.writeJSON(w, http.StatusOK, out)
	return nil
}

func (a *API) createGroup(w http.ResponseWriter, r *http.Request) error {
	var in domain.Group
	up, err := a.readBody(w, r, &in, uploads.GroupKinds(a.Dirs))
	if err != nil {
		return err
	}
	in.Thumbnail = "" // a new group has no file until one is uploaded
	if p, ok := up.Path("thumbnail"); ok {
		in.Thumbnail = p
	}
	out, err := a.Services.Groups.Create(r.Context(), in)
	if err != nil {
		a.discard(up)
		return err
	}
	a.created(w, "/groups", out.ID, out)
	return nil
}

func (a *API) importGroupText(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		domain.Group
		Text string `json:"text"`
	}
	if err := decodeJSON(w, r, &in); err != nil {
		return err
	}
	in.Thumbnail = ""
	out, err := a.Services.Groups.ImportText(r.Context(), in.Group, in.Text)
	if err != nil {
		return err
	}
	a.created(w, "/groups", out.ID, out)
	return nil
}

func (a *API) getGroup(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "id")
	if err != nil {
		return err
	}
	out, err := a.Services.Groups.Get(r.Context(), id)
	if err != nil {
		return err
	}
	a.writeJSON(w, http.StatusOK, out)
	return nil
}

func (a *API) updateGroup(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "id")
	if err != nil {
		return err
	}
	old, err := a.Services.Groups.Get(r.Context(), id)
	if err != nil {
		return err
	}
	var in domain.Group
	up, err := a.readBody(w, r, &in, uploads.GroupKinds(a.Dirs))
	if err != nil {
		return err
	}
	if path, ok := up.Path("thumbnail"); ok {
		in.Thumbnail = path
	} else if err := uploads.OwnedPath(in.Thumbnail, up, []string{old.Thumbnail}); err != nil {
		return err
	}
	out, err := a.Services.Groups.Update(r.Context(), id, in)
	if err != nil {
		a.discard(up)
		return err
	}
	a.writeJSON(w, http.StatusOK, out)
	return nil
}

func (a *API) deleteGroup(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "id")
	if err != nil {
		return err
	}
	if err := a.Services.Groups.Delete(r.Context(), id); err != nil {
		return err
	}
	noContent(w)
	return nil
}

// ---- items ----

func (a *API) itemRoutes() {
	a.add(route{Method: "GET", Path: "/groups/{id}/items", Paged: true, Summary: "List the items of a group", Errors: []string{"GroupNotFound"}, Run: a.listItems})
	a.add(route{Method: "POST", Path: "/groups/{id}/items", Summary: "Create an item; its images may be uploaded", Success: http.StatusCreated,
		Body: "either", Errors: []string{"ItemCodeNameTaken", "GroupNotFound", "UploadTooLarge", "MaskGenerationFailed", "CodeNameInvalid"}, Run: a.createItem})
	a.add(route{Method: "GET", Path: "/items/{id}", Summary: "Get an item", Errors: []string{"ItemNotFound"}, Run: a.getItem})
	a.add(route{Method: "PUT", Path: "/items/{id}", Summary: "Replace the editable fields of an item", Body: "either",
		Errors: []string{"ItemNotFound", "ItemCodeNameTaken", "UploadTooLarge", "MaskGenerationFailed"}, Run: a.updateItem})
	a.add(route{Method: "DELETE", Path: "/items/{id}", Summary: "Delete an item with its files", Success: http.StatusNoContent,
		Errors: []string{"ItemNotFound"}, Response: "none", Run: a.deleteItem})
}

func (a *API) listItems(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "id")
	if err != nil {
		return err
	}
	p, err := pageOf(r)
	if err != nil {
		return err
	}
	out, err := a.Services.Items.ListByGroup(r.Context(), id, p)
	if err != nil {
		return err
	}
	a.writeJSON(w, http.StatusOK, out)
	return nil
}

func (a *API) createItem(w http.ResponseWriter, r *http.Request) error {
	group, err := pathID(r, "id")
	if err != nil {
		return err
	}
	var in domain.Item
	up, err := a.readBody(w, r, &in, uploads.ItemKinds(a.Dirs))
	if err != nil {
		return err
	}
	if err := uploads.ApplyItem(&in, up, nil); err != nil {
		a.discard(up)
		return err
	}
	out, err := a.Services.Items.Create(r.Context(), group, in)
	if err != nil {
		a.discard(up)
		return err
	}
	a.created(w, "/items", out.ID, out)
	return nil
}

func (a *API) getItem(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "id")
	if err != nil {
		return err
	}
	out, err := a.Services.Items.Get(r.Context(), id)
	if err != nil {
		return err
	}
	a.writeJSON(w, http.StatusOK, out)
	return nil
}

func (a *API) updateItem(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "id")
	if err != nil {
		return err
	}
	old, err := a.Services.Items.Get(r.Context(), id)
	if err != nil {
		return err
	}
	var in domain.Item
	up, err := a.readBody(w, r, &in, uploads.ItemKinds(a.Dirs))
	if err != nil {
		return err
	}
	if err := uploads.ApplyItem(&in, up, old.Files()); err != nil {
		a.discard(up)
		return err
	}
	out, err := a.Services.Items.Update(r.Context(), id, in)
	if err != nil {
		a.discard(up)
		return err
	}
	a.writeJSON(w, http.StatusOK, out)
	return nil
}

func (a *API) deleteItem(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "id")
	if err != nil {
		return err
	}
	if err := a.Services.Items.Delete(r.Context(), id); err != nil {
		return err
	}
	noContent(w)
	return nil
}
