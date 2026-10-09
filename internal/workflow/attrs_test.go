package workflow

import (
	"strings"
	"testing"
)

func TestApplyAttributes(t *testing.T) {
	g := example(t, "creators/illustrious_xl.json")
	check(t, ApplyAttributes(g, attrs("steps", 30, "cfg", 6.5, "sampler_name", "dpmpp_2m", "scheduler", "karras",
		"denoise", 0.75, "seed", 7, "width", 640, "height", 896, "batch_size", 2, "file_type", "png", "fps", 24)))
	sampler := node(t, g, "3")
	for input, want := range map[string]string{
		"steps": "30", "cfg": "6.5", "sampler_name": `"dpmpp_2m"`, "scheduler": `"karras"`, "denoise": "0.75", "seed": "7",
	} {
		if got := string(sampler.Inputs[input]); got != want {
			t.Errorf("sampler %s = %s, want %s", input, got, want)
		}
	}
	latent := node(t, g, "5")
	for input, want := range map[string]string{"width": "640", "height": "896", "batch_size": "2"} {
		if got := string(latent.Inputs[input]); got != want {
			t.Errorf("latent %s = %s, want %s", input, got, want)
		}
	}
	// Untouched: links, other inputs, and attributes with no node to receive them.
	if l := link(t, g, "3", "model"); l != (Link{"4", 0}) {
		t.Errorf("model link = %v", l)
	}
	assertWellFormed(t, g)
}

func TestApplyOnlyWhatIsGiven(t *testing.T) {
	g := example(t, "creators/illustrious_xl.json")
	check(t, ApplyAttributes(g, attrs("steps", 12)))
	if got := string(node(t, g, "3").Inputs["steps"]); got != "12" {
		t.Errorf("steps = %s", got)
	}
	if got := string(node(t, g, "3").Inputs["cfg"]); got != "6" {
		t.Errorf("cfg changed to %s", got)
	}
	if got := string(node(t, g, "5").Inputs["width"]); got != "1024" {
		t.Errorf("width changed to %s", got)
	}
	check(t, ApplyAttributes(g, nil))
}

func TestApplyAttributesToAdvancedSampler(t *testing.T) {
	g := mustParse(t, `{"1":{"class_type":"KSamplerAdvanced","inputs":{"noise_seed":1,"steps":20,"cfg":7,"sampler_name":"euler","scheduler":"normal"}}}`)
	check(t, ApplyAttributes(g, attrs("seed", 77, "steps", 10, "denoise", 0.5)))
	n := node(t, g, "1")
	if string(n.Inputs["noise_seed"]) != "77" || string(n.Inputs["steps"]) != "10" {
		t.Errorf("inputs = %v", n.Inputs)
	}
	for _, bad := range []string{"seed", "denoise"} {
		if _, ok := n.Inputs[bad]; ok {
			t.Errorf("KSamplerAdvanced got a %s input", bad)
		}
	}
}

func TestApplyAttributesToVideo(t *testing.T) {
	g := mustParse(t, `{
	  "1":{"class_type":"EmptyWanLatentVideo","inputs":{"width":832,"height":480,"length":81,"batch_size":1}},
	  "2":{"class_type":"EmptyHunyuanLatentVideo","inputs":{"width":640,"height":360,"length":33}},
	  "3":{"class_type":"VHS_VideoCombine","inputs":{"frame_rate":16}}}`)
	check(t, ApplyAttributes(g, attrs("width", 512, "height", 288, "num_frames", 49, "fps", 24)))
	for _, id := range []string{"1", "2"} {
		n := node(t, g, id)
		if string(n.Inputs["width"]) != "512" || string(n.Inputs["height"]) != "288" || string(n.Inputs["length"]) != "49" {
			t.Errorf("node %s = %v", id, n.Inputs)
		}
	}
	if got := string(node(t, g, "3").Inputs["frame_rate"]); got != "24" {
		t.Errorf("frame_rate = %s", got)
	}
	// The frame count follows from the duration when it is not given.
	g2 := mustParse(t, `{"1":{"class_type":"EmptyWanLatentVideo","inputs":{"width":1,"height":1,"length":81}}}`)
	check(t, ApplyAttributes(g2, attrs("duration_seconds", 2.5, "fps", 16)))
	if got := string(node(t, g2, "1").Inputs["length"]); got != "40" {
		t.Errorf("length = %s, want 40", got)
	}
}

func TestApplyAttributesWithoutSampler(t *testing.T) {
	g := example(t, "fixers/rembg.json")
	wantTag(t, ApplyAttributes(g, attrs("steps", 10)), "ApplyAttributes", "NoSamplerInWorkflow")
	check(t, ApplyAttributes(g, attrs("width", 10, "file_type", "png"))) // nothing needs a sampler
}

// Acceptance test 6 depends on this: a seed is written to every sampler.
func TestSetSeed(t *testing.T) {
	g := mustParse(t, `{
	  "1":{"class_type":"KSampler","inputs":{"seed":1,"steps":20}},
	  "2":{"class_type":"KSamplerAdvanced","inputs":{"noise_seed":2,"steps":20}},
	  "3":{"class_type":"KSampler","inputs":{"seed":["9",0]}}}`)
	check(t, SetSeed(g, 18446744073709551615))
	for id, input := range map[string]string{"1": "seed", "2": "noise_seed", "3": "seed"} {
		if got := string(node(t, g, id).Inputs[input]); got != "18446744073709551615" {
			t.Errorf("node %s %s = %s, want the exact 64-bit value", id, input, got)
		}
	}
	if !strings.Contains(string(g["1"]), "18446744073709551615") {
		t.Errorf("raw node = %s", g["1"])
	}
	if string(node(t, g, "1").Inputs["steps"]) != "20" {
		t.Error("another input changed")
	}
	wantTag(t, SetSeed(example(t, "fixers/rembg.json"), 1), "SetSeed", "NoSamplerInWorkflow")
}

func TestSeedsDifferBetweenJobs(t *testing.T) {
	a, b := example(t, "creators/illustrious_xl.json"), example(t, "creators/illustrious_xl.json")
	check(t, SetSeed(a, 111))
	check(t, SetSeed(b, 222))
	if string(node(t, a, "3").Inputs["seed"]) == string(node(t, b, "3").Inputs["seed"]) {
		t.Error("different seeds gave the same graph")
	}
}
