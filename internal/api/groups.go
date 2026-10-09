package api

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/rm4n0s/bos/internal/domain"
)

func (a *API) groupKinds(field string) (uploadKind, bool) {
	if field == "thumbnail" {
		return uploadKind{dir: a.Dirs.Thumbnails, mimes: thumbnailTypes}, true
	}
	return uploadKind{}, false
}

// ownedPath checks that a file path a client sent is one the request stored or
// one the entity already had: clients upload files, they do not name them.
func ownedPath(path string, up *uploadSet, existing []string) error {
	if path == "" || up.has(path) {
		return nil
	}
	for _, e := range existing {
		if e == path {
			return nil
		}
	}
	return badRequest("file paths cannot be set; upload the file in the multipart form instead")
}

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
	up, err := a.readBody(w, r, &in, a.groupKinds)
	if err != nil {
		return err
	}
	in.Thumbnail = up.files["thumbnail"]
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
	up, err := a.readBody(w, r, &in, a.groupKinds)
	if err != nil {
		return err
	}
	if path, ok := up.files["thumbnail"]; ok {
		in.Thumbnail = path
	} else if err := ownedPath(in.Thumbnail, up, []string{old.Thumbnail}); err != nil {
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

// controlNetField is the form field of the image of the n-th ControlNet.
func controlNetIndex(field string) (int, bool) {
	rest, ok := strings.CutPrefix(field, "controlnet_")
	if !ok {
		return 0, false
	}
	n, err := strconv.Atoi(rest)
	return n, err == nil && n >= 0 && strconv.Itoa(n) == rest
}

func (a *API) itemKinds(field string) (uploadKind, bool) {
	switch field {
	case "thumbnail":
		return uploadKind{dir: a.Dirs.Thumbnails, mimes: thumbnailTypes}, true
	case "ip_adapter_image":
		return uploadKind{dir: a.Dirs.IPAdapter, mimes: referenceTypes}, true
	case "mask_reference":
		return uploadKind{dir: a.Dirs.Regions, mimes: maskTypes}, true
	}
	if _, ok := controlNetIndex(field); ok {
		return uploadKind{dir: a.Dirs.ControlNet, mimes: referenceTypes}, true
	}
	return uploadKind{}, false
}

// applyItemUploads puts the stored files into the item, and checks that every
// other file path in it is one the item already had.
func applyItemUploads(in *domain.Item, up *uploadSet, existing []string) error {
	if p, ok := up.files["thumbnail"]; ok {
		in.Thumbnail = p
	}
	if p, ok := up.files["ip_adapter_image"]; ok {
		if in.IPAdapter == nil {
			return badRequest("ip_adapter_image needs ip_adapter in the data part")
		}
		in.IPAdapter.ImageFile = p
	}
	if p, ok := up.files["mask_reference"]; ok {
		if in.MaskRegion == nil {
			return badRequest("mask_reference needs mask_region_images in the data part")
		}
		in.MaskRegion.ReferencePath = p
	}
	for field, p := range up.files {
		if n, ok := controlNetIndex(field); ok {
			if n >= len(in.ControlNets) {
				return badRequest("the file part has no matching entry in controlnets", "part", field)
			}
			in.ControlNets[n].ImagePath = p
		}
	}
	// The masks and their folder are made by the server, never named by a client.
	if in.MaskRegion != nil {
		in.MaskRegion.FolderPath, in.MaskRegion.MaskFiles = "", nil
	}
	var paths []string
	paths = append(paths, in.Thumbnail)
	if in.IPAdapter != nil {
		paths = append(paths, in.IPAdapter.ImageFile)
	}
	if in.MaskRegion != nil {
		paths = append(paths, in.MaskRegion.ReferencePath)
	}
	for _, c := range in.ControlNets {
		paths = append(paths, c.ImagePath)
	}
	for _, p := range paths {
		if err := ownedPath(p, up, existing); err != nil {
			return err
		}
	}
	return nil
}

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
	up, err := a.readBody(w, r, &in, a.itemKinds)
	if err != nil {
		return err
	}
	if err := applyItemUploads(&in, up, nil); err != nil {
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
	up, err := a.readBody(w, r, &in, a.itemKinds)
	if err != nil {
		return err
	}
	if err := applyItemUploads(&in, up, old.Files()); err != nil {
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
