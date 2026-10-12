package scheduler

import (
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/rm4n0s/bos/internal/domain"
)

func (r *rig) file(name, content string) string {
	r.t.Helper()
	p := filepath.Join(r.root, name)
	check(r.t, os.MkdirAll(filepath.Dir(p), 0o750))
	check(r.t, os.WriteFile(p, []byte(content), 0o600))
	return p
}

// Everything a workflow reads reaches the server before the prompt does
// (SPEC §8.7), and the injectors run (§8.2).
func TestAttachmentsAreUploadedAndInjected(t *testing.T) {
	r := newRig(t)
	_, gpu1 := r.server("gpu1", 1)
	w := r.world(false)
	cn := r.file("controlnet/pose.png", "pose bytes")
	ip := r.file("ipadapter/face.png", "face bytes")
	red := r.file("regions/mask_red.png", "red mask")
	blue := r.file("regions/mask_blue.png", "blue mask")

	_, err := r.svc.Items.Create(ctx(t), w.group.ID, domain.Item{
		CodeName: "styled", PositivePrompt: "styled",
		LoRAs:       []domain.LoRA{{Name: "detail", StrengthModel: 0.8, StrengthClip: 0.6}},
		ControlNets: []domain.ControlNet{{Type: "canny", ImagePath: cn, IsReference: true, ModelPattern: "canny.safetensors", Strength: 0.9}},
		IPAdapter:   &domain.IPAdapter{ImageFile: ip, Weight: 0.7},
	})
	check(t, err)
	duo, err := r.svc.Groups.Create(ctx(t), domain.Group{Name: "duo", CodeName: "duo"})
	check(t, err)
	_, err = r.svc.Items.Create(ctx(t), duo.ID, domain.Item{CodeName: "scene", MaskRegion: &domain.MaskRegion{MaskFiles: map[string]string{"red": red, "blue": blue}}})
	check(t, err)

	cmd := r.command(w, "gpu1 -$ anime: duo{red: chars(a), blue: chars(b)} * chars(styled)")
	r.start()
	r.run(cmd)
	r.waitStatus(cmd, domain.StatusFinished)

	for _, name := range []string{"pose.png", "face.png", "mask_red.png", "mask_blue.png"} {
		if !slices.Contains(gpu1.uploads, name) {
			t.Errorf("%s was not uploaded: %v", name, gpu1.uploads)
		}
	}
	if got := string(gpu1.uploaded["pose.png"]); got != "pose bytes" {
		t.Errorf("uploaded content = %q", got)
	}

	g := gpu1.graphs()[0]
	classes := map[string]int{}
	loadImages := []string{}
	for _, n := range g.Nodes() {
		classes[n.ClassType]++
		if n.ClassType == "LoadImage" {
			loadImages = append(loadImages, strings.Trim(string(n.Inputs["image"]), `"`))
		}
	}
	for _, class := range []string{"LoraLoader", "ControlNetApplyAdvanced", "CannyEdgePreprocessor", "IPAdapterAdvanced", "ConditioningSetMask", "ConditioningCombine"} {
		if classes[class] == 0 {
			t.Errorf("the submitted workflow has no %s: %v", class, classes)
		}
	}
	slices.Sort(loadImages)
	want := []string{"gpu1/face.png", "gpu1/mask_blue.png", "gpu1/mask_red.png", "gpu1/pose.png"}
	if !slices.Equal(loadImages, want) {
		t.Errorf("LoadImage nodes read %v, want %v", loadImages, want)
	}
	// The LoRA and the job's own prompt are in place.
	if got := promptText(t, g); !strings.Contains(got, "styled") {
		t.Errorf("prompt = %q", got)
	}
}

// Every LoRA of every selected item is loaded before the prompt is queued: one
// loader for each file in the order of the command's terms, the first
// strengths for a file two items share, and the sampler and the text encoders
// read from the end of the chain.
func TestLoRAsOfAllSelectedItemsAreChainedInTheWorkflow(t *testing.T) {
	r := newRig(t)
	_, gpu1 := r.server("gpu1", 1)
	w := r.world(false)
	poses, err := r.svc.Groups.Create(ctx(t), domain.Group{Name: "poses", CodeName: "poses"})
	check(t, err)
	_, err = r.svc.Items.Create(ctx(t), w.group.ID, domain.Item{CodeName: "look", PositivePrompt: "look", LoRAs: []domain.LoRA{
		{Name: "chars/x", StrengthModel: 0.9, StrengthClip: 0.8}, {Name: "shared", StrengthModel: 0.7, StrengthClip: 0.6},
	}})
	check(t, err)
	_, err = r.svc.Items.Create(ctx(t), poses.ID, domain.Item{CodeName: "dance", PositivePrompt: "dance", LoRAs: []domain.LoRA{
		{Name: "shared.safetensors", StrengthModel: 0.1, StrengthClip: 0.1}, {Name: "y.safetensors", StrengthModel: 0.5, StrengthClip: 0.4},
	}})
	check(t, err)

	cmd := r.command(w, "gpu1 -$ anime: chars(look) * poses(dance)")
	r.start()
	r.run(cmd)
	r.waitStatus(cmd, domain.StatusFinished)

	g := gpu1.graphs()[0]
	// Walk from the sampler back to the checkpoint.
	samplers := 0
	var chain []string
	for _, n := range g.Nodes() {
		if n.ClassType != "KSampler" {
			continue
		}
		samplers++
		cur, ok := n.Link("model")
		for ok {
			node, err := g.Node(cur.Node)
			check(t, err)
			if node.ClassType != "LoraLoader" {
				break
			}
			chain = append([]string{strings.Trim(string(node.Inputs["lora_name"]), `"`) + " " + string(node.Inputs["strength_model"]) + "/" + string(node.Inputs["strength_clip"])}, chain...)
			cur, ok = node.Link("model")
		}
	}
	if samplers == 0 {
		t.Fatal("no sampler in the submitted workflow")
	}
	want := []string{"chars/x.safetensors 0.9/0.8", "shared.safetensors 0.7/0.6", "y.safetensors 0.5/0.4"}
	if !slices.Equal(chain, want) {
		t.Errorf("LoRA chain = %v, want %v", chain, want)
	}
	loaders := 0
	for _, n := range g.Nodes() {
		if n.ClassType == "LoraLoader" {
			loaders++
		}
	}
	if loaders != 3 {
		t.Errorf("%d LoRA loaders in the workflow, want 3", loaders)
	}
}

// At debug the log shows the LoRAs that are in the workflow, and the whole
// workflow, as an object, with their loader nodes.
func TestLogShowsTheLoRAsOfTheWorkflow(t *testing.T) {
	var logs syncBuffer
	logger := slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	r := newRig(t, func(d *Deps) { d.Logger = logger })
	r.server("gpu1", 1)
	w := r.world(false)
	_, err := r.svc.Items.Create(ctx(t), w.group.ID, domain.Item{CodeName: "look", PositivePrompt: "look", LoRAs: []domain.LoRA{{Name: "styles/fox", StrengthModel: 0.8, StrengthClip: 0.6}}})
	check(t, err)
	cmd := r.command(w, "gpu1 -$ anime: chars(look)")
	r.start()
	r.run(cmd)
	r.waitStatus(cmd, domain.StatusFinished)

	var lorasLogged, loaderLogged bool
	for _, line := range strings.Split(strings.TrimSpace(logs.String()), "\n") {
		var e struct {
			Msg      string
			Step     string
			Loras    []string
			Workflow map[string]struct {
				ClassType string                     `json:"class_type"`
				Inputs    map[string]json.RawMessage `json:"inputs"`
			}
		}
		check(t, json.Unmarshal([]byte(line), &e))
		switch {
		case e.Msg == "workflow sent to ComfyUI" && e.Step == "generation":
			lorasLogged = slices.Equal(e.Loras, []string{"styles/fox.safetensors 0.8/0.6"})
			for _, n := range e.Workflow {
				if n.ClassType == "LoraLoader" && string(n.Inputs["lora_name"]) == `"styles/fox.safetensors"` {
					loaderLogged = true
				}
			}
		}
	}
	if !lorasLogged || !loaderLogged {
		t.Errorf("loras attribute: %v; LoraLoader in the logged workflow: %v\n%s", lorasLogged, loaderLogged, logs.String())
	}
}
