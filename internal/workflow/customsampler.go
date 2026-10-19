package workflow

// SamplerCustomAdvanced (FLUX.2, Z-Image, SD3 and others) holds none of the
// sampling settings itself: it reads them from the nodes its inputs link to.
// The seed is in the noise node, cfg in the guider, the sampler name in the
// sampler-select node, and steps, scheduler and denoise in the sigmas node.
const classSamplerCustomAdvanced = "SamplerCustomAdvanced"

// customSamplerSources maps the link inputs of a SamplerCustomAdvanced to the
// attributes that live in the node behind them, with the input that holds each.
var customSamplerSources = []struct {
	link   string
	inputs map[string]string
}{
	{"noise", map[string]string{"seed": "noise_seed"}},
	{"guider", map[string]string{"cfg": "cfg"}},
	{"sampler", map[string]string{"sampler_name": "sampler_name"}},
	{"sigmas", map[string]string{"steps": "steps", "scheduler": "scheduler", "denoise": "denoise"}},
}

// sigmasSizeInputs are the size inputs of a scheduler that shapes its sigmas by
// the image size (Flux2Scheduler): they follow width and height.
var sigmasSizeInputs = []string{"width", "height"}

// slot is one input of one node that takes a sampler attribute.
type slot struct {
	Node  string
	Input string
}

// samplerSlots returns, for every sampler of the graph in id order, the inputs
// that take each sampler attribute (`steps`, `cfg`, `sampler_name`,
// `scheduler`, `denoise`, `seed`, and `width` and `height` of a scheduler that
// has them). A KSampler holds its own; a SamplerCustomAdvanced has the ones its
// linked nodes have as literal inputs, so an attribute the graph cannot take
// (a `scheduler` for Flux2Scheduler) has no slot.
func (g Graph) samplerSlots() []map[string]slot {
	var out []map[string]slot
	for _, n := range g.Nodes() {
		switch {
		case isSampler(n.ClassType):
			slots := map[string]slot{}
			for _, key := range samplerKeys {
				if input, ok := samplerInput(n.ClassType, key); ok {
					slots[key] = slot{n.ID, input}
				}
			}
			out = append(out, slots)
		case n.ClassType == classSamplerCustomAdvanced:
			out = append(out, g.customSlots(n))
		}
	}
	return out
}

func (g Graph) customSlots(sampler Node) map[string]slot {
	slots := map[string]slot{}
	for _, src := range customSamplerSources {
		l, ok := sampler.Link(src.link)
		if !ok {
			continue
		}
		n, err := g.Node(l.Node)
		if err != nil {
			continue
		}
		for key, input := range src.inputs {
			if _, has := n.Inputs[input]; has {
				slots[key] = slot{n.ID, input}
			}
		}
		if src.link == "sigmas" {
			for _, input := range sigmasSizeInputs {
				if _, has := n.Inputs[input]; has {
					slots[input] = slot{n.ID, input}
				}
			}
		}
	}
	return slots
}
