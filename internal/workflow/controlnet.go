package workflow

import "strconv"

// ControlNet is one ControlNet to apply. Image is the value for the LoadImage
// node (SPEC §18.6). With IsReference the image is preprocessed first; without
// it the image is used as it is.
type ControlNet struct {
	Type        string
	Image       string
	IsReference bool
	ModelName   string
	Strength    float64
}

// preprocessors maps a ControlNet type to its preprocessor node (SPEC §8.2,
// step 6); tile has none.
var preprocessors = map[string]string{
	"openpose": "OpenposePreprocessor", "dwpose": "DWPreprocessor", "canny": "CannyEdgePreprocessor",
	"midas": "MidasDepthMapPreprocessor", "depth": "DepthAnythingPreprocessor", "tile": "",
	"lineart": "LineArtPreprocessor", "softedge": "HEDPreprocessor", "scribble": "ScribblePreprocessor",
	"normal": "BAE-NormalMapPreprocessor",
}

// preprocessorInputs are the default settings of the preprocessors that need
// them.
func preprocessorInputs(typ string, image Link) map[string]any {
	in := map[string]any{"image": image.value()}
	switch typ {
	case "canny":
		in["low_threshold"], in["high_threshold"] = 100, 200
	case "openpose", "dwpose":
		in["detect_hand"], in["detect_body"], in["detect_face"] = "enable", "enable", "enable"
	case "midas":
		in["a"], in["bg_threshold"] = 6.2, 0.1
	}
	return in
}

// InjectControlNets adds, per ControlNet, an image loader, an optional
// preprocessor with a preview of its result, a ControlNet loader and a
// ControlNetApplyAdvanced node, chained between the text encoders and the
// samplers (SPEC §8.2, step 6).
func InjectControlNets(g Graph, controlNets []ControlNet) error {
	if len(controlNets) == 0 {
		return nil
	}
	for _, c := range controlNets {
		if _, known := preprocessors[c.Type]; !known {
			return injectorFailed("ControlNet", "unknown ControlNet type", "type", c.Type)
		}
	}
	samplers := g.samplers()
	if len(samplers) == 0 {
		return noSampler("ControlNet")
	}
	first := samplers[0]
	positive, okP := first.Link("positive")
	negative, okN := first.Link("negative")
	if !okP || !okN {
		return injectorFailed("ControlNet", "the sampler's positive or negative input is not linked", "node", first.ID)
	}
	var targets []Node
	for _, s := range g.samplersSharing(first, "positive") {
		if l, ok := s.Link("negative"); ok && l == negative {
			targets = append(targets, s)
		}
	}

	e := newEditor(g)
	for i, c := range controlNets {
		label := c.Type + " " + strconv.Itoa(i+1)
		image, err := e.add("LoadImage", "BOS ControlNet Image "+label, map[string]any{"image": c.Image})
		if err != nil {
			return err
		}
		source := Link{image, 0}
		if pre := preprocessors[c.Type]; c.IsReference && pre != "" {
			p, err := e.add(pre, "BOS Preprocessor "+label, preprocessorInputs(c.Type, source))
			if err != nil {
				return err
			}
			source = Link{p, 0}
			if _, err := e.add("PreviewImage", "BOS Preview "+label, map[string]any{"images": source.value()}); err != nil {
				return err
			}
		}
		loader, err := e.add("ControlNetLoader", "BOS ControlNet Model "+label, map[string]any{"control_net_name": c.ModelName})
		if err != nil {
			return err
		}
		strength := c.Strength
		if strength <= 0 {
			strength = 1
		}
		apply, err := e.add("ControlNetApplyAdvanced", "BOS Apply ControlNet "+label, map[string]any{
			"strength": strength, "start_percent": 0.0, "end_percent": 1.0,
			"positive": positive.value(), "negative": negative.value(),
			"control_net": Link{loader, 0}.value(), "image": source.value(),
		})
		if err != nil {
			return err
		}
		positive, negative = Link{apply, 0}, Link{apply, 1}
	}
	for _, s := range targets {
		if err := e.set(s.ID, "positive", positive.value()); err != nil {
			return err
		}
		if err := e.set(s.ID, "negative", negative.value()); err != nil {
			return err
		}
	}
	return nil
}
