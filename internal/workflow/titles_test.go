package workflow

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// Acceptance test 32.
func TestCheckTitlesRejectsDuplicates(t *testing.T) {
	g := mustParse(t, `{
	  "1":{"class_type":"CLIPTextEncode","_meta":{"title":"Positive"},"inputs":{"text":""}},
	  "2":{"class_type":"CLIPTextEncode","_meta":{"title":"Positive"},"inputs":{"text":""}},
	  "3":{"class_type":"CLIPTextEncode","_meta":{"title":"Negative"},"inputs":{"text":""}}}`)
	appErr := wantTagMeta(t, CheckTitles(g, "Positive", "Negative"), "CheckTitles", "WorkflowTitleDuplicated")
	if appErr.Metadata["title"] != "Positive" {
		t.Errorf("metadata = %v", appErr.Metadata)
	}
	check(t, CheckTitles(g, "Negative", "", "   "))
	wantTag(t, CheckTitles(g, "Missing"), "CheckTitles", "WorkflowTitleNotFound")
	_, err := FindByTitle(g, "Positive")
	wantTag(t, err, "FindByTitle", "WorkflowTitleDuplicated")
}

// The library matches a title against `class_type` as well as `_meta.title`, so
// a title equal to a class name counts for that node (SPEC §18.5).
func TestTitleMatchesClassTypeToo(t *testing.T) {
	g := mustParse(t, `{
	  "1":{"class_type":"SaveImage","_meta":{"title":"Save"},"inputs":{}},
	  "2":{"class_type":"PreviewImage","_meta":{"title":"SaveImage"},"inputs":{}}}`)
	wantTag(t, CheckTitles(g, "SaveImage"), "CheckTitles", "WorkflowTitleDuplicated")
	check(t, CheckTitles(g, "Save"))
	n, err := FindByTitle(g, "Save")
	check(t, err)
	if n.ID != "1" {
		t.Errorf("found node %s", n.ID)
	}
}

func TestSetPrompts(t *testing.T) {
	g := example(t, "creators/illustrious_xl.json")
	check(t, SetPrompts(g, "Positive", "alice, smiling", "Negative", "ugly"))
	for id, want := range map[string]string{"6": `"alice, smiling"`, "7": `"ugly"`} {
		if got := string(node(t, g, id).Inputs["text"]); got != want {
			t.Errorf("node %s text = %s, want %s", id, got, want)
		}
	}
	// The other inputs and the title survive.
	if l := link(t, g, "6", "clip"); l != (Link{"4a", 0}) {
		t.Errorf("clip link = %v", l)
	}
	if node(t, g, "6").Title != "Positive" {
		t.Error("the title changed")
	}

	// A prompt without a configured title is left out.
	g = example(t, "creators/illustrious_xl.json")
	check(t, SetPrompts(g, "Positive", "only positive", "", "ignored"))
	if got := string(node(t, g, "7").Inputs["text"]); got != `""` {
		t.Errorf("negative prompt written without a title: %s", got)
	}
	// Special characters are escaped.
	check(t, SetPrompts(g, "Positive", "a \"quoted\"\nline <b>&", "", ""))
	if got := string(node(t, g, "6").Inputs["text"]); got != `"a \"quoted\"\nline \u003cb\u003e\u0026"` { // valid JSON; Go escapes < > &
		t.Errorf("text = %q", got)
	}

	wantTag(t, SetPrompts(g, "Nope", "x", "", ""), "SetPrompts", "WorkflowTitleNotFound")
	wantTag(t, SetInput(g, "Nope", "text", "x"), "SetInput", "WorkflowTitleNotFound")
}

func TestSetInputForFixer(t *testing.T) {
	g := example(t, "fixers/simple_img2img_refinement.json")
	check(t, SetInput(g, "Load Image", "image", "bos/abc123.png"))
	if got := string(node(t, g, "8").Inputs["image"]); got != `"bos/abc123.png"` {
		t.Errorf("image = %s", got)
	}
}

// qwen reads the Qwen Image 2.1 test workflow, whose one text node holds both
// prompts, in the inputs `prompt` and `negative_prompt`.
func qwen(t *testing.T) Graph {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "qwen_image_21.json"))
	check(t, err)
	return mustParse(t, string(data))
}

func inputText(t *testing.T, g Graph, node, input string) (string, bool) {
	t.Helper()
	n, err := g.Node(node)
	check(t, err)
	raw, ok := n.Inputs[input]
	if !ok {
		return "", false
	}
	var s string
	check(t, json.Unmarshal(raw, &s))
	return s, true
}

// A node that has a `prompt` input and a `negative_prompt` input receives the
// positive prompt in the first and the negative one in the second, whether the
// negative title is left out or names the same node. It must not be given a
// `text` input that ComfyUI would ignore.
func TestSetPromptsInAQwenNode(t *testing.T) {
	for _, negativeTitle := range []string{"", "TextEncodeQwenImage21"} {
		t.Run("negative title "+negativeTitle, func(t *testing.T) {
			g := qwen(t)
			check(t, SetPrompts(g, "TextEncodeQwenImage21", "a red fox", negativeTitle, "blurry"))
			if got, _ := inputText(t, g, "4", "prompt"); got != "a red fox" {
				t.Errorf("prompt = %q", got)
			}
			if got, _ := inputText(t, g, "4", "negative_prompt"); got != "blurry" {
				t.Errorf("negative_prompt = %q", got)
			}
			if _, has := inputText(t, g, "4", "text"); has {
				t.Error("a text input was added to a node that has none")
			}
			n, _ := g.Node("4")
			if string(n.Inputs["resolution"]) != "1024" || string(n.Inputs["clip"]) != `["2",0]` {
				t.Errorf("other inputs changed: %s %s", n.Inputs["resolution"], n.Inputs["clip"])
			}
		})
	}
}

func TestSetPromptsStillUsesTextWhereTheNodeHasIt(t *testing.T) {
	g := example(t, "creators/illustrious_xl.json")
	check(t, SetPrompts(g, "Positive", "a red fox", "Negative", "blurry"))
	if got, _ := inputText(t, g, "6", "text"); got != "a red fox" {
		t.Errorf("text = %q", got)
	}
	if got, _ := inputText(t, g, "7", "text"); got != "blurry" {
		t.Errorf("negative text = %q", got)
	}
}

// A node with no input for the text cannot take it; saying so beats a prompt
// that silently goes nowhere.
func TestSetPromptsRefusesANodeWithoutATextInput(t *testing.T) {
	g := mustParse(t, `{
	  "1":{"class_type":"CLIPTextEncode","_meta":{"title":"Positive"},"inputs":{"clip":["9",0]}},
	  "2":{"class_type":"ConditioningZeroOut","_meta":{"title":"Negative"},"inputs":{"conditioning":["1",0]}}}`)
	appErr := wantTagMeta(t, SetPrompts(g, "Positive", "x", "", ""), "SetPrompts->noPromptInput", "InjectorFailed")
	if appErr.Metadata["node"] != "1" {
		t.Errorf("metadata = %v", appErr.Metadata)
	}
	wantTag(t, SetPrompts(g, "", "", "Negative", "x"), "SetPrompts->noPromptInput", "InjectorFailed")
}

// MOSS-Image has an editable string primitive in front of an optional prompt
// upsampler and the text encoder. Naming the primitive writes its `value`; the
// encoder, whose `text` is a link, still takes the text directly.
func TestSetPromptsWritesAStringPrimitive(t *testing.T) {
	for _, title := range []string{"Positive Prompt (edit me)", "Positive Encode"} {
		g := example(t, "creators/moss.json")
		check(t, SetPrompts(g, title, "a fox", "Negative Prompt", "blurry"))
		id, input := "10", "value"
		if title == "Positive Encode" {
			id, input = "4", "text"
		}
		if got := inputOf(t, g, id, input); got != `"a fox"` {
			t.Errorf("%s: %s = %s", title, input, got)
		}
		if got := inputOf(t, g, "5", "text"); got != `"blurry"` {
			t.Errorf("%s: negative = %s", title, got)
		}
	}

	// Only the string primitives count: a primitive of another kind with a
	// `value` input still has nowhere for a prompt.
	g := mustParse(t, `{"1":{"class_type":"PrimitiveInt","_meta":{"title":"Positive"},"inputs":{"value":1}}}`)
	wantTag(t, SetPrompts(g, "Positive", "x", "", ""), "SetPrompts->noPromptInput", "InjectorFailed")
}

func TestPromptsGoWithThePositiveNode(t *testing.T) {
	if !NegativeGoesWithPositive(qwen(t), "TextEncodeQwenImage21") {
		t.Error("the Qwen node takes the negative prompt too")
	}
	g := example(t, "creators/illustrious_xl.json")
	if NegativeGoesWithPositive(g, "Positive") || NegativeGoesWithPositive(g, "Nowhere") {
		t.Error("a CLIPTextEncode takes one prompt, and a missing node none")
	}
}

// Region prompts copy the base text encoder, which only works for one with a
// `text` input. Another kind is refused rather than given a text it ignores.
func TestRegionsRefuseATextNodeWithoutATextInput(t *testing.T) {
	g := qwen(t)
	err := InjectRegions(g, []Region{{Keyword: "left", Prompt: "a fox", Rect: &Rect{Width: 512, Height: 512}}})
	appErr := wantTagMeta(t, err, "InjectRegions", "InjectorFailed")
	if appErr.Metadata["node"] != "4" {
		t.Errorf("metadata = %v", appErr.Metadata)
	}
}
