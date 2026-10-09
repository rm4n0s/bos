package ui

import (
	"net/http"

	"github.com/rm4n0s/bos/internal/domain"
)

type projectForm struct {
	ID   int64
	Name string
}

type projectsTable struct {
	Items []domain.Project
	Pager pagerData
	Blank projectForm
}

func (u *UI) projectsTable(r *http.Request) (projectsTable, error) {
	p, err := pageParam(r)
	if err != nil {
		return projectsTable{}, err
	}
	out, err := u.Services.Projects.List(r.Context(), p)
	if err != nil {
		return projectsTable{}, err
	}
	return projectsTable{Items: out.Items, Pager: pagerOf("/ui/projects/table", out)}, nil
}

func (u *UI) projectRoutes() {
	u.handle("GET /projects", func(w http.ResponseWriter, r *http.Request) {
		t, err := u.projectsTable(r)
		if err != nil {
			u.failPage(w, r, err)
			return
		}
		u.renderPage(w, r, "projects", page{Title: "Projects", Nav: "projects", Data: t})
	})
	u.handle("GET /ui/projects/table", view(u, func(r *http.Request, _ *struct{}, s *stream) error {
		t, err := u.projectsTable(r)
		if err != nil {
			return err
		}
		return s.render("projects_table", t)
	}))
	u.handle("GET /ui/projects/new", view(u, func(_ *http.Request, _ *struct{}, s *stream) error {
		if err := s.into("project-form-body", "project_form", projectForm{}); err != nil {
			return err
		}
		return s.set(map[string]any{"projects": map[string]any{"open": true}})
	}))
	u.handle("GET /ui/projects/{id}/edit", view(u, func(r *http.Request, _ *struct{}, s *stream) error {
		id, err := pathID(r, "id")
		if err != nil {
			return err
		}
		p, err := u.Services.Projects.Get(r.Context(), id)
		if err != nil {
			return err
		}
		if err := s.into("project-form-body", "project_form", projectForm{ID: p.ID, Name: p.Name}); err != nil {
			return err
		}
		return s.set(map[string]any{"projects": map[string]any{"open": true}})
	}))
	u.handle("POST /ui/projects", u.actForm("projects-error", func(r *http.Request, f *form, s *stream) error {
		id, err := f.id()
		if err != nil {
			return err
		}
		in := domain.Project{Name: f.str("name")}
		if id > 0 {
			_, err = u.Services.Projects.Update(r.Context(), id, in)
		} else {
			_, err = u.Services.Projects.Create(r.Context(), in)
		}
		if err != nil {
			return err
		}
		return u.refreshProjects(r, s, "Project saved.")
	}))
	u.handle("DELETE /ui/projects/{id}", act(u, "", func(r *http.Request, _ *struct{}, s *stream) error {
		id, err := pathID(r, "id")
		if err != nil {
			return err
		}
		if err := u.Services.Projects.Delete(r.Context(), id); err != nil {
			return err
		}
		return u.refreshProjects(r, s, "Project deleted.")
	}))
}

func (u *UI) refreshProjects(r *http.Request, s *stream, message string) error {
	t, err := u.projectsTable(r)
	if err != nil {
		return err
	}
	if err := s.render("projects_table", t); err != nil {
		return err
	}
	if err := s.set(map[string]any{"projects": map[string]any{"open": false}, "_armed": 0}); err != nil {
		return err
	}
	if err := s.into("projects-error", "error_text", ""); err != nil {
		return err
	}
	return s.toast("ok", message)
}
