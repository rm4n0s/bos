package workflow

import (
	"encoding/json"
	"slices"
)

// PromptSource is the node a sampler's prompt comes from, as the graph stands:
// what ComfyUI would encode, which is not always the node a title names.
type PromptSource struct {
	Node  string
	Class string
	Title string
	// Texts are the literal text inputs of the node, by input name.
	Texts map[string]string
	// Linked lists the text inputs that are filled by another node, so their
	// text is not in this node.
	Linked []string
}

// SamplerPrompts are the positive and negative prompt of one sampler.
type SamplerPrompts struct {
	Sampler            string
	Positive, Negative PromptSource
}

// maxConditioningHops bounds the walk from a sampler to its text encoder; a
// loop in a malformed graph ends there.
const maxConditioningHops = 16

// EffectivePrompts finds, for every sampler, the node that its positive and
// negative conditioning ends in and the text that node holds. It is for
// diagnosis: it shows what a workflow will encode after the prompts were
// written, whichever node they were written into. A prompt that cannot be
// traced stays empty.
func EffectivePrompts(g Graph) []SamplerPrompts {
	var out []SamplerPrompts
	for _, s := range g.samplers() {
		out = append(out, SamplerPrompts{
			Sampler:  s.ID,
			Positive: g.promptSource(s, "positive"),
			Negative: g.promptSource(s, "negative"),
		})
	}
	return out
}

// promptSource follows an input of a node through the nodes that pass
// conditioning along (ControlNet, combine, …) to the node that holds text.
func (g Graph) promptSource(from Node, input string) PromptSource {
	cur, in := from, input
	for range maxConditioningHops {
		l, ok := cur.Link(in)
		if !ok {
			return PromptSource{}
		}
		next, err := g.Node(l.Node)
		if err != nil {
			return PromptSource{}
		}
		if src := sourceOf(next); src.Texts != nil || src.Linked != nil {
			return src
		}
		// Not a text node: it passes conditioning on through the same input.
		cur = next
	}
	return PromptSource{}
}

// sourceOf reads the text inputs of a node.
func sourceOf(n Node) PromptSource {
	src := PromptSource{Node: n.ID, Class: n.ClassType, Title: n.Title}
	for name, raw := range n.Inputs {
		var text string
		switch {
		case json.Unmarshal(raw, &text) == nil && isTextInput(name):
			if src.Texts == nil {
				src.Texts = map[string]string{}
			}
			src.Texts[name] = text
		case isTextInput(name):
			if _, ok := linkOf(raw); ok {
				src.Linked = append(src.Linked, name)
			}
		}
	}
	slices.Sort(src.Linked)
	return src
}

// isTextInput says whether an input of a text encoder holds prompt text.
func isTextInput(name string) bool {
	switch name {
	case "text", "text_g", "text_l", "prompt", "negative_prompt":
		return true
	}
	return false
}
