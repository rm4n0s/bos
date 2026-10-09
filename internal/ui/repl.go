package ui

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/rm4n0s/bos/internal/domain"
	"github.com/rm4n0s/bos/internal/service"
	"github.com/rm4n0s/errors"
)

// replView is what the result area shows.
type replView struct {
	Job       *domain.Job
	ResultURL string
	IsImage   bool
}

type replPage struct {
	Servers    []service.ServerView
	Generators []domain.Generator
	Last       replView
}

// lastRepl reads the previous REPL job. There being none is not a failure.
func (u *UI) lastRepl(r *http.Request) (replView, error) {
	j, err := u.Services.Repl.Last(r.Context())
	if err != nil {
		if appErr, ok := errors.FromError(err); ok && appErr.Tag == "JobNotFound" {
			return replView{}, nil
		}
		return replView{}, err
	}
	v := replView{Job: j, IsImage: j.OutputType == domain.OutputImage}
	if j.Status == domain.StatusFinished {
		// The result always has the same name, so the time keeps the browser
		// from showing the previous picture.
		if url := u.fileURL("results", j.ResultPath); url != "" {
			v.ResultURL = url + "?v=" + strconv.FormatInt(j.UpdatedAt.UnixNano(), 10)
		}
	}
	return v, nil
}

func (u *UI) replRoutes() {
	u.handle("GET /repl", func(w http.ResponseWriter, r *http.Request) {
		all := domain.Page{Number: 1, Size: domain.MaxPageSize}
		servers, err := u.Services.Servers.List(r.Context(), all)
		if err != nil {
			u.failPage(w, r, err)
			return
		}
		gens, err := u.Services.Generators.List(r.Context(), all)
		if err != nil {
			u.failPage(w, r, err)
			return
		}
		last, err := u.lastRepl(r)
		if err != nil {
			u.failPage(w, r, err)
			return
		}
		u.renderPage(w, r, "repl", page{Title: "REPL", Nav: "repl", Data: replPage{Servers: servers.Items, Generators: gens.Items, Last: last}})
	})
	u.handle("POST /ui/repl/run", u.actForm("repl-error", func(r *http.Request, f *form, s *stream) error {
		loras, err := parseLoRAs(f.raw("loras"))
		if err != nil {
			return err
		}
		_, err = u.Services.Repl.Run(r.Context(), service.ReplInput{
			Server: f.str("server"), Generator: f.str("generator"), Items: strings.TrimSpace(f.raw("items")),
			Positive: f.raw("positive"), Negative: f.raw("negative"), LoRAs: loras,
		})
		if err != nil {
			return err
		}
		if err := s.into("repl-error", "error_text", ""); err != nil {
			return err
		}
		return u.drawRepl(r, s)
	}))
}

// drawRepl redraws the result area.
func (u *UI) drawRepl(r *http.Request, s *stream) error {
	last, err := u.lastRepl(r)
	if err != nil {
		return err
	}
	return s.render("repl_result", last)
}
