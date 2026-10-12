package workflow

import (
	"path"
	"strings"

	"github.com/rm4n0s/bos/internal/lorainjector"
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

func injectorFailed(injector, reason string, kv ...any) *errors.Error {
	return errors.New("InjectorFailed", "cannot inject "+injector+": "+reason, append([]any{"injector", injector, "reason", reason}, kv...)...)
}

// InjectLoRAs adds the LoRAs to the workflow with internal/lorainjector, which
// does the work (SPEC §8.2, step 5): a checkpoint workflow gets a LoraLoader
// for each, a split one (UNETLoader + CLIPLoader) a LoraLoaderModelOnly, chained
// in the order given, and the readers of the model (and, for a checkpoint, the
// CLIP) are rewired to the last one. A name without ".safetensors" gets it. The
// graph is edited in place. A LoRA listed twice is loaded twice here: the job
// plan drops the repeats before it gets this far.
func InjectLoRAs(g Graph, loras []LoRA) error {
	if len(loras) == 0 {
		return nil
	}
	configs := make([]lorainjector.LoRAConfig, len(loras))
	for i, l := range loras {
		configs[i] = lorainjector.LoRAConfig{Name: l.Name, StrengthModel: l.StrengthModel, StrengthCLIP: l.StrengthClip}
	}
	_, err := lorainjector.NewLoRAInjector(g).AddMultipleLoRAs(configs, "")
	return err
}
