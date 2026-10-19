package workflow

import (
	"reflect"
	"testing"

	"github.com/rm4n0s/bos/internal/domain"
)

// klein reads the FLUX.2 Klein 9B workflow: split loaders, a text encoder with
// a zeroed negative, and sampling done by SamplerCustomAdvanced, whose seed,
// steps, cfg and sampler name live in the nodes it reads from.
func klein(t *testing.T) Graph {
	t.Helper()
	return example(t, "creators/flux2_klein_9b.json")
}

func inputOf(t *testing.T, g Graph, id, input string) string {
	t.Helper()
	return string(node(t, g, id).Inputs[input])
}

// The negative of Klein is a ConditioningZeroOut, which has no text: naming it
// as the negative prompt node fails, naming only the positive node works.
func TestKleinPrompts(t *testing.T) {
	g := klein(t)
	wantTag(t, SetPrompts(g, "Positive Prompt", "a fox", "Negative (zeroed)", "blurry"), "SetPrompts", "InjectorFailed")

	// With no negative prompt there is nothing to lose: a generator saved with
	// the zeroed node as its negative node still runs.
	g = klein(t)
	check(t, SetPrompts(g, "Positive Prompt", "a fox", "Negative (zeroed)", ""))
	if got := inputOf(t, g, "4", "text"); got != `"a fox"` {
		t.Errorf("text = %s", got)
	}

	g = klein(t)
	check(t, SetPrompts(g, "Positive Prompt", "a fox", "", ""))
	if got := inputOf(t, g, "4", "text"); got != `"a fox"` {
		t.Errorf("text = %s", got)
	}
}

// The analysis does not offer the zeroed negative as a prompt node.
func TestAnalyzeKlein(t *testing.T) {
	a, err := Analyze(klein(t))
	check(t, err)
	if a.PositiveTitle != "Positive Prompt" || a.NegativeTitle != "" {
		t.Errorf("suggested %q / %q", a.PositiveTitle, a.NegativeTitle)
	}
	want := domain.OutputAttributes{
		"file_type": {Kind: domain.AttrText, Text: "png"},
		"width":     intAttr(1024), "height": intAttr(1024), "batch_size": intAttr(1),
		"steps":        intAttr(4),
		"cfg":          {Kind: domain.AttrDecimal, Float: 1},
		"sampler_name": {Kind: domain.AttrText, Text: "euler"},
		"seed":         intAttr(123456789),
	}
	if !reflect.DeepEqual(a.Attributes, want) {
		t.Errorf("attributes = %+v\nwant         %+v", a.Attributes, want)
	}
}

func TestApplyAttributesToKlein(t *testing.T) {
	g := klein(t)
	check(t, ApplyAttributes(g, attrs("steps", 8, "cfg", 2.5, "sampler_name", "dpmpp_2m", "width", 768, "height", 512, "batch_size", 2, "scheduler", "simple", "denoise", 0.5)))
	for _, c := range []struct{ id, input, want string }{
		{"7", "steps", "8"}, {"10", "cfg", "2.5"}, {"8", "sampler_name", `"dpmpp_2m"`},
		{"6", "width", "768"}, {"6", "height", "512"}, {"6", "batch_size", "2"},
		// The scheduler takes the size too: its shift depends on it.
		{"7", "width", "768"}, {"7", "height", "512"},
	} {
		if got := inputOf(t, g, c.id, c.input); got != c.want {
			t.Errorf("node %s %s = %s, want %s", c.id, c.input, got, c.want)
		}
	}
	// Flux2Scheduler has no scheduler or denoise input: nothing is added.
	for _, in := range []string{"scheduler", "denoise"} {
		if _, ok := node(t, g, "7").Inputs[in]; ok {
			t.Errorf("%s was added to the Flux2Scheduler", in)
		}
	}
}

func TestSetSeedOfKlein(t *testing.T) {
	g := klein(t)
	check(t, SetSeed(g, 18446744073709551615))
	if got := inputOf(t, g, "9", "noise_seed"); got != "18446744073709551615" {
		t.Errorf("noise_seed = %s", got)
	}
	check(t, ApplyAttributes(g, attrs("seed", 42)))
	if got := inputOf(t, g, "9", "noise_seed"); got != "42" {
		t.Errorf("noise_seed = %s", got)
	}
}

// A BasicScheduler carries the scheduler and denoise of a custom sampler.
func TestApplyAttributesToABasicScheduler(t *testing.T) {
	g := mustParse(t, `{
	  "1":{"class_type":"RandomNoise","inputs":{"noise_seed":1}},
	  "2":{"class_type":"BasicGuider","inputs":{}},
	  "3":{"class_type":"KSamplerSelect","inputs":{"sampler_name":"euler"}},
	  "4":{"class_type":"BasicScheduler","inputs":{"scheduler":"simple","steps":20,"denoise":1}},
	  "5":{"class_type":"SamplerCustomAdvanced","inputs":{"noise":["1",0],"guider":["2",0],"sampler":["3",0],"sigmas":["4",0]}}}`)
	check(t, ApplyAttributes(g, attrs("steps", 30, "scheduler", "beta", "denoise", 0.6, "cfg", 3)))
	for _, c := range []struct{ input, want string }{{"steps", "30"}, {"scheduler", `"beta"`}, {"denoise", "0.6"}} {
		if got := inputOf(t, g, "4", c.input); got != c.want {
			t.Errorf("%s = %s, want %s", c.input, got, c.want)
		}
	}
	// A BasicGuider has no cfg: nothing is added.
	if _, ok := node(t, g, "2").Inputs["cfg"]; ok {
		t.Error("cfg was added to the BasicGuider")
	}
}

// A LoRA goes to the UNET loader of the split workflow, and the guider reads it.
func TestInjectLoRAIntoKlein(t *testing.T) {
	g := klein(t)
	check(t, InjectLoRAs(g, []LoRA{{Name: "style", StrengthModel: 0.8}}))
	var lora Node
	for _, n := range g.Nodes() {
		if n.ClassType == "LoraLoaderModelOnly" {
			lora = n
		}
	}
	if lora.ID == "" {
		t.Fatal("no LoRA loader was added")
	}
	if l, ok := lora.Link("model"); !ok || l.Node != "1" {
		t.Errorf("the LoRA reads %v, want the UNET loader", l)
	}
	if l, ok := node(t, g, "10").Link("model"); !ok || l.Node != lora.ID {
		t.Errorf("the guider reads %v, want the LoRA loader %s", l, lora.ID)
	}
}
