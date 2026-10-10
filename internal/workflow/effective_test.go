package workflow

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestEffectivePromptsFollowTheSampler(t *testing.T) {
	g := example(t, "creators/illustrious_xl.json")
	if err := SetPrompts(g, "Positive", "a red fox", "Negative", "blurry"); err != nil {
		t.Fatal(err)
	}
	got := EffectivePrompts(g)
	if len(got) == 0 {
		t.Fatal("no sampler found")
	}
	p := got[0]
	if p.Positive.Texts["text"] != "a red fox" || p.Negative.Texts["text"] != "blurry" {
		t.Errorf("prompts = %+v / %+v", p.Positive, p.Negative)
	}
	if p.Positive.Title != "Positive" || p.Positive.Class != "CLIPTextEncode" || p.Positive.Node != "6" {
		t.Errorf("positive node = %+v", p.Positive)
	}
	if p.Negative.Node != "7" {
		t.Errorf("negative node = %+v", p.Negative)
	}
}

// A prompt written into a node that no sampler reads shows up as it is: the
// sampler still reports the text the workflow had.
func TestEffectivePromptsShowAnIgnoredNode(t *testing.T) {
	g := Graph{
		"1": json.RawMessage(`{"class_type":"CLIPTextEncode","_meta":{"title":"Used"},"inputs":{"text":"from the file","clip":["9",0]}}`),
		"2": json.RawMessage(`{"class_type":"CLIPTextEncode","_meta":{"title":"Positive"},"inputs":{"text":"","clip":["9",0]}}`),
		"3": json.RawMessage(`{"class_type":"CLIPTextEncode","_meta":{"title":"Negative"},"inputs":{"text":"bad","clip":["9",0]}}`),
		"4": json.RawMessage(`{"class_type":"KSampler","inputs":{"positive":["1",0],"negative":["3",0],"seed":1}}`),
	}
	if err := SetPrompts(g, "Positive", "a red fox", "Negative", "blurry"); err != nil {
		t.Fatal(err)
	}
	p := EffectivePrompts(g)[0]
	if p.Positive.Node != "1" || p.Positive.Texts["text"] != "from the file" {
		t.Errorf("the sampler reads node 1, got %+v", p.Positive)
	}
}

func TestEffectivePromptsEdgeCases(t *testing.T) {
	tests := []struct {
		name  string
		graph string
		want  PromptSource
	}{
		{
			"text linked from another node",
			`{"1":{"class_type":"CLIPTextEncode","inputs":{"text":["2",0]}},
			  "2":{"class_type":"PrimitiveString","inputs":{"value":"hi"}},
			  "3":{"class_type":"KSampler","inputs":{"positive":["1",0]}}}`,
			PromptSource{Node: "1", Class: "CLIPTextEncode", Linked: []string{"text"}},
		},
		{
			"two text inputs",
			`{"1":{"class_type":"CLIPTextEncodeSDXL","inputs":{"text_g":"g","text_l":"l","width":1024}},
			  "3":{"class_type":"KSampler","inputs":{"positive":["1",0]}}}`,
			PromptSource{Node: "1", Class: "CLIPTextEncodeSDXL", Texts: map[string]string{"text_g": "g", "text_l": "l"}},
		},
		{
			"through a ControlNet node",
			`{"1":{"class_type":"CLIPTextEncode","inputs":{"text":"deep"}},
			  "2":{"class_type":"ControlNetApplyAdvanced","inputs":{"positive":["1",0],"strength":1}},
			  "3":{"class_type":"KSampler","inputs":{"positive":["2",0]}}}`,
			PromptSource{Node: "1", Class: "CLIPTextEncode", Texts: map[string]string{"text": "deep"}},
		},
		{
			"a loop does not hang",
			`{"1":{"class_type":"X","inputs":{"positive":["2",0]}},
			  "2":{"class_type":"X","inputs":{"positive":["1",0]}},
			  "3":{"class_type":"KSampler","inputs":{"positive":["1",0]}}}`,
			PromptSource{},
		},
		{
			"a link to a node that is not there",
			`{"3":{"class_type":"KSampler","inputs":{"positive":["99",0]}}}`,
			PromptSource{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g, err := Parse([]byte(tt.graph))
			if err != nil {
				t.Fatal(err)
			}
			got := EffectivePrompts(g)
			if len(got) != 1 {
				t.Fatalf("%d samplers", len(got))
			}
			if !reflect.DeepEqual(got[0].Positive, tt.want) {
				t.Errorf("positive = %+v, want %+v", got[0].Positive, tt.want)
			}
		})
	}
}

func TestEffectivePromptsWithoutASampler(t *testing.T) {
	g, err := Parse([]byte(`{"1":{"class_type":"CLIPTextEncode","inputs":{"text":"x"}}}`))
	if err != nil {
		t.Fatal(err)
	}
	if got := EffectivePrompts(g); len(got) != 0 {
		t.Errorf("prompts = %+v", got)
	}
}
