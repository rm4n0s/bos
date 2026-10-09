package workflow

import (
	"strings"

	comfyui "github.com/rm4n0s/comfyui-client-go"
	"github.com/rm4n0s/errors"
)

// CheckTitles verifies that every non-empty title names exactly one node
// (SPEC §18.5). The client library matches a title against a node's
// `_meta.title` or its `class_type`, so a title equal to some node's class name
// counts for that node too, and this check uses the same rule.
func CheckTitles(g Graph, titles ...string) error {
	for _, t := range titles {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		switch n := len(comfyui.FindNodeIDsByTitle(g, t)); {
		case n == 0:
			return errors.New("WorkflowTitleNotFound", "no node has this title", "title", t)
		case n > 1:
			return errors.New("WorkflowTitleDuplicated", "more than one node has this title", "title", t, "nodes", comfyui.FindNodeIDsByTitle(g, t))
		}
	}
	return nil
}

// FindByTitle returns the node with a title, which must be unique.
func FindByTitle(g Graph, title string) (Node, error) {
	if err := CheckTitles(g, strings.TrimSpace(title)); err != nil {
		return Node{}, err
	}
	id, ok := comfyui.FindNodeIDByTitle(g, strings.TrimSpace(title))
	if !ok {
		return Node{}, errors.New("WorkflowTitleNotFound", "no node has this title", "title", title)
	}
	return g.Node(id)
}

// SetInput writes one input of the node with a title: the prompt text of a text
// encoder, or the image of a fixer's image-input node.
func SetInput(g Graph, title, input string, value any) error {
	n, err := FindByTitle(g, title)
	if err != nil {
		return err
	}
	if _, err := comfyui.EditWorkflowByNodeID(g, n.ID, input, value); err != nil {
		return errors.NewErr("InjectorFailed", err).SetMetadata("reason", "cannot edit node", "node", n.ID, "input", input)
	}
	return nil
}

// SetPrompts writes the positive and negative prompt into the nodes with the
// configured titles (SPEC §8.2, step 1). An empty title leaves that prompt out.
func SetPrompts(g Graph, positiveTitle, positive, negativeTitle, negative string) error {
	if err := CheckTitles(g, positiveTitle, negativeTitle); err != nil {
		return err
	}
	if strings.TrimSpace(positiveTitle) != "" {
		if err := SetInput(g, positiveTitle, "text", positive); err != nil {
			return err
		}
	}
	if strings.TrimSpace(negativeTitle) != "" {
		if err := SetInput(g, negativeTitle, "text", negative); err != nil {
			return err
		}
	}
	return nil
}
