package api

import (
	"net/http"
	"strconv"

	"github.com/rm4n0s/bos/internal/domain"
)

func (a *API) commandRoutes() {
	cmdErrs := []string{"CommandLexError", "CommandSyntaxError", "CommandRestrictionViolated", "CommandValidationFailed"}
	a.add(route{Method: "GET", Path: "/projects/{id}/commands", Paged: true, Summary: "List the commands of a project with their progress",
		Errors: []string{"ProjectNotFound"}, Run: a.listCommands})
	a.add(route{Method: "POST", Path: "/projects/{id}/commands", Summary: "Add a command and create its jobs", Success: http.StatusCreated, Body: "json",
		Errors: append([]string{"ProjectNotFound", "NameInvalid"}, cmdErrs...), Run: a.addCommand})
	a.add(route{Method: "POST", Path: "/projects/{id}/commands/renumber", Summary: "Number the commands of a project 1..N",
		Errors: []string{"ProjectNotFound"}, Run: a.renumberCommands})
	a.add(route{Method: "POST", Path: "/commands/preview", Summary: "Validate a command text and report what it would create; persists nothing",
		Body: "json", Errors: []string{"InvalidRequest"}, Run: a.previewCommand})
	a.add(route{Method: "GET", Path: "/commands/{id}", Summary: "Get a command", Errors: []string{"CommandNotFound"}, Run: a.getCommand})
	a.add(route{Method: "PUT", Path: "/commands/{id}", Summary: "Edit a command; a changed text recreates its jobs", Body: "json",
		Errors: append([]string{"CommandNotFound", "CommandBusy"}, cmdErrs...), Run: a.editCommand})
	a.add(route{Method: "DELETE", Path: "/commands/{id}", Summary: "Delete a command with its jobs and files", Success: http.StatusNoContent,
		Errors: []string{"CommandNotFound", "CommandBusy"}, Response: "none", Run: a.deleteCommand})
	a.add(route{Method: "POST", Path: "/commands/{id}/move", Summary: "Move a command to a position in its project", Body: "json",
		Errors: []string{"CommandNotFound"}, Run: a.moveCommand})
	a.add(route{Method: "POST", Path: "/commands/{id}/run", Summary: "Queue the idle jobs of a command", Success: http.StatusAccepted,
		Errors: []string{"CommandNotFound", "CommandNotRunnable"}, Run: a.runCommand})
	a.add(route{Method: "POST", Path: "/commands/{id}/stop", Summary: "Return the queued jobs of a command to idle",
		Errors: []string{"CommandNotFound"}, Run: a.stopCommand})
	a.add(route{Method: "POST", Path: "/commands/{id}/recreate", Summary: "Delete the jobs of a command and create them again", Success: http.StatusAccepted,
		Errors: append([]string{"CommandNotFound", "CommandBusy"}, cmdErrs...), Run: a.recreateCommand})
	a.add(route{Method: "GET", Path: "/commands/{id}/download", Summary: "Download a ZIP of the results and a summary of the prompts",
		Response: "file", Errors: []string{"CommandNotFound", "CommandNotFinished", "FileNotFound"}, Run: a.downloadCommand})
}

func (a *API) listCommands(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "id")
	if err != nil {
		return err
	}
	p, err := pageOf(r)
	if err != nil {
		return err
	}
	return a.writeCommandPage(w, r, id, p)
}

func (a *API) writeCommandPage(w http.ResponseWriter, r *http.Request, project domain.ID, p domain.Page) error {
	page, err := a.Services.Commands.List(r.Context(), project, p)
	if err != nil {
		return err
	}
	items := make([]commandSummaryBody, len(page.Items))
	for i := range page.Items {
		items[i] = summaryOf(&page.Items[i])
	}
	a.writeJSON(w, http.StatusOK, domain.Paged[commandSummaryBody]{
		Items: items, Total: page.Total, Page: page.Page, PageSize: page.PageSize, TotalPages: page.TotalPages,
	})
	return nil
}

func (a *API) addCommand(w http.ResponseWriter, r *http.Request) error {
	project, err := pathID(r, "id")
	if err != nil {
		return err
	}
	var in commandInput
	if err := decodeJSON(w, r, &in); err != nil {
		return err
	}
	out, err := a.Services.Commands.Add(r.Context(), project, in.service())
	if err != nil {
		return err
	}
	a.created(w, "/commands", out.ID, commandOf(out))
	return nil
}

func (a *API) renumberCommands(w http.ResponseWriter, r *http.Request) error {
	project, err := pathID(r, "id")
	if err != nil {
		return err
	}
	if err := a.Services.Commands.Renumber(r.Context(), project); err != nil {
		return err
	}
	return a.writeCommandPage(w, r, project, domain.Page{Number: 1, Size: domain.MaxPageSize})
}

func (a *API) previewCommand(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		Code string `json:"code"`
	}
	if err := decodeJSON(w, r, &in); err != nil {
		return err
	}
	out, err := a.Services.Commands.Preview(r.Context(), in.Code)
	if err != nil {
		return err
	}
	a.writeJSON(w, http.StatusOK, out)
	return nil
}

func (a *API) getCommand(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "id")
	if err != nil {
		return err
	}
	out, err := a.Services.Commands.Get(r.Context(), id)
	if err != nil {
		return err
	}
	a.writeJSON(w, http.StatusOK, commandOf(out))
	return nil
}

func (a *API) editCommand(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "id")
	if err != nil {
		return err
	}
	var in commandInput
	if err := decodeJSON(w, r, &in); err != nil {
		return err
	}
	out, err := a.Services.Commands.Edit(r.Context(), id, in.service())
	if err != nil {
		return err
	}
	a.writeJSON(w, http.StatusOK, commandOf(out))
	return nil
}

func (a *API) deleteCommand(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "id")
	if err != nil {
		return err
	}
	if err := a.Services.Commands.Delete(r.Context(), id); err != nil {
		return err
	}
	noContent(w)
	return nil
}

func (a *API) moveCommand(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "id")
	if err != nil {
		return err
	}
	var in struct {
		Order int `json:"order"`
	}
	if err := decodeJSON(w, r, &in); err != nil {
		return err
	}
	if _, err := a.Services.Commands.Move(r.Context(), id, in.Order); err != nil {
		return err
	}
	return a.respondCommand(w, r, id, http.StatusOK)
}

func (a *API) runCommand(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "id")
	if err != nil {
		return err
	}
	out, err := a.Services.Commands.Run(r.Context(), id)
	if err != nil {
		return err
	}
	a.writeJSON(w, http.StatusAccepted, commandOf(out))
	return nil
}

func (a *API) stopCommand(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "id")
	if err != nil {
		return err
	}
	if _, err := a.Services.Commands.Stop(r.Context(), id); err != nil {
		return err
	}
	return a.respondCommand(w, r, id, http.StatusOK)
}

func (a *API) recreateCommand(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "id")
	if err != nil {
		return err
	}
	out, err := a.Services.Commands.Recreate(r.Context(), id)
	if err != nil {
		return err
	}
	a.writeJSON(w, http.StatusAccepted, commandOf(out))
	return nil
}

// respondCommand answers with the current state of a command.
func (a *API) respondCommand(w http.ResponseWriter, r *http.Request, id domain.ID, status int) error {
	out, err := a.Services.Commands.Get(r.Context(), id)
	if err != nil {
		return err
	}
	a.writeJSON(w, status, commandOf(out))
	return nil
}

func (a *API) downloadCommand(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "id")
	if err != nil {
		return err
	}
	path, err := a.Services.Commands.Download(r.Context(), id)
	if err != nil {
		return err
	}
	name := "command-" + strconv.FormatInt(id, 10) + ".zip"
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	return a.fileServer.Send(w, r, path, "application/zip")
}
