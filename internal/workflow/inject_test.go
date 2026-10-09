package workflow

import (
	"strings"
	"testing"
)

var twoLoRAs = []LoRA{
	{Name: "style_a", StrengthModel: 0.8, StrengthClip: 0.6},
	{Name: "chars/b.safetensors", StrengthModel: 1, StrengthClip: 0.5},
}

// addedIDs lists the nodes of a class that are not in the original graph.
func addedIDs(orig, g Graph, class string) []string {
	var out []string
	for _, n := range g.Nodes() {
		if _, existed := orig[n.ID]; !existed && n.ClassType == class {
			out = append(out, n.ID)
		}
	}
	return out
}

// Acceptance test 7, checkpoint style.
func TestInjectLoRAsCheckpointStyle(t *testing.T) {
	g := example(t, "creators/illustrious_xl.json")
	orig := g.Clone()
	check(t, InjectLoRAs(g, twoLoRAs))
	assertWellFormed(t, g)

	ids := addedIDs(orig, g, "LoraLoader")
	if len(ids) != 2 || classes(g)["LoraLoaderModelOnly"] != 0 {
		t.Fatalf("added %v", ids)
	}
	l1, l2 := ids[0], ids[1]
	if n := node(t, g, l1); string(n.Inputs["lora_name"]) != `"style_a.safetensors"` || string(n.Inputs["strength_model"]) != "0.8" || string(n.Inputs["strength_clip"]) != "0.6" {
		t.Errorf("first LoRA = %v", n.Inputs)
	}
	if n := node(t, g, l2); string(n.Inputs["lora_name"]) != `"chars/b.safetensors"` {
		t.Errorf("second LoRA = %v", n.Inputs)
	}
	// The chain: checkpoint → l1 → l2; consumers of MODEL and CLIP read l2; the VAE stays.
	if link(t, g, l1, "model") != (Link{"4", 0}) || link(t, g, l1, "clip") != (Link{"4", 1}) {
		t.Error("the first LoRA is not attached to the checkpoint")
	}
	if link(t, g, l2, "model") != (Link{l1, 0}) || link(t, g, l2, "clip") != (Link{l1, 1}) {
		t.Error("the second LoRA does not follow the first")
	}
	if link(t, g, "3", "model") != (Link{l2, 0}) {
		t.Errorf("sampler model = %v", link(t, g, "3", "model"))
	}
	if link(t, g, "4a", "clip") != (Link{l2, 1}) {
		t.Errorf("CLIP skip clip = %v", link(t, g, "4a", "clip"))
	}
	if link(t, g, "8", "vae") != (Link{"4", 2}) {
		t.Errorf("the VAE was rewired: %v", link(t, g, "8", "vae"))
	}
}

// Acceptance test 7, split style.
func TestInjectLoRAsSplitStyle(t *testing.T) {
	g := example(t, "creators/anima_aio.json")
	orig := g.Clone()
	check(t, InjectLoRAs(g, twoLoRAs))
	assertWellFormed(t, g)

	ids := addedIDs(orig, g, "LoraLoaderModelOnly")
	if len(ids) != 2 || classes(g)["LoraLoader"] != 0 {
		t.Fatalf("added %v", ids)
	}
	l1, l2 := ids[0], ids[1]
	if n := node(t, g, l1); string(n.Inputs["strength_model"]) != "0.8" || n.Inputs["strength_clip"] != nil {
		t.Errorf("first LoRA = %v", n.Inputs)
	}
	if link(t, g, l1, "model") != (Link{"1", 0}) || link(t, g, l2, "model") != (Link{l1, 0}) {
		t.Error("the LoRAs are not chained on the UNET")
	}
	// The model-sampling patch reads the end of the chain; CLIP and VAE are untouched.
	if link(t, g, "4", "model") != (Link{l2, 0}) {
		t.Errorf("model sampling input = %v", link(t, g, "4", "model"))
	}
	if link(t, g, "5", "clip") != (Link{"2", 0}) {
		t.Error("the CLIP loader link changed")
	}
}

func TestInjectLoRAsKeepsOriginalWhenNone(t *testing.T) {
	g := example(t, "creators/illustrious_xl.json")
	before, _ := g.MarshalIndent()
	check(t, InjectLoRAs(g, nil))
	after, _ := g.MarshalIndent()
	if string(before) != string(after) {
		t.Error("an empty LoRA list changed the graph")
	}
}

func TestLoRAFileExtension(t *testing.T) {
	for in, want := range map[string]string{
		"style": "style.safetensors", "dir/style": "dir/style.safetensors", "a.safetensors": "a.safetensors",
		"a.pt": "a.pt", "v1.5/style": "v1.5/style.safetensors", "style.v2": "style.v2",
	} {
		if got := loraFile(in); got != want {
			t.Errorf("loraFile(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestInjectLoRAsFindsTheLoaderThroughPatches(t *testing.T) {
	// Two checkpoints; only the second feeds the sampler, through a patch node.
	g := mustParse(t, `{
	  "1":{"class_type":"CheckpointLoaderSimple","inputs":{"ckpt_name":"a"}},
	  "2":{"class_type":"CheckpointLoaderSimple","inputs":{"ckpt_name":"b"}},
	  "3":{"class_type":"ModelSamplingDiscrete","inputs":{"model":["2",0]}},
	  "4":{"class_type":"KSampler","inputs":{"model":["3",0],"positive":["5",0],"negative":["5",0]}},
	  "5":{"class_type":"CLIPTextEncode","inputs":{"clip":["2",1],"text":""}}}`)
	check(t, InjectLoRAs(g, []LoRA{{Name: "x", StrengthModel: 1, StrengthClip: 1}}))
	assertWellFormed(t, g)
	if link(t, g, "6", "model") != (Link{"2", 0}) || link(t, g, "3", "model") != (Link{"6", 0}) || link(t, g, "5", "clip") != (Link{"6", 1}) {
		t.Errorf("LoRA attached to the wrong checkpoint:\n%s", g["6"])
	}
	if link(t, g, "6", "model").Node != "2" {
		t.Error("attached to checkpoint 1")
	}
}

func TestInjectLoRAsFailures(t *testing.T) {
	one := []LoRA{{Name: "x"}}
	tests := []struct {
		name  string
		graph string
		tag   string
	}{
		{"no sampler", `{"1":{"class_type":"CheckpointLoaderSimple","inputs":{}}}`, "NoSamplerInWorkflow"},
		{"model not linked", `{"1":{"class_type":"KSampler","inputs":{"model":5}}}`, "InjectorFailed"},
		{"no loader upstream", `{"1":{"class_type":"KSampler","inputs":{"model":["2",0]}},"2":{"class_type":"Mystery","inputs":{}}}`, "InjectorFailed"},
		{"link to a missing node", `{"1":{"class_type":"KSampler","inputs":{"model":["9",0]}}}`, "InjectorFailed"},
		{"loop", `{"1":{"class_type":"KSampler","inputs":{"model":["2",0]}},"2":{"class_type":"P","inputs":{"model":["3",0]}},"3":{"class_type":"P","inputs":{"model":["2",0]}}}`, "InjectorFailed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wantTag(t, InjectLoRAs(mustParse(t, tt.graph), one), "InjectLoRAs", tt.tag)
		})
	}
}

func TestInjectIPAdapters(t *testing.T) {
	g := example(t, "creators/illustrious_xl.json")
	orig := g.Clone()
	check(t, InjectIPAdapters(g, []IPAdapter{
		{Image: "bos/a.png", Weight: 0.7, WeightType: "style transfer", StartAt: 0.1, EndAt: 0.9, ClipVisionModel: "cv.safetensors", ModelName: "ipa.safetensors"},
		{Image: "bos/b.png", Weight: 1},
	}))
	assertWellFormed(t, g)
	for class, want := range map[string]int{"CLIPVisionLoader": 2, "LoadImage": 2, "IPAdapterModelLoader": 2, "IPAdapterAdvanced": 2} {
		if n := len(addedIDs(orig, g, class)); n != want {
			t.Errorf("added %d %s, want %d", n, class, want)
		}
	}
	apply := addedIDs(orig, g, "IPAdapterAdvanced")
	a1, a2 := node(t, g, apply[0]), node(t, g, apply[1])
	if string(a1.Inputs["weight"]) != "0.7" || string(a1.Inputs["weight_type"]) != `"style transfer"` ||
		string(a1.Inputs["start_at"]) != "0.1" || string(a1.Inputs["end_at"]) != "0.9" {
		t.Errorf("first adapter = %v", a1.Inputs)
	}
	// The second adapter takes the defaults and a full window.
	if string(a2.Inputs["weight_type"]) != `"linear"` || string(a2.Inputs["end_at"]) != "1" {
		t.Errorf("second adapter = %v", a2.Inputs)
	}
	loaders := addedIDs(orig, g, "IPAdapterModelLoader")
	if string(node(t, g, loaders[0]).Inputs["ipadapter_file"]) != `"ipa.safetensors"` || string(node(t, g, loaders[1]).Inputs["ipadapter_file"]) != `"`+defaultIPAdapter+`"` {
		t.Error("IP-Adapter model names")
	}
	clips := addedIDs(orig, g, "CLIPVisionLoader")
	if string(node(t, g, clips[1]).Inputs["clip_name"]) != `"`+defaultClipVision+`"` {
		t.Error("default CLIP vision model missing")
	}
	// Chain: checkpoint → a1 → a2 → sampler.
	if link(t, g, apply[0], "model") != (Link{"4", 0}) || link(t, g, apply[1], "model") != (Link{apply[0], 0}) || link(t, g, "3", "model") != (Link{apply[1], 0}) {
		t.Error("IP-Adapter chain")
	}
	if link(t, g, apply[0], "ipadapter").Node != loaders[0] || link(t, g, apply[0], "clip_vision").Node != clips[0] {
		t.Error("an adapter reads the wrong loaders")
	}
	wantTag(t, InjectIPAdapters(example(t, "fixers/rembg.json"), []IPAdapter{{}}), "InjectIPAdapters", "NoSamplerInWorkflow")
	wantTag(t, InjectIPAdapters(mustParse(t, `{"1":{"class_type":"KSampler","inputs":{"model":1}}}`), []IPAdapter{{}}), "InjectIPAdapters", "InjectorFailed")
	check(t, InjectIPAdapters(g.Clone(), nil))
}

func TestInjectControlNets(t *testing.T) {
	g := example(t, "creators/illustrious_xl.json")
	orig := g.Clone()
	check(t, InjectControlNets(g, []ControlNet{
		{Type: "openpose", Image: "bos/pose.png", IsReference: true, ModelName: "cn_pose.safetensors", Strength: 0.8},
		{Type: "tile", Image: "bos/tile.png", IsReference: true, ModelName: "cn_tile.safetensors"},
		{Type: "canny", Image: "bos/canny.png", IsReference: false, ModelName: "cn_canny.safetensors", Strength: 1},
	}))
	assertWellFormed(t, g)

	want := map[string]int{"LoadImage": 3, "OpenposePreprocessor": 1, "PreviewImage": 1, "ControlNetLoader": 3, "ControlNetApplyAdvanced": 3}
	for class, n := range want {
		if got := len(addedIDs(orig, g, class)); got != n {
			t.Errorf("added %d %s, want %d", got, class, n)
		}
	}
	if len(addedIDs(orig, g, "CannyEdgePreprocessor")) != 0 {
		t.Error("a canny preprocessor was added for an image used as it is")
	}
	apply := addedIDs(orig, g, "ControlNetApplyAdvanced")
	a1, a2, a3 := apply[0], apply[1], apply[2]
	// positive/negative chain through the three, from the text encoders to the sampler.
	if link(t, g, a1, "positive") != (Link{"6", 0}) || link(t, g, a1, "negative") != (Link{"7", 0}) {
		t.Error("the first ControlNet does not read the text encoders")
	}
	if link(t, g, a2, "positive") != (Link{a1, 0}) || link(t, g, a2, "negative") != (Link{a1, 1}) ||
		link(t, g, a3, "positive") != (Link{a2, 0}) || link(t, g, a3, "negative") != (Link{a2, 1}) {
		t.Error("ControlNets are not chained")
	}
	if link(t, g, "3", "positive") != (Link{a3, 0}) || link(t, g, "3", "negative") != (Link{a3, 1}) {
		t.Error("the sampler does not read the last ControlNet")
	}
	if got := string(node(t, g, a1).Inputs["strength"]); got != "0.8" {
		t.Errorf("strength = %s", got)
	}
	if got := string(node(t, g, a2).Inputs["strength"]); got != "1" { // default for an unset strength
		t.Errorf("default strength = %s", got)
	}
	pre := addedIDs(orig, g, "OpenposePreprocessor")[0]
	if link(t, g, a1, "image").Node != pre {
		t.Error("the first ControlNet does not use the preprocessed image")
	}
	if string(node(t, g, pre).Inputs["detect_hand"]) != `"enable"` {
		t.Errorf("openpose defaults = %v", node(t, g, pre).Inputs)
	}
	if prev := addedIDs(orig, g, "PreviewImage")[0]; link(t, g, prev, "images").Node != pre {
		t.Error("the preview does not show the preprocessor output")
	}
	images := addedIDs(orig, g, "LoadImage")
	if link(t, g, a2, "image").Node != images[1] || link(t, g, a3, "image").Node != images[2] {
		t.Error("tile and unprocessed images must feed their ControlNet directly")
	}
}

func TestPreprocessorTable(t *testing.T) {
	for typ, class := range map[string]string{
		"openpose": "OpenposePreprocessor", "dwpose": "DWPreprocessor", "canny": "CannyEdgePreprocessor",
		"midas": "MidasDepthMapPreprocessor", "depth": "DepthAnythingPreprocessor", "lineart": "LineArtPreprocessor",
		"softedge": "HEDPreprocessor", "scribble": "ScribblePreprocessor", "normal": "BAE-NormalMapPreprocessor",
	} {
		g := example(t, "creators/illustrious_xl.json")
		orig := g.Clone()
		check(t, InjectControlNets(g, []ControlNet{{Type: typ, Image: "i.png", IsReference: true, ModelName: "m"}}))
		assertWellFormed(t, g)
		if len(addedIDs(orig, g, class)) != 1 {
			t.Errorf("%s: no %s added: %v", typ, class, classes(g))
		}
	}
	g := example(t, "creators/illustrious_xl.json")
	orig := g.Clone()
	check(t, InjectControlNets(g, []ControlNet{{Type: "tile", Image: "i.png", IsReference: true, ModelName: "m"}}))
	if len(g) != len(orig)+3 { // image, loader, apply
		t.Errorf("tile added %d nodes, want 3", len(g)-len(orig))
	}
}

func TestInjectControlNetsFailures(t *testing.T) {
	cn := []ControlNet{{Type: "canny"}}
	wantTag(t, InjectControlNets(example(t, "creators/illustrious_xl.json"), []ControlNet{{Type: "hologram"}}), "InjectControlNets", "InjectorFailed")
	wantTag(t, InjectControlNets(example(t, "fixers/rembg.json"), cn), "InjectControlNets", "NoSamplerInWorkflow")
	wantTag(t, InjectControlNets(mustParse(t, `{"1":{"class_type":"KSampler","inputs":{"positive":["2",0]}}}`), cn), "InjectControlNets", "InjectorFailed")
	check(t, InjectControlNets(example(t, "creators/illustrious_xl.json"), nil))
}

// ControlNets and IP-Adapters reach every pass that shared the original links.
func TestInjectionReachesAllSamplersThatShareLinks(t *testing.T) {
	g := mustParse(t, `{
	  "1":{"class_type":"CheckpointLoaderSimple","inputs":{}},
	  "2":{"class_type":"CLIPTextEncode","inputs":{"clip":["1",1],"text":""}},
	  "3":{"class_type":"CLIPTextEncode","inputs":{"clip":["1",1],"text":""}},
	  "4":{"class_type":"KSampler","inputs":{"model":["1",0],"positive":["2",0],"negative":["3",0]}},
	  "5":{"class_type":"KSampler","inputs":{"model":["1",0],"positive":["2",0],"negative":["3",0]}},
	  "6":{"class_type":"KSampler","inputs":{"model":["1",0],"positive":["7",0],"negative":["3",0]}},
	  "7":{"class_type":"CLIPTextEncode","inputs":{"clip":["1",1],"text":"other"}}}`)
	check(t, InjectControlNets(g, []ControlNet{{Type: "tile", Image: "i", ModelName: "m"}}))
	check(t, InjectIPAdapters(g, []IPAdapter{{Image: "r"}}))
	assertWellFormed(t, g)
	apply := link(t, g, "4", "positive").Node
	if link(t, g, "5", "positive").Node != apply || link(t, g, "4", "model").Node != link(t, g, "5", "model").Node {
		t.Error("the second pass did not get the injected nodes")
	}
	if link(t, g, "6", "positive") != (Link{"7", 0}) {
		t.Error("a sampler with another conditioning was rewired")
	}
}

func maskRegion(kw string) Region {
	return Region{Keyword: kw, Mask: "bos/" + kw + ".png", Prompt: kw + " prompt"}
}

func TestInjectRegionsWithMasksAndRectangles(t *testing.T) {
	g := example(t, "creators/illustrious_xl.json")
	orig := g.Clone()
	check(t, InjectRegions(g, []Region{
		maskRegion("red"),
		{Keyword: "left", Rect: &Rect{Width: 512, Height: 1024, X: 0, Y: 0}, Prompt: "left prompt"},
		maskRegion("blue"),
	}))
	assertWellFormed(t, g)

	for class, want := range map[string]int{
		"CLIPTextEncode": 3, "LoadImage": 2, "ImageToMask": 2, "ConditioningSetMask": 2, "ConditioningSetArea": 1, "ConditioningCombine": 3,
	} {
		if got := len(addedIDs(orig, g, class)); got != want {
			t.Errorf("added %d %s, want %d", got, class, want)
		}
	}
	combines := addedIDs(orig, g, "ConditioningCombine")
	c1, c2, c3 := combines[0], combines[1], combines[2]
	if link(t, g, c1, "conditioning_1") != (Link{"6", 0}) || link(t, g, c2, "conditioning_1") != (Link{c1, 0}) || link(t, g, c3, "conditioning_1") != (Link{c2, 0}) {
		t.Error("the combine chain does not start at the base conditioning")
	}
	if link(t, g, "3", "positive") != (Link{c3, 0}) {
		t.Errorf("sampler positive = %v", link(t, g, "3", "positive"))
	}
	if link(t, g, "3", "negative") != (Link{"7", 0}) {
		t.Error("the negative conditioning changed")
	}
	encodes := addedIDs(orig, g, "CLIPTextEncode")
	n := node(t, g, encodes[0])
	if string(n.Inputs["text"]) != `"red prompt"` || link(t, g, encodes[0], "clip") != (Link{"4a", 0}) {
		t.Errorf("region encode = %v", n.Inputs)
	}
	area := node(t, g, addedIDs(orig, g, "ConditioningSetArea")[0])
	if string(area.Inputs["width"]) != "512" || string(area.Inputs["height"]) != "1024" {
		t.Errorf("area = %v", area.Inputs)
	}
	setMask := node(t, g, addedIDs(orig, g, "ConditioningSetMask")[0])
	if string(setMask.Inputs["strength"]) != "1" || string(setMask.Inputs["set_cond_area"]) != `"default"` {
		t.Errorf("mask node = %v", setMask.Inputs)
	}
	toMask := node(t, g, addedIDs(orig, g, "ImageToMask")[0])
	if string(toMask.Inputs["channel"]) != `"red"` {
		t.Errorf("channel = %v", toMask.Inputs["channel"])
	}
}

// Regions go behind ControlNets: the chain is followed back to the text encoder.
func TestInjectRegionsAfterControlNets(t *testing.T) {
	g := example(t, "creators/illustrious_xl.json")
	check(t, InjectControlNets(g, []ControlNet{{Type: "canny", Image: "i", ModelName: "m"}}))
	orig := g.Clone()
	check(t, InjectRegions(g, []Region{maskRegion("red")}))
	assertWellFormed(t, g)
	apply := addedIDs(example(t, "creators/illustrious_xl.json"), orig, "ControlNetApplyAdvanced")[0]
	combine := addedIDs(orig, g, "ConditioningCombine")[0]
	if link(t, g, apply, "positive") != (Link{combine, 0}) {
		t.Errorf("ControlNet positive = %v, want the combined conditioning", link(t, g, apply, "positive"))
	}
	if link(t, g, "3", "positive").Node != apply {
		t.Error("the sampler must keep reading the ControlNet")
	}
	if link(t, g, combine, "conditioning_1") != (Link{"6", 0}) {
		t.Error("the combine does not start at the base conditioning")
	}
}

// Acceptance test 5's injector rule (SPEC §8.2): a region has a mask xor coordinates.
func TestInjectRegionsRejectsInvalidRegions(t *testing.T) {
	g := example(t, "creators/illustrious_xl.json")
	both := Region{Keyword: "x", Mask: "m.png", Rect: &Rect{}, Prompt: "p"}
	neither := Region{Keyword: "y", Prompt: "p"}
	before, _ := g.MarshalIndent()
	appErr := wantTagMeta(t, InjectRegions(g, []Region{maskRegion("ok"), both}), "InjectRegions", "RegionInvalid")
	if appErr.Metadata["keyword"] != "x" {
		t.Errorf("metadata = %v", appErr.Metadata)
	}
	wantTag(t, InjectRegions(g, []Region{neither}), "InjectRegions", "RegionInvalid")
	after, _ := g.MarshalIndent()
	if string(before) != string(after) {
		t.Error("a rejected region list changed the graph")
	}
}

func TestInjectRegionsFailures(t *testing.T) {
	r := []Region{maskRegion("red")}
	wantTag(t, InjectRegions(example(t, "fixers/rembg.json"), r), "InjectRegions", "NoSamplerInWorkflow")
	wantTag(t, InjectRegions(mustParse(t, `{"1":{"class_type":"KSampler","inputs":{"positive":5}}}`), r), "InjectRegions", "InjectorFailed")
	wantTag(t, InjectRegions(mustParse(t, `{"1":{"class_type":"KSampler","inputs":{"positive":["2",0]}},"2":{"class_type":"ConditioningConcat","inputs":{}}}`), r), "InjectRegions", "InjectorFailed")
	wantTag(t, InjectRegions(mustParse(t, `{"1":{"class_type":"KSampler","inputs":{"positive":["2",0]}},"2":{"class_type":"CLIPTextEncode","inputs":{"text":""}}}`), r), "InjectRegions", "InjectorFailed")
	wantTag(t, InjectRegions(mustParse(t, `{"1":{"class_type":"KSampler","inputs":{"positive":["2",0]}},"2":{"class_type":"ControlNetApply","inputs":{}}}`), r), "InjectRegions", "InjectorFailed")
	wantTag(t, InjectRegions(mustParse(t, `{"1":{"class_type":"KSampler","inputs":{"positive":["9",0]}}}`), r), "InjectRegions", "InjectorFailed")
	wantTag(t, InjectRegions(mustParse(t, `{"1":{"class_type":"KSampler","inputs":{"positive":["2",0]}},"2":{"class_type":"ControlNetApplyAdvanced","inputs":{"positive":["3",0]}},"3":{"class_type":"ControlNetApply","inputs":{"conditioning":["2",0]}}}`), r), "InjectRegions", "InjectorFailed")
	check(t, InjectRegions(example(t, "creators/illustrious_xl.json"), nil))
	// The older ControlNetApply node is followed through its conditioning input.
	g := mustParse(t, `{"1":{"class_type":"KSampler","inputs":{"positive":["2",0]}},"2":{"class_type":"ControlNetApply","inputs":{"conditioning":["3",0]}},"3":{"class_type":"CLIPTextEncode","inputs":{"clip":["4",1],"text":""}},"4":{"class_type":"CheckpointLoaderSimple","inputs":{}}}`)
	check(t, InjectRegions(g, r))
	assertWellFormed(t, g)
	if link(t, g, "2", "conditioning").Node == "3" {
		t.Error("ControlNetApply still reads the base conditioning")
	}
}

// The full pipeline of SPEC §8.2 on every example creator, with the golden
// graph of the result.
func TestFullPipelineOnExampleCreators(t *testing.T) {
	for _, name := range exampleNames(t, "creators") {
		t.Run(name, func(t *testing.T) {
			g := example(t, name).Clone()
			orig := g.Clone()
			a, err := Analyze(g)
			check(t, err)
			titles := a.PromptTitles
			if len(titles) != 2 {
				t.Fatalf("prompt titles = %v", titles)
			}
			check(t, SetPrompts(g, titles[0], "alice, red hair", titles[1], "ugly"))
			check(t, ApplyAttributes(g, attrs("steps", 28, "width", 768, "height", 1024)))
			check(t, SetSeed(g, 4242))
			check(t, InjectIPAdapters(g, []IPAdapter{{Image: "bos/ref.png", Weight: 0.6}}))
			check(t, InjectLoRAs(g, twoLoRAs[:1]))
			check(t, InjectControlNets(g, []ControlNet{{Type: "openpose", Image: "bos/pose.png", IsReference: true, ModelName: "cn.safetensors", Strength: 0.9}}))
			check(t, InjectRegions(g, []Region{maskRegion("red"), {Keyword: "r", Rect: &Rect{Width: 100, Height: 100}, Prompt: "rect"}}))
			assertWellFormed(t, g)
			golden(t, "pipeline_"+strings.TrimSuffix(strings.ReplaceAll(name, "/", "_"), ".json"), g)

			// The sampler still ends at the output, and the original is untouched.
			if _, err := Analyze(g); err != nil {
				t.Errorf("the edited graph is no longer analysable: %v", err)
			}
			after, _ := orig.MarshalIndent()
			again, _ := example(t, name).MarshalIndent()
			if string(after) != string(again) {
				t.Error("the original changed")
			}
		})
	}
}

// Golden graphs for each injector on its own, on a checkpoint and a split workflow.
func TestInjectorGoldens(t *testing.T) {
	for _, name := range []string{"creators/illustrious_xl_with_vae.json", "creators/anima_v3_preview.json"} {
		short := strings.TrimSuffix(strings.ReplaceAll(strings.TrimPrefix(name, "creators/"), "/", "_"), ".json")
		t.Run(short, func(t *testing.T) {
			g := example(t, name)
			check(t, InjectLoRAs(g, twoLoRAs))
			assertWellFormed(t, g)
			golden(t, "lora_"+short, g)

			g = example(t, name)
			check(t, InjectControlNets(g, []ControlNet{{Type: "depth", Image: "d.png", IsReference: true, ModelName: "cn_depth.safetensors", Strength: 0.7}}))
			assertWellFormed(t, g)
			golden(t, "controlnet_"+short, g)

			g = example(t, name)
			check(t, InjectRegions(g, []Region{maskRegion("red"), maskRegion("blue")}))
			assertWellFormed(t, g)
			golden(t, "regions_"+short, g)
		})
	}
}

// Every injector leaves every example workflow that has a sampler well-formed.
func TestInjectorsKeepAllExamplesWellFormed(t *testing.T) {
	for _, dir := range []string{"creators", "fixers"} {
		for _, name := range exampleNames(t, dir) {
			g := example(t, name)
			if len(g.samplers()) == 0 {
				continue
			}
			for label, inject := range map[string]func(Graph) error{
				"lora":      func(g Graph) error { return InjectLoRAs(g, twoLoRAs) },
				"ipadapter": func(g Graph) error { return InjectIPAdapters(g, []IPAdapter{{Image: "i"}}) },
				"controlnet": func(g Graph) error {
					return InjectControlNets(g, []ControlNet{{Type: "canny", Image: "i", IsReference: true, ModelName: "m"}})
				},
				"region": func(g Graph) error { return InjectRegions(g, []Region{maskRegion("red")}) },
			} {
				c := g.Clone()
				if err := inject(c); err != nil {
					// Some fixers wire their sampler in ways an injector rightly refuses.
					wantTag(t, err, "Inject", "InjectorFailed")
					continue
				}
				assertWellFormed(t, c)
				_ = label
			}
		}
	}
}
