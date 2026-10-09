package command

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/rm4n0s/errors"
)

// The example of SPEC §6.10, completed with the filter of `emotions(cry)` that
// the spec elides.
const specCommand = `gpu1, gpu2 -$ anime: characters * poses(~jumping) * emotions(cry) * "masterpiece"
    where not #acrobatic
    sample 20 seed 7  repeat 3
    with steps=30, cfg=6.5
    spread blocks
    > upscale on gpu2`

const specJSON = `{
  "servers": [{"server_code_name": "gpu1", "weight": 1}, {"server_code_name": "gpu2", "weight": 1}],
  "generator_code_name": "anime",
  "group_selections": [
    {"group_code_name": "characters", "include_only": null, "exclude": null},
    {"group_code_name": "poses", "include_only": null, "exclude": ["jumping"]},
    {"group_code_name": "emotions", "include_only": ["cry"], "exclude": null},
    {"literal": "masterpiece", "negative": false}
  ],
  "where": {"op": "not", "arg": {"op": "tag", "tag": "acrobatic"}},
  "sample": {"count": 20, "seed": 7},
  "repeat": 3,
  "overrides": {"steps": 30, "cfg": 6.5},
  "spread": {"mode": "blocks", "by_group": null, "pinned": false},
  "fixers": [{"fixer_code_name": "upscale", "on": "gpu2"}]
}`

func jsonEqual(t *testing.T, got, want string) {
	t.Helper()
	var g, w any
	if err := json.Unmarshal([]byte(got), &g); err != nil {
		t.Fatalf("got is not JSON: %v\n%s", err, got)
	}
	if err := json.Unmarshal([]byte(want), &w); err != nil {
		t.Fatalf("want is not JSON: %v", err)
	}
	if !reflect.DeepEqual(g, w) {
		t.Errorf("JSON differs\ngot:  %s\nwant: %s", got, want)
	}
}

func TestToJSONMatchesSpecExample(t *testing.T) {
	b, err := ToJSON(mustParse(t, specCommand))
	if err != nil {
		t.Fatal(err)
	}
	jsonEqual(t, string(b), specJSON)
}

func TestFromJSONSpecExample(t *testing.T) {
	c, err := FromJSON([]byte(specJSON))
	if err != nil {
		t.Fatal(err)
	}
	want := `gpu1, gpu2 -$ anime: characters * poses(~jumping) * emotions(cry) * "masterpiece" where not #acrobatic sample 20 seed 7 repeat 3 with steps=30, cfg=6.5 spread blocks > upscale on gpu2`
	if got := Format(c); got != want {
		t.Errorf("Format = %s\nwant     %s", got, want)
	}
}

func TestToJSONShapes(t *testing.T) {
	tests := []struct{ in, want string }{
		{
			`s -$ g: a && b(x) && "t" * c || d`,
			`{"servers":[{"server_code_name":"s","weight":1}],"generator_code_name":"g","group_selections":[
			  {"group_code_name":"a+b+lit","include_only":null,"exclude":null,"is_merged":true,"merged_groups":[
			    {"group_code_name":"a","include_only":null,"exclude":null},
			    {"group_code_name":"b","include_only":["x"],"exclude":null},
			    {"literal":"t","negative":false}]},
			  {"group_code_name":"c|d","include_only":null,"exclude":null,"is_zipped":true,"zipped_groups":[
			    {"group_code_name":"c","include_only":null,"exclude":null},
			    {"group_code_name":"d","include_only":null,"exclude":null}]}]}`,
		},
		{
			`s -$ g: scene(~x)[subject: chars(alice) * "p", pose: poses] * duo{red: a, blue: b}`,
			`{"servers":[{"server_code_name":"s","weight":1}],"generator_code_name":"g","group_selections":[
			  {"group_code_name":"scene","include_only":null,"exclude":["x"],"is_template":true,"template_group_selections":{
			    "subject":[{"group_code_name":"chars","include_only":["alice"],"exclude":null},{"literal":"p","negative":false}],
			    "pose":[{"group_code_name":"poses","include_only":null,"exclude":null}]}},
			  {"group_code_name":"duo","include_only":null,"exclude":null,"is_regioned":true,"region_group_selections":{
			    "red":[{"group_code_name":"a","include_only":null,"exclude":null}],
			    "blue":[{"group_code_name":"b","include_only":null,"exclude":null}]}}]}`,
		},
		{
			`a=3, b -$ g: x(p%, #t, q) where x = r% and y in (p, #t) or not (x = p and #u) limit 4 with n=7, s="two words", t=word, d=2.0 spread by x pinned`,
			`{"servers":[{"server_code_name":"a","weight":3},{"server_code_name":"b","weight":1}],"generator_code_name":"g",
			  "group_selections":[{"group_code_name":"x","include_only":["p%","#t","q"],"exclude":null}],
			  "where":{"op":"or","args":[
			    {"op":"and","args":[{"op":"eq","group":"x","selector":"r%"},{"op":"in","group":"y","selectors":["p","#t"]}]},
			    {"op":"not","arg":{"op":"and","args":[{"op":"eq","group":"x","selector":"p"},{"op":"tag","tag":"u"}]}}]},
			  "limit":4,
			  "overrides":{"n":7,"s":"two words","t":"word","d":2.0},
			  "spread":{"mode":"by","by_group":"x","pinned":true}}`,
		},
		{
			`s -$ g: a sample 5`,
			`{"servers":[{"server_code_name":"s","weight":1}],"generator_code_name":"g",
			  "group_selections":[{"group_code_name":"a","include_only":null,"exclude":null}],"sample":{"count":5}}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			b, err := ToJSON(mustParse(t, tt.in))
			if err != nil {
				t.Fatal(err)
			}
			jsonEqual(t, string(b), tt.want)
		})
	}
}

// Key order decides the order of the product, so the JSON must keep it.
func TestJSONKeepsKeyOrder(t *testing.T) {
	b, err := ToJSON(mustParse(t, `s -$ g: x[z: a, y: b, w: c] with q=1, b=2, a=3`))
	if err != nil {
		t.Fatal(err)
	}
	text := string(b)
	order := func(first, second string) {
		t.Helper()
		if strings.Index(text, first) > strings.Index(text, second) {
			t.Errorf("%s should come before %s in %s", first, second, text)
		}
	}
	order(`"z":`, `"y":`)
	order(`"y":`, `"w":`)
	order(`"q":1`, `"b":2`)
	order(`"b":2`, `"a":3`)
	c, err := FromJSON(b)
	if err != nil {
		t.Fatal(err)
	}
	if got := Format(c); got != `s -$ g: x[z: a, y: b, w: c] with q=1, b=2, a=3` {
		t.Errorf("order lost: %s", got)
	}
}

// Acceptance test 1b: every valid command converts to JSON and back unchanged.
func TestJSONRoundTripValidCommands(t *testing.T) {
	for _, tt := range validCommands {
		t.Run(tt.in, func(t *testing.T) {
			assertRoundTrip(t, mustParse(t, tt.in))
		})
	}
}

func assertRoundTrip(t *testing.T, c *Command) {
	t.Helper()
	first, err := ToJSON(c)
	if err != nil {
		t.Fatal(err)
	}
	back, err := FromJSON(first)
	if err != nil {
		t.Fatalf("FromJSON(%s): %v", first, err)
	}
	second, err := ToJSON(back)
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Errorf("JSON changed on the round trip\nfirst:  %s\nsecond: %s", first, second)
	}
	if Format(back) != Format(c) {
		t.Errorf("text changed on the round trip\nfirst:  %s\nsecond: %s", Format(c), Format(back))
	}
}

func TestJSONLargeIntegersSurvive(t *testing.T) {
	c := mustParse(t, `s -$ g: a sample 2 seed 18446744073709551615 with seed=18446744073709551615`)
	b, err := ToJSON(c)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(b), "18446744073709551615") != 2 {
		t.Errorf("64-bit integers lost precision: %s", b)
	}
	assertRoundTrip(t, c)
}

func TestFromJSONRejects(t *testing.T) {
	const sel = `[{"group_code_name":"a","include_only":null,"exclude":null}]`
	base := func(extra string) string {
		return `{"servers":[{"server_code_name":"s","weight":1}],"generator_code_name":"g","group_selections":` + sel + extra + `}`
	}
	tests := []struct {
		name string
		in   string
		tag  string
		rule string
	}{
		{"not JSON", `{`, "CommandJSONInvalid", ""},
		{"trailing data", base("") + ` {}`, "CommandJSONInvalid", ""},
		{"unknown field", base(`,"extra":1`), "CommandJSONInvalid", ""},
		{"no servers", `{"servers":[],"generator_code_name":"g","group_selections":` + sel + `}`, "CommandJSONInvalid", ""},
		{"reserved generator", strings.Replace(base(""), `"g"`, `"with"`, 1), "CommandJSONInvalid", ""},
		{"bad group name", strings.Replace(base(""), `"a"`, `"a b"`, 1), "CommandJSONInvalid", ""},
		{"empty selections", `{"servers":[{"server_code_name":"s","weight":1}],"generator_code_name":"g","group_selections":[]}`, "CommandRestrictionViolated", RuleS6},
		{"zero weight", `{"servers":[{"server_code_name":"s","weight":0}],"generator_code_name":"g","group_selections":` + sel + `}`, "CommandRestrictionViolated", RuleS8},
		{"zero limit", base(`,"limit":0`), "CommandRestrictionViolated", RuleS8},
		{"duplicate target", `{"servers":[{"server_code_name":"s","weight":1},{"server_code_name":"s","weight":1}],"generator_code_name":"g","group_selections":` + sel + `,"spread":{"mode":"even","by_group":null,"pinned":false}}`, "CommandRestrictionViolated", RuleT1},
		{"weight in dynamic", `{"servers":[{"server_code_name":"s","weight":2},{"server_code_name":"t","weight":1}],"generator_code_name":"g","group_selections":` + sel + `}`, "CommandRestrictionViolated", RuleT2},
		{"spread with one server", base(`,"spread":{"mode":"even","by_group":null,"pinned":false}`), "CommandRestrictionViolated", RuleT3},
		{"unknown spread mode", base(`,"spread":{"mode":"fast","by_group":null,"pinned":false}`), "CommandJSONInvalid", ""},
		{"by without group", base(`,"spread":{"mode":"by","by_group":null,"pinned":false}`), "CommandJSONInvalid", ""},
		{"group with non-by mode", base(`,"spread":{"mode":"even","by_group":"a","pinned":false}`), "CommandJSONInvalid", ""},
		{"empty literal", `{"servers":[{"server_code_name":"s","weight":1}],"generator_code_name":"g","group_selections":[{"literal":"  ","negative":false}]}`, "CommandRestrictionViolated", RuleS7},
		{"include and exclude", `{"servers":[{"server_code_name":"s","weight":1}],"generator_code_name":"g","group_selections":[{"group_code_name":"a","include_only":["x"],"exclude":["y"]}]}`, "CommandJSONInvalid", ""},
		{"empty filter", `{"servers":[{"server_code_name":"s","weight":1}],"generator_code_name":"g","group_selections":[{"group_code_name":"a","include_only":[],"exclude":null}]}`, "CommandJSONInvalid", ""},
		{"bad selector", `{"servers":[{"server_code_name":"s","weight":1}],"generator_code_name":"g","group_selections":[{"group_code_name":"a","include_only":["x y"],"exclude":null}]}`, "CommandJSONInvalid", ""},
		{"bad pattern", `{"servers":[{"server_code_name":"s","weight":1}],"generator_code_name":"g","group_selections":[{"group_code_name":"a","include_only":["x-%"],"exclude":null}]}`, "CommandJSONInvalid", ""},
		{"bad tag selector", `{"servers":[{"server_code_name":"s","weight":1}],"generator_code_name":"g","group_selections":[{"group_code_name":"a","include_only":["#with"],"exclude":null}]}`, "CommandJSONInvalid", ""},
		{"merge of one", `{"servers":[{"server_code_name":"s","weight":1}],"generator_code_name":"g","group_selections":[{"group_code_name":"a","include_only":null,"exclude":null,"is_merged":true,"merged_groups":` + sel + `}]}`, "CommandJSONInvalid", ""},
		{"merge and zip", `{"servers":[{"server_code_name":"s","weight":1}],"generator_code_name":"g","group_selections":[{"group_code_name":"a","include_only":null,"exclude":null,"is_merged":true,"is_zipped":true}]}`, "CommandJSONInvalid", ""},
		{"operands without flag", `{"servers":[{"server_code_name":"s","weight":1}],"generator_code_name":"g","group_selections":[{"group_code_name":"a","include_only":null,"exclude":null,"merged_groups":` + sel + `}]}`, "CommandJSONInvalid", ""},
		{"merge with filter", `{"servers":[{"server_code_name":"s","weight":1}],"generator_code_name":"g","group_selections":[{"group_code_name":"a","include_only":["x"],"exclude":null,"is_merged":true,"merged_groups":[{"literal":"a","negative":false},{"literal":"b","negative":false}]}]}`, "CommandJSONInvalid", ""},
		{"template without binds", `{"servers":[{"server_code_name":"s","weight":1}],"generator_code_name":"g","group_selections":[{"group_code_name":"a","include_only":null,"exclude":null,"is_template":true,"template_group_selections":{}}]}`, "CommandJSONInvalid", ""},
		{"empty body", `{"servers":[{"server_code_name":"s","weight":1}],"generator_code_name":"g","group_selections":[{"group_code_name":"a","include_only":null,"exclude":null,"is_template":true,"template_group_selections":{"t":[]}}]}`, "CommandJSONInvalid", ""},
		{"duplicate bind key", `{"servers":[{"server_code_name":"s","weight":1}],"generator_code_name":"g","group_selections":[{"group_code_name":"a","include_only":null,"exclude":null,"is_template":true,"template_group_selections":{"t":` + sel + `,"t":` + sel + `}}]}`, "CommandJSONInvalid", ""},
		{"region in body", `{"servers":[{"server_code_name":"s","weight":1}],"generator_code_name":"g","group_selections":[{"group_code_name":"a","include_only":null,"exclude":null,"is_template":true,"template_group_selections":{"t":[{"group_code_name":"r","include_only":null,"exclude":null,"is_regioned":true,"region_group_selections":{"k":` + sel + `}}]}}]}`, "CommandRestrictionViolated", RuleS3},
		{"literal with group", `{"servers":[{"server_code_name":"s","weight":1}],"generator_code_name":"g","group_selections":[{"literal":"x","group_code_name":"a"}]}`, "CommandJSONInvalid", ""},
		{"negative on group", `{"servers":[{"server_code_name":"s","weight":1}],"generator_code_name":"g","group_selections":[{"group_code_name":"a","include_only":null,"exclude":null,"negative":true}]}`, "CommandJSONInvalid", ""},
		{"negative override", base(`,"overrides":{"n":-1}`), "CommandJSONInvalid", ""},
		{"bool override", base(`,"overrides":{"n":true}`), "CommandJSONInvalid", ""},
		{"duplicate override", base(`,"overrides":{"n":1,"n":2}`), "CommandJSONInvalid", ""},
		{"reserved override key", base(`,"overrides":{"with":1}`), "CommandJSONInvalid", ""},
		{"bad where op", base(`,"where":{"op":"xor"}`), "CommandJSONInvalid", ""},
		{"and with one arg", base(`,"where":{"op":"and","args":[{"op":"tag","tag":"t"}]}`), "CommandJSONInvalid", ""},
		{"not without arg", base(`,"where":{"op":"not"}`), "CommandJSONInvalid", ""},
		{"in without selectors", base(`,"where":{"op":"in","group":"a"}`), "CommandJSONInvalid", ""},
		{"eq with bad selector", base(`,"where":{"op":"eq","group":"a","selector":"x y"}`), "CommandJSONInvalid", ""},
		{"bad tag", base(`,"where":{"op":"tag","tag":"in"}`), "CommandJSONInvalid", ""},
		{"bad fixer", base(`,"fixers":[{"fixer_code_name":"on"}]`), "CommandJSONInvalid", ""},
		{"bad fixer server", base(`,"fixers":[{"fixer_code_name":"f","on":"a b"}]`), "CommandJSONInvalid", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := FromJSON([]byte(tt.in))
			appErr, ok := errors.FromError(err)
			if !ok {
				t.Fatalf("expected *errors.Error, got %T (%v)", err, err)
			}
			if appErr.Tag != tt.tag {
				t.Errorf("tag = %s (%s), want %s", appErr.Tag, appErr.Message, tt.tag)
			}
			if !appErr.HasRoute("FromJSON") {
				t.Errorf("unexpected failure path: %s", appErr.Route())
			}
			if tt.rule != "" {
				if p, _ := ProblemOf(err); p.Rule != tt.rule {
					t.Errorf("rule = %q, want %q", p.Rule, tt.rule)
				}
			}
		})
	}
}

func TestToJSONRefusesRepeatedKeys(t *testing.T) {
	tests := []struct{ in, key string }{
		{`s -$ g: a with n=1, n=2`, "n"},
		{`s -$ g: x[t: a, t: b]`, "t"},
		{`s -$ g: r{k: a, k: b}`, "k"},
		{`s -$ g: p && x[t: y[u: a, u: b]]`, "u"},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			c := mustParse(t, tt.in)
			key, dup := DuplicateKey(c)
			if !dup || key != tt.key {
				t.Fatalf("DuplicateKey = %q, %v; want %q", key, dup, tt.key)
			}
			_, err := ToJSON(c)
			appErr, ok := errors.FromError(err)
			if !ok || appErr.Tag != "CommandJSONInvalid" || !appErr.HasRoute("ToJSON") {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
	if _, dup := DuplicateKey(mustParse(t, `s -$ g: a * b with n=1, m=2`)); dup {
		t.Error("DuplicateKey reported a duplicate in a clean command")
	}
}
