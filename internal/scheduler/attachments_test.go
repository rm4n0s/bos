package scheduler

import (
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
