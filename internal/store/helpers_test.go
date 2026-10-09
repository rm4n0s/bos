package store

import (
	"context"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rm4n0s/bos/internal/domain"
	"github.com/rm4n0s/errors"
)

// fakeClock returns a time that moves one second with every call, so ordering
// by timestamp is deterministic.
func fakeClock() func() time.Time {
	var n atomic.Int64
	start := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	return func() time.Time { return start.Add(time.Duration(n.Add(1)) * time.Second) }
}

// newDB opens a migrated in-memory database.
func newDB(t *testing.T) *DB {
	t.Helper()
	db, err := Open(t.Context(), InMemory, WithClock(fakeClock()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	if err := db.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	return db
}

// newFileDB opens a migrated database file, which gives real concurrency
// between connections.
func newFileDB(t *testing.T) *DB {
	t.Helper()
	db, err := Open(t.Context(), filepath.Join(t.TempDir(), "bos.db"), WithClock(fakeClock()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	if err := db.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	return db
}

func ctx(t *testing.T) context.Context { return t.Context() }

func check(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// wantRoute asserts that err is an application error that carries tag, raised
// under the method entry (the first function of its route after the test).
func wantRoute(t *testing.T, err error, entry, tag string) {
	t.Helper()
	appErr, ok := errors.FromError(err)
	if !ok {
		t.Fatalf("expected *errors.Error, got %T (%v)", err, err)
	}
	if appErr.Tag != tag || !appErr.HasRoute(entry) || !strings.HasSuffix(appErr.Route(), "."+tag) {
		t.Errorf("unexpected failure path: %s (tag %s)\nwant %s ... %s", appErr.Route(), appErr.Tag, entry, tag)
	}
}

func newServer(code string) *domain.Server {
	return &domain.Server{Name: "Server " + code, Host: "http://" + code + ":8188", CodeName: code, MaxConcurrentJobs: 1}
}

func mustGroup(t *testing.T, r *Repos, code string, tags ...string) *domain.Group {
	t.Helper()
	g := &domain.Group{Name: code, CodeName: code, Tags: tags}
	check(t, r.Groups.Create(ctx(t), g))
	return g
}

func mustItem(t *testing.T, r *Repos, group domain.ID, code string, tags ...string) *domain.Item {
	t.Helper()
	it := &domain.Item{GroupID: group, CodeName: code, Name: code, PositivePrompt: code, Tags: tags}
	check(t, r.Items.Create(ctx(t), it))
	return it
}

func mustProject(t *testing.T, r *Repos, name string) *domain.Project {
	t.Helper()
	p := &domain.Project{Name: name}
	check(t, r.Projects.Create(ctx(t), p))
	return p
}

func mustCommand(t *testing.T, r *Repos, project domain.ID, name string, at int) *domain.Command {
	t.Helper()
	c := &domain.Command{ProjectID: project, Name: name, CommandCode: "s -$ g: " + name}
	check(t, r.Commands.Create(ctx(t), c, at))
	return c
}

func orderOf(t *testing.T, r *Repos, project domain.ID) []string {
	t.Helper()
	cmds, err := r.Commands.AllByProject(ctx(t), project)
	check(t, err)
	out := make([]string, len(cmds))
	for i, c := range cmds {
		if c.Order != i+1 {
			t.Errorf("command %s has order %d at position %d", c.Name, c.Order, i+1)
		}
		out[i] = c.Name
	}
	return out
}

func newJob(command domain.ID, text string) *domain.Job {
	return &domain.Job{
		ProjectID: 1, CommandID: command, CommandText: "s -$ g: a", Status: domain.StatusIdle, RegionIndex: -1,
		GeneratorCodeName: "g", OutputType: domain.OutputImage, PositivePrompt: text, ResultPath: "results/" + text + ".png",
	}
}

func mustJobs(t *testing.T, r *Repos, command domain.ID, n int) []*domain.Job {
	t.Helper()
	jobs := make([]*domain.Job, n)
	for i := range jobs {
		jobs[i] = newJob(command, "job"+string(rune('a'+i%26))+string(rune('a'+i/26)))
	}
	check(t, r.Jobs.CreateMany(ctx(t), jobs))
	return jobs
}
