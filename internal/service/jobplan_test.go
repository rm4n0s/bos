package service

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/rm4n0s/bos/internal/command"
	"github.com/rm4n0s/bos/internal/domain"
)

func TestWhatAnItemCarriesReachesTheJob(t *testing.T) {
	e := newEnv(t)
	w := e.world(t)
	_, err := e.Items.Create(ctx(t), w.characters.ID, domain.Item{
		CodeName: "full", PositivePrompt: "full",
		IPAdapter:   &domain.IPAdapter{ImageFile: "ip.png", Weight: 0.5, WeightType: "linear", StartAt: 0.1, EndAt: 0.9, ClipVisionModel: "cv", ModelName: "ipm"},
		ControlNets: []domain.ControlNet{{Type: "canny", ImagePath: "c.png", IsReference: true, ModelPattern: "canny", Strength: 0.7}},
		MaskRegion:  &domain.MaskRegion{MaskFiles: map[string]string{"red": "m_red.png", "blue": "m_blue.png"}},
	})
	check(t, err)
	cmd := e.add(t, w, "gpu1 -$ anime: characters(full)")
	j := e.jobsOf(t, cmd.ID)[0]
	if want := (domain.IPAdapter{ImageFile: "ip.png", Weight: 0.5, WeightType: "linear", StartAt: 0.1, EndAt: 0.9, ClipVisionModel: "cv", ModelName: "ipm"}); len(j.IPAdapters) != 1 || j.IPAdapters[0] != want {
		t.Errorf("IP-Adapters = %+v", j.IPAdapters)
	}
	if want := (domain.ControlNet{Type: "canny", ImagePath: "c.png", IsReference: true, ModelPattern: "canny", Strength: 0.7}); len(j.ControlNets) != 1 || j.ControlNets[0] != want {
		t.Errorf("ControlNets = %+v", j.ControlNets)
	}
}

func TestMaskRegionsReachTheJob(t *testing.T) {
	e := newEnv(t)
	w := e.world(t)
	duo := e.group(t, "duo")
	_, err := e.Items.Create(ctx(t), duo.ID, domain.Item{CodeName: "scene", MaskRegion: &domain.MaskRegion{MaskFiles: map[string]string{"red": "m_red.png", "blue": "m_blue.png"}}})
	check(t, err)
	cmd := e.add(t, w, "gpu1 -$ anime: duo{red: characters(a), blue: characters(b)}")
	j := e.jobsOf(t, cmd.ID)[0]
	if len(j.RegionPrompts) != 2 || j.RegionPrompts[0].MaskFile != "m_red.png" || j.RegionPrompts[1].MaskFile != "m_blue.png" || j.RegionPrompts[0].Coordinates != nil {
		t.Errorf("region prompts = %+v", j.RegionPrompts)
	}
}

func TestOutputSettingsOfTheJob(t *testing.T) {
	e := newEnv(t)
	w := e.world(t)
	_, err := e.Items.Create(ctx(t), w.characters.ID, domain.Item{
		CodeName: "film", PositivePrompt: "film", OutputType: domain.OutputVideo,
		OutputTypeAttributes: domain.OutputAttributes{"fps": {Kind: domain.AttrInt, Int: 24}, "file_type": {Kind: domain.AttrText, Text: "webm"}},
	})
	check(t, err)

	cmd := e.add(t, w, "gpu1 -$ anime: characters(a) with cfg=6.5, sampler_name=euler, steps=12")
	j := e.jobsOf(t, cmd.ID)[0]
	if a := j.OutputAttributes["cfg"]; a.Kind != domain.AttrDecimal || a.Float != 6.5 {
		t.Errorf("cfg = %+v", a)
	}
	if a := j.OutputAttributes["sampler_name"]; a.Kind != domain.AttrText || a.Text != "euler" {
		t.Errorf("sampler_name = %+v", a)
	}
	if a := j.OutputAttributes["steps"]; a.Kind != domain.AttrInt || a.Int != 12 {
		t.Errorf("steps = %+v", a)
	}

	// An item that defines an output type and attributes overrides the
	// generator's; the planned file takes the item's file type.
	cmd = e.add(t, w, "gpu1 -$ anime: characters(film)")
	j = e.jobsOf(t, cmd.ID)[0]
	if j.OutputType != domain.OutputVideo || j.OutputAttributes["fps"].Int != 24 || !strings.HasSuffix(j.ResultPath, ".webm") {
		t.Errorf("output = %s %v, result %s", j.OutputType, j.OutputAttributes, j.ResultPath)
	}
}

func TestResultPaths(t *testing.T) {
	e := newEnv(t)
	s := e.Commands
	job := func(parts ...command.NamePart) *command.Job { return &command.Job{Parts: parts, RegionIndex: -1} }
	used := map[string]bool{}
	at := func(index int, pj *command.Job, repeats bool, typ string, attrs domain.OutputAttributes) string {
		return strings.TrimPrefix(s.resultPath(7, "gen", pj, repeats, typ, attrs, index, used), e.dirs.Results+string(filepath.Separator))
	}

	same := job(command.NamePart{Group: "g", Code: "a"})
	if got := at(0, same, false, "image", nil); got != "7/gen_c7_g-a.png" {
		t.Errorf("path = %s", got)
	}
	if got := at(1, same, false, "image", nil); got != "7/gen_c7_g-a_1.png" {
		t.Errorf("a second job with the same name = %s, want the job index added", got)
	}
	if got := at(2, job(command.NamePart{Code: "lit0"}, command.NamePart{Group: "g", Code: "we ird/code"}), true, "image", nil); got != "7/gen_c7_lit0_g-we_ird_code_r0.png" {
		t.Errorf("path = %s", got)
	}
	region := job(command.NamePart{Group: "g", Code: "z"})
	region.RegionIndex, region.Repeat = 3, 2
	if got := at(3, region, true, "image", nil); got != "7/gen_c7_g-z_g3_r2.png" {
		t.Errorf("path = %s", got)
	}

	long := job(command.NamePart{Group: "g", Code: strings.Repeat("x", 400)})
	longer := job(command.NamePart{Group: "g", Code: strings.Repeat("x", 400) + "y"})
	p1, p2 := at(4, long, false, "image", nil), at(5, longer, false, "image", nil)
	if len(filepath.Base(p1)) > 200 || p1 == p2 {
		t.Errorf("long names: %d characters, %q == %q: they must be cut and stay different", len(filepath.Base(p1)), p1, p2)
	}

	for _, tt := range []struct {
		typ   string
		attrs domain.OutputAttributes
		ext   string
	}{
		{"video", nil, "mp4"}, {"3d", nil, "glb"}, {"image", nil, "png"}, {"unknown", nil, "bin"},
		{"image", domain.OutputAttributes{"file_type": {Kind: domain.AttrText, Text: ".WebP"}}, "webp"},
		{"image", domain.OutputAttributes{"file_type": {Kind: domain.AttrText, Text: "a/../b"}}, "png"},
		{"image", domain.OutputAttributes{"file_type": {Kind: domain.AttrInt, Int: 3}}, "png"},
	} {
		if got := fileExtension(tt.typ, tt.attrs); got != tt.ext {
			t.Errorf("fileExtension(%s, %v) = %s, want %s", tt.typ, tt.attrs, got, tt.ext)
		}
	}
}

func TestDefaults(t *testing.T) {
	first, second := RandomSeed(), RandomSeed()
	if first == second {
		t.Error("two random seeds are equal")
	}
	e := newEnv(t)
	s := New(Deps{Store: NewStore(e.db), Files: e.files})
	if s.Groups.Logger == nil || s.Groups.NewSeed == nil {
		t.Fatal("defaults were not filled in")
	}
	// A default logger accepts the warning of a file that cannot be removed.
	s.Groups.removeFiles([]string{"/outside/the/storage"})
}

func TestRandomSeedGeneratorsLeaveTheSeedToTheRun(t *testing.T) {
	e := newEnv(t)
	w := e.world(t)
	_, err := e.Generators.Create(ctx(t), domain.Generator{Name: "dice", CodeName: "dice", Workflow: w.generator.Workflow, HasRandomSeed: true})
	check(t, err)

	plain := e.jobsOf(t, e.add(t, w, "gpu1 -$ dice: characters(a)").ID)[0]
	if plain.Seed != nil {
		t.Errorf("seed = %d: a random-seed generator picks its seed when the job runs", *plain.Seed)
	}
	set := e.jobsOf(t, e.add(t, w, "gpu1 -$ dice: characters(a) repeat 2 with seed=5").ID)
	if set[0].Seed == nil || *set[0].Seed != 5 || *set[1].Seed != 6 {
		t.Errorf("seeds = %v %v, want the command's 5 and 6", set[0].Seed, set[1].Seed)
	}
}
