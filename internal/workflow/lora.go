package workflow

import (
	"path"
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

// InjectLoRAs chains one LoRA loader per LoRA after the model loader and
// rewires every consumer of the loader's outputs to the last one (SPEC §8.2,
// step 5). A checkpoint-style workflow gets LoraLoader nodes that patch MODEL
// and CLIP; a split-style one (UNETLoader + CLIPLoader) gets
// LoraLoaderModelOnly nodes, so strength_clip is not used there.
func InjectLoRAs(g Graph, loras []LoRA) error {
	if len(loras) == 0 {
		return nil
	}
	loader, style, err := modelLoader(g, "LoRA")
	if err != nil {
		return err
	}
	e := newEditor(g)
	src := loader.ID
	for _, l := range loras {
		name := loraFile(l.Name)
		title := "BOS LoRA " + name
		var id string
		outputs := []int{0}
		if style == styleCheckpoint {
			outputs = []int{0, 1} // MODEL and CLIP, not the VAE
			id, err = e.add("LoraLoader", title, map[string]any{
				"lora_name": name, "strength_model": l.StrengthModel, "strength_clip": l.StrengthClip,
				"model": Link{src, 0}.value(), "clip": Link{src, 1}.value(),
			})
		} else {
			id, err = e.add("LoraLoaderModelOnly", title, map[string]any{
				"lora_name": name, "strength_model": l.StrengthModel, "model": Link{src, 0}.value(),
			})
		}
		if err != nil {
			return err
		}
		for _, out := range outputs {
			if err := e.redirect(e.consumers(src, out, id), Link{id, out}); err != nil {
				return err
			}
		}
		src = id
	}
	return nil
}
