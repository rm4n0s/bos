package command

import (
	"reflect"
	"strings"
	"testing"

	"github.com/rm4n0s/errors"
)

func expand(t *testing.T, cat Catalogue, text string) *Expansion {
	t.Helper()
	x, err := Expand(mustParse(t, text), cat, Options{})
	if err != nil {
		t.Fatalf("Expand(%q): %v", text, err)
	}
	return x
}

// codes lists, per job, the item code names joined with '/' in term order.
func codes(x *Expansion) []string {
	out := make([]string, len(x.Jobs))
	for i, j := range x.Jobs {
		parts := make([]string, len(j.Parts))
		for k, p := range j.Parts {
			parts[k] = p.Code
		}
		out[i] = strings.Join(parts, "/")
	}
	return out
}

func servers(x *Expansion) string {
	out := make([]string, len(x.Jobs))
	for i, j := range x.Jobs {
		out[i] = j.Preferred
	}
	return strings.Join(out, " ")
}

// The valid table of SPEC §6.12 with the job count of each row (acceptance
// tests 1a and 14).
func TestExpandValidTable(t *testing.T) {
	tests := []struct {
		in   string
		jobs int
	}{
		{`gpu1 -$ g: characters * poses(~jumping) * emotions(cry)`, 9},
		{`gpu1 -$ g: characters(alice, bob) * emotions`, 6},
		{`gpu1 -$ g: characters && anime`, 5},
		{`gpu1 -$ g: characters || emotions`, 3},
		{`gpu1 -$ g: characters*poses(standing)`, 3},
		{`gpu1 -$ g: characters * poses(#acrobatic)`, 6},
		{`gpu1 -$ g: characters * poses(run%)`, 3},
		{`gpu1 -$ g: characters * "masterpiece, best quality"`, 3},
		{`gpu1 -$ g: characters * !"blurry"`, 3},
		{`gpu1 -$ g: "red hair" && "blue hair" * characters`, 6},
		{`gpu1 -$ g: scene[subject: characters(alice, bob)]`, 4},
		{`gpu1 -$ g: duo{red: characters(alice), blue: characters(bob, carol)} * style`, 4},
		{`gpu1 -$ g: characters * poses where not #acrobatic`, 6},
		{`gpu1 -$ g: characters * poses where not (characters = alice and poses = jumping)`, 11},
		{`gpu1 -$ g: characters * poses * emotions limit 20`, 20},
		{`gpu1 -$ g: characters * poses * emotions sample 10 seed 7`, 10},
		{`gpu1 -$ gr: characters * poses limit 5 repeat 4`, 20},
		{`gpu1 -$ g: characters * poses repeat 3 with seed=42`, 36},
		{`gpu1 -$ g: characters with steps=30, cfg=6.5, sampler_name=dpmpp_2m`, 3},
		{`gpu1, gpu2 -$ g: characters * poses`, 12},
		{`gpu1=3, gpu2=1 -$ g: characters * poses spread blocks`, 12},
		{`gpu1, gpu2 -$ g: characters * poses spread even pinned`, 12},
		{`gpu1, gpu2 -$ g: characters * poses spread by characters`, 12},
		{`gpu1 -$ g: characters > upscale on gpu2 > face`, 3},
	}
	cat := sampleCatalogue()
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			x := expand(t, cat, tt.in)
			if len(x.Jobs) != tt.jobs || x.Counts.Jobs != tt.jobs {
				t.Errorf("jobs = %d (counts %d), want %d", len(x.Jobs), x.Counts.Jobs, tt.jobs)
			}
			// Acceptance 24: the preview reports what Add creates.
			pv := PreviewOf(mustParse(t, tt.in), cat, Options{})
			if !pv.Valid || pv.Jobs != tt.jobs || pv.Counts != x.Counts {
				t.Errorf("preview = %+v, expansion counts %+v", pv.Counts, x.Counts)
			}
		})
	}
}

// Acceptance tests 1c and 3: job order has the last term varying fastest and
// is the same on every run.
func TestExpandOrder(t *testing.T) {
	cat := newFakeCatalogue()
	cat.servers["s"] = true
	cat.generators["g"] = Generator{OutputType: OutputImage}
	cat.group("A", words("A1", "A2")...).group("B", words("B1", "B2", "B3")...)
	want := []string{"A1/B1", "A1/B2", "A1/B3", "A2/B1", "A2/B2", "A2/B3"}
	for range 3 {
		if got := codes(expand(t, cat, `s -$ g: A * B`)); !reflect.DeepEqual(got, want) {
			t.Errorf("order = %v, want %v", got, want)
		}
	}

	cat.group("C", words("C1", "C2", "C3", "C4")...).group("D", words("D1", "D2")...)
	x := expand(t, cat, `s -$ g: B * C * D`)
	if len(x.Jobs) != 24 {
		t.Fatalf("got %d jobs, want 24", len(x.Jobs))
	}
	got := codes(x)
	if got[0] != "B1/C1/D1" || got[1] != "B1/C1/D2" || got[2] != "B1/C2/D1" || got[8] != "B2/C1/D1" || got[23] != "B3/C4/D2" {
		t.Errorf("order = %v", got)
	}
	for i, j := range x.Jobs {
		if j.Index != i || j.Combination != i {
			t.Errorf("job %d has index %d, combination %d", i, j.Index, j.Combination)
		}
	}
}

func TestExpandMergeKeepsDuplicates(t *testing.T) {
	x := expand(t, sampleCatalogue(), `gpu1 -$ g: characters && anime && characters`)
	want := []string{"alice", "bob", "carol", "anime1", "anime2", "alice", "bob", "carol"}
	if got := codes(x); !reflect.DeepEqual(got, want) {
		t.Errorf("merge = %v, want %v", got, want)
	}
}

// Acceptance test 2: a zip pairs items positionally and fuses them.
func TestExpandZip(t *testing.T) {
	cat := sampleCatalogue()
	alice := &cat.groups["characters"][0]
	alice.ControlNets = []ControlNet{{Type: "openpose", ImagePath: "a.png"}}
	alice.LoRAs = []LoRA{{Name: "alice_lora", StrengthModel: 1, StrengthClip: 1}}
	alice.Negative = "ugly"
	cat.groups["emotions"][0].ControlNets = []ControlNet{{Type: "canny", ImagePath: "h.png"}}
	cat.groups["emotions"][0].Negative = "blurry"

	x := expand(t, cat, `gpu1 -$ g: characters || emotions`)
	if got, want := codes(x), []string{"alice+happy", "bob+sad", "carol+cry"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("zip = %v, want %v", got, want)
	}
	j := x.Jobs[0]
	if j.Positive != "alice happy" || j.Negative != "ugly blurry" {
		t.Errorf("prompts = %q / %q", j.Positive, j.Negative)
	}
	if len(j.ControlNets) != 2 || j.ControlNets[0].Type != "openpose" || j.ControlNets[1].Type != "canny" {
		t.Errorf("controlnets = %+v", j.ControlNets)
	}
	if len(j.LoRAs) != 1 || j.LoRAs[0].Name != "alice_lora" {
		t.Errorf("loras = %+v", j.LoRAs)
	}
	if len(j.Items) != 2 || j.Items[0].String() != "characters(alice)" || j.Items[1].String() != "emotions(happy)" {
		t.Errorf("items = %v", j.Items)
	}

	// The shortest operand decides; a three-way zip.
	x = expand(t, cat, `gpu1 -$ g: characters || anime || emotions`)
	if got := codes(x); !reflect.DeepEqual(got, []string{"alice+anime1+happy", "bob+anime2+sad"}) {
		t.Errorf("zip = %v", got)
	}
}

// Acceptance test 16.
func TestExpandLiterals(t *testing.T) {
	x := expand(t, sampleCatalogue(), `gpu1 -$ g: characters(alice) * "masterpiece" * !"blurry" * "best quality"`)
	if len(x.Jobs) != 1 {
		t.Fatalf("got %d jobs", len(x.Jobs))
	}
	j := x.Jobs[0]
	if j.Positive != "alice masterpiece best quality" || j.Negative != "blurry" {
		t.Errorf("prompts = %q / %q", j.Positive, j.Negative)
	}
	if len(j.Items) != 1 || j.Items[0].Code != "alice" {
		t.Errorf("a literal must not appear in the used items: %v", j.Items)
	}
	var got []string
	for _, p := range j.Parts {
		got = append(got, p.Code)
	}
	if want := []string{"alice", "lit1", "lit2", "lit3"}; !reflect.DeepEqual(got, want) {
		t.Errorf("name parts = %v, want %v", got, want)
	}

	// A merge of two literals gives two alternatives.
	x = expand(t, sampleCatalogue(), `gpu1 -$ g: "red hair" && "blue hair"`)
	if len(x.Jobs) != 2 || x.Jobs[0].Positive != "red hair" || x.Jobs[1].Positive != "blue hair" {
		t.Errorf("alternatives = %+v", x.Jobs)
	}
	if len(x.Jobs[0].Items) != 0 {
		t.Errorf("literal listed as used item: %v", x.Jobs[0].Items)
	}
}

// Acceptance test 15.
func TestExpandSelectors(t *testing.T) {
	cat := sampleCatalogue()
	cat.group("poses", append(words("standing", "jumping", "sitting", "running"), words("pose_1", "pose_2", "pose_12")...)...)
	cat.groups["poses"][1].Tags = []string{"acrobatic"}
	cat.groups["poses"][3].Tags = []string{"acrobatic"}
	tests := []struct {
		sel  string
		want []string
	}{
		{`poses(run%)`, []string{"running"}},
		{`poses(%ing)`, []string{"standing", "jumping", "sitting", "running"}},
		{`poses(pose_?)`, []string{"pose_1", "pose_2"}},
		{`poses(pose_%)`, []string{"pose_1", "pose_2", "pose_12"}},
		{`poses(%s%)`, []string{"standing", "sitting", "pose_1", "pose_2", "pose_12"}},
		{`poses(#acrobatic)`, []string{"jumping", "running"}},
		{`poses(#ACROBATIC)`, []string{"jumping", "running"}},
		{`poses(sitting, #acrobatic, running)`, []string{"jumping", "sitting", "running"}},
		{`poses(~#acrobatic, ~pose_%)`, []string{"standing", "sitting"}},
		{`poses(~run%)`, []string{"standing", "jumping", "sitting", "pose_1", "pose_2", "pose_12"}},
		{`poses(running, run%)`, []string{"running"}},
	}
	for _, tt := range tests {
		t.Run(tt.sel, func(t *testing.T) {
			x := expand(t, cat, "gpu1 -$ g: "+tt.sel)
			if got := codes(x); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMatchPattern(t *testing.T) {
	tests := []struct {
		pattern, code string
		want          bool
	}{
		{"%", "", true}, {"%", "abc", true}, {"?", "", false}, {"?", "a", true}, {"?", "ab", false},
		{"a%", "a", true}, {"a%", "abc", true}, {"a%", "ba", false},
		{"%c", "abc", true}, {"%c", "abcd", false}, {"a%c", "abc", true}, {"a%c", "ac", true},
		{"a%c", "abxc", true}, {"a%c", "abxd", false}, {"a?c", "abc", true}, {"a?c", "ac", false},
		{"%a%b%", "xaxbx", true}, {"%a%b%", "xbxax", false}, {"a%%b", "ab", true},
		{"Abc", "abc", false}, {"abc", "abc", true}, {"%?", "a", true}, {"%?", "", false},
	}
	for _, tt := range tests {
		if got := matchPattern(tt.pattern, tt.code); got != tt.want {
			t.Errorf("matchPattern(%q, %q) = %v, want %v", tt.pattern, tt.code, got, tt.want)
		}
	}
}

// Acceptance test 11.
func TestExpandTemplates(t *testing.T) {
	cat := sampleCatalogue()
	cat.groups["characters"][0].Negative = "ugly"
	x := expand(t, cat, `gpu1 -$ g: scene[subject: characters(alice, bob)]`)
	var pos, neg []string
	for _, j := range x.Jobs {
		pos = append(pos, j.Positive)
		neg = append(neg, j.Negative)
	}
	wantPos := []string{"alice in a park", "bob in a park", "alice on a beach", "bob on a beach"}
	wantNeg := []string{"no ugly", "no", "", ""} // the negative marker takes the fragment's negative prompt
	if !reflect.DeepEqual(pos, wantPos) || !reflect.DeepEqual(neg, wantNeg) {
		t.Errorf("prompts = %q / %q", pos, neg)
	}
	for _, j := range x.Jobs {
		if strings.Contains(j.Positive+j.Negative, "<tmpl:") {
			t.Errorf("placeholder left in %q / %q", j.Positive, j.Negative)
		}
	}
	if got, want := codes(x), []string{"park-alice", "park-bob", "beach-alice", "beach-bob"}; !reflect.DeepEqual(got, want) {
		t.Errorf("codes = %v, want %v", got, want)
	}
	// The used items are the base item and the items of the fragment.
	if refs := x.Jobs[1].Items; len(refs) != 2 || refs[0].Code != "park" || refs[1].Code != "bob" {
		t.Errorf("used items = %v", refs)
	}

	// Identical prompts are de-duplicated, the first kept: two characters with the
	// same text give one scene item.
	cat = sampleCatalogue()
	cat.groups["characters"][1].Positive = "alice"
	x = expand(t, cat, `gpu1 -$ g: scene[subject: characters(alice, bob)]`)
	if len(x.Jobs) != 2 {
		t.Errorf("got %d jobs after de-duplication, want 2", len(x.Jobs))
	}

	// A tag that the base prompt does not contain leaves it unchanged, so all
	// fragments collapse into one.
	cat = sampleCatalogue()
	x = expand(t, cat, `gpu1 -$ g: scene[other: characters]`)
	if len(x.Jobs) != 2 {
		t.Fatalf("got %d jobs, want 2", len(x.Jobs))
	}
	// The unresolved placeholder is removed from the job prompt.
	if x.Jobs[0].Positive != "in a park" || x.Jobs[0].Negative != "no" {
		t.Errorf("prompts = %q / %q", x.Jobs[0].Positive, x.Jobs[0].Negative)
	}

	// Several tags make a product of their fragments, last tag fastest; LoRAs are
	// merged without duplicates.
	cat = sampleCatalogue()
	cat.group("scene", Item{Code: "park", Positive: "<tmpl:who> with <tmpl:mood>", LoRAs: []LoRA{{Name: "L1"}}})
	cat.groups["characters"][0].LoRAs = []LoRA{{Name: "L1"}, {Name: "L2"}}
	cat.groups["emotions"][0].LoRAs = []LoRA{{Name: "L2"}, {Name: "L3"}}
	x = expand(t, cat, `gpu1 -$ g: scene[who: characters(alice, bob), mood: emotions(happy, sad)]`)
	if got, want := codes(x), []string{"park-alice-happy", "park-alice-sad", "park-bob-happy", "park-bob-sad"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("codes = %v, want %v", got, want)
	}
	if got := x.Jobs[0].Positive; got != "alice with happy" {
		t.Errorf("positive = %q", got)
	}
	var names []string
	for _, l := range x.Jobs[0].LoRAs {
		names = append(names, l.Name)
	}
	if want := []string{"L1", "L2", "L3"}; !reflect.DeepEqual(names, want) {
		t.Errorf("loras = %v, want %v", names, want)
	}

	// A template body can hold a product, a merge and a literal.
	x = expand(t, cat, `gpu1 -$ g: scene(park)[who: characters(alice) * "smiling" , mood: emotions(happy) && emotions(sad)]`)
	if got := []string{x.Jobs[0].Positive, x.Jobs[1].Positive}; !reflect.DeepEqual(got, []string{"alice smiling with happy", "alice smiling with sad"}) {
		t.Errorf("prompts = %q", got)
	}
}

// Acceptance test 5 and the region rules of SPEC §6.6.
func TestExpandRegions(t *testing.T) {
	cat := sampleCatalogue()
	x := expand(t, cat, `gpu1 -$ g: duo{red: characters(alice), blue: characters(bob, carol)} * style`)
	if len(x.Jobs) != 4 {
		t.Fatalf("got %d jobs, want 4", len(x.Jobs))
	}
	// R varies fastest: style1 with both region sets, then style2.
	wantRegions := [][2]string{{"alice", "bob"}, {"alice", "carol"}, {"alice", "bob"}, {"alice", "carol"}}
	wantStyle := []string{"style1", "style1", "style2", "style2"}
	for i, j := range x.Jobs {
		if len(j.Regions) != 2 || j.Regions[0].Keyword != "red" || j.Regions[1].Keyword != "blue" {
			t.Fatalf("job %d regions = %+v", i, j.Regions)
		}
		if j.Regions[0].Prompt != wantRegions[i][0] || j.Regions[1].Prompt != wantRegions[i][1] {
			t.Errorf("job %d region prompts = %q, %q", i, j.Regions[0].Prompt, j.Regions[1].Prompt)
		}
		if j.Regions[0].MaskFile != "duo1_red.png" || j.Regions[1].MaskFile != "duo1_blue.png" {
			t.Errorf("job %d masks = %q, %q", i, j.Regions[0].MaskFile, j.Regions[1].MaskFile)
		}
		if j.RegionIndex != i%2 {
			t.Errorf("job %d region index = %d", i, j.RegionIndex)
		}
		if j.Parts[1].Code != wantStyle[i] {
			t.Errorf("job %d style = %s", i, j.Parts[1].Code)
		}
		if j.Positive != "two people "+wantStyle[i] {
			t.Errorf("job %d positive = %q", i, j.Positive)
		}
	}
	// The used items are the base item, the style and the region items.
	var used []string
	for _, r := range x.Jobs[1].Items {
		used = append(used, r.String())
	}
	if want := []string{"duo(duo1)", "style(style1)", "characters(alice)", "characters(carol)"}; !reflect.DeepEqual(used, want) {
		t.Errorf("used = %v, want %v", used, want)
	}

	// Coordinates and negative literals (ignored in a region), several items per keyword.
	cat.group("duo", Item{
		Code: "duo2", Positive: "pair",
		Coordinates: []Coordinates{{Keyword: "left", Width: 10, Height: 20, X: 1, Y: 2}, {Keyword: "right", Width: 10, Height: 20, X: 11, Y: 2}},
	})
	cat.groups["characters"][0].LoRAs = []LoRA{{Name: "L"}}
	x = expand(t, cat, `gpu1 -$ g: duo{left: characters(alice) * "smiling" * !"sad", right: characters(bob)}`)
	if len(x.Jobs) != 1 {
		t.Fatalf("got %d jobs", len(x.Jobs))
	}
	r := x.Jobs[0].Regions
	if r[0].Prompt != "alice smiling" || r[0].Coordinates == nil || r[0].Coordinates.X != 1 || r[0].MaskFile != "" {
		t.Errorf("left region = %+v", r[0])
	}
	if len(r[0].LoRAs) != 1 || r[0].LoRAs[0].Name != "L" {
		t.Errorf("left loras = %+v", r[0].LoRAs)
	}
	if r[1].Coordinates == nil || r[1].Coordinates.X != 11 {
		t.Errorf("right region = %+v", r[1])
	}
}

func TestExpandWhere(t *testing.T) {
	cat := sampleCatalogue()
	tests := []struct {
		name       string
		in         string
		n          int // jobs
		afterWhere int
	}{
		{"not tag", `characters * poses where not #acrobatic`, 6, 6},
		{"tag", `characters * poses where #acrobatic`, 6, 6},
		{"eq item", `characters * poses where characters = alice`, 4, 4},
		{"eq pattern", `characters * poses where poses = run%`, 3, 3},
		{"eq tag selector", `characters * poses where poses = #acrobatic`, 6, 6},
		{"in", `characters * poses where poses in (standing, sitting)`, 6, 6},
		{"in mixed", `characters * poses where poses in (#acrobatic, standing)`, 9, 9},
		{"and", `characters * poses where characters = alice and poses = jumping`, 1, 1},
		{"or", `characters * poses where characters = alice or poses = jumping`, 6, 6},
		{"parens", `characters * poses where (characters = alice or characters = bob) and not #acrobatic`, 4, 4},
		{"not parens", `characters * poses where not (characters = alice and poses = jumping)`, 11, 11},
		{"double not", `characters * poses where not not characters = alice`, 4, 4},
		{"precedence", `characters * poses where characters = alice or characters = bob and poses = jumping`, 5, 5},
		{"zip members", `characters || emotions where characters = bob`, 1, 1},
		{"zip other member", `characters || emotions where emotions = cry`, 1, 1},
		{"merge", `characters && anime where anime = anime2`, 1, 1},
		{"merge tag-free", `characters && anime where not characters = alice`, 4, 4},
		{"template base", `scene[subject: characters(alice, bob)] where scene = park`, 2, 2},
		{"template fragment", `scene[subject: characters(alice, bob)] where characters = bob`, 2, 2},
		{"region base", `duo{red: characters(alice), blue: characters(bob, carol)} where duo = duo1`, 2, 2},
		{"region prompt item", `duo{red: characters(alice), blue: characters(bob, carol)} * style where characters = carol`, 2, 2},
		{"group tag never visible", `characters * poses where #character`, 0, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			text := "gpu1 -$ g: " + tt.in
			if tt.n == 0 {
				_, err := Expand(mustParse(t, text), cat, Options{})
				if !hasProblem(t, err, "CommandWhereEmpty") {
					t.Errorf("expected CommandWhereEmpty, got %v", err)
				}
				return
			}
			x := expand(t, cat, text)
			if len(x.Jobs) != tt.n || x.AfterWhere != tt.afterWhere {
				t.Errorf("jobs = %d, after where = %d; want %d, %d", len(x.Jobs), x.AfterWhere, tt.n, tt.afterWhere)
			}
		})
	}

	// Combinations keep their product order after where.
	x := expand(t, cat, `gpu1 -$ g: characters * poses where poses = sitting`)
	if got, want := codes(x), []string{"alice/sitting", "bob/sitting", "carol/sitting"}; !reflect.DeepEqual(got, want) {
		t.Errorf("order = %v, want %v", got, want)
	}
	if got := []int{x.Jobs[0].Combination, x.Jobs[1].Combination, x.Jobs[2].Combination}; !reflect.DeepEqual(got, []int{2, 6, 10}) {
		t.Errorf("combination indices = %v", got)
	}
}

// Acceptance test 18.
func TestExpandLimitSampleRepeat(t *testing.T) {
	cat := sampleCatalogue()
	cat.generators["gr"] = Generator{OutputType: OutputImage, HasRandomSeed: true}

	x := expand(t, cat, `gpu1 -$ g: characters * poses * emotions limit 20`)
	if x.Combinations != 36 || x.AfterWhere != 36 || x.Kept != 20 || x.Counts.Jobs != 20 {
		t.Errorf("counts = %+v", x.Counts)
	}
	if got := x.Jobs[19].Combination; got != 19 {
		t.Errorf("limit must keep the first 20 in product order; last is %d", got)
	}

	x = expand(t, cat, `gpu1 -$ gr: characters * poses * emotions limit 20 repeat 4`)
	if x.Counts.Jobs != 80 || x.Kept != 20 {
		t.Errorf("limit 20 repeat 4 gave %+v", x.Counts)
	}
	for i, j := range x.Jobs {
		if j.Combination != i/4 || j.Repeat != i%4 {
			t.Fatalf("job %d: combination %d repeat %d", i, j.Combination, j.Repeat)
		}
	}

	// Sample: reproducible, ascending (product order), no repeats of a combination.
	a := expand(t, cat, `gpu1 -$ g: characters * poses * emotions sample 10 seed 7`)
	b := expand(t, cat, `gpu1 -$ g: characters * poses * emotions sample 10 seed 7`)
	c := expand(t, cat, `gpu1 -$ g: characters * poses * emotions sample 10 seed 8`)
	pick := func(x *Expansion) []int {
		out := make([]int, len(x.Jobs))
		for i, j := range x.Jobs {
			out[i] = j.Combination
		}
		return out
	}
	if !reflect.DeepEqual(pick(a), pick(b)) {
		t.Errorf("same seed, different selection: %v vs %v", pick(a), pick(b))
	}
	if reflect.DeepEqual(pick(a), pick(c)) {
		t.Errorf("different seeds gave the same selection %v", pick(a))
	}
	for i := 1; i < len(pick(a)); i++ {
		if pick(a)[i] <= pick(a)[i-1] {
			t.Fatalf("sample is not in product order: %v", pick(a))
		}
	}
	// A known answer pins the algorithm: a stored command must select the same
	// combinations after an upgrade.
	if want := []int{1, 3, 4, 5, 7, 17, 20, 21, 24, 32}; !reflect.DeepEqual(pick(a), want) {
		t.Errorf("sample 10 seed 7 = %v, want %v", pick(a), want)
	}

	// Sample after where chooses among the surviving combinations.
	x = expand(t, cat, `gpu1 -$ g: characters * poses where not #acrobatic sample 4 seed 3`)
	if len(x.Jobs) != 4 || x.AfterWhere != 6 {
		t.Fatalf("counts = %+v", x.Counts)
	}
	for _, j := range x.Jobs {
		if j.Parts[1].Code == "jumping" || j.Parts[1].Code == "running" {
			t.Errorf("sample returned a combination that where removed: %v", j.Parts)
		}
	}

	// Sample and limit larger than the available combinations keep everything,
	// with a warning.
	x = expand(t, cat, `gpu1 -$ g: characters sample 10`)
	if len(x.Jobs) != 3 || len(x.Warnings) != 1 || x.Warnings[0].Code != "SampleNotSmaller" {
		t.Errorf("jobs = %d, warnings = %+v", len(x.Jobs), x.Warnings)
	}
	x = expand(t, cat, `gpu1 -$ g: characters limit 3`)
	if len(x.Jobs) != 3 || len(x.Warnings) != 1 || x.Warnings[0].Code != "LimitNotSmaller" {
		t.Errorf("jobs = %d, warnings = %+v", len(x.Jobs), x.Warnings)
	}
	x = expand(t, cat, `gpu1 -$ g: characters limit 2`)
	if len(x.Warnings) != 0 {
		t.Errorf("unexpected warnings %+v", x.Warnings)
	}
}

func TestEnsureSampleSeed(t *testing.T) {
	c := mustParse(t, `gpu1 -$ g: characters sample 2`)
	if !EnsureSampleSeed(c, func() uint64 { return 99 }) || c.Sample.Seed == nil || c.Sample.Seed.N != 99 {
		t.Fatalf("seed not stored: %+v", c.Sample)
	}
	if EnsureSampleSeed(c, func() uint64 { return 1 }) || c.Sample.Seed.N != 99 {
		t.Error("an existing seed was replaced")
	}
	if EnsureSampleSeed(mustParse(t, `gpu1 -$ g: characters`), func() uint64 { return 1 }) {
		t.Error("a seed was added to a command without sample")
	}
	// The stored command reproduces the same selection (acceptance 18).
	cat := sampleCatalogue()
	first, err := Expand(c, cat, Options{})
	if err != nil {
		t.Fatal(err)
	}
	b, err := ToJSON(c)
	if err != nil {
		t.Fatal(err)
	}
	back, err := FromJSON(b)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Expand(back, cat, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(codes(first), codes(second)) {
		t.Errorf("re-creating changed the jobs: %v vs %v", codes(first), codes(second))
	}
	if !strings.Contains(Format(c), "seed 99") {
		t.Errorf("seed missing from the text: %s", Format(c))
	}
}

// Acceptance test 19.
func TestExpandSeeds(t *testing.T) {
	cat := sampleCatalogue()
	x := expand(t, cat, `gpu1 -$ g: characters * poses repeat 3 with seed=42`)
	for i, j := range x.Jobs {
		if j.Seed == nil || *j.Seed != 42+uint64(i%3) {
			t.Fatalf("job %d seed = %v, want %d", i, j.Seed, 42+i%3)
		}
	}
	// Without repeat every job uses the seed unchanged.
	x = expand(t, cat, `gpu1 -$ g: characters with seed=7`)
	for _, j := range x.Jobs {
		if j.Seed == nil || *j.Seed != 7 {
			t.Errorf("seed = %v", j.Seed)
		}
	}
	// A random-seed generator leaves the seed to the run.
	x = expand(t, cat, `gpu1 -$ gr: characters repeat 2`)
	for _, j := range x.Jobs {
		if j.Seed != nil {
			t.Errorf("seed = %d, want none", *j.Seed)
		}
	}
	// A seed override still wins and varies by repeat on a random-seed generator.
	x = expand(t, cat, `gpu1 -$ gr: characters(alice) repeat 2 with seed=10`)
	if *x.Jobs[0].Seed != 10 || *x.Jobs[1].Seed != 11 {
		t.Errorf("seeds = %d, %d", *x.Jobs[0].Seed, *x.Jobs[1].Seed)
	}
	// The workflow's seed stays unset without an override.
	x = expand(t, cat, `gpu1 -$ g: characters`)
	if x.Jobs[0].Seed != nil {
		t.Error("seed set without an override")
	}
}

// Acceptance test 20.
func TestEffectiveOutput(t *testing.T) {
	cat := sampleCatalogue()
	cat.generators["g"] = Generator{OutputType: OutputImage, Attrs: map[string]Value{
		"width": {Kind: ValueInt, Int: 512}, "steps": {Kind: ValueInt, Int: 20}, "cfg": {Kind: ValueDecimal, Float: 7},
	}}
	cat.groups["poses"][0].Output = &OutputSpec{Type: OutputImage, Attrs: map[string]Value{"steps": {Kind: ValueInt, Int: 40}, "width": {Kind: ValueInt, Int: 768}}}
	cat.groups["poses"][1].Output = &OutputSpec{Type: OutputVideo, Attrs: map[string]Value{"fps": {Kind: ValueInt, Int: 24}}}

	x := expand(t, cat, `gpu1 -$ g: poses with steps=30, sampler_name=euler, seed=5`)
	byPose := map[string]OutputSpec{}
	for i := range x.Jobs {
		byPose[x.Jobs[i].Parts[0].Code] = x.EffectiveOutput(&x.Jobs[i])
	}

	generatorOnly := byPose["sitting"]
	if generatorOnly.Type != OutputImage || generatorOnly.Attrs["width"].Int != 512 || generatorOnly.Attrs["steps"].Int != 30 || generatorOnly.Attrs["cfg"].Float != 7 {
		t.Errorf("generator + command = %+v", generatorOnly)
	}
	if generatorOnly.Attrs["sampler_name"].Text != "euler" || generatorOnly.Attrs["seed"].Int != 5 {
		t.Errorf("command settings missing: %+v", generatorOnly.Attrs)
	}
	item := byPose["standing"]
	if item.Attrs["width"].Int != 768 {
		t.Errorf("item must override the generator: %+v", item.Attrs)
	}
	if item.Attrs["steps"].Int != 30 {
		t.Errorf("command must override the item: %+v", item.Attrs)
	}
	// The item switches to video: the generator's attributes are dropped and the
	// image-only settings of the command are ignored for this job.
	video := byPose["jumping"]
	if video.Type != OutputVideo || video.Attrs["fps"].Int != 24 {
		t.Errorf("video job = %+v", video)
	}
	for _, key := range []string{"steps", "width", "sampler_name", "seed", "cfg"} {
		if _, ok := video.Attrs[key]; ok {
			t.Errorf("video job kept the image setting %s", key)
		}
	}
	// Text settings given as digits become text.
	x = expand(t, cat, `gpu1 -$ g: characters(alice) with file_type=123`)
	if v := x.Settings["file_type"]; v.Kind != ValueText || v.Text != "123" {
		t.Errorf("file_type = %+v", v)
	}
}

// Acceptance test 21 and SPEC §6.9.
func TestExpandSpread(t *testing.T) {
	cat := sampleCatalogue()

	x := expand(t, cat, `gpu1=3, gpu2=1 -$ g: characters * poses spread blocks`)
	want := strings.Repeat("gpu1 ", 9) + "gpu2 gpu2 gpu2"
	if got := servers(x); got != want {
		t.Errorf("blocks = %s\nwant     %s", got, want)
	}
	if !reflect.DeepEqual(x.PerServer, []ServerCount{{"gpu1", 9}, {"gpu2", 3}}) || x.Dynamic {
		t.Errorf("per server = %+v, dynamic = %v", x.PerServer, x.Dynamic)
	}

	x = expand(t, cat, `gpu1=3, gpu2=1 -$ g: characters * poses spread even`)
	if got, want := servers(x), strings.TrimSpace(strings.Repeat("gpu1 gpu1 gpu1 gpu2 ", 3)); got != want {
		t.Errorf("even weighted = %s\nwant          %s", got, want)
	}
	x = expand(t, cat, `gpu1, gpu2, gpu3 -$ g: characters * poses spread even`)
	if got, want := servers(x), strings.TrimSpace(strings.Repeat("gpu1 gpu2 gpu3 ", 4)); got != want {
		t.Errorf("even = %s\nwant   %s", got, want)
	}

	// The remainder of a block plan goes one each to the first targets.
	x = expand(t, cat, `gpu1, gpu2, gpu3 -$ g: characters * poses * emotions limit 10 spread blocks`)
	if want := []ServerCount{{"gpu1", 4}, {"gpu2", 3}, {"gpu3", 3}}; !reflect.DeepEqual(x.PerServer, want) {
		t.Errorf("remainder = %+v, want %+v", x.PerServer, want)
	}
	if got, want := servers(x), "gpu1 gpu1 gpu1 gpu1 gpu2 gpu2 gpu2 gpu3 gpu3 gpu3"; got != want {
		t.Errorf("blocks = %s, want %s", got, want)
	}

	// by: every job with the same item goes to the same target.
	x = expand(t, cat, `gpu1, gpu2 -$ g: characters * poses spread by characters`)
	for _, j := range x.Jobs {
		want := map[string]string{"alice": "gpu1", "bob": "gpu2", "carol": "gpu1"}[j.Parts[0].Code]
		if j.Preferred != want {
			t.Errorf("%s -> %s, want %s", j.Parts[0].Code, j.Preferred, want)
		}
	}
	// by with weights: the position k is the one in the term's own (filtered)
	// list, so jumping, sitting and running are items 0, 1 and 2.
	x = expand(t, cat, `gpu1=2, gpu2 -$ g: poses(~standing) * characters spread by poses`)
	for _, j := range x.Jobs {
		want := map[string]string{"jumping": "gpu1", "sitting": "gpu1", "running": "gpu2"}[j.Parts[0].Code]
		if j.Preferred != want {
			t.Errorf("%s -> %s, want %s", j.Parts[0].Code, j.Preferred, want)
		}
	}

	// pinned is recorded on every job; dynamic assigns nobody.
	x = expand(t, cat, `gpu1, gpu2 -$ g: characters spread even pinned`)
	for _, j := range x.Jobs {
		if !j.Pinned || j.Preferred == "" {
			t.Fatalf("job %d: pinned=%v preferred=%q", j.Index, j.Pinned, j.Preferred)
		}
	}
	for _, in := range []string{`gpu1, gpu2 -$ g: characters`, `gpu1, gpu2 -$ g: characters spread dynamic`, `gpu1 -$ g: characters`} {
		x = expand(t, cat, in)
		if !x.Dynamic || len(x.PerServer) != 0 {
			t.Errorf("%s: dynamic=%v perServer=%v", in, x.Dynamic, x.PerServer)
		}
		for _, j := range x.Jobs {
			if j.Preferred != "" || j.Pinned {
				t.Errorf("%s: job %d has server %q pinned %v", in, j.Index, j.Preferred, j.Pinned)
			}
		}
	}

	// Repeats of one combination are spread like any other jobs.
	x = expand(t, cat, `gpu1, gpu2 -$ gr: characters(alice) repeat 4 spread even`)
	if got := servers(x); got != "gpu1 gpu2 gpu1 gpu2" {
		t.Errorf("repeats = %s", got)
	}
}

func TestPreviewWarnsAboutServerWithoutJobs(t *testing.T) {
	cat := sampleCatalogue()
	pv := PreviewOf(mustParse(t, `gpu1, gpu2 -$ g: characters(alice) spread even`), cat, Options{})
	if !pv.Valid || len(pv.Warnings) != 1 || pv.Warnings[0].Code != "ServerWithoutJobs" {
		t.Fatalf("preview = %+v", pv)
	}
	if want := []ServerCount{{"gpu1", 1}, {"gpu2", 0}}; !reflect.DeepEqual(pv.PerServer, want) {
		t.Errorf("per server = %+v", pv.PerServer)
	}
	// By item: with one character only the first target gets work.
	pv = PreviewOf(mustParse(t, `gpu1, gpu2 -$ g: characters(bob) spread by characters`), cat, Options{})
	if len(pv.Warnings) != 1 {
		t.Errorf("warnings = %+v", pv.Warnings)
	}
}

// Fixer steps keep their order and their servers.
func TestExpandFixers(t *testing.T) {
	x := expand(t, sampleCatalogue(), `gpu1 -$ g: characters > upscale on gpu2 > face`)
	want := []FixerStep{{Fixer: "upscale", On: "gpu2"}, {Fixer: "face"}}
	if !reflect.DeepEqual(x.Fixers, want) {
		t.Errorf("fixers = %+v, want %+v", x.Fixers, want)
	}
}

func TestExpandCopiesItemData(t *testing.T) {
	cat := sampleCatalogue()
	cat.groups["characters"][0].LoRAs = []LoRA{{Name: "L", StrengthModel: 0.8, StrengthClip: 0.6}}
	cat.groups["characters"][0].IPAdapters = []IPAdapter{{ImageFile: "ref.png", Weight: 0.5}}
	x := expand(t, cat, `gpu1 -$ g: characters(alice) * characters(alice)`)
	j := x.Jobs[0]
	// The same item twice carries its LoRA once.
	if len(j.LoRAs) != 1 || j.LoRAs[0] != (LoRA{Name: "L", StrengthModel: 0.8, StrengthClip: 0.6}) {
		t.Errorf("loras = %+v", j.LoRAs)
	}
	if len(j.IPAdapters) != 2 || j.IPAdapters[0].ImageFile != "ref.png" {
		t.Errorf("ip adapters = %+v", j.IPAdapters)
	}
	if j.Positive != "alice alice" {
		t.Errorf("positive = %q", j.Positive)
	}
	// Mutating a job must not touch the catalogue or other jobs.
	j.LoRAs[0].Name = "changed"
	if cat.groups["characters"][0].LoRAs[0].Name != "L" {
		t.Error("job shares its LoRA slice with the catalogue")
	}
}

func TestRemovePlaceholders(t *testing.T) {
	tests := []struct{ in, want string }{
		{"", ""},
		{"plain", "plain"},
		{"a <tmpl:x> b", "a b"},
		{"<tmpl:x> b", "b"},
		{"a <tmpl:x>", "a"},
		{"a<tmpl:x>b", "ab"},
		{"<tmpl:x><tmpl:y>", ""},
		{"a <tmpl:x> <tmpl:y> b", "a b"},
		{"a <tmpl: b", "a <tmpl: b"},
		{"a <tmpl:not a word> b", "a <tmpl:not a word> b"},
		{"1 < 2 and <tmpl:z> done", "1 < 2 and done"},
	}
	for _, tt := range tests {
		if got := removePlaceholders(tt.in); got != tt.want {
			t.Errorf("removePlaceholders(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func hasProblem(t *testing.T, err error, tag string) bool {
	t.Helper()
	appErr, ok := errors.FromError(err)
	if !ok || appErr.Tag != "CommandValidationFailed" {
		return false
	}
	problems, _ := appErr.Metadata["errors"].([]Problem)
	for _, p := range problems {
		if p.Tag == tag {
			return true
		}
	}
	return false
}

// The LoRAs of all the selected items reach the job, in term order, and a file
// that two items share is listed once with the strengths of the first.
func TestExpandCollectsTheLoRAsOfEveryItem(t *testing.T) {
	cat := sampleCatalogue()
	cat.groups["characters"][0].LoRAs = []LoRA{{Name: "chars/alice", StrengthModel: 1, StrengthClip: 1}, {Name: "shared", StrengthModel: 0.7, StrengthClip: 0.7}}
	cat.groups["poses"][0].LoRAs = []LoRA{{Name: "pose.safetensors", StrengthModel: 0.5, StrengthClip: 0.4}}
	cat.groups["style"][0].LoRAs = []LoRA{{Name: "shared.safetensors", StrengthModel: 0.2, StrengthClip: 0.2}, {Name: `chars\alice.safetensors`, StrengthModel: 0.1, StrengthClip: 0.1}}
	x := expand(t, cat, `gpu1 -$ g: characters(alice) * poses(standing) * style(style1)`)
	if len(x.Jobs) != 1 {
		t.Fatalf("%d jobs", len(x.Jobs))
	}
	want := []LoRA{
		{Name: "chars/alice", StrengthModel: 1, StrengthClip: 1},
		{Name: "shared", StrengthModel: 0.7, StrengthClip: 0.7},
		{Name: "pose.safetensors", StrengthModel: 0.5, StrengthClip: 0.4},
	}
	if got := x.Jobs[0].LoRAs; !reflect.DeepEqual(got, want) {
		t.Errorf("loras = %+v, want %+v", got, want)
	}
}
