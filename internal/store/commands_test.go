package store

import (
	"reflect"
	"testing"

	"github.com/rm4n0s/bos/internal/domain"
	"gorm.io/gorm"
)

func TestProjects(t *testing.T) {
	r := newDB(t).Repos()
	p := mustProject(t, r, "first")
	mustProject(t, r, "second")
	got, err := r.Projects.Get(ctx(t), p.ID)
	check(t, err)
	if !reflect.DeepEqual(got, p) {
		t.Errorf("Get = %+v\nwant  %+v", got, p)
	}
	p.Name = "renamed"
	check(t, r.Projects.Update(ctx(t), p))
	if got, _ = r.Projects.Get(ctx(t), p.ID); got.Name != "renamed" || !got.UpdatedAt.After(got.CreatedAt) {
		t.Errorf("after update: %+v", got)
	}
	page, err := r.Projects.List(ctx(t), domain.Page{})
	check(t, err)
	if page.Total != 2 || page.Items[0].Name != "renamed" {
		t.Errorf("page = %+v", page)
	}
	wantRoute(t, r.Projects.Update(ctx(t), &domain.Project{ID: 99, Name: "x"}), "ProjectRepo.Update", "ProjectNotFound")
	_, err = r.Projects.Get(ctx(t), 99)
	wantRoute(t, err, "ProjectRepo.Get", "ProjectNotFound")
	_, err = r.Projects.Delete(ctx(t), 99)
	wantRoute(t, err, "ProjectRepo.Delete", "ProjectNotFound")
}

// Acceptance of SPEC §7.2: add at the end or at a position, delete, move.
func TestCommandOrder(t *testing.T) {
	db := newDB(t)
	r := db.Repos()
	p := mustProject(t, r, "p")
	other := mustProject(t, r, "other")
	mustCommand(t, r, other.ID, "x", 0)

	a := mustCommand(t, r, p.ID, "a", 0)
	b := mustCommand(t, r, p.ID, "b", 0)
	c := mustCommand(t, r, p.ID, "c", 0)
	if a.Order != 1 || b.Order != 2 || c.Order != 3 {
		t.Fatalf("append gave orders %d %d %d", a.Order, b.Order, c.Order)
	}
	if !reflect.DeepEqual(orderOf(t, r, p.ID), []string{"a", "b", "c"}) {
		t.Fatal("order")
	}

	// Insert at a position shifts the later commands by one.
	d := mustCommand(t, r, p.ID, "d", 2)
	if d.Order != 2 {
		t.Errorf("inserted at %d", d.Order)
	}
	if got := orderOf(t, r, p.ID); !reflect.DeepEqual(got, []string{"a", "d", "b", "c"}) {
		t.Errorf("after insert = %v", got)
	}
	mustCommand(t, r, p.ID, "e", 1)
	mustCommand(t, r, p.ID, "f", 99) // beyond the end appends
	mustCommand(t, r, p.ID, "g", -3) // below 1 appends
	if got := orderOf(t, r, p.ID); !reflect.DeepEqual(got, []string{"e", "a", "d", "b", "c", "f", "g"}) {
		t.Errorf("after inserts = %v", got)
	}
	if got := orderOf(t, r, other.ID); !reflect.DeepEqual(got, []string{"x"}) {
		t.Errorf("another project changed: %v", got)
	}

	// Move: up, down, to the ends, no-ops at the boundaries.
	move := func(name string, to int) int {
		t.Helper()
		cmds, _ := r.Commands.AllByProject(ctx(t), p.ID)
		for _, c := range cmds {
			if c.Name == name {
				final, err := r.Commands.Move(ctx(t), c.ID, to)
				check(t, err)
				return final
			}
		}
		t.Fatalf("no command %s", name)
		return 0
	}
	if move("c", 2) != 2 {
		t.Error("move up")
	}
	if got := orderOf(t, r, p.ID); !reflect.DeepEqual(got, []string{"e", "c", "a", "d", "b", "f", "g"}) {
		t.Errorf("after moving c to 2 = %v", got)
	}
	move("e", 5)
	if got := orderOf(t, r, p.ID); !reflect.DeepEqual(got, []string{"c", "a", "d", "b", "e", "f", "g"}) {
		t.Errorf("after moving e to 5 = %v", got)
	}
	if move("c", 0) != 1 || move("g", 100) != 7 { // clamped; both already there
		t.Error("boundary moves")
	}
	if got := orderOf(t, r, p.ID); !reflect.DeepEqual(got, []string{"c", "a", "d", "b", "e", "f", "g"}) {
		t.Errorf("boundary moves changed the order: %v", got)
	}
	move("g", 1)
	move("a", 100)
	if got := orderOf(t, r, p.ID); !reflect.DeepEqual(got, []string{"g", "c", "d", "b", "e", "f", "a"}) {
		t.Errorf("after end moves = %v", got)
	}

	// Delete closes the gap.
	cmds, _ := r.Commands.AllByProject(ctx(t), p.ID)
	_, err := r.Commands.Delete(ctx(t), cmds[2].ID) // d
	check(t, err)
	if got := orderOf(t, r, p.ID); !reflect.DeepEqual(got, []string{"g", "c", "b", "e", "f", "a"}) {
		t.Errorf("after delete = %v", got)
	}

	// Renumber repairs gaps and keeps the sequence.
	check(t, db.gorm.Model(&commandRecord{}).Where("project_id = ?", p.ID).UpdateColumn("order_no", gorm.Expr("order_no * 10")).Error)
	check(t, r.Commands.Renumber(ctx(t), p.ID))
	if got := orderOf(t, r, p.ID); !reflect.DeepEqual(got, []string{"g", "c", "b", "e", "f", "a"}) {
		t.Errorf("after renumber = %v", got)
	}

	_, err = r.Commands.Move(ctx(t), 999, 1)
	wantRoute(t, err, "CommandRepo.Move", "CommandNotFound")
	_, err = r.Commands.Delete(ctx(t), 999)
	wantRoute(t, err, "CommandRepo.Delete", "CommandNotFound")
	wantRoute(t, r.Commands.Create(ctx(t), &domain.Command{ProjectID: 999, CommandCode: "x"}, 0), "CommandRepo.Create", "ProjectNotFound")
}

func TestCommandFields(t *testing.T) {
	db := newDB(t)
	r := db.Repos()
	p := mustProject(t, r, "p")
	c := &domain.Command{ProjectID: p.ID, Name: "n", Description: "d", CommandCode: "s -$ g: a", CommandJSON: []byte(`{"x":1}`)}
	check(t, r.Commands.Create(ctx(t), c, 0))
	if c.Status != domain.StatusIdle {
		t.Errorf("default status = %q", c.Status)
	}
	got, err := r.Commands.Get(ctx(t), c.ID)
	check(t, err)
	if !reflect.DeepEqual(got, c) {
		t.Errorf("Get = %+v\nwant  %+v", got, c)
	}
	c.Name, c.CommandCode, c.DownloadFile, c.Status = "n2", "s -$ g: b", "downloads/1.zip", domain.StatusFinished
	c.CommandJSON = []byte(`{"y":2}`)
	check(t, r.Commands.Update(ctx(t), c))
	got, _ = r.Commands.Get(ctx(t), c.ID)
	if got.Name != "n2" || got.DownloadFile != "downloads/1.zip" || got.Status != domain.StatusFinished || string(got.CommandJSON) != `{"y":2}` || got.Order != 1 {
		t.Errorf("after update: %+v", got)
	}
	check(t, r.Commands.SetStatus(ctx(t), c.ID, domain.StatusQueued))
	if got, _ = r.Commands.Get(ctx(t), c.ID); got.Status != domain.StatusQueued {
		t.Errorf("status = %s", got.Status)
	}
	wantRoute(t, r.Commands.Update(ctx(t), &domain.Command{ID: 999, CommandCode: "x"}), "CommandRepo.Update", "CommandNotFound")
	wantRoute(t, r.Commands.SetStatus(ctx(t), 999, domain.StatusIdle), "CommandRepo.SetStatus", "CommandNotFound")
	_, err = r.Commands.Get(ctx(t), 999)
	wantRoute(t, err, "CommandRepo.Get", "CommandNotFound")

	// FindByText ignores case (a renamed item tag is looked up this way).
	mustCommand(t, r, p.ID, "other", 0)
	check(t, db.gorm.Model(&commandRecord{}).Where("id = ?", c.ID).UpdateColumn("command_code", "s -$ g: a(#Combat)").Error)
	found, err := r.Commands.FindByText(ctx(t), "#combat")
	check(t, err)
	if len(found) != 1 || found[0].ID != c.ID {
		t.Errorf("FindByText = %+v", found)
	}
	if found, _ = r.Commands.FindByText(ctx(t), "#nothing"); len(found) != 0 {
		t.Errorf("FindByText matched %+v", found)
	}
}

func TestCommandProgress(t *testing.T) {
	r := newDB(t).Repos()
	p := mustProject(t, r, "p")
	a := mustCommand(t, r, p.ID, "a", 0)
	b := mustCommand(t, r, p.ID, "b", 0)
	jobs := mustJobs(t, r, a.ID, 4)
	for _, j := range jobs[:3] {
		j.Status = domain.StatusFinished
		check(t, r.Jobs.Update(ctx(t), j))
	}
	page, err := r.Commands.ListByProject(ctx(t), p.ID, domain.Page{})
	check(t, err)
	if page.Total != 2 || page.Items[0].ID != a.ID || page.Items[1].ID != b.ID {
		t.Fatalf("page = %+v", page)
	}
	if page.Items[0].FinishedJobs != 3 || page.Items[0].TotalJobs != 4 || page.Items[1].TotalJobs != 0 {
		t.Errorf("progress = %d/%d and %d/%d", page.Items[0].FinishedJobs, page.Items[0].TotalJobs, page.Items[1].FinishedJobs, page.Items[1].TotalJobs)
	}
	_, err = r.Commands.ListByProject(ctx(t), 999, domain.Page{})
	wantRoute(t, err, "CommandRepo.ListByProject", "ProjectNotFound")
}

// Acceptance test 12 for projects (SPEC §7.4): jobs, commands, then the project.
func TestDeleteProjectCascades(t *testing.T) {
	db := newDB(t)
	r := db.Repos()
	p := mustProject(t, r, "p")
	keep := mustProject(t, r, "keep")
	a := mustCommand(t, r, p.ID, "a", 0)
	b := mustCommand(t, r, p.ID, "b", 0)
	b.DownloadFile = "downloads/b.zip"
	check(t, r.Commands.Update(ctx(t), b))
	k := mustCommand(t, r, keep.ID, "k", 0)
	mustJobsFor(t, r, p.ID, a.ID, 2)
	mustJobsFor(t, r, p.ID, b.ID, 1)
	kept := mustJobsFor(t, r, keep.ID, k.ID, 2)
	repl := newJob(0, "repl")
	_, err := r.Jobs.ReplaceRepl(ctx(t), repl)
	check(t, err)

	files, err := r.Projects.Delete(ctx(t), p.ID)
	check(t, err)
	// Two result files (the third job reuses a name, which is listed once) and the archive.
	if want := []string{"results/jobaa.png", "results/jobba.png", "downloads/b.zip"}; !reflect.DeepEqual(files, want) {
		t.Errorf("files = %v, want %v", files, want)
	}
	for table, rows := range map[string]int{"projects": 1, "commands": 1, "jobs": 3} { // the kept project's two jobs and the REPL job
		if n := rowCount(t, db, table); n != rows {
			t.Errorf("%s has %d rows, want %d", table, n, rows)
		}
	}
	if _, err := r.Jobs.Get(ctx(t), kept[0].ID); err != nil {
		t.Errorf("a job of another project was deleted: %v", err)
	}
	if _, err := r.Jobs.GetRepl(ctx(t)); err != nil {
		t.Errorf("the REPL job was deleted with a project: %v", err)
	}
}

func mustJobsFor(t *testing.T, r *Repos, project, command domain.ID, n int) []*domain.Job {
	t.Helper()
	jobs := mustJobs(t, r, command, n)
	for _, j := range jobs {
		j.ProjectID = project
		check(t, r.Jobs.Update(ctx(t), j))
	}
	return jobs
}

func TestListCommandsByStatus(t *testing.T) {
	r := newDB(t).Repos()
	p := mustProject(t, r, "p")
	a := mustCommand(t, r, p.ID, "a", 0)
	b := mustCommand(t, r, p.ID, "b", 0)
	c := mustCommand(t, r, p.ID, "c", 0)
	check(t, r.Commands.SetStatus(ctx(t), a.ID, domain.StatusQueued))
	check(t, r.Commands.SetStatus(ctx(t), c.ID, domain.StatusProcessing))
	got, err := r.Commands.ListByStatus(ctx(t), domain.StatusQueued, domain.StatusProcessing)
	check(t, err)
	if len(got) != 2 || got[0].ID != a.ID || got[1].ID != c.ID {
		t.Errorf("commands = %+v, want a and c but not b (%d)", got, b.ID)
	}
	if got, _ = r.Commands.ListByStatus(ctx(t)); len(got) != 0 {
		t.Errorf("no status: %+v", got)
	}
}
