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
	return setNodeInput(g, n, input, value)
}

func setNodeInput(g Graph, n Node, input string, value any) error {
	if _, err := comfyui.EditWorkflowByNodeID(g, n.ID, input, value); err != nil {
		return errors.NewErr("InjectorFailed", err).SetMetadata("reason", "cannot edit node", "node", n.ID, "input", input)
	}
	return nil
}

// Inputs of a text node that hold a prompt. Most text encoders have `text`;
// some, like the Qwen Image 2.1 one, have `prompt` and also hold the negative
// prompt, in `negative_prompt`.
const (
	inputPositiveText = "text"
	inputPositiveAlt  = "prompt"
	inputNegativeAlt  = "negative_prompt"
)

// promptInput is the input of a node that takes a prompt of its own: `text`, or
// else `prompt`.
func promptInput(n Node) (string, bool) {
	for _, name := range []string{inputPositiveText, inputPositiveAlt} {
		if _, ok := n.Inputs[name]; ok {
			return name, true
		}
	}
	return "", false
}

func hasNegativeInput(n Node) bool {
	_, ok := n.Inputs[inputNegativeAlt]
	return ok
}

// NegativeGoesWithPositive reports whether the node with this title takes the
// negative prompt itself, in a `negative_prompt` input, so that no separate
// negative node is needed.
func NegativeGoesWithPositive(g Graph, positiveTitle string) bool {
	if strings.TrimSpace(positiveTitle) == "" {
		return false
	}
	n, err := FindByTitle(g, positiveTitle)
	return err == nil && hasNegativeInput(n)
}

// noPromptInput is the failure of a node that has nowhere to put a prompt.
func noPromptInput(n Node, want string) error {
	return errors.New("InjectorFailed", "the node has no input for the prompt: clear the prompt node title of the generator or fixer, or choose a text encoder",
		"reason", "no prompt input", "node", n.ID, "class_type", n.ClassType, "title", n.Title, "wanted", want)
}

// SetPrompts writes the positive and negative prompt into the nodes with the
// configured titles (SPEC §8.2, step 1). An empty title leaves that prompt out,
// except that a positive node with a `negative_prompt` input takes the negative
// prompt too. A node that has no input for its prompt is an error: the prompt
// would otherwise go nowhere and ComfyUI would run the text in the file. The
// exception is a negative node without a text input when the negative prompt is
// empty: there is no text to lose.
func SetPrompts(g Graph, positiveTitle, positive, negativeTitle, negative string) error {
	if err := CheckTitles(g, positiveTitle, negativeTitle); err != nil {
		return err
	}
	var pos *Node
	if strings.TrimSpace(positiveTitle) != "" {
		n, err := FindByTitle(g, positiveTitle)
		if err != nil {
			return err
		}
		input, ok := promptInput(n)
		if !ok {
			return noPromptInput(n, inputPositiveText+" or "+inputPositiveAlt)
		}
		if err := setNodeInput(g, n, input, positive); err != nil {
			return err
		}
		pos = &n
	}
	if strings.TrimSpace(negativeTitle) == "" {
		if pos != nil && hasNegativeInput(*pos) {
			return setNodeInput(g, *pos, inputNegativeAlt, negative)
		}
		return nil
	}
	n, err := FindByTitle(g, negativeTitle)
	if err != nil {
		return err
	}
	switch input, ok := promptInput(n); {
	case pos != nil && n.ID == pos.ID && hasNegativeInput(n):
		// One node for both prompts: its second input takes the negative one.
		return setNodeInput(g, n, inputNegativeAlt, negative)
	case pos != nil && n.ID == pos.ID:
		return noPromptInput(n, inputNegativeAlt)
	case ok:
		return setNodeInput(g, n, input, negative)
	case strings.TrimSpace(negative) == "":
		// A node that takes no text (a ConditioningZeroOut) has nothing to lose
		// when the job has no negative prompt.
		return nil
	default:
		return noPromptInput(n, inputPositiveText+" or "+inputPositiveAlt)
	}
}
