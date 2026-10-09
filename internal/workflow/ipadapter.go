package workflow

import "strconv"

// IPAdapter is one IP-Adapter reference to apply. Image is the value for the
// LoadImage node: the name ComfyUI gave the uploaded file (SPEC §18.6).
type IPAdapter struct {
	Image           string
	Weight          float64
	WeightType      string
	StartAt         float64
	EndAt           float64
	ClipVisionModel string
	ModelName       string
}

// Defaults of the original application for an IP-Adapter left unconfigured.
const (
	defaultClipVision = "CLIP-ViT-H-14-laion2B-s32B-b79K.safetensors"
	defaultIPAdapter  = "ip-adapter_sd15.bin"
	defaultWeightType = "linear"
)

// InjectIPAdapters adds, for each adapter, a CLIP-vision loader, an image
// loader, an IP-Adapter model loader and an advanced IP-Adapter node, chained
// on the model (SPEC §8.2, step 4). The samplers that used the original model
// are pointed at the end of the chain.
func InjectIPAdapters(g Graph, adapters []IPAdapter) error {
	if len(adapters) == 0 {
		return nil
	}
	samplers := g.samplers()
	if len(samplers) == 0 {
		return noSampler("IP-Adapter")
	}
	first := samplers[0]
	current, ok := first.Link("model")
	if !ok {
		return injectorFailed("IP-Adapter", "the sampler's model input is not linked", "node", first.ID)
	}
	targets := g.samplersSharing(first, "model")

	e := newEditor(g)
	for i, a := range adapters {
		n := strconv.Itoa(i + 1)
		clipModel, model, weightType := a.ClipVisionModel, a.ModelName, a.WeightType
		if clipModel == "" {
			clipModel = defaultClipVision
		}
		if model == "" {
			model = defaultIPAdapter
		}
		if weightType == "" {
			weightType = defaultWeightType
		}
		end := a.EndAt
		if a.StartAt == 0 && end == 0 {
			end = 1 // a window of zero length would apply nothing
		}
		clipVision, err := e.add("CLIPVisionLoader", "BOS CLIP Vision "+n, map[string]any{"clip_name": clipModel})
		if err != nil {
			return err
		}
		image, err := e.add("LoadImage", "BOS IPAdapter Reference "+n, map[string]any{"image": a.Image})
		if err != nil {
			return err
		}
		loader, err := e.add("IPAdapterModelLoader", "BOS IPAdapter Model "+n, map[string]any{"ipadapter_file": model})
		if err != nil {
			return err
		}
		apply, err := e.add("IPAdapterAdvanced", "BOS Apply IPAdapter "+n, map[string]any{
			"weight": a.Weight, "weight_type": weightType, "start_at": a.StartAt, "end_at": end,
			"model": current.value(), "ipadapter": Link{loader, 0}.value(), "image": Link{image, 0}.value(),
			"clip_vision": Link{clipVision, 0}.value(), "embeds_scaling": "V only", "combine_embeds": "concat",
		})
		if err != nil {
			return err
		}
		current = Link{apply, 0}
	}
	for _, s := range targets {
		if err := e.set(s.ID, "model", current.value()); err != nil {
			return err
		}
	}
	return nil
}
