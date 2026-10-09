package ui

import (
	"net/http"

	"github.com/rm4n0s/bos/internal/domain"
)

func (u *UI) homeRoutes() {
	u.mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		p := domain.Page{Number: 1, Size: domain.MaxPageSize}
		ctx := r.Context()
		servers, err := u.Services.Servers.List(ctx, p)
		if err != nil {
			u.failPage(w, r, err)
			return
		}
		online := 0
		for _, s := range servers.Items {
			if s.Online {
				online++
			}
		}
		count := func(total int, err error) int {
			if err != nil {
				u.Logger.Warn("could not count for the home page", "error", err.Error())
			}
			return total
		}
		gens, gerr := u.Services.Generators.List(ctx, domain.Page{Number: 1, Size: 1})
		groups, gperr := u.Services.Groups.List(ctx, "", domain.Page{Number: 1, Size: 1})
		projects, perr := u.Services.Projects.List(ctx, domain.Page{Number: 1, Size: 1})
		u.renderPage(w, r, "home", page{Title: "Home", Nav: "home", Data: map[string]int{
			"Servers": servers.Total, "Online": online,
			"Generators": count(gens.Total, gerr), "Groups": count(groups.Total, gperr), "Projects": count(projects.Total, perr),
		}})
	})
}

// failPage answers a page request whose data could not be read.
func (u *UI) failPage(w http.ResponseWriter, r *http.Request, err error) {
	logFailure(u, r, err)
	http.Error(w, "Something went wrong on the server. Look at its log.", http.StatusInternalServerError)
}

// pagerData is what the pager fragment needs.
type pagerData struct {
	Base                          string
	Page, TotalPages, Total, Size int
}

func pagerOf[T any](base string, p domain.Paged[T]) pagerData {
	return pagerData{Base: base, Page: p.Page, TotalPages: p.TotalPages, Total: p.Total, Size: p.PageSize}
}
