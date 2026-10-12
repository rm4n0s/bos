package api

import (
	"net/http"

	"github.com/rm4n0s/bos/internal/domain"
)

func (a *API) serverRoutes() {
	a.add(route{Method: "GET", Path: "/servers", Paged: true, Summary: "List servers with their live status", Run: a.listServers})
	a.add(route{Method: "POST", Path: "/servers", Summary: "Create a server", Success: http.StatusCreated, Body: "json",
		Errors: []string{"CodeNameInvalid", "ServerCodeNameTaken", "ServerHostInvalid", "ServerMaxJobsInvalid", "NameInvalid"}, Run: a.createServer})
	a.add(route{Method: "GET", Path: "/servers/{id}", Summary: "Get a server", Errors: []string{"ServerNotFound"}, Run: a.getServer})
	a.add(route{Method: "PUT", Path: "/servers/{id}", Summary: "Replace the editable fields of a server", Body: "json",
		Errors: []string{"ServerNotFound", "ServerCodeNameTaken", "ServerHostInvalid", "ServerMaxJobsInvalid", "NameInvalid"}, Run: a.updateServer})
	a.add(route{Method: "DELETE", Path: "/servers/{id}", Summary: "Delete a server", Success: http.StatusNoContent,
		Errors: []string{"ServerNotFound"}, Response: "none", Run: a.deleteServer})
	a.add(route{Method: "GET", Path: "/servers/{id}/loras", Summary: "List the LoRAs installed on a server's ComfyUI",
		Errors: []string{"ServerNotFound", "ServerUnreachable"}, Run: a.serverLoRAs})
}

func (a *API) listServers(w http.ResponseWriter, r *http.Request) error {
	p, err := pageOf(r)
	if err != nil {
		return err
	}
	out, err := a.Services.Servers.List(r.Context(), p)
	if err != nil {
		return err
	}
	a.writeJSON(w, http.StatusOK, out)
	return nil
}

func (a *API) createServer(w http.ResponseWriter, r *http.Request) error {
	var in domain.Server
	if err := decodeJSON(w, r, &in); err != nil {
		return err
	}
	out, err := a.Services.Servers.Create(r.Context(), in)
	if err != nil {
		return err
	}
	a.created(w, "/servers", out.ID, out)
	return nil
}

func (a *API) getServer(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "id")
	if err != nil {
		return err
	}
	out, err := a.Services.Servers.Get(r.Context(), id)
	if err != nil {
		return err
	}
	a.writeJSON(w, http.StatusOK, out)
	return nil
}

func (a *API) updateServer(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "id")
	if err != nil {
		return err
	}
	var in domain.Server
	if err := decodeJSON(w, r, &in); err != nil {
		return err
	}
	out, err := a.Services.Servers.Update(r.Context(), id, in)
	if err != nil {
		return err
	}
	a.writeJSON(w, http.StatusOK, out)
	return nil
}

func (a *API) deleteServer(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "id")
	if err != nil {
		return err
	}
	if err := a.Services.Servers.Delete(r.Context(), id); err != nil {
		return err
	}
	noContent(w)
	return nil
}

func (a *API) serverLoRAs(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "id")
	if err != nil {
		return err
	}
	names, err := a.Services.Servers.LoRAs(r.Context(), id)
	if err != nil {
		return err
	}
	if names == nil {
		names = []string{}
	}
	a.writeJSON(w, http.StatusOK, map[string]any{"loras": names})
	return nil
}
