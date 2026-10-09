package api

import (
	"net/http"

	"github.com/rm4n0s/bos/internal/domain"
	"github.com/rm4n0s/bos/internal/service"
)

func (a *API) jobRoutes() {
	a.add(route{Method: "GET", Path: "/commands/{id}/jobs", Paged: true, Summary: "List the jobs of a command", Query: []string{"status"},
		Errors: []string{"CommandNotFound", "InvalidRequest"}, Run: a.listJobs})
	a.add(route{Method: "GET", Path: "/jobs/{id}", Summary: "Get a job with its error and trace", Errors: []string{"JobNotFound"}, Run: a.getJob})
	a.add(route{Method: "PUT", Path: "/jobs/{id}", Summary: "Edit the prompts of a job; a change resets it", Body: "json",
		Errors: []string{"JobNotFound", "JobBusy"}, Run: a.editJob})
	a.add(route{Method: "POST", Path: "/jobs/{id}/run", Summary: "Queue an idle job", Success: http.StatusAccepted,
		Errors: []string{"JobNotFound", "JobNotRunnable"}, Run: a.runJob})
	a.add(route{Method: "POST", Path: "/jobs/{id}/stop", Summary: "Return a queued job to idle",
		Errors: []string{"JobNotFound", "JobNotRunnable"}, Run: a.stopJob})
}

func (a *API) listJobs(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "id")
	if err != nil {
		return err
	}
	p, err := pageOf(r)
	if err != nil {
		return err
	}
	out, err := a.Services.Jobs.ListByCommand(r.Context(), id, domain.Status(r.URL.Query().Get("status")), p)
	if err != nil {
		return err
	}
	a.writeJSON(w, http.StatusOK, out)
	return nil
}

// jobAction runs an operation on the job named in the path and answers with
// the job.
func (a *API) jobAction(w http.ResponseWriter, r *http.Request, status int, do func(*service.JobService, domain.ID) (*domain.Job, error)) error {
	id, err := pathID(r, "id")
	if err != nil {
		return err
	}
	out, err := do(a.Services.Jobs, id)
	if err != nil {
		return err
	}
	a.writeJSON(w, status, out)
	return nil
}

func (a *API) getJob(w http.ResponseWriter, r *http.Request) error {
	return a.jobAction(w, r, http.StatusOK, func(s *service.JobService, id domain.ID) (*domain.Job, error) { return s.Get(r.Context(), id) })
}

func (a *API) runJob(w http.ResponseWriter, r *http.Request) error {
	return a.jobAction(w, r, http.StatusAccepted, func(s *service.JobService, id domain.ID) (*domain.Job, error) { return s.Run(r.Context(), id) })
}

func (a *API) stopJob(w http.ResponseWriter, r *http.Request) error {
	return a.jobAction(w, r, http.StatusOK, func(s *service.JobService, id domain.ID) (*domain.Job, error) { return s.Stop(r.Context(), id) })
}

func (a *API) editJob(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		PositivePrompt string `json:"positive_prompt"`
		NegativePrompt string `json:"negative_prompt"`
	}
	if err := decodeJSON(w, r, &in); err != nil {
		return err
	}
	return a.jobAction(w, r, http.StatusOK, func(s *service.JobService, id domain.ID) (*domain.Job, error) {
		return s.EditPrompts(r.Context(), id, in.PositivePrompt, in.NegativePrompt)
	})
}

func (a *API) replRoutes() {
	a.add(route{Method: "POST", Path: "/repl/run", Summary: "Run one ad-hoc job without a project", Success: http.StatusAccepted, Body: "json",
		Errors: []string{"ReplInvalid", "ServerNotFound", "GeneratorNotFound", "GroupNotFound", "ItemNotFound", "JobBusy"}, Run: a.runRepl})
	a.add(route{Method: "GET", Path: "/repl/last", Summary: "Get the previous REPL job", Errors: []string{"JobNotFound"}, Run: a.lastRepl})
}

func (a *API) runRepl(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		Server    string        `json:"server"`
		Generator string        `json:"generator"`
		Positive  string        `json:"positive"`
		Negative  string        `json:"negative"`
		Items     string        `json:"items"`
		LoRAs     []domain.LoRA `json:"loras"`
	}
	if err := decodeJSON(w, r, &in); err != nil {
		return err
	}
	out, err := a.Services.Repl.Run(r.Context(), service.ReplInput{
		Server: in.Server, Generator: in.Generator, Positive: in.Positive, Negative: in.Negative, Items: in.Items, LoRAs: in.LoRAs,
	})
	if err != nil {
		return err
	}
	a.writeJSON(w, http.StatusAccepted, out)
	return nil
}

func (a *API) lastRepl(w http.ResponseWriter, r *http.Request) error {
	out, err := a.Services.Repl.Last(r.Context())
	if err != nil {
		return err
	}
	a.writeJSON(w, http.StatusOK, out)
	return nil
}
