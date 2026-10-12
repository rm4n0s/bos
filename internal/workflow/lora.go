package workflow

import (
	"path"
	"slices"
	"strings"

	"github.com/rm4n0s/errors"
)

// LoRA is one LoRA to load.
type LoRA struct {
	Name          string
	StrengthModel float64
	StrengthClip  float64
}

// loraFile gives a LoRA name its extension when it has none, so `style_a`
// loads `style_a.safetensors`.
func loraFile(name string) string {
	if path.Ext(name) == "" {
		return name + ".safetensors"
	}
	return name
}

// SameLoRA reports whether two names are one file for ComfyUI: they differ at
// most in the extension left out and in the direction of the slashes.
func SameLoRA(a, b string) bool {
	return strings.ReplaceAll(loraFile(a), `\`, "/") == strings.ReplaceAll(loraFile(b), `\`, "/")
}

type loaderStyle int

const (
	styleCheckpoint loaderStyle = iota // a CheckpointLoader* node gives MODEL, CLIP and VAE
	styleSplit                         // a UNETLoader gives MODEL; a separate CLIPLoader gives CLIP
)

func injectorFailed(injector, reason string, kv ...any) *errors.Error {
	return errors.New("InjectorFailed", "cannot inject "+injector+": "+reason, append([]any{"injector", injector, "reason", reason}, kv...)...)
}

// modelLoader follows the first sampler's model input upstream, through nodes
// that pass a model on (a model-sampling patch, an IP-Adapter), to the loader
// that produced it.
func modelLoader(g Graph, injector string) (Node, loaderStyle, error) {
	samplers := g.samplers()
	if len(samplers) == 0 {
		return Node{}, 0, noSampler(injector)
	}
	cur, ok := samplers[0].Link("model")
	if !ok {
		return Node{}, 0, injectorFailed(injector, "the sampler's model input is not linked", "node", samplers[0].ID)
	}
	seen := map[string]bool{}
	for !seen[cur.Node] {
		seen[cur.Node] = true
		n, err := g.Node(cur.Node)
		if err != nil {
			return Node{}, 0, injectorFailed(injector, "a model link points to a missing node", "node", cur.Node)
		}
		switch {
		case strings.Contains(n.ClassType, "CheckpointLoader"):
			return n, styleCheckpoint, nil
		case n.ClassType == "UNETLoader":
			return n, styleSplit, nil
		}
		next, ok := n.Link("model")
		if !ok {
			return Node{}, 0, injectorFailed(injector, "no checkpoint or UNET loader upstream of the sampler", "node", n.ID, "class_type", n.ClassType)
		}
		cur = next
	}
	return Node{}, 0, injectorFailed(injector, "the model chain loops")
}

// clipSource finds the node output that every text encoder of the graph reads
// its CLIP from, following the `clip` inputs upstream through nodes that pass
// CLIP on (CLIPSetLastLayer). It reports false when there is no such source or
// when the encoders read from more than one.
func clipSource(g Graph) (Link, bool) {
	var root Link
	found := false
	for _, n := range g.Nodes() {
		cur, ok := n.Link("clip")
		if !ok {
			continue
		}
		seen := map[string]bool{}
		for !seen[cur.Node] {
			seen[cur.Node] = true
			up, err := g.Node(cur.Node)
			if err != nil {
				return Link{}, false
			}
			next, ok := up.Link("clip")
			if !ok {
				break
			}
			cur = next
		}
		if found && cur != root {
			return Link{}, false
		}
		root, found = cur, true
	}
	return root, found
}

// InjectLoRAs chains one LoRA loader per LoRA after the model loader and
// rewires every consumer of the loader's outputs to the last one (SPEC §8.2,
// step 5). A LoRA that is listed twice, even spelled differently, is loaded
// once with the strengths of its first entry: a second loader would apply it
// twice.
//
// Every loader is a LoraLoader, which patches MODEL and CLIP with the two
// strengths. A checkpoint-style workflow reads both from the checkpoint; a
// split-style one (UNETLoader + CLIPLoader) reads MODEL from the UNET loader
// and CLIP from the node its text encoders read. Only when no single CLIP
// source can be found are LoraLoaderModelOnly nodes used, and strength_clip is
// then not applied.
func InjectLoRAs(g Graph, loras []LoRA) error {
	if len(loras) == 0 {
		return nil
	}
	loader, style, err := modelLoader(g, "LoRA")
	if err != nil {
		return err
	}
	model, clip, withClip := Link{loader.ID, 0}, Link{loader.ID, 1}, true
	if style == styleSplit {
		clip, withClip = clipSource(g)
	}
	e := newEditor(g)
	var loaded []string
	for _, l := range loras {
		if slices.ContainsFunc(loaded, func(n string) bool { return SameLoRA(n, l.Name) }) {
			continue
		}
		loaded = append(loaded, l.Name)
		name := loraFile(l.Name)
		title := "BOS LoRA " + name
		var id string
		if withClip {
			id, err = e.add("LoraLoader", title, map[string]any{
				"lora_name": name, "strength_model": l.StrengthModel, "strength_clip": l.StrengthClip,
				"model": model.value(), "clip": clip.value(),
			})
		} else {
			id, err = e.add("LoraLoaderModelOnly", title, map[string]any{
				"lora_name": name, "strength_model": l.StrengthModel, "model": model.value(),
			})
		}
		if err != nil {
			return err
		}
		if err := e.redirect(e.consumers(model.Node, model.Index, id), Link{id, 0}); err != nil {
			return err
		}
		model = Link{id, 0}
		if withClip {
			if err := e.redirect(e.consumers(clip.Node, clip.Index, id), Link{id, 1}); err != nil {
				return err
			}
			clip = Link{id, 1}
		}
	}
	return nil
}
