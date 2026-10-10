// Package workflow edits ComfyUI API-format graphs (SPEC §8.2, §18.5): it finds
// nodes by title, applies prompts, output attributes and seeds, injects LoRAs,
// IP-Adapters, ControlNets and regions, and analyses what a workflow produces
// (SPEC §3.2).
//
// All editing functions change the graph they are given. The scheduler edits a
// per-job copy made with Graph.Clone, never the stored workflow.
package workflow

import (
	"bytes"
	"encoding/json"
	"slices"
	"strconv"
	"strings"

	comfyui "github.com/rm4n0s/comfyui-client-go"
	"github.com/rm4n0s/errors"
)

// Graph is a ComfyUI workflow in API format: a map of node id to
// {class_type, inputs, _meta.title}. Nodes are kept as the JSON they came as, so
// fields this package does not know and integers too large for float64 survive
// an edit.
type Graph map[string]json.RawMessage

// Parse reads a workflow from JSON. It must be an object of nodes, each with a
// class_type.
func Parse(data []byte) (Graph, error) {
	var g Graph
	dec := json.NewDecoder(bytes.NewReader(data))
	if err := dec.Decode(&g); err != nil {
		return nil, errors.NewErr("WorkflowInvalid", err).SetMetadata("reason", "not a JSON object of nodes")
	}
	if len(g) == 0 {
		return nil, errors.New("WorkflowInvalid", "the workflow has no nodes", "reason", "empty")
	}
	for _, id := range g.IDs() {
		n, err := g.Node(id)
		if err != nil {
			return nil, err
		}
		if n.ClassType == "" {
			return nil, errors.New("WorkflowInvalid", "a node has no class_type", "node", id, "reason", "no class_type")
		}
	}
	return g, nil
}

// Clone returns an independent copy.
func (g Graph) Clone() Graph {
	out := make(Graph, len(g))
	for id, raw := range g {
		out[id] = slices.Clone(raw)
	}
	return out
}

// MarshalIndent writes the graph with sorted keys, for stored and golden files.
func (g Graph) MarshalIndent() ([]byte, error) {
	b, err := json.MarshalIndent(map[string]json.RawMessage(g), "", "  ")
	if err != nil {
		return nil, errors.NewErr("WorkflowInvalid", err).SetMetadata("reason", "cannot encode")
	}
	return b, nil
}

// Node is a decoded view of one graph node.
type Node struct {
	ID        string
	ClassType string
	Title     string
	Inputs    map[string]json.RawMessage
}

type nodeJSON struct {
	ClassType string `json:"class_type"`
	Meta      struct {
		Title string `json:"title"`
	} `json:"_meta"`
	Inputs map[string]json.RawMessage `json:"inputs"`
}

// Label is the name a node can be found by: its title, or its class when it
// has no title. The client library matches both (SPEC §18.5).
func (n Node) Label() string {
	if n.Title != "" {
		return n.Title
	}
	return n.ClassType
}

// Node decodes the node with the given id.
func (g Graph) Node(id string) (Node, error) {
	raw, ok := g[id]
	if !ok {
		return Node{}, errors.New("WorkflowInvalid", "the workflow has no such node", "node", id, "reason", "missing node")
	}
	var n nodeJSON
	if err := json.Unmarshal(raw, &n); err != nil {
		return Node{}, errors.NewErr("WorkflowInvalid", err).SetMetadata("node", id, "reason", "malformed node")
	}
	return Node{ID: id, ClassType: strings.TrimSpace(n.ClassType), Title: strings.TrimSpace(n.Meta.Title), Inputs: n.Inputs}, nil
}

// IDs returns the node ids in a fixed order: numeric ids first in numeric
// order, then the others alphabetically (the order of the client library).
func (g Graph) IDs() []string {
	ids := make([]string, 0, len(g))
	for id := range g {
		ids = append(ids, id)
	}
	slices.SortFunc(ids, compareIDs)
	return ids
}

func isNumeric(id string) bool {
	if id == "" {
		return false
	}
	for _, r := range id {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func compareIDs(a, b string) int {
	na, nb := isNumeric(a), isNumeric(b)
	switch {
	case na && nb:
		ta, tb := strings.TrimLeft(a, "0"), strings.TrimLeft(b, "0")
		if len(ta) != len(tb) {
			return len(ta) - len(tb)
		}
		if c := strings.Compare(ta, tb); c != 0 {
			return c
		}
		return strings.Compare(a, b)
	case na:
		return -1
	case nb:
		return 1
	}
	return strings.Compare(a, b)
}

// Nodes decodes every node, in id order. Nodes that cannot be decoded are
// skipped; Parse has already rejected them.
func (g Graph) Nodes() []Node {
	var out []Node
	for _, id := range g.IDs() {
		if n, err := g.Node(id); err == nil {
			out = append(out, n)
		}
	}
	return out
}

// ---- links ----

// Link is a reference to an output of another node: ["id", index].
type Link struct {
	Node  string
	Index int
}

func (l Link) value() []any { return []any{l.Node, l.Index} }

// linkOf reads an input value as a link. A literal value is not a link.
func linkOf(raw json.RawMessage) (Link, bool) {
	var parts []json.RawMessage
	if json.Unmarshal(raw, &parts) != nil || len(parts) != 2 {
		return Link{}, false
	}
	var id string
	var idx int
	if json.Unmarshal(parts[0], &id) != nil || json.Unmarshal(parts[1], &idx) != nil {
		return Link{}, false
	}
	return Link{Node: id, Index: idx}, true
}

// Link returns the link an input holds, or false for a missing or literal input.
func (n Node) Link(input string) (Link, bool) {
	raw, ok := n.Inputs[input]
	if !ok {
		return Link{}, false
	}
	return linkOf(raw)
}

// ---- editing ----

// editor adds and rewires nodes, allocating new ids above the largest numeric
// id of the graph (SPEC §18.5).
type editor struct {
	g    Graph
	next int
}

func newEditor(g Graph) *editor {
	return &editor{g: g, next: comfyui.MaxNumericNodeID(g) + 1}
}

// add inserts a node and returns its id.
func (e *editor) add(classType, title string, inputs map[string]any) (string, error) {
	id := strconv.Itoa(e.next)
	e.next++
	node := map[string]any{"class_type": classType, "inputs": inputs, "_meta": map[string]string{"title": title}}
	raw, err := json.Marshal(node)
	if err != nil {
		return "", errors.NewErr("InjectorFailed", err).SetMetadata("reason", "cannot encode node", "class_type", classType)
	}
	e.g[id] = raw
	return id, nil
}

// set writes one input of an existing node.
func (e *editor) set(id, key string, value any) error {
	if _, err := comfyui.EditWorkflowByNodeID(e.g, id, key, value); err != nil {
		return errors.NewErr("InjectorFailed", err).SetMetadata("reason", "cannot edit node", "node", id, "input", key)
	}
	return nil
}

// inputRef names one input of one node.
type inputRef struct {
	Node  string
	Input string
}

// consumers lists the inputs that read output index of node src (any output
// when index is negative), skipping the nodes in except.
func (e *editor) consumers(src string, index int, except ...string) []inputRef {
	var refs []inputRef
	for _, n := range e.g.Nodes() {
		if slices.Contains(except, n.ID) {
			continue
		}
		keys := make([]string, 0, len(n.Inputs))
		for k := range n.Inputs {
			keys = append(keys, k)
		}
		slices.Sort(keys)
		for _, k := range keys {
			if l, ok := linkOf(n.Inputs[k]); ok && l.Node == src && (index < 0 || l.Index == index) {
				refs = append(refs, inputRef{Node: n.ID, Input: k})
			}
		}
	}
	return refs
}

// redirect points the given inputs at a new source.
func (e *editor) redirect(refs []inputRef, to Link) error {
	for _, r := range refs {
		if err := e.set(r.Node, r.Input, to.value()); err != nil {
			return err
		}
	}
	return nil
}

// ---- samplers ----

const (
	classKSampler         = "KSampler"
	classKSamplerAdvanced = "KSamplerAdvanced"
)

func isSampler(class string) bool { return class == classKSampler || class == classKSamplerAdvanced }

// samplers returns the sampler nodes in id order.
func (g Graph) samplers() []Node {
	var out []Node
	for _, n := range g.Nodes() {
		if isSampler(n.ClassType) {
			out = append(out, n)
		}
	}
	return out
}

// samplersSharing returns the samplers whose input holds the same link as the
// first sampler's, so an injected node reaches every pass that used the
// original (for example both samplers of a hires workflow).
func (g Graph) samplersSharing(first Node, input string) []Node {
	want, ok := first.Link(input)
	if !ok {
		return []Node{first}
	}
	var out []Node
	for _, s := range g.samplers() {
		if l, ok := s.Link(input); ok && l == want {
			out = append(out, s)
		}
	}
	return out
}

func noSampler(injector string) *errors.Error {
	return errors.New("NoSamplerInWorkflow", "the workflow has no KSampler or KSamplerAdvanced node", "injector", injector)
}
