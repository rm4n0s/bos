package acceptance

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rm4n0s/bos/internal/apptest"
	"github.com/rm4n0s/bos/internal/command"
	"github.com/rm4n0s/bos/internal/domain"
	"github.com/rm4n0s/bos/internal/service"
	"github.com/rm4n0s/errors"
)

func check(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func example(t *testing.T, kind, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "examples", "workflows", kind, name))
	check(t, err)
	return b
}

// world is the sample data of SPEC §6.12, in small: servers gpu1 and gpu2, a
// generator, a fixer, and the groups characters and poses.
func world(t *testing.T) (*apptest.Env, domain.ID) {
	t.Helper()
	e := apptest.New(t)
	svc, ctx := e.Services, t.Context()
	for _, code := range []string{"gpu1", "gpu2"} {
		_, err := svc.Servers.Create(ctx, domain.Server{Name: code, Host: "http://" + code + ":8188", CodeName: code, MaxConcurrentJobs: 1})
		check(t, err)
	}
	_, err := svc.Generators.Create(ctx, domain.Generator{Name: "g", CodeName: "g", Workflow: example(t, "creators", "illustrious_xl.json")})
	check(t, err)
	_, err = svc.Fixers.Create(ctx, domain.Fixer{Name: "up", CodeName: "upscale", Workflow: example(t, "fixers", "simple_upscale.json"), LoadImageTitle: "Load Image", SaveImageTitle: "Save Image"})
	check(t, err)
	chars, err := svc.Groups.Create(ctx, domain.Group{Name: "characters", CodeName: "characters"})
	check(t, err)
	poses, err := svc.Groups.Create(ctx, domain.Group{Name: "poses", CodeName: "poses"})
	check(t, err)
	for _, c := range []string{"alice", "bob", "carol"} {
		_, err := svc.Items.Create(ctx, chars.ID, domain.Item{CodeName: c, Name: c, PositivePrompt: c, Tags: []string{"person"}})
		check(t, err)
	}
	for _, p := range []string{"standing", "jumping"} {
		_, err := svc.Items.Create(ctx, poses.ID, domain.Item{CodeName: p, Name: p, PositivePrompt: p})
		check(t, err)
	}
	project, err := svc.Projects.Create(ctx, domain.Project{Name: "p"})
	check(t, err)
	return e, project.ID
}

// Acceptance 4: a command that names things that do not exist, also inside a
// template body or a region body, gets one error for each problem and creates
// nothing.
func TestAcceptance04UnknownNamesAreEachReportedAndNothingIsCreated(t *testing.T) {
	e, project := world(t)
	svc, ctx := e.Services, t.Context()
	scene, err := svc.Groups.Create(ctx, domain.Group{Name: "scene", CodeName: "scene"})
	check(t, err)
	_, err = svc.Items.Create(ctx, scene.ID, domain.Item{CodeName: "s1", Name: "s1", PositivePrompt: "a <tmpl:subject> here"})
	check(t, err)

	code := "nosrv -$ nogen: nogroup(noitem) * characters(nobody) * scene[subject: ghosts] > nofix"
	_, err = svc.Commands.Add(ctx, project, service.CommandInput{Name: "x", Code: code})
	appErr, ok := errors.FromError(err)
	if !ok || appErr.Tag != "CommandValidationFailed" {
		t.Fatalf("err = %v", err)
	}
	problems, _ := appErr.Metadata["errors"].([]command.Problem)
	byTag := map[string][]string{}
	for _, p := range problems {
		byTag[p.Tag] = append(byTag[p.Tag], p.Message)
	}
	for _, tag := range []string{"ServerNotFound", "GeneratorNotFound", "FixerNotFound", "GroupNotFound", "ItemNotFound"} {
		if len(byTag[tag]) == 0 {
			t.Errorf("no %s among %v", tag, byTag)
		}
	}
	// The group that is only mentioned inside the template body is found too.
	found := false
	for _, m := range byTag["GroupNotFound"] {
		if strings.Contains(m, "ghosts") {
			found = true
		}
	}
	if !found {
		t.Errorf("the unknown group in the template body was not reported: %v", byTag["GroupNotFound"])
	}
	// One error for each problem: no name is reported twice for the same tag.
	for tag, msgs := range byTag {
		seen := map[string]bool{}
		for _, m := range msgs {
			if seen[m] {
				t.Errorf("%s: %q is reported twice", tag, m)
			}
			seen[m] = true
		}
	}
	list, err := svc.Commands.List(ctx, project, domain.Page{})
	check(t, err)
	if list.Total != 0 {
		t.Errorf("a refused command was stored: %+v", list.Items)
	}
}

// Acceptance 38: tags are lower-cased on save, an invalid one is refused,
// duplicates collapse, and `#Combat` in a command matches an item tagged `combat`.
func TestAcceptance38TagsAreLowerCasedAndMatchedWhateverTheCase(t *testing.T) {
	e, project := world(t)
	svc, ctx := e.Services, t.Context()
	chars, err := svc.Groups.Get(ctx, 1)
	check(t, err)

	item, err := svc.Items.Create(ctx, chars.ID, domain.Item{CodeName: "dave", Name: "dave", PositivePrompt: "dave", Tags: []string{"Combat", "COMBAT", "combat", "Hero"}})
	check(t, err)
	if len(item.Tags) != 2 || item.Tags[0] != "combat" || item.Tags[1] != "hero" {
		t.Errorf("tags = %v, want [combat hero]", item.Tags)
	}
	_, err = svc.Items.Create(ctx, chars.ID, domain.Item{CodeName: "erin", Name: "erin", Tags: []string{"Bad Tag!"}})
	if appErr, ok := errors.FromError(err); !ok || appErr.Tag != "TagInvalid" {
		t.Errorf("err = %v, want TagInvalid", err)
	}

	cmd, err := svc.Commands.Add(ctx, project, service.CommandInput{Name: "x", Code: "gpu1 -$ g: characters where #Combat"})
	check(t, err)
	jobs, err := e.DB.Repos().Jobs.AllByCommand(ctx, cmd.ID)
	check(t, err)
	if len(jobs) != 1 || jobs[0].PositivePrompt != "dave" {
		t.Errorf("jobs = %d, want the one item tagged combat", len(jobs))
	}
	// And by selector, in any case.
	cmd, err = svc.Commands.Add(ctx, project, service.CommandInput{Name: "y", Code: "gpu1 -$ g: characters(#HERO)"})
	check(t, err)
	if jobs, _ := e.DB.Repos().Jobs.AllByCommand(ctx, cmd.ID); len(jobs) != 1 {
		t.Errorf("selector jobs = %d", len(jobs))
	}
}

// Acceptance 41: a group tag organises groups and is invisible to commands.
func TestAcceptance41GroupTagsAreNotSelectable(t *testing.T) {
	e, project := world(t)
	svc, ctx := e.Services, t.Context()
	_, err := svc.Groups.Update(ctx, 1, domain.Group{Name: "characters", CodeName: "characters", Tags: []string{"character"}})
	check(t, err)
	list, err := svc.Groups.List(ctx, "character", domain.Page{})
	check(t, err)
	if list.Total != 1 || list.Items[0].CodeName != "characters" {
		t.Errorf("groups tagged character = %+v", list.Items)
	}
	_, err = svc.Commands.Add(ctx, project, service.CommandInput{Name: "x", Code: "gpu1 -$ g: characters where #character"})
	if appErr, ok := errors.FromError(err); !ok || appErr.Tag != "CommandValidationFailed" {
		t.Errorf("a group tag selected items: %v", err)
	}
}

// Acceptance 32 through the whole core: two nodes with the same title are
// refused when the generator is created.
func TestAcceptance32DuplicateTitlesAreRefused(t *testing.T) {
	e := apptest.New(t)
	var graph map[string]map[string]any
	check(t, json.Unmarshal(example(t, "creators", "illustrious_xl.json"), &graph))
	var titled []string
	for id, node := range graph {
		if meta, ok := node["_meta"].(map[string]any); ok && strings.HasPrefix(node["class_type"].(string), "CLIPTextEncode") {
			meta["title"] = "Positive"
			titled = append(titled, id)
		}
	}
	if len(titled) < 2 {
		t.Skip("the example has fewer than two text-encode nodes")
	}
	wf, err := json.Marshal(graph)
	check(t, err)
	_, err = e.Services.Generators.Create(t.Context(), domain.Generator{Name: "g", CodeName: "g", Workflow: wf, PositivePromptTitle: "Positive"})
	if appErr, ok := errors.FromError(err); !ok || appErr.Tag != "WorkflowTitleDuplicated" {
		t.Errorf("err = %v, want WorkflowTitleDuplicated", err)
	}
}
