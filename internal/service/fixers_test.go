package service

import (
	"testing"

	"github.com/rm4n0s/bos/internal/domain"
)

func TestFixers(t *testing.T) {
	e := newEnv(t)
	f := e.fixer(t, "upscale")
	if f.ID == 0 || f.LoadImageTitle != "Load Image" {
		t.Errorf("created = %+v", f)
	}

	upd, err := e.Fixers.Update(ctx(t), f.ID, domain.Fixer{
		Name: "Better", CodeName: "upscale", Workflow: f.Workflow, LoadImageTitle: "Load Image", SaveImageTitle: "Save Image",
		PositivePrompt: "sharp",
	})
	check(t, err)
	if upd.Name != "Better" || upd.PositivePrompt != "sharp" || !upd.CreatedAt.Equal(f.CreatedAt) {
		t.Errorf("updated = %+v", upd)
	}
	e.fixer(t, "second")
	page, err := e.Fixers.List(ctx(t), domain.Page{})
	check(t, err)
	if page.Total != 2 {
		t.Errorf("total = %d", page.Total)
	}
	got, err := e.Fixers.Get(ctx(t), f.ID)
	check(t, err)
	if got.Name != "Better" {
		t.Errorf("got %+v", got)
	}
	check(t, e.Fixers.Delete(ctx(t), f.ID))
	_, err = e.Fixers.Get(ctx(t), f.ID)
	wantRoute(t, err, "FixerService.Get", "FixerNotFound")
}

func TestFixerFailures(t *testing.T) {
	e := newEnv(t)
	f := e.fixer(t, "upscale")
	good := domain.Fixer{Name: "n", CodeName: "other", Workflow: f.Workflow, LoadImageTitle: "Load Image", SaveImageTitle: "Save Image"}
	tests := []struct {
		name   string
		change func(*domain.Fixer)
		tag    string
	}{
		{"blank name", func(v *domain.Fixer) { v.Name = "" }, "NameInvalid"},
		{"bad code", func(v *domain.Fixer) { v.CodeName = "up-scale" }, "CodeNameInvalid"},
		{"reserved code", func(v *domain.Fixer) { v.CodeName = "on" }, "CodeNameReserved"},
		{"not JSON", func(v *domain.Fixer) { v.Workflow = []byte("nope") }, "WorkflowInvalid"},
		{"no image input title", func(v *domain.Fixer) { v.LoadImageTitle = "" }, "FixerTitleRequired"},
		{"no image output title", func(v *domain.Fixer) { v.SaveImageTitle = "" }, "FixerTitleRequired"},
		{"input title not in workflow", func(v *domain.Fixer) { v.LoadImageTitle = "Nowhere" }, "WorkflowTitleNotFound"},
		{"prompt title not in workflow", func(v *domain.Fixer) { v.PositivePromptTitle = "Nowhere" }, "WorkflowTitleNotFound"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := good
			tt.change(&in)
			_, err := e.Fixers.Create(ctx(t), in)
			wantRoute(t, err, "FixerService.Create", tt.tag)
			_, err = e.Fixers.Update(ctx(t), f.ID, in)
			wantRoute(t, err, "FixerService.Update", tt.tag)
		})
	}
	if page, _ := e.Fixers.List(ctx(t), domain.Page{}); page.Total != 1 {
		t.Errorf("%d fixers, a rejected create stored something", page.Total)
	}

	_, err := e.Fixers.Create(ctx(t), domain.Fixer{Name: "n", CodeName: "upscale", Workflow: f.Workflow, LoadImageTitle: "Load Image", SaveImageTitle: "Save Image"})
	wantRoute(t, err, "FixerService.Create", "FixerCodeNameTaken")
	_, err = e.Fixers.Update(ctx(t), 999, good)
	wantRoute(t, err, "FixerService.Update", "FixerNotFound")
	wantRoute(t, e.Fixers.Delete(ctx(t), 999), "FixerService.Delete", "FixerNotFound")
}
