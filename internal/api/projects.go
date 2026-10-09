package api

import (
	"encoding/json"
	"net/http"

	"github.com/rm4n0s/bos/internal/domain"
	"github.com/rm4n0s/bos/internal/service"
)

// commandBody is a command on the wire: the parsed form is JSON, not base64.
type commandBody struct {
	domain.Command
	CommandJSON json.RawMessage `json:"command_json"`
}

func commandOf(c *domain.Command) commandBody {
	return commandBody{Command: *c, CommandJSON: c.CommandJSON}
}

// commandSummaryBody is a command with the progress of its jobs.
type commandSummaryBody struct {
	commandBody
	FinishedJobs int `json:"finished_jobs"`
	TotalJobs    int `json:"total_jobs"`
}

func summaryOf(c *domain.CommandSummary) commandSummaryBody {
	return commandSummaryBody{commandBody: commandOf(&c.Command), FinishedJobs: c.FinishedJobs, TotalJobs: c.TotalJobs}
}

func (a *API) projectRoutes() {
	a.add(route{Method: "GET", Path: "/projects", Paged: true, Summary: "List projects", Run: a.listProjects})
	a.add(route{Method: "POST", Path: "/projects", Summary: "Create a project", Success: http.StatusCreated, Body: "json",
		Errors: []string{"NameInvalid"}, Run: a.createProject})
	a.add(route{Method: "GET", Path: "/projects/{id}", Summary: "Get a project", Errors: []string{"ProjectNotFound"}, Run: a.getProject})
	a.add(route{Method: "PUT", Path: "/projects/{id}", Summary: "Rename a project", Body: "json",
		Errors: []string{"ProjectNotFound", "NameInvalid"}, Run: a.updateProject})
	a.add(route{Method: "DELETE", Path: "/projects/{id}", Summary: "Delete a project with its commands and jobs", Success: http.StatusNoContent,
		Errors: []string{"ProjectNotFound", "CommandBusy"}, Response: "none", Run: a.deleteProject})
}

func (a *API) listProjects(w http.ResponseWriter, r *http.Request) error {
	p, err := pageOf(r)
	if err != nil {
		return err
	}
	out, err := a.Services.Projects.List(r.Context(), p)
	if err != nil {
		return err
	}
	a.writeJSON(w, http.StatusOK, out)
	return nil
}

func (a *API) createProject(w http.ResponseWriter, r *http.Request) error {
	var in domain.Project
	if err := decodeJSON(w, r, &in); err != nil {
		return err
	}
	out, err := a.Services.Projects.Create(r.Context(), in)
	if err != nil {
		return err
	}
	a.created(w, "/projects", out.ID, out)
	return nil
}

func (a *API) getProject(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "id")
	if err != nil {
		return err
	}
	out, err := a.Services.Projects.Get(r.Context(), id)
	if err != nil {
		return err
	}
	a.writeJSON(w, http.StatusOK, out)
	return nil
}

func (a *API) updateProject(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "id")
	if err != nil {
		return err
	}
	var in domain.Project
	if err := decodeJSON(w, r, &in); err != nil {
		return err
	}
	out, err := a.Services.Projects.Update(r.Context(), id, in)
	if err != nil {
		return err
	}
	a.writeJSON(w, http.StatusOK, out)
	return nil
}

func (a *API) deleteProject(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "id")
	if err != nil {
		return err
	}
	if err := a.Services.Projects.Delete(r.Context(), id); err != nil {
		return err
	}
	noContent(w)
	return nil
}

// commandInput is the body that adds or edits a command.
type commandInput struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Code        string `json:"code"`
	InsertAt    int    `json:"insert_at"`
}

func (c commandInput) service() service.CommandInput {
	return service.CommandInput{Name: c.Name, Description: c.Description, Code: c.Code, InsertAt: c.InsertAt}
}
