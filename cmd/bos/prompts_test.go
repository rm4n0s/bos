package main

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/rm4n0s/bos/internal/comfy/comfytest"
)

// encoderText reads the text of a node of a workflow the stand-in ComfyUI
// received: what a real ComfyUI would encode.
func encoderText(t *testing.T, prompt map[string]json.RawMessage, node string) string {
	t.Helper()
	var n struct {
		Inputs struct {
			Text string `json:"text"`
		} `json:"inputs"`
	}
	if err := json.Unmarshal(prompt[node], &n); err != nil {
		t.Fatalf("node %s: %v", node, err)
	}
	return n.Inputs.Text
}

// TestEveryCombinationReachesComfyUI runs the whole program on two groups, x
// (characters) and y (clothes), and checks the prompts the stand-in ComfyUI
// receives: one per combination, the last group changing fastest, each prompt
// holding the text of its own two items joined by a single space (SPEC §6.4).
func TestEveryCombinationReachesComfyUI(t *testing.T) {
	fake := comfytest.New(10 * time.Millisecond)
	t.Cleanup(fake.Close)
	p := startProgram(t, testEnv(t, nil))

	p.must(201, "POST", "/servers", map[string]any{"name": "Fake", "host": fake.URL, "code_name": "gpu1"})
	p.must(201, "POST", "/generators", map[string]any{
		"name": "Anime", "code_name": "anime", "workflow": readFile(t, "creators", "illustrious_xl.json"),
		"positive_prompt_title": "Positive", "negative_prompt_title": "Negative",
	})
	p.must(201, "POST", "/groups", map[string]any{"name": "x", "code_name": "x"})
	p.must(201, "POST", "/groups", map[string]any{"name": "y", "code_name": "y"})
	for _, c := range []string{"x1", "x2"} {
		p.must(201, "POST", "/groups/1/items", map[string]any{"code_name": c, "name": c, "positive_prompt": c, "negative_prompt": "bad " + c})
	}
	for _, c := range []string{"y1", "y2", "y3"} {
		p.must(201, "POST", "/groups/2/items", map[string]any{"code_name": c, "name": c, "positive_prompt": c, "negative_prompt": "bad " + c})
	}
	p.must(201, "POST", "/projects", map[string]any{"name": "Demo"})
	p.must(201, "POST", "/projects/1/commands", map[string]any{"name": "grid", "code": "gpu1 -$ anime: x * y"})
	p.must(202, "POST", "/commands/1/run", nil)
	p.waitFor("the command to finish", 20*time.Second, func() bool { return p.commandStatus(1) == "finished" })

	// "6" and "7" are the Positive and Negative nodes of illustrious_xl.json.
	var got, gotNeg []string
	for i, prompt := range fake.Prompts() {
		pos, neg := encoderText(t, prompt, "6"), encoderText(t, prompt, "7")
		t.Logf("ComfyUI received #%d: positive=%q negative=%q", i+1, pos, neg)
		got, gotNeg = append(got, pos), append(gotNeg, neg)
	}
	want := []string{"x1 y1", "x1 y2", "x1 y3", "x2 y1", "x2 y2", "x2 y3"}
	wantNeg := []string{"bad x1 bad y1", "bad x1 bad y2", "bad x1 bad y3", "bad x2 bad y1", "bad x2 bad y2", "bad x2 bad y3"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ComfyUI received positive prompts %q, want %q", got, want)
	}
	if !reflect.DeepEqual(gotNeg, wantNeg) {
		t.Errorf("ComfyUI received negative prompts %q, want %q", gotNeg, wantNeg)
	}
}
