package ui

import (
	"net/http"
	"net/url"

	"github.com/rm4n0s/bos/internal/domain"
)

func urlEscape(s string) string { return url.QueryEscape(s) }

type tagSignals struct {
	Tags struct {
		Scope string `json:"scope"`
		From  string `json:"from"`
		To    string `json:"to"`
	} `json:"tags"`
}

type tagsResult struct {
	Renamed          int
	From             string
	AffectedCommands []domain.Command
}

func (u *UI) tagRoutes() {
	u.handle("POST /ui/tags/rename", act(u, "tags-error", func(r *http.Request, in *tagSignals, s *stream) error {
		res, err := u.Services.Tags.Rename(r.Context(), domain.TagScope(in.Tags.Scope), in.Tags.From, in.Tags.To)
		if err != nil {
			return err
		}
		list, err := u.tagList(r)
		if err != nil {
			return err
		}
		if err := s.render("tags_panel_box", list); err != nil {
			return err
		}
		if err := s.render("tags_result", tagsResult{Renamed: res.Renamed, From: in.Tags.From, AffectedCommands: res.AffectedCommands}); err != nil {
			return err
		}
		if err := s.set(map[string]any{"tags": map[string]any{"from": "", "to": ""}}); err != nil {
			return err
		}
		return s.toast("ok", "Tag renamed.")
	}))
}
