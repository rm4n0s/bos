package workflow

import "testing"

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
