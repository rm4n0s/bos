package service

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/rm4n0s/bos/internal/domain"
)

func TestGeneratorIsInferredFromItsWorkflow(t *testing.T) {
	e := newEnv(t)
	w := example(t, "creators", "illustrious_xl.json")

	a, err := e.Generators.Analyze(w)
	check(t, err)
	if a.OutputType != domain.OutputImage || a.OutputNodeClassType != "SaveImage" || !reflect.DeepEqual(a.PromptTitles, []string{"Positive", "Negative"}) {
		t.Errorf("analysis = %+v", a)
	}

	g, err := e.Generators.Create(ctx(t), domain.Generator{
		Name: "Illustrious", CodeName: "anime", Workflow: w, PositivePromptTitle: "Positive", NegativePromptTitle: "Negative",
	})
	check(t, err)
	if g.ID == 0 || g.OutputType != domain.OutputImage || g.OutputNodeTitle != "Save Image" || g.OutputNodeClassType != "SaveImage" {
		t.Errorf("created = %+v", g)
	}
	if !reflect.DeepEqual(g.OutputAttributes, a.Attributes) {
		t.Errorf("attributes = %v, want the analysed %v", g.OutputAttributes, a.Attributes)
	}
	got, err := e.Generators.Get(ctx(t), g.ID)
	check(t, err)
	if got.Name != "Illustrious" || string(got.Workflow) != string(w) {
		t.Errorf("stored generator differs: %+v", got)
	}
}

func TestGeneratorKeepsWhatItIsGiven(t *testing.T) {
	e := newEnv(t)
	attrs := domain.OutputAttributes{"width": {Kind: domain.AttrInt, Int: 640}}
	g, err := e.Generators.Create(ctx(t), domain.Generator{
		Name: "g", CodeName: "g", Workflow: example(t, "creators", "illustrious_xl.json"),
		OutputType: domain.OutputUnknown, OutputAttributes: attrs, OutputNodeClassType: "SaveImage", OutputNodeTitle: "Save Image",
	})
	check(t, err)
	if g.OutputType != domain.OutputUnknown || !reflect.DeepEqual(g.OutputAttributes, attrs) {
		t.Errorf("given values were replaced: %+v", g)
	}
}

func TestGeneratorUpdateListDelete(t *testing.T) {
	e := newEnv(t)
	g := e.generator(t, "anime")
	e.generator(t, "photo")

	upd, err := e.Generators.Update(ctx(t), g.ID, domain.Generator{
		Name: "Renamed", CodeName: "anime", Workflow: g.Workflow, HasRandomSeed: true,
	})
	check(t, err)
	if upd.Name != "Renamed" || !upd.HasRandomSeed || upd.OutputType != domain.OutputImage || !upd.CreatedAt.Equal(g.CreatedAt) {
		t.Errorf("updated = %+v", upd)
	}
	page, err := e.Generators.List(ctx(t), domain.Page{})
	check(t, err)
	if page.Total != 2 {
		t.Errorf("total = %d", page.Total)
	}
	check(t, e.Generators.Delete(ctx(t), g.ID))
	_, err = e.Generators.Get(ctx(t), g.ID)
	wantRoute(t, err, "GeneratorService.Get", "GeneratorNotFound")
}

func TestGeneratorFailures(t *testing.T) {
	e := newEnv(t)
	g := e.generator(t, "anime")
	w := g.Workflow
	good := domain.Generator{Name: "n", CodeName: "other", Workflow: w}
	tests := []struct {
		name   string
		change func(*domain.Generator)
		tag    string
	}{
		{"blank name", func(v *domain.Generator) { v.Name = "" }, "NameInvalid"},
		{"bad code", func(v *domain.Generator) { v.CodeName = "a b" }, "CodeNameInvalid"},
		{"reserved code", func(v *domain.Generator) { v.CodeName = "with" }, "CodeNameReserved"},
		{"not JSON", func(v *domain.Generator) { v.Workflow = []byte("{nope") }, "WorkflowInvalid"},
		{"no nodes", func(v *domain.Generator) { v.Workflow = []byte("{}") }, "WorkflowInvalid"},
		{"no output node", func(v *domain.Generator) {
			v.Workflow = []byte(`{"1":{"class_type":"CLIPTextEncode","inputs":{"text":"x"},"_meta":{"title":"P"}}}`)
		}, "OutputNodeNotFound"},
		{"bad output type", func(v *domain.Generator) { v.OutputType = "audio" }, "OutputTypeInvalid"},
		{"unknown prompt title", func(v *domain.Generator) { v.PositivePromptTitle = "Nowhere" }, "WorkflowTitleNotFound"},
		{"unknown output title", func(v *domain.Generator) {
			v.OutputType, v.OutputAttributes, v.OutputNodeClassType, v.OutputNodeTitle = domain.OutputImage, domain.OutputAttributes{}, "SaveImage", "Nowhere"
		}, "WorkflowTitleNotFound"},
		{"duplicate title", func(v *domain.Generator) {
			v.Workflow = []byte(`{"1":{"class_type":"CLIPTextEncode","inputs":{},"_meta":{"title":"P"}},` +
				`"2":{"class_type":"CLIPTextEncode","inputs":{},"_meta":{"title":"P"}},` +
				`"3":{"class_type":"SaveImage","inputs":{},"_meta":{"title":"Save"}}}`)
			v.PositivePromptTitle = "P"
		}, "WorkflowTitleDuplicated"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := good
			tt.change(&in)
			_, err := e.Generators.Create(ctx(t), in)
			wantRoute(t, err, "GeneratorService", tt.tag)
			_, err = e.Generators.Update(ctx(t), g.ID, in)
			wantRoute(t, err, "GeneratorService.Update", tt.tag)
		})
	}
	if page, _ := e.Generators.List(ctx(t), domain.Page{}); page.Total != 1 {
		t.Errorf("%d generators, a rejected create stored something", page.Total)
	}
	if got, _ := e.Generators.Get(ctx(t), g.ID); got.Name != g.Name {
		t.Error("a rejected update changed the generator")
	}

	_, err := e.Generators.Create(ctx(t), domain.Generator{Name: "n", CodeName: "anime", Workflow: w})
	wantRoute(t, err, "GeneratorService.Create", "GeneratorCodeNameTaken")
	_, err = e.Generators.Update(ctx(t), 999, good)
	wantRoute(t, err, "GeneratorService.Update", "GeneratorNotFound")
	wantRoute(t, e.Generators.Delete(ctx(t), 999), "GeneratorService.Delete", "GeneratorNotFound")
	_, err = e.Generators.Analyze([]byte("[]"))
	wantRoute(t, err, "GeneratorService.Analyze", "WorkflowInvalid")
}

func TestGeneratorPromptNodesDefaultToPositiveAndNegative(t *testing.T) {
	e := newEnv(t)
	tests := []struct{ file, positive, negative string }{
		{"illustrious_xl.json", "Positive", "Negative"},
		{"anima_aio.json", "Positive Prompt", "Negative Prompt"},
	}
	for _, tt := range tests {
		t.Run(tt.file, func(t *testing.T) {
			w := example(t, "creators", tt.file)
			g, err := e.Generators.Create(ctx(t), domain.Generator{Name: "g", CodeName: "g" + tt.file[:3], Workflow: w})
			check(t, err)
			if g.PositivePromptTitle != tt.positive || g.NegativePromptTitle != tt.negative {
				t.Errorf("created with %q / %q, want %q / %q", g.PositivePromptTitle, g.NegativePromptTitle, tt.positive, tt.negative)
			}
			// An update that leaves them empty gets the defaults too.
			got, err := e.Generators.Update(ctx(t), g.ID, domain.Generator{Name: "g2", CodeName: g.CodeName, Workflow: w})
			check(t, err)
			if got.PositivePromptTitle != tt.positive || got.NegativePromptTitle != tt.negative {
				t.Errorf("updated with %q / %q", got.PositivePromptTitle, got.NegativePromptTitle)
			}
		})
	}

	// What the user chose stays.
	w := example(t, "creators", "illustrious_xl.json")
	g, err := e.Generators.Create(ctx(t), domain.Generator{Name: "g", CodeName: "chosen", Workflow: w, PositivePromptTitle: "Negative", NegativePromptTitle: "Positive"})
	check(t, err)
	if g.PositivePromptTitle != "Negative" || g.NegativePromptTitle != "Positive" {
		t.Errorf("chosen titles were replaced: %q / %q", g.PositivePromptTitle, g.NegativePromptTitle)
	}
}

func TestGeneratorNeedsAPositivePromptNode(t *testing.T) {
	e := newEnv(t)
	w := example(t, "creators", "illustrious_xl.json")
	noPositive := bytes.ReplaceAll(w, []byte(`"Positive"`), []byte(`"Wanted text"`))
	noNegative := bytes.ReplaceAll(w, []byte(`"Negative"`), []byte(`"Unwanted text"`))
	if bytes.Equal(w, noPositive) || bytes.Equal(w, noNegative) {
		t.Fatal("the example no longer has nodes titled Positive and Negative")
	}

	// No node starts with "positive" and none was named: refused, and the
	// message says which nodes could be used.
	_, err := e.Generators.Create(ctx(t), domain.Generator{Name: "g", CodeName: "g", Workflow: noPositive})
	appErr := wantRouteErr(t, err, "GeneratorService.Create", "GeneratorTitleRequired")
	if !reflect.DeepEqual(appErr.Metadata["candidates"], []string{"Wanted text", "Negative"}) {
		t.Errorf("metadata = %v", appErr.Metadata)
	}
	_, err = e.Generators.Update(ctx(t), 1, domain.Generator{Name: "g", CodeName: "g", Workflow: noPositive})
	wantRoute(t, err, "GeneratorService.Update", "GeneratorTitleRequired")
	// A blank title is as good as none.
	_, err = e.Generators.Create(ctx(t), domain.Generator{Name: "g", CodeName: "g", Workflow: noPositive, PositivePromptTitle: "  "})
	wantRoute(t, err, "GeneratorService.Create", "GeneratorTitleRequired")
	if page, _ := e.Generators.List(ctx(t), domain.Page{}); page.Total != 0 {
		t.Errorf("%d generators, a refused create stored something", page.Total)
	}

	// Naming the node by hand is enough.
	g, err := e.Generators.Create(ctx(t), domain.Generator{Name: "g", CodeName: "g", Workflow: noPositive, PositivePromptTitle: "Wanted text"})
	check(t, err)
	if g.PositivePromptTitle != "Wanted text" || g.NegativePromptTitle != "Negative" {
		t.Errorf("created with %q / %q", g.PositivePromptTitle, g.NegativePromptTitle)
	}

	// The negative node stays optional: a workflow may have none.
	g, err = e.Generators.Create(ctx(t), domain.Generator{Name: "g", CodeName: "g2", Workflow: noNegative})
	check(t, err)
	if g.PositivePromptTitle != "Positive" || g.NegativePromptTitle != "" {
		t.Errorf("created with %q / %q", g.PositivePromptTitle, g.NegativePromptTitle)
	}
}

// FLUX.2 Klein zeroes its negative with a ConditioningZeroOut, which has no
// text: the generator takes no negative prompt node from it, so a job does not
// fail with InjectorFailed.
func TestGeneratorOfKleinHasNoNegativePromptNode(t *testing.T) {
	e := newEnv(t)
	w, err := os.ReadFile(filepath.Join("..", "..", "examples", "workflows", "creators", "flux2_klein_9b.json"))
	check(t, err)
	g, err := e.Generators.Create(ctx(t), domain.Generator{Name: "Klein", CodeName: "klein", Workflow: w})
	check(t, err)
	if g.PositivePromptTitle != "Positive Prompt" || g.NegativePromptTitle != "" {
		t.Errorf("created with %q / %q", g.PositivePromptTitle, g.NegativePromptTitle)
	}
	if g.OutputAttributes["steps"].Int != 4 || g.OutputAttributes["width"].Int != 1024 {
		t.Errorf("attributes = %v", g.OutputAttributes)
	}
}
