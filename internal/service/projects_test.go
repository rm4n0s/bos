package service

import (
	"strings"
	"testing"

	"github.com/rm4n0s/bos/internal/domain"
)

func TestProjects(t *testing.T) {
	e := newEnv(t)
	p := e.project(t, "first")
	e.project(t, "second")
	upd, err := e.Projects.Update(ctx(t), p.ID, domain.Project{Name: "renamed"})
	check(t, err)
	if upd.Name != "renamed" || !upd.CreatedAt.Equal(p.CreatedAt) {
		t.Errorf("updated = %+v", upd)
	}
	got, err := e.Projects.Get(ctx(t), p.ID)
	check(t, err)
	if got.Name != "renamed" {
		t.Errorf("got %+v", got)
	}
	page, err := e.Projects.List(ctx(t), domain.Page{Size: 1})
	check(t, err)
	if page.Total != 2 || len(page.Items) != 1 {
		t.Errorf("page = %+v", page)
	}
	check(t, e.Projects.Delete(ctx(t), p.ID))
	_, err = e.Projects.Get(ctx(t), p.ID)
	wantRoute(t, err, "ProjectService.Get", "ProjectNotFound")
}

func TestProjectFailures(t *testing.T) {
	e := newEnv(t)
	p := e.project(t, "p")
	for _, name := range []string{"", "   ", strings.Repeat("n", 301)} {
		_, err := e.Projects.Create(ctx(t), domain.Project{Name: name})
		wantRoute(t, err, "ProjectService.Create", "NameInvalid")
		_, err = e.Projects.Update(ctx(t), p.ID, domain.Project{Name: name})
		wantRoute(t, err, "ProjectService.Update", "NameInvalid")
	}
	_, err := e.Projects.Update(ctx(t), 999, domain.Project{Name: "x"})
	wantRoute(t, err, "ProjectService.Update", "ProjectNotFound")
	wantRoute(t, e.Projects.Delete(ctx(t), 999), "ProjectService.Delete", "ProjectNotFound")
}
