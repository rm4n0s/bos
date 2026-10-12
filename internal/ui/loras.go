package ui

import (
	"net/http"
	"strings"

	"github.com/rm4n0s/bos/internal/service"
)

// lorasTable is what the LoRA table shows: one page of the list for a search.
type lorasTable struct {
	Q           string
	Items       []service.LoRAEntry
	Unreachable []string
	Pager       pagerData
}

func (u *UI) lorasTable(r *http.Request) (lorasTable, error) {
	p, err := pageParam(r)
	if err != nil {
		return lorasTable{}, err
	}
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	list, err := u.Services.LoRAs.List(r.Context(), q, p)
	if err != nil {
		return lorasTable{}, err
	}
	return lorasTable{
		Q: q, Items: list.Items, Unreachable: list.Unreachable,
		Pager: pagerOf("/ui/loras/table?q="+urlEscape(q), list.Paged),
	}, nil
}

// loraDetail is what the dialog of one LoRA shows.
type loraDetail struct {
	Q string
	*service.LoRADetail
}

// loraSuggestion is one button of the item editor's autocomplete.
type loraSuggestions struct{ Names []string }

func (u *UI) loraRoutes() {
	u.handle("GET /loras", func(w http.ResponseWriter, r *http.Request) {
		t, err := u.lorasTable(r)
		if err != nil {
			u.failPage(w, r, err)
			return
		}
		u.renderPage(w, r, "loras", page{Title: "LoRAs", Nav: "loras", Data: t})
	})
	u.handle("GET /ui/loras/table", view(u, func(r *http.Request, _ *struct{}, s *stream) error {
		t, err := u.lorasTable(r)
		if err != nil {
			return err
		}
		return s.render("loras_table", t)
	}))
	u.handle("GET /ui/loras/detail", view(u, func(r *http.Request, _ *struct{}, s *stream) error {
		d, err := u.Services.LoRAs.Get(r.Context(), r.URL.Query().Get("name"))
		if err != nil {
			return err
		}
		if err := s.into("lora-detail", "lora_detail", loraDetail{Q: strings.TrimSpace(r.URL.Query().Get("q")), LoRADetail: d}); err != nil {
			return err
		}
		return s.set(map[string]any{"loras": map[string]any{"open": true, "armed": false}})
	}))
	u.handle("DELETE /ui/loras", act(u, "lora-error", func(r *http.Request, _ *struct{}, s *stream) error {
		out, err := u.Services.LoRAs.Delete(r.Context(), r.URL.Query().Get("name"))
		if err != nil {
			return err
		}
		t, err := u.lorasTable(r)
		if err != nil {
			return err
		}
		if err := s.render("loras_table", t); err != nil {
			return err
		}
		if err := s.set(map[string]any{"loras": map[string]any{"open": false, "armed": false}}); err != nil {
			return err
		}
		if err := s.into("lora-error", "error_text", ""); err != nil {
			return err
		}
		return s.toast("ok", deletedMessage(out))
	}))
	u.handle("GET /ui/loras/suggest", view(u, func(r *http.Request, _ *struct{}, s *stream) error {
		names, err := u.Services.LoRAs.Suggest(r.Context(), r.URL.Query().Get("q"))
		if err != nil {
			return err
		}
		return s.into("lora-suggest", "lora_suggest", loraSuggestions{Names: names})
	}))
}

// deletedMessage says what a deletion removed.
func deletedMessage(d *service.LoRADeletion) string {
	parts := []string{"deleted from " + plural(len(d.Servers), "server"), plural(d.Items, "item") + " deleted"}
	if d.Files > 0 {
		parts = append(parts, plural(d.Files, "cached file")+" removed")
	}
	return d.Name + ": " + strings.Join(parts, ", ") + "."
}
