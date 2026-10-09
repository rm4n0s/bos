package ui

import (
	"net/http"
	"strconv"

	"github.com/rm4n0s/bos/internal/domain"
)

// jobView is a job as the grid shows it.
type jobView struct {
	// Job is embedded, so a template reads both .ID and .Job.PositivePrompt.
	domain.Job
	ResultURL  string
	IsImage    bool
	SeedText   string
	FixerCount int
	Running    bool
}

type statusCount struct {
	Status domain.Status
	Count  int
}

type commandHeader struct {
	Command         domain.Command
	Finished, Total int
	Counts          []statusCount
}

type jobsTable struct {
	Project  domain.Project
	Command  domain.Command
	Header   commandHeader
	Items    []jobView
	Pager    pagerData
	Status   string
	Statuses []domain.Status
}

func (u *UI) jobViewOf(j *domain.Job) jobView {
	v := jobView{Job: *j, FixerCount: len(j.Fixers), Running: j.Status == domain.StatusProcessing}
	if j.Status == domain.StatusFinished {
		v.ResultURL = u.fileURL("results", j.ResultPath)
	}
	v.IsImage = j.OutputType == domain.OutputImage
	if j.Seed != nil {
		v.SeedText = strconv.FormatUint(*j.Seed, 10)
	}
	return v
}

func (u *UI) commandHeaderOf(r *http.Request, id domain.ID) (commandHeader, error) {
	sum, err := u.Services.Commands.Summary(r.Context(), id)
	if err != nil {
		return commandHeader{}, err
	}
	counts, err := u.Services.Jobs.Counts(r.Context(), id)
	if err != nil {
		return commandHeader{}, err
	}
	h := commandHeader{Command: sum.Command, Finished: sum.FinishedJobs, Total: sum.TotalJobs}
	for _, st := range jobStatuses {
		if n := counts[st]; n > 0 && st != domain.StatusFinished {
			h.Counts = append(h.Counts, statusCount{Status: st, Count: n})
		}
	}
	return h, nil
}

var jobStatuses = []domain.Status{domain.StatusIdle, domain.StatusQueued, domain.StatusProcessing, domain.StatusError, domain.StatusFinished}

func (u *UI) jobsTable(r *http.Request, commandID domain.ID) (jobsTable, error) {
	p, err := pageParam(r)
	if err != nil {
		return jobsTable{}, err
	}
	status := r.URL.Query().Get("status")
	header, err := u.commandHeaderOf(r, commandID)
	if err != nil {
		return jobsTable{}, err
	}
	project, err := u.Services.Projects.Get(r.Context(), header.Command.ProjectID)
	if err != nil {
		return jobsTable{}, err
	}
	out, err := u.Services.Jobs.ListByCommand(r.Context(), commandID, domain.Status(status), p)
	if err != nil {
		return jobsTable{}, err
	}
	items := make([]jobView, len(out.Items))
	for i := range out.Items {
		items[i] = u.jobViewOf(&out.Items[i])
	}
	base := "/ui/commands/" + strconv.FormatInt(commandID, 10) + "/jobs/table"
	if status != "" {
		base += "?status=" + urlEscape(status)
	}
	return jobsTable{
		Project: *project, Command: header.Command, Header: header, Items: items, Pager: pagerOf(base, out),
		Status: status, Statuses: jobStatuses,
	}, nil
}

func (u *UI) jobRoutes() {
	u.handle("GET /commands/{id}/jobs", func(w http.ResponseWriter, r *http.Request) {
		id, err := pathID(r, "id")
		if err != nil {
			http.NotFound(w, r)
			return
		}
		t, err := u.jobsTable(r, id)
		if err != nil {
			u.notFoundOrFail(w, r, err)
			return
		}
		u.renderPage(w, r, "jobs", page{Title: "Jobs", Nav: "projects", Data: t})
	})
	u.handle("GET /ui/commands/{id}/jobs/table", view(u, func(r *http.Request, _ *struct{}, s *stream) error {
		id, err := pathID(r, "id")
		if err != nil {
			return err
		}
		t, err := u.jobsTable(r, id)
		if err != nil {
			return err
		}
		return s.render("jobs_table", t)
	}))
	u.handle("GET /ui/jobs/{id}/open", view(u, func(r *http.Request, _ *struct{}, s *stream) error {
		id, err := pathID(r, "id")
		if err != nil {
			return err
		}
		j, err := u.Services.Jobs.Get(r.Context(), id)
		if err != nil {
			return err
		}
		if err := s.into("job-detail-body", "job_detail", u.jobViewOf(j)); err != nil {
			return err
		}
		return s.set(map[string]any{"jobs": map[string]any{"open": true}})
	}))
	u.handle("POST /ui/jobs/save", u.actForm("jobs-error", func(r *http.Request, f *form, s *stream) error {
		id, err := f.id()
		if err != nil {
			return err
		}
		j, err := u.Services.Jobs.EditPrompts(r.Context(), id, f.raw("positive_prompt"), f.raw("negative_prompt"))
		if err != nil {
			return err
		}
		if err := s.set(map[string]any{"jobs": map[string]any{"open": false}}); err != nil {
			return err
		}
		if err := s.into("jobs-error", "error_text", ""); err != nil {
			return err
		}
		return u.patchJob(r, s, j)
	}))
	u.handle("POST /ui/jobs/{id}/run", act(u, "", func(r *http.Request, _ *struct{}, s *stream) error {
		id, err := pathID(r, "id")
		if err != nil {
			return err
		}
		j, err := u.Services.Jobs.Run(r.Context(), id)
		if err != nil {
			return err
		}
		return u.patchJob(r, s, j)
	}))
	u.handle("POST /ui/jobs/{id}/stop", act(u, "", func(r *http.Request, _ *struct{}, s *stream) error {
		id, err := pathID(r, "id")
		if err != nil {
			return err
		}
		j, err := u.Services.Jobs.Stop(r.Context(), id)
		if err != nil {
			return err
		}
		return u.patchJob(r, s, j)
	}))
}

// patchJob redraws the row of a job and the progress of its command.
func (u *UI) patchJob(r *http.Request, s *stream, j *domain.Job) error {
	if err := s.render("job_row", u.jobViewOf(j)); err != nil {
		return err
	}
	if j.CommandID < 1 {
		return nil
	}
	h, err := u.commandHeaderOf(r, j.CommandID)
	if err != nil {
		return err
	}
	return s.render("command_header", h)
}
