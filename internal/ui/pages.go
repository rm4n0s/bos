package ui

import (
	"net/http"
	"strings"

	"github.com/rm4n0s/bos/internal/domain"
)

func (u *UI) homeRoutes() {
	u.handle("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
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
	// Base is the fragment's address, with its filters; Sep joins the page number to it.
	Base, Sep                     string
	Page, TotalPages, Total, Size int
}

// pageNumber is one place in the row of page numbers: a page, or a gap.
type pageNumber struct {
	N       int
	Current bool
	Gap     bool
}

// pagerReach is how many pages are shown on each side of the current one.
const pagerReach = 2

// Numbers is the row of page buttons: the first page, the last page, the ones
// next to the current page, and a gap where pages are left out. A gap that
// would hide a single page shows that page instead.
func (p pagerData) Numbers() []pageNumber {
	var out []pageNumber
	last := 0
	for n := 1; n <= p.TotalPages; n++ {
		if n != 1 && n != p.TotalPages && (n < p.Page-pagerReach || n > p.Page+pagerReach) {
			continue
		}
		switch skipped := n - last - 1; {
		case skipped == 1:
			out = append(out, pageNumber{N: n - 1})
		case skipped > 1:
			out = append(out, pageNumber{Gap: true})
		}
		out = append(out, pageNumber{N: n, Current: n == p.Page})
		last = n
	}
	return out
}

func pagerOf[T any](base string, p domain.Paged[T]) pagerData {
	sep := "?"
	if strings.Contains(base, "?") {
		sep = "&"
	}
	return pagerData{Base: base, Sep: sep, Page: p.Page, TotalPages: p.TotalPages, Total: p.Total, Size: p.PageSize}
}
