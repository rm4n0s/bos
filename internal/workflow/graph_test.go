package workflow

import (
	"reflect"
	"testing"
)

func TestParse(t *testing.T) {
	tests := []struct {
		name string
		in   string
		ok   bool
	}{
		{"minimal", `{"1":{"class_type":"A","inputs":{}}}`, true},
		{"not JSON", `{`, false},
		{"array", `[]`, false},
		{"empty object", `{}`, false},
		{"node without class_type", `{"1":{"inputs":{}}}`, false},
		{"node is not an object", `{"1":5}`, false},
		{"empty class_type", `{"1":{"class_type":"  "}}`, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g, err := Parse([]byte(tt.in))
			if tt.ok {
				check(t, err)
				if len(g) != 1 {
					t.Errorf("got %d nodes", len(g))
				}
				return
			}
			wantTag(t, err, "Parse", "WorkflowInvalid")
		})
	}
}

func TestParseKeepsExamples(t *testing.T) {
	for _, dir := range []string{"creators", "fixers"} {
		for _, name := range exampleNames(t, dir) {
			g := example(t, name)
			assertWellFormed(t, g)
		}
	}
}

func TestIDsOrder(t *testing.T) {
	g := mustParse(t, `{"10":{"class_type":"A"},"2":{"class_type":"A"},"4b":{"class_type":"A"},"4a":{"class_type":"A"},"1":{"class_type":"A"},"007":{"class_type":"A"},"7":{"class_type":"A"}}`)
	want := []string{"1", "2", "007", "7", "10", "4a", "4b"}
	if got := g.IDs(); !reflect.DeepEqual(got, want) {
		t.Errorf("IDs = %v, want %v", got, want)
	}
}

func TestNodeAndLinks(t *testing.T) {
	g := mustParse(t, `{"1":{"class_type":" K ","_meta":{"title":" T "},"inputs":{"a":["2",1],"b":5,"c":["x"],"d":[2,1],"e":["2","1"],"f":"text","seed":18446744073709551615}}}`)
	n := node(t, g, "1")
	if n.ClassType != "K" || n.Title != "T" {
		t.Errorf("node = %+v", n)
	}
	if l, ok := n.Link("a"); !ok || l != (Link{"2", 1}) {
		t.Errorf("a = %v, %v", l, ok)
	}
	for _, in := range []string{"b", "c", "d", "e", "f", "missing"} {
		if _, ok := n.Link(in); ok {
			t.Errorf("%s was read as a link", in)
		}
	}
	_, err := g.Node("9")
	wantTag(t, err, "Node", "WorkflowInvalid")
	g["bad"] = raw(t, []int{1})
	_, err = g.Node("bad")
	wantTag(t, err, "Node", "WorkflowInvalid")
	if len(g.Nodes()) != 1 {
		t.Error("Nodes must skip nodes that cannot be decoded")
	}
}

func TestCloneIsIndependent(t *testing.T) {
	g := example(t, "creators/illustrious_xl.json")
	before, err := g.MarshalIndent()
	check(t, err)
	c := g.Clone()
	check(t, SetSeed(c, 42))
	check(t, InjectLoRAs(c, []LoRA{{Name: "x", StrengthModel: 1, StrengthClip: 1}}))
	after, err := g.MarshalIndent()
	check(t, err)
	if string(before) != string(after) {
		t.Error("editing the clone changed the original")
	}
	if len(c) <= len(g) {
		t.Error("the clone was not edited")
	}
}

func TestEditorAllocatesAboveTheLargestNumericID(t *testing.T) {
	g := mustParse(t, `{"4a":{"class_type":"A"},"9":{"class_type":"A"},"x":{"class_type":"A"}}`)
	e := newEditor(g)
	id, err := e.add("B", "t", map[string]any{})
	check(t, err)
	id2, err := e.add("B", "t", map[string]any{})
	check(t, err)
	if id != "10" || id2 != "11" {
		t.Errorf("ids = %s, %s", id, id2)
	}
}
