package ui

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/rm4n0s/bos/internal/domain"
	"github.com/rm4n0s/bos/internal/events"
	"github.com/rm4n0s/bos/internal/service"
	"github.com/rm4n0s/errors"
)

// parseModels reads the lines `id`, `id, model strength` or `id, model
// strength, text-encoder strength`; strengths that are left out are 1.
func parseModels(text string) ([]service.CivitaiModel, error) {
	var out []service.CivitaiModel
	for _, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		parts := strings.Split(line, ",")
		id, err := strconv.Atoi(strings.TrimSpace(parts[0]))
		if err != nil || id < 1 {
			return nil, errors.New("InvalidRequest", "a model line starts with the Civitai model id, a positive number: "+strings.TrimSpace(line), "line", line)
		}
		m := service.CivitaiModel{ModelID: id, StrengthModel: 1, StrengthClip: 1}
		for i, dst := range []*float64{&m.StrengthModel, &m.StrengthClip} {
			if len(parts) > i+1 && strings.TrimSpace(parts[i+1]) != "" {
				n, err := strconv.ParseFloat(strings.TrimSpace(parts[i+1]), 64)
				if err != nil {
					return nil, errors.New("InvalidRequest", "a strength is not a number: "+strings.TrimSpace(line), "line", line)
				}
				*dst = n
			}
		}
		out = append(out, m)
	}
	return out, nil
}

// civitaiForm is what the import dialogs show: the servers that have a ComfyUI
// folder, one of which can be chosen to fill the folder field.
type civitaiForm struct {
	Servers []domain.Server
}

func (u *UI) civitaiForm(r *http.Request) (civitaiForm, error) {
	servers, err := u.Services.Servers.WithComfyUIPath(r.Context())
	return civitaiForm{Servers: servers}, err
}

func (u *UI) civitaiRoutes() {
	u.handle("GET /ui/groups/civitai/new", view(u, func(r *http.Request, _ *struct{}, s *stream) error {
		form, err := u.civitaiForm(r)
		if err != nil {
			return err
		}
		if err := s.open("group-civitai-body", "group_civitai_form", form); err != nil {
			return err
		}
		return s.set(map[string]any{"groups": map[string]any{"civitaiOpen": true}})
	}))
	u.handle("POST /ui/groups/civitai", u.actForm("groups-civitai-error", func(r *http.Request, f *form, s *stream) error {
		models, err := parseModels(f.raw("models"))
		if err != nil {
			return err
		}
		_, err = u.Services.Civitai.ImportGroup(r.Context(), service.CivitaiImport{
			Group:  domain.Group{Name: f.str("name"), CodeName: f.str("code_name"), Tags: splitList(f.str("tags"))},
			Models: models, ComfyPath: f.str("comfyui_path"), Subfolder: f.str("lora_subfolder"),
		})
		if err != nil {
			return err
		}
		if err := s.set(map[string]any{"groups": map[string]any{"civitaiOpen": false}}); err != nil {
			return err
		}
		if err := s.into("groups-civitai-error", "error_text", ""); err != nil {
			return err
		}
		return u.refreshGroups(r, s, "The import started. A message tells you about each model.")
	}))
	u.handle("GET /ui/groups/{id}/items/civitai/new", view(u, func(r *http.Request, _ *struct{}, s *stream) error {
		form, err := u.civitaiForm(r)
		if err != nil {
			return err
		}
		if err := s.open("items-civitai-body", "civitai_fields", form); err != nil {
			return err
		}
		return s.set(map[string]any{"items": map[string]any{"civitaiOpen": true}})
	}))
	u.handle("POST /ui/groups/{id}/items/civitai", u.actForm("items-civitai-error", func(r *http.Request, f *form, s *stream) error {
		group, err := pathID(r, "id")
		if err != nil {
			return err
		}
		models, err := parseModels(f.raw("models"))
		if err != nil {
			return err
		}
		if len(models) == 0 {
			return errors.New("InvalidRequest", "at least one model is needed")
		}
		// One import for every line: the service runs them one at a time.
		for _, m := range models {
			if _, err := u.Services.Civitai.AddItem(r.Context(), group, m, f.str("comfyui_path"), f.str("lora_subfolder")); err != nil {
				return err
			}
		}
		if err := s.set(map[string]any{"items": map[string]any{"civitaiOpen": false}}); err != nil {
			return err
		}
		if err := s.into("items-civitai-error", "error_text", ""); err != nil {
			return err
		}
		return s.toast("ok", "The import started. A message tells you about each model.")
	}))
}

// drawImportEvents tells the user how an import is going and redraws the item
// list of the group it fills, where the page shows one.
func (u *UI) drawImportEvents(r *http.Request, s *stream, batch []events.Event) error {
	seen := map[domain.ID]bool{}
	for _, e := range batch {
		var kind string
		switch e.Type {
		case events.ImportProgress:
			kind = "ok"
		case events.ImportError:
			kind = "error"
		case events.ImportFinished:
			kind = "finished"
		default:
			continue
		}
		if err := s.toast(kind, e.Message); err != nil {
			return err
		}
		seen[e.GroupID] = true
	}
	for group := range seen {
		g, err := u.Services.Groups.Get(r.Context(), group)
		if err != nil {
			continue // the group was deleted meanwhile
		}
		t, err := u.itemsTable(r, g)
		if err != nil {
			return err
		}
		if err := s.render("items_table", t, selectorOf("#items-table[data-group=\""+strconv.FormatInt(group, 10)+"\"]")); err != nil {
			return err
		}
	}
	return nil
}
