package service

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/rm4n0s/bos/internal/domain"
)

// example reads one of the example workflows shipped with the project.
func example(t *testing.T, kind, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "examples", "workflows", kind, name))
	check(t, err)
	return b
}

func (e *env) server(t *testing.T, code string) *domain.Server {
	t.Helper()
	v, err := e.Servers.Create(ctx(t), domain.Server{Name: "Server " + code, Host: "http://" + code + ":8188", CodeName: code, MaxConcurrentJobs: 1})
	check(t, err)
	return &v.Server
}

func (e *env) generator(t *testing.T, code string) *domain.Generator {
	t.Helper()
	v, err := e.Generators.Create(ctx(t), domain.Generator{Name: code, CodeName: code, Workflow: example(t, "creators", "illustrious_xl.json")})
	check(t, err)
	return v
}

func (e *env) fixer(t *testing.T, code string) *domain.Fixer {
	t.Helper()
	v, err := e.Fixers.Create(ctx(t), domain.Fixer{
		Name: code, CodeName: code, Workflow: example(t, "fixers", "simple_upscale.json"),
		LoadImageTitle: "Load Image", SaveImageTitle: "Save Image",
	})
	check(t, err)
	return v
}

func (e *env) group(t *testing.T, code string, tags ...string) *domain.Group {
	t.Helper()
	v, err := e.Groups.Create(ctx(t), domain.Group{Name: code, CodeName: code, Tags: tags})
	check(t, err)
	return v
}

func (e *env) item(t *testing.T, group domain.ID, code, positive string, tags ...string) *domain.Item {
	t.Helper()
	v, err := e.Items.Create(ctx(t), group, domain.Item{CodeName: code, Name: code, PositivePrompt: positive, Tags: tags})
	check(t, err)
	return v
}

func (e *env) project(t *testing.T, name string) *domain.Project {
	t.Helper()
	v, err := e.Projects.Create(ctx(t), domain.Project{Name: name})
	check(t, err)
	return v
}

// commandRaw stores a command row without validating or expanding its text.
func (e *env) commandRaw(t *testing.T, project domain.ID, name, code string) *domain.Command {
	t.Helper()
	c := &domain.Command{ProjectID: project, Name: name, CommandCode: code}
	check(t, e.repos.Commands().Create(ctx(t), c, 0))
	return c
}

// world is a small catalogue: two servers, a generator, a fixer, the groups
// characters (a, b, c) and poses (p1, p2, p3) and a project.
type world struct {
	gpu1, gpu2 *domain.Server
	generator  *domain.Generator
	fixer      *domain.Fixer
	characters *domain.Group
	poses      *domain.Group
	project    *domain.Project
}

func (e *env) world(t *testing.T) *world {
	t.Helper()
	w := &world{
		gpu1: e.server(t, "gpu1"), gpu2: e.server(t, "gpu2"), generator: e.generator(t, "anime"), fixer: e.fixer(t, "upscale"),
		characters: e.group(t, "characters"), poses: e.group(t, "poses"), project: e.project(t, "p"),
	}
	e.item(t, w.characters.ID, "a", "alice", "combat")
	e.item(t, w.characters.ID, "b", "bob", "calm")
	e.item(t, w.characters.ID, "c", "carol", "combat")
	for _, p := range []string{"p1", "p2", "p3"} {
		e.item(t, w.poses.ID, p, "pose "+p)
	}
	return w
}

// add adds a command to the world's project.
func (e *env) add(t *testing.T, w *world, code string) *domain.Command {
	t.Helper()
	c, err := e.Commands.Add(ctx(t), w.project.ID, CommandInput{Name: "n", Code: code})
	check(t, err)
	return c
}

func (e *env) jobsOf(t *testing.T, command domain.ID) []domain.Job {
	t.Helper()
	jobs, err := e.db.Repos().Jobs.AllByCommand(ctx(t), command)
	check(t, err)
	return jobs
}
