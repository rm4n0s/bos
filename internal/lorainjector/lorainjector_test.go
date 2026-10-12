package lorainjector

import (
	"encoding/json"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/rm4n0s/errors"
)

func parse(t *testing.T, doc string) map[string]json.RawMessage {
	t.Helper()
	var g map[string]json.RawMessage
	if err := json.Unmarshal([]byte(doc), &g); err != nil {
		t.Fatal(err)
	}
	return g
}

// inputs decodes the inputs of a node.
func inputs(t *testing.T, g map[string]json.RawMessage, id string) map[string]any {
	t.Helper()
	var n struct {
		ClassType string         `json:"class_type"`
		Inputs    map[string]any `json:"inputs"`
	}
	raw, ok := g[id]
	if !ok {
		t.Fatalf("no node %s", id)
	}
	if err := json.Unmarshal(raw, &n); err != nil {
		t.Fatal(err)
	}
	n.Inputs["class_type"] = n.ClassType
	return n.Inputs
}

func wantInjectorFailed(t *testing.T, err error) {
	t.Helper()
	appErr, ok := errors.FromError(err)
	if !ok {
		t.Fatalf("expected *errors.Error, got %T (%v)", err, err)
	}
	if appErr.Tag != "InjectorFailed" || !appErr.HasRoute("LoRAInjector") {
		t.Errorf("unexpected failure path: %s (tag %s)", appErr.Route(), appErr.Tag)
	}
}

const checkpointDoc = `{
  "4":{"class_type":"CheckpointLoaderSimple","inputs":{"ckpt_name":"a"}},
  "4a":{"class_type":"CLIPSetLastLayer","inputs":{"clip":["4",1]}},
  "5":{"class_type":"CLIPTextEncode","inputs":{"clip":["4a",0],"text":""}},
  "3":{"class_type":"KSampler","inputs":{"model":["4",0],"positive":["5",0],"negative":["5",0]}},
  "8":{"class_type":"VAEDecode","inputs":{"vae":["4",2],"samples":["3",0]}}}`

const splitDoc = `{
  "1":{"class_type":"UNETLoader","inputs":{"unet_name":"u"}},
  "2":{"class_type":"CLIPLoader","inputs":{"clip_name":"c"}},
  "5":{"class_type":"CLIPTextEncode","inputs":{"clip":["2",0],"text":""}},
  "3":{"class_type":"KSampler","inputs":{"model":["1",0],"positive":["5",0],"negative":["5",0]}}}`

func TestCheckpointWorkflowGetsLoraLoadersOnModelAndClip(t *testing.T) {
	g := parse(t, checkpointDoc)
	inj := NewLoRAInjector(g)
	if inj.DetectWorkflowType() != WorkflowCheckpoint {
		t.Fatal("not detected as a checkpoint workflow")
	}
	ids, err := inj.AddMultipleLoRAs([]LoRAConfig{{Name: "a", StrengthModel: 0.8, StrengthCLIP: 0.6}, {Name: "b/c.safetensors", StrengthModel: 1, StrengthCLIP: 0.5}}, "")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(ids, []string{"9", "10"}) {
		t.Fatalf("ids = %v", ids)
	}
	first, second := inputs(t, g, "9"), inputs(t, g, "10")
	if first["class_type"] != "LoraLoader" || first["lora_name"] != "a.safetensors" || first["strength_model"] != 0.8 || first["strength_clip"] != 0.6 {
		t.Errorf("first = %v", first)
	}
	if !reflect.DeepEqual(first["model"], []any{"4", 0.0}) || !reflect.DeepEqual(first["clip"], []any{"4", 1.0}) {
		t.Errorf("the first LoRA is not on the checkpoint: %v", first)
	}
	if !reflect.DeepEqual(second["model"], []any{"9", 0.0}) || !reflect.DeepEqual(second["clip"], []any{"9", 1.0}) {
		t.Errorf("the second LoRA does not follow the first: %v", second)
	}
	// The readers of MODEL and CLIP read the end of the chain; the VAE stays.
	if got := inputs(t, g, "3")["model"]; !reflect.DeepEqual(got, []any{"10", 0.0}) {
		t.Errorf("sampler model = %v", got)
	}
	if got := inputs(t, g, "4a")["clip"]; !reflect.DeepEqual(got, []any{"10", 1.0}) {
		t.Errorf("clip skip = %v", got)
	}
	if got := inputs(t, g, "8")["vae"]; !reflect.DeepEqual(got, []any{"4", 2.0}) {
		t.Errorf("the VAE was rewired: %v", got)
	}
}

func TestSplitWorkflowGetsModelOnlyLoaders(t *testing.T) {
	g := parse(t, splitDoc)
	inj := NewLoRAInjector(g)
	if inj.DetectWorkflowType() != WorkflowSplit {
		t.Fatal("not detected as a split workflow")
	}
	ids, err := inj.AddMultipleLoRAs([]LoRAConfig{{Name: "a", StrengthModel: 0.8, StrengthCLIP: 0.6}, {Name: "b", StrengthModel: 1}}, "")
	if err != nil {
		t.Fatal(err)
	}
	first, second := inputs(t, g, ids[0]), inputs(t, g, ids[1])
	if first["class_type"] != "LoraLoaderModelOnly" || first["strength_model"] != 0.8 || first["strength_clip"] != nil {
		t.Errorf("first = %v", first)
	}
	if !reflect.DeepEqual(first["model"], []any{"1", 0.0}) || !reflect.DeepEqual(second["model"], []any{ids[0], 0.0}) {
		t.Errorf("the LoRAs are not chained on the UNET: %v %v", first, second)
	}
	if got := inputs(t, g, "3")["model"]; !reflect.DeepEqual(got, []any{ids[1], 0.0}) {
		t.Errorf("sampler model = %v", got)
	}
	if got := inputs(t, g, "5")["clip"]; !reflect.DeepEqual(got, []any{"2", 0.0}) {
		t.Errorf("the CLIP link changed: %v", got)
	}
}

// A link written with a number for the node id is a link too.
func TestLinksWithNumericNodeIDsAreRewired(t *testing.T) {
	g := parse(t, `{
	  "4":{"class_type":"CheckpointLoaderSimple","inputs":{}},
	  "5":{"class_type":"CLIPTextEncode","inputs":{"clip":[4,1]}},
	  "3":{"class_type":"KSampler","inputs":{"model":[4,0]}}}`)
	ids, err := NewLoRAInjector(g).AddMultipleLoRAs([]LoRAConfig{{Name: "x", StrengthModel: 1, StrengthCLIP: 1}}, "")
	if err != nil {
		t.Fatal(err)
	}
	if got := inputs(t, g, "3")["model"]; !reflect.DeepEqual(got, []any{ids[0], 0.0}) {
		t.Errorf("sampler model = %v", got)
	}
	if got := inputs(t, g, "5")["clip"]; !reflect.DeepEqual(got, []any{ids[0], 1.0}) {
		t.Errorf("encoder clip = %v", got)
	}
}

func TestNodeIDsAreNeverReused(t *testing.T) {
	g := parse(t, `{"4":{"class_type":"CheckpointLoaderSimple","inputs":{}},"5":{"class_type":"X","inputs":{}},"7":{"class_type":"X","inputs":{}}}`)
	inj := NewLoRAInjector(g)
	if info := inj.GetWorkflowInfo(); info.NextNodeID != 8 || info.LoRAType != "LoraLoader" || len(info.CheckpointLoaders) != 1 {
		t.Errorf("info = %+v", info)
	}
	id, err := inj.AddLoRA(LoRAConfig{Name: "x.safetensors"}, "")
	if err != nil || id != "8" {
		t.Errorf("id = %q, %v", id, err)
	}
	// An id the workflow already uses is skipped.
	g["9"] = json.RawMessage(`{"class_type":"X","inputs":{}}`)
	if id, err = inj.AddLoRA(LoRAConfig{Name: "y.safetensors"}, ""); err != nil || id != "10" {
		t.Errorf("id = %q, %v", id, err)
	}
}

func TestFailuresAreInjectorFailed(t *testing.T) {
	tests := []struct {
		name  string
		doc   string
		after string
	}{
		{"neither loader", `{"1":{"class_type":"KSampler","inputs":{}}}`, ""},
		{"the node to insert after is missing (checkpoint)", checkpointDoc, "99"},
		{"the node to insert after is missing (split)", splitDoc, "99"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewLoRAInjector(parse(t, tt.doc)).AddMultipleLoRAs([]LoRAConfig{{Name: "x"}}, tt.after)
			wantInjectorFailed(t, err)
		})
	}
	// A node that is not JSON cannot be edited.
	g := parse(t, checkpointDoc)
	g["3"] = json.RawMessage(`"text"`)
	inj := NewLoRAInjector(g)
	if err := inj.updateNodeInput("3", "model", json.RawMessage(`["1",0]`)); err == nil {
		t.Error("a node that is not an object was edited")
	} else {
		wantInjectorFailed(t, err)
	}
	wantInjectorFailed(t, inj.updateNodeInput("404", "model", json.RawMessage(`1`)))
}

func TestSaveWritesTheWorkflow(t *testing.T) {
	inj := NewLoRAInjector(parse(t, splitDoc))
	path := filepath.Join(t.TempDir(), "wf.json")
	if err := inj.Save(path); err != nil {
		t.Fatal(err)
	}
	appErr, ok := errors.FromError(inj.Save(filepath.Join(t.TempDir(), "missing", "wf.json")))
	if !ok || appErr.Tag != "FileStoreFailure" {
		t.Errorf("a path that cannot be written: %v", appErr)
	}
	if got := inj.GetWorkflow(); len(got) != 4 {
		t.Errorf("copy has %d nodes", len(got))
	}
}
