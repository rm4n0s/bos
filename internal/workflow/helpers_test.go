package workflow

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rm4n0s/errors"
)

var update = flag.Bool("update", false, "rewrite the golden files")

const examplesDir = "../../examples/workflows"

// example loads one of the example workflows shipped with the project.
func example(t *testing.T, name string) Graph {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(examplesDir, name))
	if err != nil {
		t.Fatal(err)
	}
	g, err := Parse(data)
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return g
}

func exampleNames(t *testing.T, dir string) []string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(examplesDir, dir, "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatalf("no examples in %s", dir)
	}
	names := make([]string, len(files))
	for i, f := range files {
		names[i] = dir + "/" + filepath.Base(f)
	}
	return names
}

func mustParse(t *testing.T, text string) Graph {
	t.Helper()
	g, err := Parse([]byte(text))
	if err != nil {
		t.Fatal(err)
	}
	return g
}

func check(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// wantTag asserts that err is an application error with the tag, raised under
// entry.
func wantTag(t *testing.T, err error, entry, tag string) {
	t.Helper()
	_ = wantTagMeta(t, err, entry, tag) // the error itself is not needed here
}

// wantTagMeta is wantTag for tests that go on to look at the error.
func wantTagMeta(t *testing.T, err error, entry, tag string) *errors.Error {
	t.Helper()
	appErr, ok := errors.FromError(err)
	if !ok {
		t.Fatalf("expected *errors.Error, got %T (%v)", err, err)
	}
	if appErr.Tag != tag || !appErr.HasRoute(entry) || !strings.HasSuffix(appErr.Route(), "."+tag) {
		t.Errorf("unexpected failure path: %s (tag %s)\nwant %s ... %s", appErr.Route(), appErr.Tag, entry, tag)
	}
	return appErr
}

// node decodes a node or fails the test.
func node(t *testing.T, g Graph, id string) Node {
	t.Helper()
	n, err := g.Node(id)
	check(t, err)
	return n
}

// link returns the link an input holds or fails the test.
func link(t *testing.T, g Graph, id, input string) Link {
	t.Helper()
	l, ok := node(t, g, id).Link(input)
	if !ok {
		t.Fatalf("node %s input %s is not a link: %s", id, input, g[id])
	}
	return l
}

// assertWellFormed checks that every link points at an existing node and that
// the graph has no cycle, which ComfyUI would reject.
func assertWellFormed(t *testing.T, g Graph) {
	t.Helper()
	deps := map[string][]string{}
	for _, n := range g.Nodes() {
		if n.ClassType == "" {
			t.Fatalf("node %s has no class_type", n.ID)
		}
		for input, raw := range n.Inputs {
			if l, ok := linkOf(raw); ok {
				if _, exists := g[l.Node]; !exists {
					t.Fatalf("node %s input %s links to missing node %s", n.ID, input, l.Node)
				}
				if l.Index < 0 {
					t.Fatalf("node %s input %s links to output %d", n.ID, input, l.Index)
				}
				deps[n.ID] = append(deps[n.ID], l.Node)
			}
		}
	}
	state := map[string]int{} // 1 visiting, 2 done
	var visit func(id string)
	visit = func(id string) {
		switch state[id] {
		case 1:
			t.Fatalf("the graph has a cycle through node %s", id)
		case 2:
			return
		}
		state[id] = 1
		for _, d := range deps[id] {
			visit(d)
		}
		state[id] = 2
	}
	for id := range g {
		visit(id)
	}
}

// golden compares the graph with testdata/golden/<name>.json, or rewrites that
// file with -update.
func golden(t *testing.T, name string, g Graph) {
	t.Helper()
	got, err := g.MarshalIndent()
	check(t, err)
	got = append(got, '\n')
	path := filepath.Join("testdata", "golden", name+".json")
	if *update {
		check(t, os.MkdirAll(filepath.Dir(path), 0o750))
		check(t, os.WriteFile(path, got, 0o600))
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run the tests with -update to create it)", err)
	}
	if string(got) != string(want) {
		t.Errorf("%s differs from the golden file %s (run with -update to accept)\n--- got ---\n%s", name, path, got)
	}
}

// classes counts the nodes of each class.
func classes(g Graph) map[string]int {
	out := map[string]int{}
	for _, n := range g.Nodes() {
		out[n.ClassType]++
	}
	return out
}

func raw(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	check(t, err)
	return b
}
