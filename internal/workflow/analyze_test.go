package workflow

import (
	"reflect"
	"testing"

	"github.com/rm4n0s/bos/internal/domain"
)

func attrs(kv ...any) domain.OutputAttributes {
	out := domain.OutputAttributes{}
	for i := 0; i < len(kv); i += 2 {
		switch v := kv[i+1].(type) {
		case int:
			out[kv[i].(string)] = domain.Attr{Kind: domain.AttrInt, Int: uint64(v)}
		case float64:
			out[kv[i].(string)] = domain.Attr{Kind: domain.AttrDecimal, Float: v}
		case string:
			out[kv[i].(string)] = domain.Attr{Kind: domain.AttrText, Text: v}
		}
	}
	return out
}

func TestAnalyzeExampleGenerators(t *testing.T) {
	tests := []struct {
		file   string
		node   string
		title  string
		attrs  domain.OutputAttributes
		prompt []string
	}{
		{
			"creators/illustrious_xl.json", "9", "Save Image",
			attrs("file_type", "png", "width", 1024, "height", 1024, "batch_size", 1, "steps", 24, "cfg", 6, "sampler_name", "euler_ancestral",
				"scheduler", "normal", "denoise", 1, "seed", 41754494229476),
			[]string{"Positive", "Negative"},
		},
		{
			"creators/anima_aio.json", "10", "Save Image",
			attrs("file_type", "png", "width", 960, "height", 1440, "batch_size", 1, "steps", 16, "cfg", 1.0, "sampler_name", "er_sde",
				"scheduler", "beta", "denoise", 1.0, "seed", 1234567890),
			[]string{"Positive Prompt", "Negative Prompt"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.file, func(t *testing.T) {
			a, err := Analyze(example(t, tt.file))
			check(t, err)
			if a.OutputType != domain.OutputImage || a.OutputNodeClassType != "SaveImage" || a.OutputNodeID != tt.node || a.OutputNodeTitle != tt.title {
				t.Errorf("analysis = %+v", a)
			}
			if !reflect.DeepEqual(a.Attributes, tt.attrs) {
				t.Errorf("attributes = %+v\nwant        %+v", a.Attributes, tt.attrs)
			}
			if !reflect.DeepEqual(a.PromptTitles, tt.prompt) {
				t.Errorf("prompt titles = %v, want %v", a.PromptTitles, tt.prompt)
			}
		})
	}
}

// Every shipped workflow has a recognised output node (SPEC §3.2).
func TestAnalyzeAllExamples(t *testing.T) {
	for _, dir := range []string{"creators", "fixers"} {
		for _, name := range exampleNames(t, dir) {
			a, err := Analyze(example(t, name))
			if err != nil {
				t.Errorf("%s: %v", name, err)
				continue
			}
			if a.OutputType != domain.OutputImage || a.OutputNodeTitle == "" {
				t.Errorf("%s: analysis = %+v", name, a)
			}
		}
	}
	a, err := Analyze(example(t, "fixers/simple_img2img_refinement.json"))
	check(t, err)
	if !reflect.DeepEqual(a.LoadImageTitles, []string{"Load Image"}) || !reflect.DeepEqual(a.SaveImageTitles, []string{"Save Image"}) {
		t.Errorf("fixer candidates = %v / %v", a.LoadImageTitles, a.SaveImageTitles)
	}
}

func TestAnalyzeOutputKinds(t *testing.T) {
	tests := []struct {
		name  string
		graph string
		typ   domain.OutputType
		class string
		attrs domain.OutputAttributes
	}{
		{
			"video", `{
			  "1":{"class_type":"EmptyWanLatentVideo","inputs":{"width":832,"height":480,"length":81,"batch_size":1}},
			  "2":{"class_type":"VHS_VideoCombine","_meta":{"title":"Video"},"inputs":{"frame_rate":16,"format":"video/h264-mp4"}},
			  "3":{"class_type":"PreviewImage","inputs":{}}}`,
			domain.OutputVideo, "VHS_VideoCombine",
			attrs("file_type", "h264-mp4", "width", 832, "height", 480, "fps", 16, "num_frames", 81, "duration_seconds", 5.06),
		},
		{
			"3d", `{"1":{"class_type":"SaveGLB","_meta":{"title":"Mesh"},"inputs":{}}}`,
			domain.Output3D, "SaveGLB", attrs("file_type", "glb"),
		},
		{
			"3d with a format input", `{"1":{"class_type":"Save3DModel","inputs":{"format":"OBJ"}}}`,
			domain.Output3D, "Save3DModel", attrs("file_type", "obj"),
		},
		{
			"animated webp", `{"1":{"class_type":"SaveAnimatedWEBP","inputs":{}},"2":{"class_type":"EmptyLatentImage","inputs":{"width":64,"height":64,"batch_size":8}}}`,
			domain.OutputImage, "SaveAnimatedWEBP", attrs("file_type", "webp", "width", 64, "height", 64, "batch_size", 8),
		},
		{
			"image save with an extension", `{"1":{"class_type":"Image Save","inputs":{"extension":"JPEG"}}}`,
			domain.OutputImage, "Image Save", attrs("file_type", "jpeg"),
		},
		{
			"save image wins over preview", `{"1":{"class_type":"PreviewImage","inputs":{}},"2":{"class_type":"SaveImage","inputs":{}}}`,
			domain.OutputImage, "SaveImage", attrs("file_type", "png"),
		},
		{
			"size from the highest-priority node", `{
			  "1":{"class_type":"ImageScale","inputs":{"width":2048,"height":2048}},
			  "2":{"class_type":"EmptySD3LatentImage","inputs":{"width":1024,"height":768,"batch_size":1}},
			  "3":{"class_type":"EmptyLatentImage","inputs":{"width":["9",0],"height":512,"batch_size":1}},
			  "4":{"class_type":"SaveImage","inputs":{}}}`,
			domain.OutputImage, "SaveImage", attrs("file_type", "png", "width", 1024, "height", 768, "batch_size", 1),
		},
		{
			"advanced sampler", `{"1":{"class_type":"KSamplerAdvanced","inputs":{"noise_seed":99,"steps":30,"cfg":4.5,"sampler_name":"dpmpp","scheduler":"karras","add_noise":"enable"}},"2":{"class_type":"SaveImage","inputs":{}}}`,
			domain.OutputImage, "SaveImage", attrs("file_type", "png", "steps", 30, "cfg", 4.5, "sampler_name", "dpmpp", "scheduler", "karras", "seed", 99),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a, err := Analyze(mustParse(t, tt.graph))
			check(t, err)
			if a.OutputType != tt.typ || a.OutputNodeClassType != tt.class {
				t.Errorf("analysis = %+v", a)
			}
			if !reflect.DeepEqual(a.Attributes, tt.attrs) {
				t.Errorf("attributes = %+v\nwant        %+v", a.Attributes, tt.attrs)
			}
		})
	}
}

func TestAnalyzeWithoutOutputNode(t *testing.T) {
	_, err := Analyze(mustParse(t, `{"1":{"class_type":"KSampler","inputs":{}}}`))
	appErr := wantTagMeta(t, err, "Analyze", "OutputNodeNotFound")
	if _, ok := appErr.Metadata["recognised"]; !ok {
		t.Errorf("metadata = %v", appErr.Metadata)
	}
}

func TestAnalyzeSuggestsThePromptNodes(t *testing.T) {
	tests := []struct {
		file, positive, negative string
	}{
		{"creators/illustrious_xl.json", "Positive", "Negative"},
		{"creators/anima_aio.json", "Positive Prompt", "Negative Prompt"},
		{"creators/anima_v3_preview.json", "Positive Prompt", "Negative Prompt"},
	}
	for _, tt := range tests {
		t.Run(tt.file, func(t *testing.T) {
			a, err := Analyze(example(t, tt.file))
			check(t, err)
			if a.PositiveTitle != tt.positive || a.NegativeTitle != tt.negative {
				t.Errorf("suggested %q / %q, want %q / %q", a.PositiveTitle, a.NegativeTitle, tt.positive, tt.negative)
			}
		})
	}
}

func TestSuggestedPromptTitlesStartWithTheWord(t *testing.T) {
	// "POSITIVE hires" starts with the word in any case; "Not positive" and
	// "Prompt" do not; a text encoder wins over a node of another kind.
	g, err := Parse([]byte(`{
	  "1":{"class_type":"PrimitiveString","_meta":{"title":"positive text"},"inputs":{"value":""}},
	  "2":{"class_type":"CLIPTextEncode","_meta":{"title":"Not positive"},"inputs":{"text":""}},
	  "3":{"class_type":"CLIPTextEncode","_meta":{"title":"POSITIVE hires"},"inputs":{"text":""}},
	  "4":{"class_type":"CLIPTextEncode","_meta":{"title":"Prompt"},"inputs":{"text":""}},
	  "5":{"class_type":"CLIPTextEncode","_meta":{"title":"Negative"},"inputs":{"text":""}},
	  "6":{"class_type":"SaveImage","_meta":{"title":"Save Image"},"inputs":{"images":["3",0]}}}`))
	check(t, err)
	a, err := Analyze(g)
	check(t, err)
	if a.PositiveTitle != "POSITIVE hires" || a.NegativeTitle != "Negative" {
		t.Errorf("suggested %q / %q", a.PositiveTitle, a.NegativeTitle)
	}

	g, err = Parse([]byte(`{
	  "1":{"class_type":"CLIPTextEncode","_meta":{"title":"Prompt"},"inputs":{"text":""}},
	  "2":{"class_type":"SaveImage","_meta":{"title":"Save Image"},"inputs":{"images":["1",0]}}}`))
	check(t, err)
	if a, err = Analyze(g); err != nil || a.PositiveTitle != "" || a.NegativeTitle != "" {
		t.Errorf("no node starts with the words: %+v, %v", a, err)
	}
}

// A node without a `_meta.title` is named by its class, which is what the
// client library matches too. The Qwen text node holds both prompts, so it is
// the positive node and no negative one is suggested.
func TestAnalyzeAQwenWorkflow(t *testing.T) {
	a, err := Analyze(qwen(t))
	check(t, err)
	if !reflect.DeepEqual(a.PromptTitles, []string{"TextEncodeQwenImage21"}) {
		t.Errorf("prompt titles = %v", a.PromptTitles)
	}
	if a.PositiveTitle != "TextEncodeQwenImage21" || a.NegativeTitle != "" {
		t.Errorf("suggested %q / %q", a.PositiveTitle, a.NegativeTitle)
	}
	check(t, CheckTitles(qwen(t), a.PositiveTitle))
}

func TestEffectivePromptsOfAQwenWorkflow(t *testing.T) {
	g := qwen(t)
	check(t, SetPrompts(g, "TextEncodeQwenImage21", "a red fox", "", "blurry"))
	p := EffectivePrompts(g)
	if len(p) != 1 || p[0].Positive.Node != "4" || p[0].Negative.Node != "4" {
		t.Fatalf("prompts = %+v", p)
	}
	want := map[string]string{"prompt": "a red fox", "negative_prompt": "blurry"}
	if !reflect.DeepEqual(p[0].Positive.Texts, want) {
		t.Errorf("texts = %v, want %v", p[0].Positive.Texts, want)
	}
}
