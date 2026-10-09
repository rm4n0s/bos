package service

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rm4n0s/bos/internal/civitai"
	"github.com/rm4n0s/bos/internal/domain"
	"github.com/rm4n0s/bos/internal/events"
	"github.com/rm4n0s/errors"
)

// fakeCivitai is civitai.Models with scripted answers.
type fakeCivitai struct {
	mu         sync.Mutex
	configured bool
	models     map[int]civitai.Model
	failures   map[int]error
	installErr error
	downloads  []int
	installed  []string
	gate       chan struct{} // when set, Download waits for it
	entered    chan int
}

func (f *fakeCivitai) Configured() bool { return f.configured }

func (f *fakeCivitai) Download(ctx context.Context, id int, dir string) (civitai.Model, error) {
	if f.entered != nil {
		f.entered <- id
	}
	if f.gate != nil {
		select {
		case <-f.gate:
		case <-ctx.Done():
			return civitai.Model{}, ctx.Err()
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.downloads = append(f.downloads, id)
	if err := f.failures[id]; err != nil {
		return civitai.Model{}, err
	}
	m, ok := f.models[id]
	if !ok {
		return civitai.Model{}, errors.New("CivitaiModelNotFound", "no such model", "model_id", id)
	}
	return m, nil
}

func (f *fakeCivitai) Install(m civitai.Model, comfy, sub string) (string, error) {
	if f.installErr != nil {
		return "", f.installErr
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	name := civitai.ModelName(m.ID, sub)
	f.installed = append(f.installed, name)
	return name, nil
}

func fox(id int, name string, skipped bool, words ...string) civitai.Model {
	return civitai.Model{ID: id, Name: name, TriggerWords: words, Skipped: skipped}
}

type civitaiEnv struct {
	*env
	fake   *fakeCivitai
	log    *events.Log
	sleeps []time.Duration
	comfy  string
}

func newCivitaiEnv(t *testing.T, mutate ...func(*Deps)) *civitaiEnv {
	t.Helper()
	fake := &fakeCivitai{configured: true, models: map[int]civitai.Model{
		7: fox(7, "Dancing Fox", false, "dancing fox", "sparkles"), 8: fox(8, "Sleepy Owl", true, "owl"), 9: fox(9, "Third", false),
	}, failures: map[int]error{}}
	ce := &civitaiEnv{fake: fake, log: events.NewLog(100, nil), comfy: t.TempDir()}
	check(t, os.MkdirAll(filepath.Join(ce.comfy, "models", "loras"), 0o750))
	var mu sync.Mutex
	ce.env = newEnv(t, func(d *Deps) {
		d.Civitai, d.Events, d.Pause = fake, ce.log, 15*time.Second
		d.Dirs.CivitaiCache = filepath.Join(t.TempDir(), "civitai")
		d.Sleep = func(ctx context.Context, dur time.Duration) error {
			mu.Lock()
			ce.sleeps = append(ce.sleeps, dur)
			mu.Unlock()
			return ctx.Err()
		}
		for _, m := range mutate {
			m(d)
		}
	})
	return ce
}

func (c *civitaiEnv) imp(models ...int) CivitaiImport {
	in := CivitaiImport{Group: domain.Group{Name: "Foxes", CodeName: "foxes"}, ComfyPath: c.comfy, Subfolder: "civitai"}
	for _, id := range models {
		in.Models = append(in.Models, CivitaiModel{ModelID: id, StrengthModel: 0.8, StrengthClip: 0.6})
	}
	return in
}

func (c *civitaiEnv) messages() []string {
	list, _, _ := c.log.After(0)
	out := make([]string, len(list))
	for i, e := range list {
		out[i] = string(e.Type) + ": " + e.Message
	}
	return out
}

func TestImportGroup(t *testing.T) {
	c := newCivitaiEnv(t)
	g, err := c.Civitai.ImportGroup(ctx(t), c.imp(7, 8, 9))
	check(t, err)
	if g.CodeName != "foxes" || !g.UseLora || g.ID == 0 {
		t.Errorf("group = %+v", g)
	}
	c.Civitai.Wait()

	items, err := c.Items.ListByGroup(ctx(t), g.ID, domain.Page{})
	check(t, err)
	if items.Total != 3 {
		t.Fatalf("items = %+v", items.Items)
	}
	first := items.Items[0]
	if first.CodeName != "7" || first.Name != "Dancing Fox" || first.PositivePrompt != "dancing fox\n\nsparkles" {
		t.Errorf("first item = %+v", first)
	}
	if len(first.LoRAs) != 1 || first.LoRAs[0].Name != "civitai/7.safetensors" || first.LoRAs[0].StrengthModel != 0.8 || first.LoRAs[0].StrengthClip != 0.6 {
		t.Errorf("loras = %+v", first.LoRAs)
	}
	if items.Items[2].PositivePrompt != "" {
		t.Errorf("a model without trigger words has no prompt: %q", items.Items[2].PositivePrompt)
	}

	// A pause before the second model; none before the third, because the
	// second was already in the cache.
	if len(c.sleeps) != 1 || c.sleeps[0] != 15*time.Second {
		t.Errorf("sleeps = %v", c.sleeps)
	}
	want := []string{
		"import_progress: Imported Dancing Fox (1/3).", "import_progress: Imported Sleepy Owl (2/3).",
		"import_progress: Imported Third (3/3).", "import_finished: 3 of 3 models imported.",
	}
	if got := c.messages(); strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("events = %v", got)
	}
	list, _, _ := c.log.After(0)
	if list[0].GroupID != g.ID {
		t.Errorf("the events must name the group: %+v", list[0])
	}

	// The imported items are usable in a command, numeric code names included.
	c.server(t, "gpu1")
	c.generator(t, "anime")
	p := c.project(t, "p")
	cmd, err := c.Commands.Add(ctx(t), p.ID, CommandInput{Name: "x", Code: "gpu1 -$ anime: foxes(7)"})
	check(t, err)
	jobs := c.jobsOf(t, cmd.ID)
	if len(jobs) != 1 || jobs[0].LoRAs[0].Name != "civitai/7.safetensors" {
		t.Errorf("jobs = %+v", jobs)
	}
}

func TestImportGroupFailsOneModelAndContinues(t *testing.T) {
	c := newCivitaiEnv(t)
	c.fake.failures[8] = errors.New("CivitaiDownloadFailed", "the file could not be downloaded", "model_id", 8)
	g, err := c.Civitai.ImportGroup(ctx(t), c.imp(7, 8, 9))
	check(t, err)
	c.Civitai.Wait()
	items, _ := c.Items.ListByGroup(ctx(t), g.ID, domain.Page{})
	if items.Total != 2 || items.Items[0].CodeName != "7" || items.Items[1].CodeName != "9" {
		t.Errorf("items = %+v", items.Items)
	}
	got := strings.Join(c.messages(), "|")
	for _, want := range []string{"import_error: Model 8: the file could not be downloaded", "import_finished: 2 of 3 models imported. 1 failed."} {
		if !strings.Contains(got, want) {
			t.Errorf("events lack %q: %s", want, got)
		}
	}
	if !strings.Contains(c.logs.String(), "CivitaiDownloadFailed") {
		t.Error("the failure was not logged with its tag")
	}
}

func TestImportInstallFailureAndDuplicateItem(t *testing.T) {
	c := newCivitaiEnv(t)
	c.fake.installErr = errors.New("FileStoreFailure", "disk full")
	g, err := c.Civitai.ImportGroup(ctx(t), c.imp(7))
	check(t, err)
	c.Civitai.Wait()
	if items, _ := c.Items.ListByGroup(ctx(t), g.ID, domain.Page{}); items.Total != 0 {
		t.Errorf("a failed install made an item: %+v", items.Items)
	}
	if got := strings.Join(c.messages(), "|"); !strings.Contains(got, "disk full") {
		t.Errorf("events = %s", got)
	}

	// Importing a model that is already in the group says so.
	c.fake.installErr = nil
	_, err = c.Civitai.AddItem(ctx(t), g.ID, CivitaiModel{ModelID: 7, StrengthModel: 1, StrengthClip: 1}, c.comfy, "civitai")
	check(t, err)
	c.Civitai.Wait()
	_, err = c.Civitai.AddItem(ctx(t), g.ID, CivitaiModel{ModelID: 7, StrengthModel: 1, StrengthClip: 1}, c.comfy, "civitai")
	check(t, err)
	c.Civitai.Wait()
	if got := strings.Join(c.messages(), "|"); !strings.Contains(got, "Model 7: an item with this code name already exists") {
		t.Errorf("events = %s", got)
	}
}

func TestImportRefusals(t *testing.T) {
	c := newCivitaiEnv(t)
	good := c.imp(7)
	with := func(f func(*CivitaiImport)) CivitaiImport {
		in := good
		in.Models = append([]CivitaiModel(nil), good.Models...)
		f(&in)
		return in
	}
	many := make([]CivitaiModel, maxImportModels+1)
	for i := range many {
		many[i] = CivitaiModel{ModelID: i + 1}
	}
	tests := []struct {
		name string
		in   CivitaiImport
		tag  string
	}{
		{"no models", with(func(i *CivitaiImport) { i.Models = nil }), "InvalidRequest"},
		{"too many", with(func(i *CivitaiImport) { i.Models = many }), "InvalidRequest"},
		{"bad id", with(func(i *CivitaiImport) { i.Models[0].ModelID = 0 }), "InvalidRequest"},
		{"bad subfolder", with(func(i *CivitaiImport) { i.Subfolder = "../escape" }), "InvalidRequest"},
		{"no ComfyUI path", with(func(i *CivitaiImport) { i.ComfyPath = "" }), "InvalidRequest"},
		{"ComfyUI path without loras", with(func(i *CivitaiImport) { i.ComfyPath = t.TempDir() }), "InvalidRequest"},
		{"bad group name", with(func(i *CivitaiImport) { i.Group.CodeName = "bad name" }), "CodeNameInvalid"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := c.Civitai.ImportGroup(ctx(t), tc.in)
			appErr, ok := errors.FromError(err)
			if !ok || appErr.Tag != tc.tag {
				t.Fatalf("err = %v, want %s", err, tc.tag)
			}
			if groups, _ := c.Groups.List(ctx(t), "", domain.Page{}); groups.Total != 0 {
				t.Errorf("a refused import created a group: %+v", groups.Items)
			}
		})
	}
	if len(c.fake.downloads) != 0 {
		t.Errorf("a refused import downloaded %v", c.fake.downloads)
	}

	// A taken group name.
	c.group(t, "foxes")
	_, err := c.Civitai.ImportGroup(ctx(t), good)
	wantRoute(t, err, "GroupService.Create", "GroupCodeNameTaken")
	if len(c.fake.downloads) != 0 {
		t.Error("a refused import downloaded something")
	}

	_, err = c.Civitai.AddItem(ctx(t), 999, CivitaiModel{ModelID: 7}, c.comfy, "x")
	wantRoute(t, err, "GroupService.Get", "GroupNotFound")
}

func TestImportNeedsConfiguration(t *testing.T) {
	for name, mutate := range map[string]func(*Deps){
		"no adapter":   func(d *Deps) { d.Civitai = nil },
		"no token":     func(d *Deps) { d.Civitai = &fakeCivitai{configured: false} },
		"no cache dir": func(d *Deps) { d.Dirs.CivitaiCache = "" },
	} {
		t.Run(name, func(t *testing.T) {
			c := newCivitaiEnv(t, mutate)
			_, err := c.Civitai.ImportGroup(ctx(t), c.imp(7))
			wantRoute(t, err, "CivitaiService.check", "CivitaiNotConfigured")
			_, err = c.Civitai.AddItem(ctx(t), 1, CivitaiModel{ModelID: 7}, c.comfy, "x")
			wantRoute(t, err, "CivitaiService.check", "CivitaiNotConfigured")
			if groups, _ := c.Groups.List(ctx(t), "", domain.Page{}); groups.Total != 0 {
				t.Error("a refused import created a group")
			}
		})
	}
}

func TestImportsRunOneAtATime(t *testing.T) {
	c := newCivitaiEnv(t)
	c.fake.gate = make(chan struct{})
	c.fake.entered = make(chan int, 10)
	_, err := c.Civitai.ImportGroup(ctx(t), c.imp(7))
	check(t, err)
	second := c.imp(9)
	second.Group = domain.Group{Name: "Other", CodeName: "other"}
	_, err = c.Civitai.ImportGroup(ctx(t), second)
	check(t, err)

	if got := <-c.fake.entered; got != 7 && got != 9 {
		t.Fatalf("first download = %d", got)
	}
	select {
	case id := <-c.fake.entered:
		t.Fatalf("model %d started while the first import was still running", id)
	case <-time.After(150 * time.Millisecond):
	}
	close(c.fake.gate)
	c.Civitai.Wait()
	if len(c.fake.downloads) != 2 {
		t.Errorf("downloads = %v", c.fake.downloads)
	}
}

func TestImportStopsWhenTheProgramDoes(t *testing.T) {
	bg, cancel := context.WithCancel(t.Context())
	c := newCivitaiEnv(t, func(d *Deps) { d.Background = bg })
	c.fake.gate = make(chan struct{})
	c.fake.entered = make(chan int, 10)
	g, err := c.Civitai.ImportGroup(ctx(t), c.imp(7, 9))
	check(t, err)
	<-c.fake.entered
	cancel()
	done := make(chan struct{})
	go func() { c.Civitai.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the import did not stop")
	}
	if items, _ := c.Items.ListByGroup(ctx(t), g.ID, domain.Page{}); items.Total != 0 {
		t.Errorf("items = %+v", items.Items)
	}
	for _, m := range c.messages() {
		if strings.HasPrefix(m, "import_finished") || strings.HasPrefix(m, "import_error") {
			t.Errorf("a stopped import reported %q", m)
		}
	}
}

func TestImportAcceptsAnyNumericCodeName(t *testing.T) {
	// The model id is the code name (SPEC §9); the command language must take it.
	for _, id := range []int{7, 1234567, 100} {
		if msg := commandNameProblemOf(strconv.Itoa(id)); msg != "" {
			t.Errorf("%d: %s", id, msg)
		}
	}
}

func commandNameProblemOf(s string) string { return commandNameProblem(s) }
