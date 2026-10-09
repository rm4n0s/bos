package workflow

import (
	"strings"

	"github.com/rm4n0s/errors"
)

// Rect is a rectangle region.
type Rect struct {
	Width, Height, X, Y int
}

// Region is one regional prompt. It has a mask or a rectangle, not both. Mask
// is the value for the LoadImage node (SPEC §18.6).
type Region struct {
	Keyword string
	Mask    string
	Rect    *Rect
	Prompt  string
}

// baseConditioning follows a sampler's positive input back through ControlNet
// nodes to the text-encode node that made it (SPEC §8.2, step 7).
func baseConditioning(g Graph, start Link) (Node, error) {
	cur := start
	seen := map[string]bool{}
	for !seen[cur.Node] {
		seen[cur.Node] = true
		n, err := g.Node(cur.Node)
		if err != nil {
			return Node{}, injectorFailed("region", "the conditioning chain points to a missing node", "node", cur.Node)
		}
		var input string
		switch {
		case strings.Contains(n.ClassType, "TextEncode"):
			return n, nil
		case n.ClassType == "ControlNetApplyAdvanced":
			input = "positive"
		case n.ClassType == "ControlNetApply":
			input = "conditioning"
		default:
			return Node{}, injectorFailed("region", "unsupported node in the conditioning chain", "node", n.ID, "class_type", n.ClassType)
		}
		next, ok := n.Link(input)
		if !ok {
			return Node{}, injectorFailed("region", n.ClassType+" has no linked "+input+" input", "node", n.ID)
		}
		cur = next
	}
	return Node{}, injectorFailed("region", "the conditioning chain loops")
}

// InjectRegions adds a regional prompt per region on top of the positive
// conditioning: a text encode, then a mask (LoadImage → ImageToMask →
// ConditioningSetMask) or an area (ConditioningSetArea), combined with a
// ConditioningCombine chain. Whatever read the base conditioning now reads the
// combined result (SPEC §8.2, step 7). A region with neither a mask nor a
// rectangle, or with both, fails with RegionInvalid.
func InjectRegions(g Graph, regions []Region) error {
	if len(regions) == 0 {
		return nil
	}
	for _, r := range regions {
		hasMask, hasRect := r.Mask != "", r.Rect != nil
		if hasMask == hasRect {
			reason := "has neither a mask nor coordinates"
			if hasMask {
				reason = "has both a mask and coordinates"
			}
			return errors.New("RegionInvalid", "region "+r.Keyword+" "+reason, "keyword", r.Keyword)
		}
	}
	samplers := g.samplers()
	if len(samplers) == 0 {
		return noSampler("region")
	}
	start, ok := samplers[0].Link("positive")
	if !ok {
		return injectorFailed("region", "the sampler's positive input is not linked", "node", samplers[0].ID)
	}
	base, err := baseConditioning(g, start)
	if err != nil {
		return err
	}
	clip, ok := base.Link("clip")
	if !ok {
		return injectorFailed("region", "the base text encoder has no linked clip input", "node", base.ID)
	}
	baseOut := Link{base.ID, 0} // a text encoder has a single output

	e := newEditor(g)
	// Collect the readers of the base conditioning before any node is added.
	readers := e.consumers(baseOut.Node, baseOut.Index)

	current := baseOut
	for _, r := range regions {
		encode, err := e.add(base.ClassType, "BOS Region Prompt "+r.Keyword, map[string]any{"text": r.Prompt, "clip": clip.value()})
		if err != nil {
			return err
		}
		var conditioned string
		if r.Rect == nil {
			load, err := e.add("LoadImage", "BOS Region Mask "+r.Keyword, map[string]any{"image": r.Mask})
			if err != nil {
				return err
			}
			mask, err := e.add("ImageToMask", "BOS Region Mask Channel "+r.Keyword, map[string]any{"image": Link{load, 0}.value(), "channel": "red"})
			if err != nil {
				return err
			}
			conditioned, err = e.add("ConditioningSetMask", "BOS Region Set Mask "+r.Keyword, map[string]any{
				"conditioning": Link{encode, 0}.value(), "mask": Link{mask, 0}.value(), "strength": 1.0, "set_cond_area": "default",
			})
			if err != nil {
				return err
			}
		} else {
			conditioned, err = e.add("ConditioningSetArea", "BOS Region Set Area "+r.Keyword, map[string]any{
				"conditioning": Link{encode, 0}.value(), "width": r.Rect.Width, "height": r.Rect.Height,
				"x": r.Rect.X, "y": r.Rect.Y, "strength": 1.0,
			})
			if err != nil {
				return err
			}
		}
		combine, err := e.add("ConditioningCombine", "BOS Region Combine "+r.Keyword, map[string]any{
			"conditioning_1": current.value(), "conditioning_2": Link{conditioned, 0}.value(),
		})
		if err != nil {
			return err
		}
		current = Link{combine, 0}
	}
	return e.redirect(readers, current)
}
