package command

import (
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/rm4n0s/errors"
)

func validationProblems(t *testing.T, cat Catalogue, text string, opts Options) []Problem {
	t.Helper()
	err := Validate(mustParse(t, text), cat, opts)
	if err == nil {
		return nil
	}
	appErr, ok := errors.FromError(err)
	if !ok || appErr.Tag != "CommandValidationFailed" {
		t.Fatalf("Validate(%q) = %v, want CommandValidationFailed", text, err)
	}
	if !appErr.HasRoute("Validate->validationError.CommandValidationFailed") {
		t.Errorf("unexpected failure path: %s", appErr.Route())
	}
	problems, _ := appErr.Metadata["errors"].([]Problem)
	return problems
}

func tags(ps []Problem) []string {
	out := make([]string, len(ps))
	for i, p := range ps {
		out[i] = p.Tag
	}
	return out
}

// The semantic rows of the invalid table of SPEC §6.12.
func TestValidateSemanticRows(t *testing.T) {
	tests := []struct {
		in   string
		tag  string
		rule string
		at   string
	}{
		{`gpu1 -$ g: characters repeat 3`, "CommandRestrictionViolated", RuleR1, "repeat 3"},
		{`gpu1 -$ g: characters(nobody)`, "ItemNotFound", RuleSemantic, "nobody"},
		{`gpu1 -$ g: poses(#nosuchtag)`, "ItemNotFound", RuleSemantic, "#nosuchtag"},
		{`gpu1 -$ g: characters where emotions = sad`, "CommandWhereGroupMissing", RuleSemantic, "emotions"},
		{`gpu1 -$ g: characters with colour=3`, "CommandSettingInvalid", RuleSemantic, "colour"},
	}
	cat := sampleCatalogue()
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			ps := validationProblems(t, cat, tt.in, Options{})
			if len(ps) != 1 {
				t.Fatalf("problems = %+v, want exactly one", ps)
			}
			p := ps[0]
			if p.Tag != tt.tag || p.Rule != tt.rule {
				t.Errorf("tag, rule = %s, %s; want %s, %s (%s)", p.Tag, p.Rule, tt.tag, tt.rule, p.Message)
			}
			if i := strings.Index(tt.in, tt.at); p.Position.Offset != i || p.Position.Length != len(tt.at) {
				t.Errorf("position = %+v, want {%d %d}", p.Position, i, len(tt.at))
			}
			if p.Err == nil || p.Err.Tag != p.Tag || !p.Err.HasRoute("Validate") {
				t.Errorf("problem route = %v", p.Err)
			}
			pv := PreviewOf(mustParse(t, tt.in), cat, Options{})
			if pv.Valid || len(pv.Problems) != 1 {
				t.Errorf("preview = %+v", pv)
			}
		})
	}
}

// Acceptance test 4: one error per problem, anywhere in the command, nothing created.
func TestValidateCollectsEveryProblem(t *testing.T) {
	cat := sampleCatalogue()
	text := `nowhere, gpu1 -$ missing: nogroup * characters(nobody) * scene[subject: alsomissing, other: poses(#zzz)] ` +
		`* duo{red: characters(ghost), blue: characters} where characters = alice and nope = x ` +
		`spread by nogroup > nofixer on nohost`
	x, err := Expand(mustParse(t, text), cat, Options{})
	if x != nil || err == nil {
		t.Fatal("an invalid command must not expand")
	}
	appErr, ok := errors.FromError(err)
	if !ok || !appErr.HasRoute("Expand->validationError.CommandValidationFailed") {
		t.Fatalf("unexpected error %v", err)
	}
	ps, _ := appErr.Metadata["errors"].([]Problem)
	want := map[string]int{
		"ServerNotFound":           2, // nowhere, nohost
		"GeneratorNotFound":        1,
		"FixerNotFound":            1,
		"GroupNotFound":            4, // nogroup (term), alsomissing, nope, nogroup (spread by)
		"ItemNotFound":             3, // nobody, #zzz, ghost
		"CommandWhereGroupMissing": 1, // nope is not among the selections
	}
	got := map[string]int{}
	for _, p := range ps {
		got[p.Tag]++
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("problems by tag = %v, want %v\nall: %v", got, want, tags(ps))
	}
}

func TestValidateReportsProblemsInTheirPlaces(t *testing.T) {
	cat := sampleCatalogue()
	tests := []struct {
		name string
		in   string
		want []string
	}{
		{"unknown server", `nowhere -$ g: characters`, []string{"ServerNotFound"}},
		{"unknown generator", `gpu1 -$ nope: characters`, []string{"GeneratorNotFound"}},
		{"unknown fixer", `gpu1 -$ g: characters > nope`, []string{"FixerNotFound"}},
		{"unknown on server", `gpu1 -$ g: characters > upscale on nowhere`, []string{"ServerNotFound"}},
		{"unknown group", `gpu1 -$ g: nope`, []string{"GroupNotFound"}},
		{"unknown group in merge", `gpu1 -$ g: characters && nope`, []string{"GroupNotFound"}},
		{"unknown group in zip", `gpu1 -$ g: characters || nope`, []string{"GroupNotFound"}},
		{"unknown group in template body", `gpu1 -$ g: scene[subject: nope]`, []string{"GroupNotFound"}},
		{"unknown item in template body", `gpu1 -$ g: scene[subject: characters(nobody)]`, []string{"ItemNotFound"}},
		{"unknown item in region body", `gpu1 -$ g: duo{red: characters(nobody), blue: characters}`, []string{"ItemNotFound"}},
		{"unknown template base", `gpu1 -$ g: nope[t: characters]`, []string{"GroupNotFound"}},
		{"unknown region base", `gpu1 -$ g: nope{red: characters}`, []string{"GroupNotFound"}},
		{"unknown where group", `gpu1 -$ g: characters where nope = x`, []string{"GroupNotFound", "CommandWhereGroupMissing"}},
		{"where selector matches nothing", `gpu1 -$ g: characters where characters = nobody`, []string{"ItemNotFound"}},
		{"where in matches nothing", `gpu1 -$ g: characters where characters in (alice, nobody%)`, []string{"ItemNotFound"}},
		{"where nested", `gpu1 -$ g: characters where not (#nosuch or characters = zed)`, []string{"ItemNotFound"}},
		{"where group from a template body counts", `gpu1 -$ g: scene[subject: characters] where characters = alice`, nil},
		{"where group from a region body counts", `gpu1 -$ g: duo{red: characters, blue: style} where style = style1`, nil},
		{"where group from a merge counts", `gpu1 -$ g: characters && anime where anime = anime1`, nil},
		{"exclude selector matches nothing", `gpu1 -$ g: characters(~nobody)`, []string{"ItemNotFound"}},
		{"spread by unknown group", `gpu1, gpu2 -$ g: nope spread by nope`, []string{"GroupNotFound", "GroupNotFound"}},
		{"duplicate setting", `gpu1 -$ g: characters with steps=1, steps=2`, []string{"CommandKeyDuplicated"}},
		{"duplicate template tag", `gpu1 -$ g: scene[subject: characters, subject: poses]`, []string{"CommandKeyDuplicated"}},
		{"duplicate region keyword", `gpu1 -$ g: duo{red: characters, red: poses, blue: style}`, []string{"CommandKeyDuplicated"}},
		{"region keyword missing in command", `gpu1 -$ g: duo{red: characters}`, []string{"CommandRegionMismatch"}},
		{"region keyword missing in group", `gpu1 -$ g: duo{red: characters, blue: poses, green: style}`, []string{"CommandRegionMismatch"}},
		{"region both ways", `gpu1 -$ g: duo{red: characters, purple: poses}`, []string{"CommandRegionMismatch", "CommandRegionMismatch"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tags(validationProblems(t, cat, tt.in, Options{}))
			sort.Strings(got)
			want := append([]string(nil), tt.want...)
			sort.Strings(want)
			if len(got) != len(want) || (len(got) > 0 && !reflect.DeepEqual(got, want)) {
				t.Errorf("problems = %v, want %v", got, want)
			}
		})
	}
}

// Acceptance test 5: a region command whose keywords differ is rejected with both diff messages.
func TestValidateRegionDiffMessages(t *testing.T) {
	ps := validationProblems(t, sampleCatalogue(), `gpu1 -$ g: duo{red: characters, green: poses}`, Options{})
	if len(ps) != 2 {
		t.Fatalf("problems = %+v", ps)
	}
	var msgs []string
	for _, p := range ps {
		msgs = append(msgs, p.Message)
	}
	joined := strings.Join(msgs, "\n")
	for _, want := range []string{`"green" is missing in group "duo"`, `"blue" of group "duo" is missing in the command`} {
		if !strings.Contains(joined, want) {
			t.Errorf("no message containing %s in:\n%s", want, joined)
		}
	}
	// Two region groups in one command is S5, rejected by the parser.
	if _, err := Parse(`gpu1 -$ g: duo{red: characters, blue: poses} * duo{red: characters, blue: poses}`); err == nil {
		t.Error("two regions were accepted")
	}
	// Keywords from masks and coordinates together form the group's set.
	cat := sampleCatalogue()
	cat.group("mixed",
		Item{Code: "a", MaskFiles: []MaskFile{{Keyword: "red", File: "r.png"}}},
		Item{Code: "b", Coordinates: []Coordinates{{Keyword: "left", Width: 1, Height: 1}}},
	)
	if ps := validationProblems(t, cat, `gpu1 -$ g: mixed{red: characters, left: poses}`, Options{}); len(ps) != 0 {
		t.Errorf("problems = %+v", ps)
	}
}

func TestValidateSettings(t *testing.T) {
	cat := sampleCatalogue()
	tests := []struct {
		in string
		ok bool
	}{
		{`gpu1 -$ g: characters with width=512, height=768, steps=30, batch_size=2, seed=7`, true},
		{`gpu1 -$ g: characters with cfg=7, denoise=1`, true},
		{`gpu1 -$ g: characters with cfg=6.5, denoise=0.75`, true},
		{`gpu1 -$ g: characters with denoise=0`, true},
		{`gpu1 -$ g: characters with sampler_name=dpmpp_2m, scheduler="karras", file_type=png`, true},
		{`gpu1 -$ g: characters with seed=18446744073709551615`, true},
		{`gpu1 -$ g: characters with width=0`, false},
		{`gpu1 -$ g: characters with width=2.5`, false},
		{`gpu1 -$ g: characters with width="512"`, false},
		{`gpu1 -$ g: characters with width=99999999999`, false},
		{`gpu1 -$ g: characters with steps=abc`, false},
		{`gpu1 -$ g: characters with seed=1.5`, false},
		{`gpu1 -$ g: characters with cfg=abc`, false},
		{`gpu1 -$ g: characters with denoise=1.5`, false},
		{`gpu1 -$ g: characters with denoise=2`, false},
		{`gpu1 -$ g: characters with sampler_name=1.5`, false},
		{`gpu1 -$ g: characters with fps=24`, false},
		{`gpu1 -$ vid: characters with width=640, height=480, num_frames=16, fps=24, duration_seconds=2.5, file_type=mp4`, true},
		{`gpu1 -$ vid: characters with fps=0`, false},
		{`gpu1 -$ vid: characters with duration_seconds=0.0`, false},
		{`gpu1 -$ vid: characters with steps=20`, false},
		{`gpu1 -$ vid: characters with seed=1`, false},
		{`gpu1 -$ mesh: characters with file_type=glb`, true},
		{`gpu1 -$ mesh: characters with width=10`, false},
		{`gpu1 -$ odd: characters with file_type=png`, false},
		{`gpu1 -$ odd: characters`, true},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			ps := validationProblems(t, cat, tt.in, Options{})
			if tt.ok && len(ps) != 0 {
				t.Errorf("unexpected problems: %+v", ps)
			}
			if !tt.ok && (len(ps) != 1 || ps[0].Tag != "CommandSettingInvalid") {
				t.Errorf("problems = %+v, want one CommandSettingInvalid", ps)
			}
		})
	}
}

// Acceptance test 19: repeat needs seed variation.
func TestValidateRepeatNeedsSeed(t *testing.T) {
	cat := sampleCatalogue()
	for _, tt := range []struct {
		in string
		ok bool
	}{
		{`gpu1 -$ g: characters repeat 2`, false},
		{`gpu1 -$ g: characters repeat 1`, true},
		{`gpu1 -$ g: characters repeat 2 with seed=1`, true},
		{`gpu1 -$ gr: characters repeat 2`, true},
		{`gpu1 -$ nope: characters repeat 2`, false}, // only the unknown generator is reported
	} {
		ps := validationProblems(t, cat, tt.in, Options{})
		if tt.ok != (len(ps) == 0) {
			t.Errorf("%s: problems = %+v", tt.in, ps)
		}
	}
	ps := validationProblems(t, cat, `gpu1 -$ nope: characters repeat 2`, Options{})
	if len(ps) != 1 || ps[0].Tag != "GeneratorNotFound" {
		t.Errorf("problems = %+v", ps)
	}
}

func TestValidateEmptyResults(t *testing.T) {
	cat := sampleCatalogue()
	tests := []struct {
		name string
		in   string
		want []string
	}{
		{"empty group", `gpu1 -$ g: empty`, []string{"CommandSelectionEmpty"}},
		{"exclude everything", `gpu1 -$ g: characters(~alice, ~bob, ~carol)`, []string{"CommandSelectionEmpty"}},
		{"empty group in a product", `gpu1 -$ g: characters * empty`, []string{"CommandSelectionEmpty"}},
		{"empty operand of a zip", `gpu1 -$ g: characters || empty`, []string{"CommandSelectionEmpty"}},
		{"empty operand of a merge is fine", `gpu1 -$ g: characters && empty`, nil},
		{"empty template body", `gpu1 -$ g: scene[subject: empty]`, []string{"CommandSelectionEmpty"}},
		{"empty template base", `gpu1 -$ g: empty[subject: characters]`, []string{"CommandSelectionEmpty"}},
		{"empty region body", `gpu1 -$ g: duo{red: empty, blue: characters}`, []string{"CommandSelectionEmpty"}},
		{"where removes all", `gpu1 -$ g: characters * poses where characters = alice and characters = bob`, []string{"CommandWhereEmpty"}},
		{"where removes all by tag", `gpu1 -$ g: characters where #acrobatic`, []string{"CommandWhereEmpty"}},
		{"where leaves one", `gpu1 -$ g: characters * poses where characters = alice and poses = jumping`, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tags(validationProblems(t, cat, tt.in, Options{}))
			if len(got) != len(tt.want) || (len(got) > 0 && !reflect.DeepEqual(got, tt.want)) {
				t.Errorf("problems = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestExpandTooLarge(t *testing.T) {
	cat := newFakeCatalogue()
	cat.servers["s"] = true
	cat.generators["g"] = Generator{OutputType: OutputImage}
	cat.group("a", words(ids(40)...)...).group("b", words(ids(40)...)...).group("c", words(ids(40)...)...)
	tests := []struct {
		name string
		in   string
		opts Options
	}{
		{"product", `s -$ g: a * b * c`, Options{MaxCombinations: 1000}},
		{"jobs", `s -$ g: a * b`, Options{MaxJobs: 1000}},
		{"jobs with repeat", `s -$ g: a repeat 50 with seed=1`, Options{MaxJobs: 1000}},
		{"jobs with limit and repeat", `s -$ g: a * b limit 100 repeat 20 with seed=1`, Options{MaxJobs: 1000}},
		{"jobs after where", `s -$ g: a * b where a = i1 or b = i1 or a = i2`, Options{MaxJobs: 50}},
		{"template", `s -$ g: a[x: b * c]`, Options{MaxCombinations: 1000}},
		{"merge", `s -$ g: a && b && c`, Options{MaxCombinations: 100}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ps := validationProblems(t, cat, tt.in, tt.opts)
			if len(ps) == 0 || ps[0].Tag != "CommandTooLarge" {
				t.Errorf("problems = %+v, want CommandTooLarge", ps)
			}
		})
	}
	// Just under the limit is fine.
	if ps := validationProblems(t, cat, `s -$ g: a * b`, Options{MaxCombinations: 1600, MaxJobs: 1600}); len(ps) != 0 {
		t.Errorf("problems = %+v", ps)
	}
}

// Route tests: every tag found by evaluation names the check that found it.
func TestSemanticProblemRoutes(t *testing.T) {
	cat := sampleCatalogue()
	tests := []struct {
		in    string
		tag   string
		route string
	}{
		{`nowhere -$ g: characters`, "ServerNotFound", "Validate->analyse->checker.check->checker.add->semantic.ServerNotFound"},
		{`gpu1 -$ nope: characters`, "GeneratorNotFound", "checker.check->checker.add->semantic.GeneratorNotFound"},
		{`gpu1 -$ g: characters > nope`, "FixerNotFound", "checker.check->checker.add->semantic.FixerNotFound"},
		{`gpu1 -$ g: nope`, "GroupNotFound", "checker.checkGroupTerm->checker.add->semantic.GroupNotFound"},
		{`gpu1 -$ g: characters(nobody)`, "ItemNotFound", "checker.checkSelectors->checker.add->semantic.ItemNotFound"},
		{`gpu1 -$ g: duo{red: characters}`, "CommandRegionMismatch", "checker.checkRegionKeywords->checker.add->semantic.CommandRegionMismatch"},
		{`gpu1 -$ g: characters where emotions = sad`, "CommandWhereGroupMissing", "checker.checkCond->checker.add->semantic.CommandWhereGroupMissing"},
		{`gpu1 -$ g: characters with colour=1`, "CommandSettingInvalid", "checker.checkSettings->checker.add->semantic.CommandSettingInvalid"},
		{`gpu1 -$ g: characters with steps=1, steps=2`, "CommandKeyDuplicated", "checker.checkSettings->checker.add->semantic.CommandKeyDuplicated"},
		{`gpu1 -$ g: empty`, "CommandSelectionEmpty", "newPlan->evaluator.selection->evaluator.add->semantic.CommandSelectionEmpty"},
		{`gpu1 -$ g: characters where #acrobatic`, "CommandWhereEmpty", "newPlan->plan.pick->semantic.CommandWhereEmpty"},
		{`gpu1 -$ g: characters repeat 2`, "CommandRestrictionViolated", "checker.checkRepeat->checker.add->semantic.CommandRestrictionViolated"},
	}
	for _, tt := range tests {
		t.Run(tt.tag, func(t *testing.T) {
			var found *Problem
			for _, p := range validationProblems(t, cat, tt.in, Options{}) {
				if p.Tag == tt.tag {
					found = &p
				}
			}
			if found == nil {
				t.Fatalf("no %s problem", tt.tag)
			}
			if !found.Err.HasRoute(tt.route) {
				t.Errorf("route = %s\nwant substring %s", found.Err.Route(), tt.route)
			}
		})
	}

	var found *Problem
	big := newFakeCatalogue()
	big.servers["s"] = true
	big.generators["g"] = Generator{OutputType: OutputImage}
	big.group("a", words(ids(30)...)...)
	for _, p := range validationProblems(t, big, `s -$ g: a * a`, Options{MaxCombinations: 100}) {
		found = &p
	}
	if found == nil || found.Tag != "CommandTooLarge" || !found.Err.HasRoute("newPlan->semantic.CommandTooLarge") {
		t.Errorf("too large problem = %+v", found)
	}
}

func TestPreviewOfInvalidCommand(t *testing.T) {
	pv := PreviewOf(mustParse(t, `gpu1 -$ g: characters(nobody) * nope`), sampleCatalogue(), Options{})
	if pv.Valid || len(pv.Problems) != 2 || pv.Jobs != 0 {
		t.Errorf("preview = %+v", pv)
	}
	if pv.Warnings == nil || pv.PerServer == nil {
		t.Error("warnings and per-server lists must be empty lists, not null")
	}
}
