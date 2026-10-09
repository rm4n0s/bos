package service

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/rm4n0s/bos/internal/domain"
)

func (e *env) loraItem(t *testing.T, group domain.ID, code, positive, negative string, loras ...domain.LoRA) {
	t.Helper()
	_, err := e.Items.Create(ctx(t), group, domain.Item{CodeName: code, Name: code, PositivePrompt: positive, NegativePrompt: negative, LoRAs: loras})
	check(t, err)
}

func TestReplRunBuildsAndQueuesTheJob(t *testing.T) {
	e := newEnv(t)
	w := e.world(t)
	e.loraItem(t, w.poses.ID, "lora_pose", "dancing", "blurry", domain.LoRA{Name: "dance.safetensors", StrengthModel: 0.8, StrengthClip: 0.7})

	job, err := e.Repl.Run(ctx(t), ReplInput{
		Server: "gpu2", Generator: "anime", Positive: "at night", Negative: "ugly",
		Items: "characters(a),poses(lora_pose)",
		LoRAs: []domain.LoRA{{Name: "extra.safetensors", StrengthModel: 1, StrengthClip: 1}},
	})
	check(t, err)

	if job.ProjectID != domain.ReplID || job.CommandID != domain.ReplID || job.Status != domain.StatusQueued {
		t.Errorf("job = project %d, command %d, status %s", job.ProjectID, job.CommandID, job.Status)
	}
	if job.PositivePrompt != "alice dancing at night" || job.NegativePrompt != "blurry ugly" {
		t.Errorf("prompts = %q / %q", job.PositivePrompt, job.NegativePrompt)
	}
	wantLoRAs := []domain.LoRA{{Name: "dance.safetensors", StrengthModel: 0.8, StrengthClip: 0.7}, {Name: "extra.safetensors", StrengthModel: 1, StrengthClip: 1}}
	if !reflect.DeepEqual(job.LoRAs, wantLoRAs) {
		t.Errorf("loras = %+v", job.LoRAs)
	}
	if !job.Pinned || job.PreferredServerID == nil || *job.PreferredServerID != w.gpu2.ID {
		t.Errorf("the job must wait for gpu2: pinned = %v, preferred = %v", job.Pinned, job.PreferredServerID)
	}
	if len(job.Items) != 2 || job.Items[0].ItemCode != "a" || job.Items[1].GroupCode != "poses" {
		t.Errorf("items = %+v", job.Items)
	}
	if job.ResultPath != filepath.Join(e.dirs.Results, "repl", "repl.png") || job.GeneratorCodeName != "anime" {
		t.Errorf("result = %s, generator = %s", job.ResultPath, job.GeneratorCodeName)
	}
	if e.wakes.Load() != 1 {
		t.Errorf("wakes = %d", e.wakes.Load())
	}
	if last, err := e.Repl.Last(ctx(t)); err != nil || last.ID != job.ID {
		t.Errorf("last = %+v, %v", last, err)
	}
}

func TestReplRunReplacesThePreviousJob(t *testing.T) {
	e := newEnv(t)
	e.world(t)
	first, err := e.Repl.Run(ctx(t), ReplInput{Server: "gpu1", Generator: "anime", Positive: "one"})
	check(t, err)
	result := e.write(t, "results/repl/repl.png", "old image")
	e.setJob(t, first.ID, func(j *domain.Job) { j.Status = domain.StatusFinished })

	second, err := e.Repl.Run(ctx(t), ReplInput{Server: "gpu1", Generator: "anime", Positive: "two"})
	check(t, err)
	if exists(result) {
		t.Error("the previous result was kept")
	}
	if _, err := e.repos.Jobs().Get(ctx(t), first.ID); err == nil {
		t.Error("the previous REPL job was kept")
	}
	if last, err := e.Repl.Last(ctx(t)); err != nil || last.ID != second.ID || last.PositivePrompt != "two" {
		t.Errorf("last = %+v, %v", last, err)
	}
}

func TestReplFailures(t *testing.T) {
	e := newEnv(t)
	e.world(t)
	ok := ReplInput{Server: "gpu1", Generator: "anime"}
	with := func(f func(*ReplInput)) ReplInput { in := ok; f(&in); return in }

	tests := []struct {
		name  string
		in    ReplInput
		entry string
		tag   string
	}{
		{"items badly written", with(func(i *ReplInput) { i.Items = "characters(a) poses(p1)" }), "ReplService.Run", "ReplInvalid"},
		{"items with a trailing comma", with(func(i *ReplInput) { i.Items = "characters(a)," }), "ReplService.Run", "ReplInvalid"},
		{"lora without a name", with(func(i *ReplInput) { i.LoRAs = []domain.LoRA{{StrengthModel: 1}} }), "ReplService.Run", "ReplInvalid"},
		{"unknown server", with(func(i *ReplInput) { i.Server = "nope" }), "ReplService.Run", "ServerNotFound"},
		{"unknown generator", with(func(i *ReplInput) { i.Generator = "nope" }), "ReplService.Run", "GeneratorNotFound"},
		{"unknown group", with(func(i *ReplInput) { i.Items = "nope(a)" }), "ReplService.Run", "GroupNotFound"},
		{"unknown item", with(func(i *ReplInput) { i.Items = "characters(nope)" }), "ReplService.Run", "ItemNotFound"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := e.Repl.Run(ctx(t), tc.in)
			wantRoute(t, err, tc.entry, tc.tag)
		})
	}
	if _, err := e.Repl.Last(ctx(t)); err == nil {
		t.Error("a failed run left a REPL job")
	}
}

func TestReplLastWithoutAJob(t *testing.T) {
	e := newEnv(t)
	_, err := e.Repl.Last(ctx(t))
	wantRoute(t, err, "ReplService.Last", "JobNotFound")
}

func TestReplRunRefusesWhileTheJobRuns(t *testing.T) {
	e := newEnv(t)
	e.world(t)
	first, err := e.Repl.Run(ctx(t), ReplInput{Server: "gpu1", Generator: "anime"})
	check(t, err)
	e.setJob(t, first.ID, func(j *domain.Job) { j.Status = domain.StatusProcessing })
	_, err = e.Repl.Run(ctx(t), ReplInput{Server: "gpu1", Generator: "anime"})
	wantRoute(t, err, "ReplService.Run", "JobBusy")
}
