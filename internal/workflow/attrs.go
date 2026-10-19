package workflow

import (
	"math"
	"slices"

	"github.com/rm4n0s/bos/internal/domain"
	comfyui "github.com/rm4n0s/comfyui-client-go"
	"github.com/rm4n0s/errors"
)

// attrValue is what is written into a node input for an attribute.
func attrValue(a domain.Attr) any {
	switch a.Kind {
	case domain.AttrInt:
		return a.Int
	case domain.AttrDecimal:
		return a.Float
	}
	return a.Text
}

// samplerKeys are the attributes that go to samplers. A KSamplerAdvanced has no
// `seed` (its input is `noise_seed`) and no `denoise`.
var samplerKeys = []string{"steps", "cfg", "sampler_name", "scheduler", "denoise", "seed"}

func samplerInput(class, key string) (string, bool) {
	if class == classKSamplerAdvanced {
		switch key {
		case "seed":
			return "noise_seed", true
		case "denoise":
			return "", false
		}
	}
	return key, true
}

// ApplyAttributes writes the effective output attributes into the graph
// (SPEC §8.2, step 2): steps, cfg, sampler_name, scheduler, denoise and seed
// into every sampler (for a SamplerCustomAdvanced, into the noise, guider,
// sampler-select and scheduler nodes it reads), and width, height and batch size into the empty-latent
// nodes; for video, also the frame count into the video latent nodes and the
// frame rate into the video output node. Only attributes that are present are
// written. A graph without a sampler fails with NoSamplerInWorkflow when a
// sampler attribute was asked for.
func ApplyAttributes(g Graph, attrs domain.OutputAttributes) error {
	needSampler := slices.ContainsFunc(samplerKeys, func(k string) bool { _, ok := attrs[k]; return ok })
	slots := g.samplerSlots()
	if needSampler && len(slots) == 0 {
		return noSampler("attributes")
	}
	for _, sampler := range slots {
		for _, key := range slices.Concat(samplerKeys, sigmasSizeInputs) {
			a, ok := attrs[key]
			s, applies := sampler[key]
			if !ok || !applies {
				continue
			}
			if _, err := comfyui.EditWorkflowByNodeID(g, s.Node, s.Input, attrValue(a)); err != nil {
				return errors.NewErr("InjectorFailed", err).SetMetadata("reason", "cannot edit sampler", "node", s.Node, "input", s.Input)
			}
		}
	}

	frames, hasFrames := frameCount(attrs)
	for _, n := range g.Nodes() {
		isImage := slices.Contains(imageLatentNodes, n.ClassType)
		isVideo := slices.Contains(videoLatentNodes, n.ClassType)
		if isImage || isVideo {
			for _, key := range []string{"width", "height"} {
				if a, ok := attrs[key]; ok {
					if err := editInput(g, n.ID, key, attrValue(a)); err != nil {
						return err
					}
				}
			}
		}
		if isImage {
			if a, ok := attrs["batch_size"]; ok {
				if err := editInput(g, n.ID, "batch_size", attrValue(a)); err != nil {
					return err
				}
			}
		}
		if isVideo && hasFrames {
			for _, key := range frameInputs {
				if _, present := n.Inputs[key]; present {
					if err := editInput(g, n.ID, key, frames); err != nil {
						return err
					}
					break
				}
			}
		}
		if n.ClassType == "VHS_VideoCombine" {
			if a, ok := attrs["fps"]; ok {
				if err := editInput(g, n.ID, "frame_rate", attrValue(a)); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// frameCount returns the number of frames to write: num_frames if given,
// otherwise duration_seconds × fps rounded.
func frameCount(attrs domain.OutputAttributes) (uint64, bool) {
	if a, ok := attrs["num_frames"]; ok && a.Kind == domain.AttrInt {
		return a.Int, true
	}
	d, hasD := attrs["duration_seconds"]
	f, hasF := attrs["fps"]
	if hasD && hasF && num(d) > 0 && num(f) > 0 {
		return uint64(math.Max(1, math.Round(num(d)*num(f)))), true
	}
	return 0, false
}

func editInput(g Graph, id, key string, value any) error {
	if _, err := comfyui.EditWorkflowByNodeID(g, id, key, value); err != nil {
		return errors.NewErr("InjectorFailed", err).SetMetadata("reason", "cannot edit node", "node", id, "input", key)
	}
	return nil
}

// SetSeed writes a seed into every sampler: `seed` of a KSampler and
// `noise_seed` of a KSamplerAdvanced or of the RandomNoise node a
// SamplerCustomAdvanced reads. A 64-bit seed keeps its exact value. It
// fails with NoSamplerInWorkflow when there is no sampler (SPEC §18.5).
func SetSeed(g Graph, seed uint64) error {
	slots := g.samplerSlots()
	if len(slots) == 0 {
		return noSampler("seed")
	}
	for _, sampler := range slots {
		if s, ok := sampler["seed"]; ok {
			if err := editInput(g, s.Node, s.Input, seed); err != nil {
				return err
			}
		}
	}
	return nil
}
