package api

import (
	"net/http"
)

func (a *API) loraRoutes() {
	a.add(route{Method: "GET", Path: "/loras", Paged: true, Query: []string{"q"},
		Summary: "List the LoRAs of all items and all ComfyUI servers, with how many items carry each and the servers that have it",
		Run:     a.listLoRAs})
	a.add(route{Method: "GET", Path: "/loras/detail", Query: []string{"name"},
		Summary: "Say on which servers a LoRA is installed and which items carry it",
		Errors:  []string{"InvalidRequest", "LoRANotFound"}, Run: a.getLoRA})
	a.add(route{Method: "DELETE", Path: "/loras", Query: []string{"name"},
		Summary: "Delete a LoRA from every ComfyUI server and the Civitai cache, and delete the items that carry it",
		Errors:  []string{"InvalidRequest", "LoRANotFound", "LoRADeleteFailed"}, Run: a.deleteLoRA})
}

func (a *API) listLoRAs(w http.ResponseWriter, r *http.Request) error {
	p, err := pageOf(r)
	if err != nil {
		return err
	}
	out, err := a.Services.LoRAs.List(r.Context(), r.URL.Query().Get("q"), p)
	if err != nil {
		return err
	}
	a.writeJSON(w, http.StatusOK, out)
	return nil
}

func (a *API) getLoRA(w http.ResponseWriter, r *http.Request) error {
	out, err := a.Services.LoRAs.Get(r.Context(), r.URL.Query().Get("name"))
	if err != nil {
		return err
	}
	a.writeJSON(w, http.StatusOK, out)
	return nil
}

func (a *API) deleteLoRA(w http.ResponseWriter, r *http.Request) error {
	out, err := a.Services.LoRAs.Delete(r.Context(), r.URL.Query().Get("name"))
	if err != nil {
		return err
	}
	a.writeJSON(w, http.StatusOK, out)
	return nil
}
