package ui

import (
	"net/http"
	"strings"

	"github.com/rm4n0s/bos/internal/domain"
	"github.com/rm4n0s/bos/internal/uploads"
)

// groupForm is what the group dialog shows.
type groupForm struct {
	ID                                                                                           int64
	Name, CodeName, Description, Tags, ThumbURL                                                  string
	UseLora, UseControlnet, UseIPAdapter, UseMaskRegion, UseCoordinatesRegion, UseTypeAttributes bool
}

type groupsTable struct {
	Items   []domain.Group
	Pager   pagerData
	Tag     string
	Tags    []domain.TagCount
	Blank   groupForm
	TagList tagList
}

type tagList struct{ GroupTags, ItemTags []domain.TagCount }

func (u *UI) groupFormOf(g *domain.Group) groupForm {
	return groupForm{
		ID: g.ID, Name: g.Name, CodeName: g.CodeName, Description: g.Description, Tags: strings.Join(g.Tags, ", "),
		ThumbURL: u.fileURL("thumbnails", g.Thumbnail),
		UseLora:  g.UseLora, UseControlnet: g.UseControlnet, UseIPAdapter: g.UseIPAdapter,
		UseMaskRegion: g.UseMaskRegion, UseCoordinatesRegion: g.UseCoordinatesRegion, UseTypeAttributes: g.UseTypeAttributes,
	}
}

// splitList splits comma-, space- or line-separated words.
func splitList(s string) []string {
	return strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ' ' || r == '\n' || r == '\r' || r == '\t' })
}

func (u *UI) groupsTable(r *http.Request) (groupsTable, error) {
	p, err := pageParam(r)
	if err != nil {
		return groupsTable{}, err
	}
	tag := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("tag")))
	out, err := u.Services.Groups.List(r.Context(), tag, p)
	if err != nil {
		return groupsTable{}, err
	}
	base := "/ui/groups/table"
	if tag != "" {
		base += "?tag=" + urlEscape(tag)
	}
	tags, err := u.Services.Tags.List(r.Context(), domain.TagScopeGroup, "")
	if err != nil {
		return groupsTable{}, err
	}
	list, err := u.tagList(r)
	if err != nil {
		return groupsTable{}, err
	}
	return groupsTable{Items: out.Items, Pager: pagerOf(base, out), Tag: tag, Tags: tags, TagList: list}, nil
}

func (u *UI) tagList(r *http.Request) (tagList, error) {
	g, err := u.Services.Tags.List(r.Context(), domain.TagScopeGroup, "")
	if err != nil {
		return tagList{}, err
	}
	i, err := u.Services.Tags.List(r.Context(), domain.TagScopeItem, "")
	if err != nil {
		return tagList{}, err
	}
	return tagList{GroupTags: g, ItemTags: i}, nil
}

func (u *UI) groupRoutes() {
	u.handle("GET /groups", func(w http.ResponseWriter, r *http.Request) {
		t, err := u.groupsTable(r)
		if err != nil {
			u.failPage(w, r, err)
			return
		}
		u.renderPage(w, r, "groups", page{Title: "Groups", Nav: "groups", Data: t})
	})
	u.handle("GET /ui/groups/table", view(u, func(r *http.Request, _ *struct{}, s *stream) error {
		t, err := u.groupsTable(r)
		if err != nil {
			return err
		}
		return s.render("groups_table", t)
	}))
	u.handle("GET /ui/groups/new", view(u, func(_ *http.Request, _ *struct{}, s *stream) error {
		if err := s.into("group-form-body", "group_form", groupForm{}); err != nil {
			return err
		}
		return s.set(map[string]any{"groups": map[string]any{"open": true}})
	}))
	u.handle("GET /ui/groups/import/new", view(u, func(_ *http.Request, _ *struct{}, s *stream) error {
		if err := s.into("group-import-body", "group_import_form", groupForm{}); err != nil {
			return err
		}
		return s.set(map[string]any{"groups": map[string]any{"importOpen": true}})
	}))
	u.handle("GET /ui/groups/{id}/edit", view(u, func(r *http.Request, _ *struct{}, s *stream) error {
		id, err := pathID(r, "id")
		if err != nil {
			return err
		}
		g, err := u.Services.Groups.Get(r.Context(), id)
		if err != nil {
			return err
		}
		if err := s.into("group-form-body", "group_form", u.groupFormOf(g)); err != nil {
			return err
		}
		return s.set(map[string]any{"groups": map[string]any{"open": true}})
	}))
	u.handle("POST /ui/groups", u.actForm("groups-error", u.saveGroup))
	u.handle("POST /ui/groups/import", u.actForm("groups-import-error", func(r *http.Request, f *form, s *stream) error {
		text := f.raw("text")
		if strings.TrimSpace(text) == "" {
			var err error
			if text, err = u.textFile(f, "textfile"); err != nil {
				return err
			}
		}
		in := domain.Group{Name: f.str("name"), CodeName: f.str("code_name"), Tags: splitList(f.str("tags"))}
		if _, err := u.Services.Groups.ImportText(r.Context(), in, text); err != nil {
			return err
		}
		if err := s.set(map[string]any{"groups": map[string]any{"importOpen": false}}); err != nil {
			return err
		}
		if err := s.into("groups-import-error", "error_text", ""); err != nil {
			return err
		}
		return u.refreshGroups(r, s, "Group created from the text.")
	}))
	u.handle("DELETE /ui/groups/{id}", act(u, "", func(r *http.Request, _ *struct{}, s *stream) error {
		id, err := pathID(r, "id")
		if err != nil {
			return err
		}
		if err := u.Services.Groups.Delete(r.Context(), id); err != nil {
			return err
		}
		return u.refreshGroups(r, s, "Group deleted.")
	}))
	u.tagRoutes()
}

// saveGroup creates or updates a group from the dialog.
func (u *UI) saveGroup(r *http.Request, f *form, s *stream) (err error) {
	id, err := f.id()
	if err != nil {
		return err
	}
	in := domain.Group{
		Name: f.str("name"), CodeName: f.str("code_name"), Description: f.str("description"), Tags: splitList(f.str("tags")),
		UseLora: f.checked("use_lora"), UseControlnet: f.checked("use_controlnet"), UseIPAdapter: f.checked("use_ip_adapter"),
		UseMaskRegion: f.checked("use_mask_region"), UseCoordinatesRegion: f.checked("use_coordinates_region"),
		UseTypeAttributes: f.checked("use_type_attributes"),
	}
	if id > 0 {
		old, err := u.Services.Groups.Get(r.Context(), id)
		if err != nil {
			return err
		}
		if !f.checked("clear_thumbnail") {
			in.Thumbnail = old.Thumbnail
		}
	}
	set := uploads.NewSet()
	defer func() {
		if err != nil {
			uploads.Discard(u.Files, set, u.Logger)
		}
	}()
	if ok, uerr := u.upload(f, "thumbnail", "thumbnail", uploads.Kind{Dir: u.Dirs.Thumbnails, MIMEs: uploads.ThumbnailTypes}, set); uerr != nil {
		return uerr
	} else if ok {
		in.Thumbnail, _ = set.Path("thumbnail")
	}
	if id > 0 {
		_, err = u.Services.Groups.Update(r.Context(), id, in)
	} else {
		_, err = u.Services.Groups.Create(r.Context(), in)
	}
	if err != nil {
		return err
	}
	if err := s.set(map[string]any{"groups": map[string]any{"open": false}}); err != nil {
		return err
	}
	if err := s.into("groups-error", "error_text", ""); err != nil {
		return err
	}
	return u.refreshGroups(r, s, "Group saved.")
}

func (u *UI) refreshGroups(r *http.Request, s *stream, message string) error {
	t, err := u.groupsTable(r)
	if err != nil {
		return err
	}
	if err := s.render("groups_table", t); err != nil {
		return err
	}
	if err := s.render("tags_panel_box", t.TagList); err != nil {
		return err
	}
	if err := s.set(map[string]any{"_armed": 0}); err != nil {
		return err
	}
	return s.toast("ok", message)
}
