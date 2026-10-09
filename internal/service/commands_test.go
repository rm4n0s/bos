package service

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/rm4n0s/bos/internal/command"
	"github.com/rm4n0s/bos/internal/domain"
)

func TestAddCreatesTheJobs(t *testing.T) {
	e := newEnv(t)
	w := e.world(t)
	cmd, err := e.Commands.Add(ctx(t), w.project.ID, CommandInput{Name: "grid", Description: "d", Code: "gpu1 -$ anime: characters * poses"})
	check(t, err)
	if cmd.ID == 0 || cmd.Order != 1 || cmd.Status != domain.StatusIdle || cmd.CommandCode != "gpu1 -$ anime: characters * poses" {
		t.Errorf("command = %+v", cmd)
	}
	parsed, err := command.FromJSON(cmd.CommandJSON)
	check(t, err)
	if parsed.Generator.Text != "anime" {
		t.Errorf("parsed form = %s", cmd.CommandJSON)
	}

	jobs := e.jobsOf(t, cmd.ID)
	if len(jobs) != 9 {
		t.Fatalf("%d jobs, want 9", len(jobs))
	}
	first, last := jobs[0], jobs[8]
	if first.PositivePrompt != "alice pose p1" || last.PositivePrompt != "carol pose p3" || jobs[1].PositivePrompt != "alice pose p2" {
		t.Errorf("prompts: %q, %q, %q: the last term must vary fastest", first.PositivePrompt, jobs[1].PositivePrompt, last.PositivePrompt)
	}
	wantItems := []domain.UsedItem{
		{GroupID: w.characters.ID, GroupCode: "characters", ItemID: 1, ItemCode: "a"},
		{GroupID: w.poses.ID, GroupCode: "poses", ItemID: 4, ItemCode: "p1"},
	}
	if !reflect.DeepEqual(first.Items, wantItems) {
		t.Errorf("items = %+v, want %+v", first.Items, wantItems)
	}
	if first.ProjectID != w.project.ID || first.CommandID != cmd.ID || first.Status != domain.StatusIdle || first.CommandText != cmd.CommandCode {
		t.Errorf("job = %+v", first)
	}
	if first.GeneratorCodeName != "anime" || first.OutputType != domain.OutputImage || first.RegionIndex != -1 || first.RepeatIndex != 0 {
		t.Errorf("job = %+v", first)
	}
	if first.PreferredServerID != nil || first.Pinned || first.ServerID != nil || len(first.Fixers) != 0 {
		t.Errorf("a dynamic job without fixers carries assignments: %+v", first)
	}
	// Without a seed in the command the seed is the one the workflow has.
	if want := w.generator.OutputAttributes["seed"]; first.Seed == nil || *first.Seed != want.Int {
		t.Errorf("seed = %v, want the generator's %d", first.Seed, want.Int)
	}
	if !reflect.DeepEqual(first.OutputAttributes, w.generator.OutputAttributes) {
		t.Errorf("output attributes = %v, want the generator's %v", first.OutputAttributes, w.generator.OutputAttributes)
	}
	for i, j := range jobs {
		want := filepath.Join(e.dirs.Results, "1", "anime_c1_"+j.Items[0].GroupCode+"-"+j.Items[0].ItemCode+"_poses-"+j.Items[1].ItemCode+".png")
		if j.ResultPath != want {
			t.Errorf("job %d result = %s, want %s", i, j.ResultPath, want)
		}
	}
}

func TestAddWithSpreadFixersAndSeed(t *testing.T) {
	e := newEnv(t)
	w := e.world(t)
	cmd := e.add(t, w, "gpu1, gpu2 -$ anime: characters * poses repeat 2 with steps=30, seed=10 spread blocks pinned > upscale on gpu2 > upscale")
	jobs := e.jobsOf(t, cmd.ID)
	if len(jobs) != 18 {
		t.Fatalf("%d jobs, want 18", len(jobs))
	}
	counts := map[int64]int{}
	for i, j := range jobs {
		if j.PreferredServerID == nil || !j.Pinned {
			t.Fatalf("job %d: preferred = %v, pinned = %v", i, j.PreferredServerID, j.Pinned)
		}
		counts[*j.PreferredServerID]++
		if j.RepeatIndex != i%2 {
			t.Errorf("job %d repeat = %d", i, j.RepeatIndex)
		}
		if j.Seed == nil || *j.Seed != uint64(10+i%2) {
			t.Errorf("job %d seed = %v, want %d", i, j.Seed, 10+i%2)
		}
		if a := j.OutputAttributes["steps"]; a.Kind != domain.AttrInt || a.Int != 30 {
			t.Errorf("job %d steps = %+v", i, a)
		}
		if a := j.OutputAttributes["seed"]; a.Kind != domain.AttrInt || a.Int != uint64(10+i%2) {
			t.Errorf("job %d seed attribute = %+v", i, a)
		}
	}
	if counts[w.gpu1.ID] != 9 || counts[w.gpu2.ID] != 9 {
		t.Errorf("jobs per server = %v, want 9 and 9", counts)
	}
	wantFixers := []domain.FixerStep{
		{FixerID: w.fixer.ID, FixerCode: "upscale", ServerID: w.gpu2.ID, ServerCode: "gpu2"},
		{FixerID: w.fixer.ID, FixerCode: "upscale"},
	}
	if !reflect.DeepEqual(jobs[0].Fixers, wantFixers) {
		t.Errorf("fixers = %+v, want %+v", jobs[0].Fixers, wantFixers)
	}
	if !strings.HasSuffix(jobs[0].ResultPath, "_r0.png") || !strings.HasSuffix(jobs[1].ResultPath, "_r1.png") {
		t.Errorf("repeat index missing from %s, %s", jobs[0].ResultPath, jobs[1].ResultPath)
	}
}

func TestRegionLoRAsJoinTheJobsList(t *testing.T) {
	e := newEnv(t)
	w := e.world(t)
	duo := e.group(t, "duo")
	_, err := e.Items.Create(ctx(t), duo.ID, domain.Item{CodeName: "scene", PositivePrompt: "two people", CoordinatedRegions: []domain.CoordinatedRegion{
		{Keyword: "red", Width: 100, Height: 200, X: 0, Y: 0}, {Keyword: "blue", Width: 100, Height: 200, X: 100, Y: 0},
	}})
	check(t, err)
	people := e.group(t, "people")
	for _, p := range []struct{ code, lora string }{{"alice", "L1"}, {"bob", "L1"}, {"carol", "L2"}} {
		_, err := e.Items.Create(ctx(t), people.ID, domain.Item{CodeName: p.code, PositivePrompt: p.code, LoRAs: []domain.LoRA{{Name: p.lora, StrengthModel: 0.8, StrengthClip: 0.7}}})
		check(t, err)
	}
	_, err = e.Items.Create(ctx(t), w.characters.ID, domain.Item{CodeName: "withlora", PositivePrompt: "base", LoRAs: []domain.LoRA{{Name: "L2", StrengthModel: 1, StrengthClip: 1}}})
	check(t, err)

	cmd := e.add(t, w, "gpu1 -$ anime: duo{red: people(alice), blue: people(bob, carol)} * characters(withlora)")
	jobs := e.jobsOf(t, cmd.ID)
	if len(jobs) != 2 {
		t.Fatalf("%d jobs, want 1 item × 1 item × R = 2", len(jobs))
	}
	j := jobs[0]
	if j.RegionIndex != 0 || len(j.RegionPrompts) != 2 {
		t.Fatalf("region prompts = %+v", j.RegionPrompts)
	}
	red := j.RegionPrompts[0]
	if red.Keyword != "red" || red.Prompt != "alice" || red.Coordinates == nil || red.Coordinates.Width != 100 || red.MaskFile != "" {
		t.Errorf("red = %+v", red)
	}
	if blue := j.RegionPrompts[1]; blue.Keyword != "blue" || blue.Coordinates == nil || blue.Coordinates.X != 100 {
		t.Errorf("blue = %+v", blue)
	}
	// The job's own LoRA first, then the regions' LoRAs, each name once.
	var names []string
	for _, l := range j.LoRAs {
		names = append(names, l.Name)
	}
	if !reflect.DeepEqual(names, []string{"L2", "L1"}) {
		t.Errorf("job LoRAs = %v, want L2 then L1", names)
	}
	if jobs[1].RegionIndex != 1 || !strings.Contains(jobs[1].ResultPath, "_g1.png") {
		t.Errorf("second region set = %d, %s", jobs[1].RegionIndex, jobs[1].ResultPath)
	}
}

func TestAddInsertsAtAPosition(t *testing.T) {
	e := newEnv(t)
	w := e.world(t)
	a := e.add(t, w, "gpu1 -$ anime: characters(a)")
	b := e.add(t, w, "gpu1 -$ anime: characters(b)")
	mid, err := e.Commands.Add(ctx(t), w.project.ID, CommandInput{Name: "mid", Code: "gpu1 -$ anime: characters(c)", InsertAt: 2})
	check(t, err)
	if a.Order != 1 || b.Order != 2 || mid.Order != 2 {
		t.Fatalf("orders: %d, %d, %d", a.Order, b.Order, mid.Order)
	}
	page, err := e.Commands.List(ctx(t), w.project.ID, domain.Page{})
	check(t, err)
	var got []string
	for _, c := range page.Items {
		got = append(got, c.Name)
		if c.TotalJobs != 1 || c.FinishedJobs != 0 {
			t.Errorf("progress of %s = %d/%d", c.Name, c.FinishedJobs, c.TotalJobs)
		}
	}
	if !reflect.DeepEqual(got, []string{"n", "mid", "n"}) || page.Items[1].ID != mid.ID || page.Items[2].ID != b.ID {
		t.Errorf("order = %v", got)
	}
}

func TestInvalidCommandStoresNothing(t *testing.T) {
	e := newEnv(t)
	w := e.world(t)
	existing := e.add(t, w, "gpu1 -$ anime: characters(a)")

	tests := []struct {
		name string
		code string
		tag  string
	}{
		{"lexical error", "gpu1 -$ anime: characters @", "CommandLexError"},
		{"syntax error", "gpu1 -$ anime characters", "CommandSyntaxError"},
		{"restriction", "gpu1 -$ anime: characters && poses || characters", "CommandRestrictionViolated"},
		{"unknown server", "gpu9 -$ anime: characters", "CommandValidationFailed"},
		{"unknown group and item", "gpu1 -$ anime: nowhere * characters(zzz)", "CommandValidationFailed"},
		{"where removes everything", "gpu1 -$ anime: characters where #calm and #combat", "CommandValidationFailed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := e.Commands.Add(ctx(t), w.project.ID, CommandInput{Code: tt.code})
			wantRoute(t, err, "CommandService.Add", tt.tag)
			_, err = e.Commands.Edit(ctx(t), existing.ID, CommandInput{Code: tt.code})
			wantRoute(t, err, "CommandService.Edit", tt.tag)
		})
	}
	if page, _ := e.Commands.List(ctx(t), w.project.ID, domain.Page{}); page.Total != 1 {
		t.Errorf("%d commands, a rejected add stored something", page.Total)
	}
	if jobs := e.jobsOf(t, existing.ID); len(jobs) != 1 || jobs[0].PositivePrompt != "alice" {
		t.Errorf("a rejected edit changed the jobs: %+v", jobs)
	}
	if got, _ := e.Commands.Get(ctx(t), existing.ID); got.CommandCode != "gpu1 -$ anime: characters(a)" {
		t.Errorf("a rejected edit changed the text: %q", got.CommandCode)
	}

	// The problems are all listed, with positions.
	_, err := e.Commands.Add(ctx(t), w.project.ID, CommandInput{Code: "gpu9 -$ nope: characters"})
	appErr := wantRouteErr(t, err, "CommandService.Add", "CommandValidationFailed")
	problems, _ := appErr.Metadata["errors"].([]command.Problem)
	if len(problems) != 2 {
		t.Errorf("problems = %+v, want the unknown server and generator together", problems)
	}
}

func TestAddInputFailures(t *testing.T) {
	e := newEnv(t)
	w := e.world(t)
	_, err := e.Commands.Add(ctx(t), 999, CommandInput{Code: "gpu1 -$ anime: characters"})
	wantRoute(t, err, "CommandService.Add", "ProjectNotFound")
	_, err = e.Commands.Add(ctx(t), w.project.ID, CommandInput{Name: strings.Repeat("n", 301), Code: "gpu1 -$ anime: characters"})
	wantRoute(t, err, "CommandService.Add", "NameInvalid")
	c := e.add(t, w, "gpu1 -$ anime: characters")
	_, err = e.Commands.Edit(ctx(t), c.ID, CommandInput{Name: strings.Repeat("n", 301), Code: c.CommandCode})
	wantRoute(t, err, "CommandService.Edit", "NameInvalid")
	_, err = e.Commands.Edit(ctx(t), 999, CommandInput{Code: "x"})
	wantRoute(t, err, "CommandService.Edit", "CommandNotFound")
	_, err = e.Commands.Recreate(ctx(t), 999)
	wantRoute(t, err, "CommandService.Recreate", "CommandNotFound")
	wantRoute(t, e.Commands.Delete(ctx(t), 999), "CommandService.Delete", "CommandNotFound")
	_, err = e.Commands.Get(ctx(t), 999)
	wantRoute(t, err, "CommandService.Get", "CommandNotFound")
	_, err = e.Commands.List(ctx(t), 999, domain.Page{})
	wantRoute(t, err, "CommandService.List", "ProjectNotFound")
	_, err = e.Commands.Up(ctx(t), 999)
	wantRoute(t, err, "CommandService.step", "CommandNotFound")
	wantRoute(t, e.Commands.Renumber(ctx(t), 999), "CommandService.Renumber", "ProjectNotFound")
	_, err = e.Commands.Move(ctx(t), 999, 1)
	wantRoute(t, err, "CommandService.Move", "CommandNotFound")
}

func TestEditReplacesTheJobsWhenTheTextChanges(t *testing.T) {
	e := newEnv(t)
	w := e.world(t)
	cmd := e.add(t, w, "gpu1 -$ anime: characters")
	old := e.jobsOf(t, cmd.ID)
	oldResult := e.write(t, strings.TrimPrefix(old[0].ResultPath, e.root+"/"), "image")
	archive := e.write(t, "downloads/1.zip", "zip")
	stored, _ := e.repos.Commands().Get(ctx(t), cmd.ID)
	stored.DownloadFile, stored.Status = archive, domain.StatusFinished
	check(t, e.repos.Commands().Update(ctx(t), stored))

	// Only the name and description change: the jobs and the archive stay.
	same, err := e.Commands.Edit(ctx(t), cmd.ID, CommandInput{Name: "renamed", Description: "new", Code: cmd.CommandCode})
	check(t, err)
	if same.Name != "renamed" || same.Description != "new" || same.Status != domain.StatusFinished || same.DownloadFile != archive {
		t.Errorf("renamed = %+v", same)
	}
	if got := e.jobsOf(t, cmd.ID); !reflect.DeepEqual(got, old) {
		t.Error("the jobs changed although the text did not")
	}
	if !exists(oldResult) || !exists(archive) {
		t.Error("files were removed although the text did not change")
	}

	// A new text: new jobs, old files and archive gone, command idle.
	edited, err := e.Commands.Edit(ctx(t), cmd.ID, CommandInput{Name: "renamed", Code: "gpu1 -$ anime: poses"})
	check(t, err)
	if edited.Status != domain.StatusIdle || edited.DownloadFile != "" || edited.Order != 1 || edited.CommandCode != "gpu1 -$ anime: poses" {
		t.Errorf("edited = %+v", edited)
	}
	jobs := e.jobsOf(t, cmd.ID)
	if len(jobs) != 3 || jobs[0].PositivePrompt != "pose p1" {
		t.Errorf("jobs after the edit: %+v", jobs)
	}
	if exists(oldResult) || exists(archive) {
		t.Error("the old result or archive was kept")
	}
	if got, _ := e.Commands.Get(ctx(t), cmd.ID); got.Status != domain.StatusIdle || got.CommandCode != "gpu1 -$ anime: poses" {
		t.Errorf("stored = %+v", got)
	}
}

func TestRecreateReproducesASample(t *testing.T) {
	e := newEnv(t)
	w := e.world(t)
	cmd := e.add(t, w, "gpu1 -$ anime: characters * poses sample 4")
	parsed, err := command.FromJSON(cmd.CommandJSON)
	check(t, err)
	if parsed.Sample == nil || parsed.Sample.Seed == nil || parsed.Sample.Seed.N != 100 {
		t.Fatalf("the stored form must carry the generated seed: %s", cmd.CommandJSON)
	}
	before := e.jobsOf(t, cmd.ID)
	if len(before) != 4 {
		t.Fatalf("%d jobs", len(before))
	}
	stale := e.write(t, strings.TrimPrefix(before[0].ResultPath, e.root+"/"), "image")

	again, err := e.Commands.Recreate(ctx(t), cmd.ID)
	check(t, err)
	after := e.jobsOf(t, cmd.ID)
	if again.Status != domain.StatusIdle || len(after) != 4 {
		t.Fatalf("recreated = %+v with %d jobs", again, len(after))
	}
	for i := range after {
		if after[i].PositivePrompt != before[i].PositivePrompt || after[i].ResultPath != before[i].ResultPath {
			t.Errorf("job %d is %q, was %q: the sample changed", i, after[i].PositivePrompt, before[i].PositivePrompt)
		}
		if after[i].ID == before[i].ID {
			t.Errorf("job %d kept its id", i)
		}
	}
	if exists(stale) {
		t.Error("the old result file was kept")
	}
	if got, _ := e.Commands.Get(ctx(t), cmd.ID); !reflect.DeepEqual(got.CommandJSON, cmd.CommandJSON) {
		t.Error("the stored parsed form changed")
	}
}

func TestRecreateUsesTheCatalogueAsItIsNow(t *testing.T) {
	e := newEnv(t)
	w := e.world(t)
	cmd := e.add(t, w, "gpu1 -$ anime: characters")
	e.item(t, w.characters.ID, "d", "dave")
	_, err := e.Commands.Recreate(ctx(t), cmd.ID)
	check(t, err)
	if jobs := e.jobsOf(t, cmd.ID); len(jobs) != 4 {
		t.Errorf("%d jobs, want the new item included", len(jobs))
	}

	check(t, e.Items.Delete(ctx(t), 1)) // item a
	e.generator(t, "other")
	check(t, e.Servers.Delete(ctx(t), w.gpu1.ID))
	_, err = e.Commands.Recreate(ctx(t), cmd.ID)
	wantRoute(t, err, "CommandService.Recreate", "CommandValidationFailed")
	if jobs := e.jobsOf(t, cmd.ID); len(jobs) != 4 {
		t.Errorf("a failed recreate removed the jobs: %d left", len(jobs))
	}
}

func TestRecreateWithBrokenStoredForm(t *testing.T) {
	e := newEnv(t)
	w := e.world(t)
	c := e.commandRaw(t, w.project.ID, "raw", "gpu1 -$ anime: characters")
	_, err := e.Commands.Recreate(ctx(t), c.ID)
	wantRoute(t, err, "CommandService.Recreate", "CommandJSONInvalid")
}

func TestBusyCommandsAreProtected(t *testing.T) {
	e := newEnv(t)
	w := e.world(t)
	for _, status := range []domain.Status{domain.StatusQueued, domain.StatusProcessing} {
		cmd := e.add(t, w, "gpu1 -$ anime: characters")
		check(t, e.repos.Commands().SetStatus(ctx(t), cmd.ID, status))
		_, err := e.Commands.Edit(ctx(t), cmd.ID, CommandInput{Name: "x", Code: cmd.CommandCode})
		wantRoute(t, err, "CommandService.Edit", "CommandBusy")
		_, err = e.Commands.Recreate(ctx(t), cmd.ID)
		wantRoute(t, err, "CommandService.Recreate", "CommandBusy")
		wantRoute(t, e.Commands.Delete(ctx(t), cmd.ID), "CommandService.Delete", "CommandBusy")
		wantRoute(t, e.Projects.Delete(ctx(t), w.project.ID), "ProjectService.Delete", "CommandBusy")
		if len(e.jobsOf(t, cmd.ID)) != 3 {
			t.Error("jobs of a busy command were touched")
		}
		check(t, e.repos.Commands().SetStatus(ctx(t), cmd.ID, domain.StatusIdle))
	}
}

func TestDeleteCommandAndProject(t *testing.T) {
	e := newEnv(t)
	w := e.world(t)
	a := e.add(t, w, "gpu1 -$ anime: characters(a)")
	b := e.add(t, w, "gpu1 -$ anime: characters(b)")
	ra := e.write(t, strings.TrimPrefix(e.jobsOf(t, a.ID)[0].ResultPath, e.root+"/"), "x")
	rb := e.write(t, strings.TrimPrefix(e.jobsOf(t, b.ID)[0].ResultPath, e.root+"/"), "x")

	check(t, e.Commands.Delete(ctx(t), a.ID))
	if exists(ra) || len(e.jobsOf(t, a.ID)) != 0 {
		t.Error("the deleted command left a result or jobs")
	}
	if got, _ := e.Commands.Get(ctx(t), b.ID); got.Order != 1 {
		t.Errorf("order after the delete = %d, want the gap closed", got.Order)
	}

	check(t, e.Projects.Delete(ctx(t), w.project.ID))
	if exists(rb) {
		t.Error("the project's result file survived")
	}
	_, err := e.Commands.Get(ctx(t), b.ID)
	wantRoute(t, err, "CommandService.Get", "CommandNotFound")
}

func TestMoveUpDownRenumber(t *testing.T) {
	e := newEnv(t)
	w := e.world(t)
	var ids []int64
	for _, c := range []string{"a", "b", "c"} {
		ids = append(ids, e.add(t, w, "gpu1 -$ anime: characters("+c+")").ID)
	}
	order := func() []int64 {
		cmds, err := e.repos.Commands().AllByProject(ctx(t), w.project.ID)
		check(t, err)
		var out []int64
		for _, c := range cmds {
			out = append(out, c.ID)
		}
		return out
	}

	if at, err := e.Commands.Up(ctx(t), ids[0]); err != nil || at != 1 {
		t.Errorf("Up at the start = %d, %v: it must stay", at, err)
	}
	if at, err := e.Commands.Down(ctx(t), ids[0]); err != nil || at != 2 {
		t.Errorf("Down = %d, %v", at, err)
	}
	if got := order(); !reflect.DeepEqual(got, []int64{ids[1], ids[0], ids[2]}) {
		t.Errorf("order = %v", got)
	}
	if at, err := e.Commands.Down(ctx(t), ids[2]); err != nil || at != 3 {
		t.Errorf("Down at the end = %d, %v: it must stay", at, err)
	}
	if at, err := e.Commands.Up(ctx(t), ids[2]); err != nil || at != 2 {
		t.Errorf("Up = %d, %v", at, err)
	}
	if at, err := e.Commands.Move(ctx(t), ids[2], 99); err != nil || at != 3 {
		t.Errorf("Move past the end = %d, %v", at, err)
	}
	check(t, e.Commands.Renumber(ctx(t), w.project.ID))
	if got := order(); !reflect.DeepEqual(got, []int64{ids[1], ids[0], ids[2]}) {
		t.Errorf("order after renumber = %v", got)
	}
}
