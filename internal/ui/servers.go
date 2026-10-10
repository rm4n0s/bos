package ui

import (
	"net/http"

	"github.com/rm4n0s/bos/internal/domain"
	"github.com/rm4n0s/bos/internal/service"
)

type serversTable struct {
	Items []service.ServerView
	Pager pagerData
}

func (u *UI) serversTable(r *http.Request) (serversTable, error) {
	p, err := pageParam(r)
	if err != nil {
		return serversTable{}, err
	}
	out, err := u.Services.Servers.List(r.Context(), p)
	if err != nil {
		return serversTable{}, err
	}
	return serversTable{Items: out.Items, Pager: pagerOf("/ui/servers/table", out)}, nil
}

// serverSignals is the part of the signals the server screen sends.
type serverSignals struct {
	Servers struct {
		Form struct {
			ID                flexInt `json:"id"`
			Name              string  `json:"name"`
			Host              string  `json:"host"`
			CodeName          string  `json:"code_name"`
			IsLocal           bool    `json:"is_local"`
			MaxConcurrentJobs flexInt `json:"max_concurrent_jobs"`
			ComfyUIPath       string  `json:"comfyui_path"`
		} `json:"form"`
	} `json:"servers"`
}

func (u *UI) serverRoutes() {
	u.handle("GET /servers", func(w http.ResponseWriter, r *http.Request) {
		t, err := u.serversTable(r)
		if err != nil {
			u.failPage(w, r, err)
			return
		}
		u.renderPage(w, r, "servers", page{Title: "Servers", Nav: "servers", Data: t})
	})
	u.handle("GET /ui/servers/table", view(u, func(r *http.Request, _ *struct{}, s *stream) error {
		t, err := u.serversTable(r)
		if err != nil {
			return err
		}
		return s.render("servers_table", t)
	}))
	u.handle("GET /ui/servers/{id}/edit", view(u, func(r *http.Request, _ *struct{}, s *stream) error {
		id, err := pathID(r, "id")
		if err != nil {
			return err
		}
		v, err := u.Services.Servers.Get(r.Context(), id)
		if err != nil {
			return err
		}
		return s.set(map[string]any{"servers": map[string]any{"open": true, "form": map[string]any{
			"id": v.ID, "name": v.Name, "host": v.Host, "code_name": v.CodeName, "is_local": v.IsLocal, "max_concurrent_jobs": v.MaxConcurrentJobs,
			"comfyui_path": v.ComfyUIPath,
		}}})
	}))
	u.handle("POST /ui/servers", act(u, "servers-error", func(r *http.Request, in *serverSignals, s *stream) error {
		f := in.Servers.Form
		server := domain.Server{Name: f.Name, Host: f.Host, CodeName: f.CodeName, IsLocal: f.IsLocal, MaxConcurrentJobs: int(f.MaxConcurrentJobs), ComfyUIPath: f.ComfyUIPath}
		var err error
		if f.ID > 0 {
			_, err = u.Services.Servers.Update(r.Context(), int64(f.ID), server)
		} else {
			_, err = u.Services.Servers.Create(r.Context(), server)
		}
		if err != nil {
			return err
		}
		return u.refreshServers(r, s, "Server saved.")
	}))
	u.handle("POST /ui/servers/{id}/test", act(u, "", func(r *http.Request, _ *struct{}, s *stream) error {
		id, err := pathID(r, "id")
		if err != nil {
			return err
		}
		if err := u.Services.Servers.Test(r.Context(), id); err != nil {
			return err
		}
		return s.toast("ok", "The server answers.")
	}))
	u.handle("DELETE /ui/servers/{id}", act(u, "", func(r *http.Request, _ *struct{}, s *stream) error {
		id, err := pathID(r, "id")
		if err != nil {
			return err
		}
		if err := u.Services.Servers.Delete(r.Context(), id); err != nil {
			return err
		}
		return u.refreshServers(r, s, "Server deleted.")
	}))
}

// refreshServers redraws the table, closes the dialog and says what happened.
func (u *UI) refreshServers(r *http.Request, s *stream, message string) error {
	t, err := u.serversTable(r)
	if err != nil {
		return err
	}
	if err := s.render("servers_table", t); err != nil {
		return err
	}
	if err := s.set(map[string]any{"servers": map[string]any{"open": false}, "_armed": 0}); err != nil {
		return err
	}
	if err := s.into("servers-error", "error_text", ""); err != nil {
		return err
	}
	return s.toast("ok", message)
}
