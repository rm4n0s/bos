package command

import (
	"strings"
	"testing"

	"github.com/rm4n0s/errors"
)

func mustParse(t *testing.T, text string) *Command {
	t.Helper()
	c, err := Parse(text)
	if err != nil {
		t.Fatalf("Parse(%q): %v", text, err)
	}
	return c
}

// validCommands are the valid rows of SPEC §6.12 (job counts are checked by
// the expander tests) with the canonical text each must format to, plus the
// constructs of the grammar that table does not cover.
var validCommands = []struct{ in, canonical string }{
	{`gpu1 -$ g: characters * poses(~jumping) * emotions(cry)`, `gpu1 -$ g: characters * poses(~jumping) * emotions(cry)`},
	{`gpu1 -$ g: characters(alice, bob) * emotions`, `gpu1 -$ g: characters(alice, bob) * emotions`},
	{`gpu1 -$ g: characters && anime`, `gpu1 -$ g: characters && anime`},
	{`gpu1 -$ g: characters || emotions`, `gpu1 -$ g: characters || emotions`},
	{`gpu1 -$ g: characters*poses(standing)`, `gpu1 -$ g: characters * poses(standing)`},
	{`gpu1 -$ g: characters * poses(#acrobatic)`, `gpu1 -$ g: characters * poses(#acrobatic)`},
	{`gpu1 -$ g: characters * poses(run%)`, `gpu1 -$ g: characters * poses(run%)`},
	{`gpu1 -$ g: characters * "masterpiece, best quality"`, `gpu1 -$ g: characters * "masterpiece, best quality"`},
	{`gpu1 -$ g: characters * !"blurry"`, `gpu1 -$ g: characters * !"blurry"`},
	{`gpu1 -$ g: "red hair" && "blue hair" * characters`, `gpu1 -$ g: "red hair" && "blue hair" * characters`},
	{`gpu1 -$ g: scene[subject: characters(alice, bob)]`, `gpu1 -$ g: scene[subject: characters(alice, bob)]`},
	{`gpu1 -$ g: duo{red: characters(alice), blue: characters(bob, carol)} * style`, `gpu1 -$ g: duo{red: characters(alice), blue: characters(bob, carol)} * style`},
	{`gpu1 -$ g: characters * poses where not #acrobatic`, `gpu1 -$ g: characters * poses where not #acrobatic`},
	{`gpu1 -$ g: characters * poses where not (characters = alice and poses = jumping)`, `gpu1 -$ g: characters * poses where not (characters = alice and poses = jumping)`},
	{`gpu1 -$ g: characters * poses * emotions limit 20`, `gpu1 -$ g: characters * poses * emotions limit 20`},
	{`gpu1 -$ g: characters * poses * emotions sample 10 seed 7`, `gpu1 -$ g: characters * poses * emotions sample 10 seed 7`},
	{`gpu1 -$ gr: characters * poses limit 5 repeat 4`, `gpu1 -$ gr: characters * poses limit 5 repeat 4`},
	{`gpu1 -$ g: characters * poses repeat 3 with seed=42`, `gpu1 -$ g: characters * poses repeat 3 with seed=42`},
	{`gpu1 -$ g: characters with steps=30, cfg=6.5, sampler_name=dpmpp_2m`, `gpu1 -$ g: characters with steps=30, cfg=6.5, sampler_name=dpmpp_2m`},
	{`gpu1, gpu2 -$ g: characters * poses`, `gpu1, gpu2 -$ g: characters * poses`},
	{`gpu1=3, gpu2=1 -$ g: characters * poses spread blocks`, `gpu1=3, gpu2 -$ g: characters * poses spread blocks`},
	{`gpu1, gpu2 -$ g: characters * poses spread even pinned`, `gpu1, gpu2 -$ g: characters * poses spread even pinned`},
	{`gpu1, gpu2 -$ g: characters * poses spread by characters`, `gpu1, gpu2 -$ g: characters * poses spread by characters`},
	{`gpu1 -$ g: characters > upscale on gpu2 > face`, `gpu1 -$ g: characters > upscale on gpu2 > face`},

	// The long example of SPEC §6, wrapped as in the text.
	{"gpu1, gpu2 -$ anime: characters * poses(~jumping) * emotions(cry) * \"masterpiece\"\n    where not #acrobatic\n    sample 20 seed 7  repeat 3\n    with steps=30, cfg=6.5\n    spread blocks\n    > upscale on gpu2",
		`gpu1, gpu2 -$ anime: characters * poses(~jumping) * emotions(cry) * "masterpiece" where not #acrobatic sample 20 seed 7 repeat 3 with steps=30, cfg=6.5 spread blocks > upscale on gpu2`},

	// Further grammar coverage.
	{`g1 -$ g: a(x,y%,#t) * b(~x, ~#t, ~p?)`, `g1 -$ g: a(x, y%, #t) * b(~x, ~#t, ~p?)`},
	{`g1 -$ g: a && b && c`, `g1 -$ g: a && b && c`},
	{`g1 -$ g: a || b || c`, `g1 -$ g: a || b || c`},
	{`g1 -$ g: a(x) && "t" && !"n" && s[tag: b]`, `g1 -$ g: a(x) && "t" && !"n" && s[tag: b]`},
	{`g1 -$ g: s(a)[t1: x * y, t2: z || w]`, `g1 -$ g: s(a)[t1: x * y, t2: z || w]`},
	{`g1 -$ g: s[t: u[v: "x" && y]]`, `g1 -$ g: s[t: u[v: "x" && y]]`},
	{`g1 -$ g: a where x = y or x in (p, q%, #r) and not #s`, `g1 -$ g: a where x = y or x in (p, q%, #r) and not #s`},
	{`g1 -$ g: a where (x = y or #z) and w = #t`, `g1 -$ g: a where (x = y or #z) and w = #t`},
	{`g1 -$ g: a where a = 1 or b = 2 or c = 3`, `g1 -$ g: a where a = 1 or b = 2 or c = 3`},
	{`g1 -$ g: a where (a = 1 or b = 2) or c = 3`, `g1 -$ g: a where a = 1 or b = 2 or c = 3`},
	{`g1 -$ g: a where not not #t`, `g1 -$ g: a where not not #t`},
	{`g1 -$ g: a where not (#t or #u)`, `g1 -$ g: a where not (#t or #u)`},
	{`g1 -$ g: a with k="two words", n=7, d=0.50, w=word, q="42", s=in`, `g1 -$ g: a with k="two words", n=7, d=0.5, w=word, q="42", s=in`},
	{`g1 -$ g: a with seed=18446744073709551615`, `g1 -$ g: a with seed=18446744073709551615`},
	{`g1 -$ g: a sample 3`, `g1 -$ g: a sample 3`},
	{`g1, g2 -$ g: a spread dynamic`, `g1, g2 -$ g: a spread dynamic`},
	{`g1, g2=2 -$ g: a spread by a pinned`, `g1, g2=2 -$ g: a spread by a pinned`},
	{`g1, g2 -$ g: pinned spread by pinned`, `g1, g2 -$ g: pinned spread by pinned`},
	{`g1 -$ g: seed * dynamic * even * blocks * by * pinned`, `g1 -$ g: seed * dynamic * even * blocks * by * pinned`},
	{`g1 -$ g: a > f1 > f2 on s`, `g1 -$ g: a > f1 > f2 on s`},
	{`g1 -$ g: "say \"hi\" \\ done"`, `g1 -$ g: "say \"hi\" \\ done"`},
	{`g1 -$ g: " padded "`, `g1 -$ g: " padded "`},
	{`g1 -$ g: a(12345, 678)`, `g1 -$ g: a(12345, 678)`},
	{`g1 -$ g: a   *   b`, `g1 -$ g: a * b`},
	{`  g1 -$ g:a*b  `, `g1 -$ g: a * b`},
}

func TestParseValid(t *testing.T) {
	for _, tt := range validCommands {
		t.Run(tt.in, func(t *testing.T) {
			c := mustParse(t, tt.in)
			if got := Format(c); got != tt.canonical {
				t.Errorf("Format = %s\nwant       %s", got, tt.canonical)
			}
			again := mustParse(t, tt.canonical)
			if Format(again) != tt.canonical {
				t.Errorf("canonical text is not stable: %s", Format(again))
			}
		})
	}
}

func TestParseStructure(t *testing.T) {
	// Acceptance test 1.
	c := mustParse(t, `s -$ g: a * b(~x) * c(y) > f1 > f2`)
	if len(c.Servers) != 1 || c.Servers[0].Server.Text != "s" || c.Servers[0].Weight != 1 {
		t.Errorf("servers = %+v", c.Servers)
	}
	if c.Generator.Text != "g" {
		t.Errorf("generator = %q", c.Generator.Text)
	}
	if len(c.Selections) != 3 {
		t.Fatalf("got %d selections, want 3", len(c.Selections))
	}
	a, b, cc := c.Selections[0], c.Selections[1], c.Selections[2]
	if a.Group.Text != "a" || a.Filter != nil {
		t.Errorf("a = %+v", a)
	}
	if b.Group.Text != "b" || b.Filter == nil || !b.Filter.Exclude || b.Filter.Selectors[0].Text != "x" {
		t.Errorf("b = %+v", b)
	}
	if cc.Group.Text != "c" || cc.Filter == nil || cc.Filter.Exclude || cc.Filter.Selectors[0].Text != "y" {
		t.Errorf("c = %+v", cc)
	}
	if len(c.Fixers) != 2 || c.Fixers[0].Fixer.Text != "f1" || c.Fixers[1].Fixer.Text != "f2" {
		t.Errorf("fixers = %+v", c.Fixers)
	}

	// Selector kinds, weights, spread and overrides.
	c = mustParse(t, `a=3, b -$ g: x(p, q%, #t) with n=7, d=6.5, w=word, s="a b" spread blocks`)
	kinds := []SelectorKind{SelItem, SelPattern, SelTag}
	for i, s := range c.Selections[0].Filter.Selectors {
		if s.Kind != kinds[i] {
			t.Errorf("selector %d kind = %v, want %v", i, s.Kind, kinds[i])
		}
	}
	if c.Servers[0].Weight != 3 || c.Servers[1].Weight != 1 {
		t.Errorf("weights = %d, %d", c.Servers[0].Weight, c.Servers[1].Weight)
	}
	if c.Spread == nil || c.Spread.Mode != SpreadBlocks || c.Spread.Pinned {
		t.Errorf("spread = %+v", c.Spread)
	}
	wantKinds := []ValueKind{ValueInt, ValueDecimal, ValueText, ValueText}
	for i, o := range c.Overrides {
		if o.Value.Kind != wantKinds[i] {
			t.Errorf("override %s kind = %v, want %v", o.Key.Text, o.Value.Kind, wantKinds[i])
		}
	}
	if o := c.Overrides[1].Value; o.Float != 6.5 {
		t.Errorf("cfg = %v", o.Float)
	}

	// Literals.
	c = mustParse(t, `s -$ g: "pos" * !"neg"`)
	if l := c.Selections[0]; l.Kind != SelLiteral || l.Negative || l.Text != "pos" {
		t.Errorf("positive literal = %+v", l)
	}
	if l := c.Selections[1]; l.Kind != SelLiteral || !l.Negative || l.Text != "neg" {
		t.Errorf("negative literal = %+v", l)
	}

	// Merge and zip keep the operands in order, duplicates included.
	c = mustParse(t, `s -$ g: a && b && a`)
	if m := c.Selections[0]; m.Kind != SelMerge || len(m.Operands) != 3 || m.Operands[2].Group.Text != "a" {
		t.Errorf("merge = %+v", m)
	}

	// Where: associative operators are flattened, not binds tighter than and, and tighter than or.
	c = mustParse(t, `s -$ g: a where #p or #q and not #r`)
	w := c.Where
	if w.Op != CondOr || len(w.Args) != 2 || w.Args[1].Op != CondAnd || w.Args[1].Args[1].Op != CondNot {
		t.Errorf("where tree = %+v", w)
	}
	c = mustParse(t, `s -$ g: a where #p or (#q or #r)`)
	if c.Where.Op != CondOr || len(c.Where.Args) != 3 {
		t.Errorf("or was not flattened: %+v", c.Where)
	}

	// Sample without a seed leaves it nil: the service stores the generated one.
	c = mustParse(t, `s -$ g: a sample 4`)
	if c.Sample == nil || c.Sample.Count.N != 4 || c.Sample.Seed != nil {
		t.Errorf("sample = %+v", c.Sample)
	}
}

// Acceptance test 1b: spacing does not change the structure.
func TestParseSpacingEquivalence(t *testing.T) {
	variants := []string{
		`s -$ g: a*b`, `s -$ g: a * b`, `s -$ g: a   *   b`, "s -$ g:\n  a\n  *\n  b\n", "  s-$g:a*b  ",
	}
	var want string
	for i, v := range variants {
		j, err := ToJSON(mustParse(t, v))
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			want = string(j)
		} else if string(j) != want {
			t.Errorf("%q parses differently:\n%s\n%s", v, j, want)
		}
	}
}

// Syntactically valid commands that only the validator (step 3) rejects.
func TestParseAcceptsSemanticCases(t *testing.T) {
	for _, in := range []string{
		`gpu1 -$ g: characters repeat 3`,
		`gpu1 -$ g: characters(nobody)`,
		`gpu1 -$ g: poses(#nosuchtag)`,
		`gpu1 -$ g: characters where emotions = sad`,
		`gpu1 -$ g: characters with colour=3`,
		`gpu1 -$ g: characters with steps=1, steps=2`,
	} {
		if _, err := Parse(in); err != nil {
			t.Errorf("Parse(%q): %v", in, err)
		}
	}
}

type invalidCase struct {
	in    string
	tag   string
	rule  string
	at    string // the offending text
	nth   int    // which occurrence of at (default 1)
	atEOF bool   // the error is at the end of the input
}

// invalidCommands are the invalid rows of SPEC §6.12 that the parser decides
// (R1 and the semantic rows need the catalogue), then more cases per rule.
var invalidCommands = []invalidCase{
	{in: `gpu1 -$ g:`, tag: "CommandRestrictionViolated", rule: RuleS6, atEOF: true},
	{in: `gpu1 - g: characters`, tag: "CommandLexError", rule: RuleLex, at: "-"},
	{in: `gpu1 -$ g: characters && poses || emotions`, tag: "CommandRestrictionViolated", rule: RuleS1, at: "||"},
	{in: `gpu1 -$ g: poses(standing, ~jumping)`, tag: "CommandRestrictionViolated", rule: RuleS2, at: "~"},
	{in: `gpu1 -$ g: poses(a)(~b)`, tag: "CommandRestrictionViolated", rule: RuleS2, at: "(~b)"},
	{in: `gpu1 -$ g: scene[subject: duo{red: x}]`, tag: "CommandRestrictionViolated", rule: RuleS3, at: "duo"},
	{in: `gpu1 -$ g: duo{red: x} && characters`, tag: "CommandRestrictionViolated", rule: RuleS3, at: "duo"},
	{in: `gpu1 -$ g: duo(a){red: x}`, tag: "CommandRestrictionViolated", rule: RuleS4, at: "(a)"},
	{in: `gpu1 -$ g: duo{red: x} * duo{blue: y}`, tag: "CommandRestrictionViolated", rule: RuleS5, at: "duo", nth: 2},
	{in: `gpu1 -$ g: characters * ""`, tag: "CommandRestrictionViolated", rule: RuleS7, at: `""`},
	{in: `gpu1 -$ g: characters limit 5 sample 3`, tag: "CommandSyntaxError", rule: RuleSyntax, at: "sample"},
	{in: `gpu1 -$ g: characters repeat 3 where #acrobatic`, tag: "CommandSyntaxError", rule: RuleSyntax, at: "where"},
	{in: `gpu1 -$ g: characters with steps=`, tag: "CommandSyntaxError", rule: RuleSyntax, atEOF: true},
	{in: `gpu1 -$ g: characters limit 0`, tag: "CommandRestrictionViolated", rule: RuleS8, at: "0"},
	{in: `gpu1, gpu1 -$ g: characters`, tag: "CommandRestrictionViolated", rule: RuleT1, at: "gpu1", nth: 2},
	{in: `gpu1=2, gpu2 -$ g: characters`, tag: "CommandRestrictionViolated", rule: RuleT2, at: "2"},
	{in: `gpu1 -$ g: characters spread even`, tag: "CommandRestrictionViolated", rule: RuleT3, at: "spread even"},
	{in: `gpu1, gpu2 -$ g: characters spread dynamic pinned`, tag: "CommandRestrictionViolated", rule: RuleT4, at: "pinned"},
	{in: `gpu1, gpu2 -$ g: characters && anime spread by characters`, tag: "CommandRestrictionViolated", rule: RuleT5, at: "characters", nth: 2},
	{in: `gpu1 -$ with: characters`, tag: "CommandSyntaxError", rule: RuleSyntax, at: "with"},
	{in: `gpu1 -$ g: chars$`, tag: "CommandLexError", rule: RuleLex, at: "$", nth: 2},

	// More S1–S8.
	{in: `s -$ g: a || b && c`, tag: "CommandRestrictionViolated", rule: RuleS1, at: "&&"},
	{in: `s -$ g: a(x, ~y, z)`, tag: "CommandRestrictionViolated", rule: RuleS2, at: "~"},
	{in: `s -$ g: a(~x, y)`, tag: "CommandRestrictionViolated", rule: RuleS2, at: "y"},
	{in: `s -$ g: a(x)(y)`, tag: "CommandRestrictionViolated", rule: RuleS2, at: "(y)"},
	{in: `s -$ g: x[t: a || r{k: b}]`, tag: "CommandRestrictionViolated", rule: RuleS3, at: "r"},
	{in: `s -$ g: r{k: q{j: b}}`, tag: "CommandRestrictionViolated", rule: RuleS3, at: "q"},
	{in: `s -$ g: a * r{k: b} || c`, tag: "CommandRestrictionViolated", rule: RuleS3, at: "r"},
	{in: `s -$ g: where #t`, tag: "CommandRestrictionViolated", rule: RuleS6, at: "where"},
	{in: `s -$ g: with a=1`, tag: "CommandRestrictionViolated", rule: RuleS6, at: "with"},
	{in: `s -$ g: > f`, tag: "CommandRestrictionViolated", rule: RuleS6, at: ">"},
	{in: `s -$ g: *`, tag: "CommandSyntaxError", rule: RuleSyntax, at: "*"},
	{in: `s -$ g: a * "  "`, tag: "CommandRestrictionViolated", rule: RuleS7, at: `"  "`},
	{in: `s -$ g: a && !""`, tag: "CommandRestrictionViolated", rule: RuleS7, at: `!""`},
	{in: `s -$ g: x[t: ""]`, tag: "CommandRestrictionViolated", rule: RuleS7, at: `""`},
	{in: `s -$ g: a limit 2.5`, tag: "CommandRestrictionViolated", rule: RuleS8, at: "2.5"},
	{in: `s -$ g: a limit abc`, tag: "CommandRestrictionViolated", rule: RuleS8, at: "abc"},
	{in: `s -$ g: a repeat 0`, tag: "CommandRestrictionViolated", rule: RuleS8, at: "0"},
	{in: `s -$ g: a sample 0`, tag: "CommandRestrictionViolated", rule: RuleS8, at: "0"},
	{in: `s -$ g: a sample 3 seed 1.5`, tag: "CommandRestrictionViolated", rule: RuleS8, at: "1.5"},
	{in: `s -$ g: a sample 3 seed 99999999999999999999`, tag: "CommandRestrictionViolated", rule: RuleS8, at: "99999999999999999999"},
	{in: `s -$ g: a limit 99999999999`, tag: "CommandRestrictionViolated", rule: RuleS8, at: "99999999999"},
	{in: `a=0, b -$ g: x spread even`, tag: "CommandRestrictionViolated", rule: RuleS8, at: "0"},
	{in: `a=1.5, b -$ g: x spread even`, tag: "CommandRestrictionViolated", rule: RuleS8, at: "1.5"},
	{in: `s -$ g: a with n=99999999999999999999`, tag: "CommandRestrictionViolated", rule: RuleS8, at: "99999999999999999999"},

	// More T1–T5.
	{in: `aa, bb, aa -$ g: x spread even`, tag: "CommandRestrictionViolated", rule: RuleT1, at: "aa", nth: 2},
	{in: `a=1, b -$ g: x`, tag: "CommandRestrictionViolated", rule: RuleT2, at: "1"},
	{in: `a=2, b -$ g: x spread dynamic`, tag: "CommandRestrictionViolated", rule: RuleT2, at: "2"},
	{in: `a=2 -$ g: x`, tag: "CommandRestrictionViolated", rule: RuleT2, at: "2"},
	{in: `a -$ g: x spread blocks`, tag: "CommandRestrictionViolated", rule: RuleT3, at: "spread blocks"},
	{in: `a -$ g: x spread by x pinned`, tag: "CommandRestrictionViolated", rule: RuleT3, at: "spread by x pinned"},
	{in: `a, b -$ g: x spread by yy`, tag: "CommandRestrictionViolated", rule: RuleT5, at: "yy"},
	{in: `a, b -$ g: xx * xx spread by xx`, tag: "CommandRestrictionViolated", rule: RuleT5, at: "xx", nth: 3},
	{in: `a, b -$ g: x(p) * grp[t: y] spread by grp`, tag: "CommandRestrictionViolated", rule: RuleT5, at: "grp", nth: 2},

	// Syntax errors.
	{in: ``, tag: "CommandSyntaxError", rule: RuleSyntax, atEOF: true},
	{in: `   `, tag: "CommandSyntaxError", rule: RuleSyntax, atEOF: true},
	{in: `-$ g: a`, tag: "CommandSyntaxError", rule: RuleSyntax, at: "-$"},
	{in: `s g: a`, tag: "CommandSyntaxError", rule: RuleSyntax, at: "g"},
	{in: `s -$ : a`, tag: "CommandSyntaxError", rule: RuleSyntax, at: ":"},
	{in: `s -$ g a`, tag: "CommandSyntaxError", rule: RuleSyntax, at: "a"},
	{in: `s, -$ g: a`, tag: "CommandSyntaxError", rule: RuleSyntax, at: "-$"},
	{in: `s=2 -$ g: a`, tag: "CommandRestrictionViolated", rule: RuleT2, at: "2"},
	{in: `s -$ g: a *`, tag: "CommandSyntaxError", rule: RuleSyntax, atEOF: true},
	{in: `s -$ g: a * * b`, tag: "CommandSyntaxError", rule: RuleSyntax, at: "*", nth: 2},
	{in: `s -$ g: a &&`, tag: "CommandSyntaxError", rule: RuleSyntax, atEOF: true},
	{in: `s -$ g: !a`, tag: "CommandSyntaxError", rule: RuleSyntax, at: "a"},
	{in: `s -$ g: a(`, tag: "CommandSyntaxError", rule: RuleSyntax, atEOF: true},
	{in: `s -$ g: a()`, tag: "CommandSyntaxError", rule: RuleSyntax, at: ")"},
	{in: `s -$ g: a(x`, tag: "CommandSyntaxError", rule: RuleSyntax, atEOF: true},
	{in: `s -$ g: a(x y)`, tag: "CommandSyntaxError", rule: RuleSyntax, at: "y"},
	{in: `s -$ g: a(#)`, tag: "CommandSyntaxError", rule: RuleSyntax, at: ")"},
	{in: `s -$ g: a(where)`, tag: "CommandSyntaxError", rule: RuleSyntax, at: "where"},
	{in: `s -$ g: a(#in)`, tag: "CommandSyntaxError", rule: RuleSyntax, at: "in"},
	{in: `s -$ g: a(x, )`, tag: "CommandSyntaxError", rule: RuleSyntax, at: ")"},
	{in: `s -$ g: x[`, tag: "CommandSyntaxError", rule: RuleSyntax, atEOF: true},
	{in: `s -$ g: x[t]`, tag: "CommandSyntaxError", rule: RuleSyntax, at: "]"},
	{in: `s -$ g: x[t: ]`, tag: "CommandSyntaxError", rule: RuleSyntax, at: "]"},
	{in: `s -$ g: x[t: a`, tag: "CommandSyntaxError", rule: RuleSyntax, atEOF: true},
	{in: `s -$ g: x[t: a, ]`, tag: "CommandSyntaxError", rule: RuleSyntax, at: "]"},
	{in: `s -$ g: x{k: a]`, tag: "CommandSyntaxError", rule: RuleSyntax, at: "]"},
	{in: `s -$ g: x{}`, tag: "CommandSyntaxError", rule: RuleSyntax, at: "}"},
	{in: `s -$ g: a%`, tag: "CommandSyntaxError", rule: RuleSyntax, at: "a%"},
	{in: `s%x -$ g: a`, tag: "CommandSyntaxError", rule: RuleSyntax, at: "s%x"},
	{in: `s -$ g: a(x%)[t%: b]`, tag: "CommandSyntaxError", rule: RuleSyntax, at: "t%"},
	{in: `s -$ g: a where`, tag: "CommandSyntaxError", rule: RuleSyntax, atEOF: true},
	{in: `s -$ g: a where x`, tag: "CommandSyntaxError", rule: RuleSyntax, atEOF: true},
	{in: `s -$ g: a where x =`, tag: "CommandSyntaxError", rule: RuleSyntax, atEOF: true},
	{in: `s -$ g: a where x in`, tag: "CommandSyntaxError", rule: RuleSyntax, atEOF: true},
	{in: `s -$ g: a where x in ()`, tag: "CommandSyntaxError", rule: RuleSyntax, at: ")"},
	{in: `s -$ g: a where x in (~y)`, tag: "CommandSyntaxError", rule: RuleSyntax, at: "~"},
	{in: `s -$ g: a where (x = y`, tag: "CommandSyntaxError", rule: RuleSyntax, atEOF: true},
	{in: `s -$ g: a where #t and`, tag: "CommandSyntaxError", rule: RuleSyntax, atEOF: true},
	{in: `s -$ g: a where not`, tag: "CommandSyntaxError", rule: RuleSyntax, atEOF: true},
	{in: `s -$ g: a where #t #u`, tag: "CommandSyntaxError", rule: RuleSyntax, at: "#", nth: 2},
	{in: `s -$ g: a limit`, tag: "CommandSyntaxError", rule: RuleSyntax, atEOF: true},
	{in: `s -$ g: a limit where`, tag: "CommandSyntaxError", rule: RuleSyntax, at: "where"},
	{in: `s -$ g: a limit "3"`, tag: "CommandSyntaxError", rule: RuleSyntax, at: `"3"`},
	{in: `s -$ g: a sample 3 seed`, tag: "CommandSyntaxError", rule: RuleSyntax, atEOF: true},
	{in: `s -$ g: a limit 3 limit 4`, tag: "CommandSyntaxError", rule: RuleSyntax, at: "limit", nth: 2},
	{in: `s -$ g: a repeat 2 repeat 3`, tag: "CommandSyntaxError", rule: RuleSyntax, at: "repeat", nth: 2},
	{in: `s -$ g: a with k=1 where #t`, tag: "CommandSyntaxError", rule: RuleSyntax, at: "where"},
	{in: `s, t -$ g: a spread even with k=1`, tag: "CommandSyntaxError", rule: RuleSyntax, at: "with"},
	{in: `s -$ g: a > f where #t`, tag: "CommandSyntaxError", rule: RuleSyntax, at: "where"},
	{in: `s -$ g: a with`, tag: "CommandSyntaxError", rule: RuleSyntax, atEOF: true},
	{in: `s -$ g: a with k`, tag: "CommandSyntaxError", rule: RuleSyntax, atEOF: true},
	{in: `s -$ g: a with k=1,`, tag: "CommandSyntaxError", rule: RuleSyntax, atEOF: true},
	{in: `s -$ g: a with k=a%`, tag: "CommandSyntaxError", rule: RuleSyntax, at: "a%"},
	{in: `s -$ g: a with k=1000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000.5`, tag: "CommandSyntaxError", rule: RuleSyntax, at: "1000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000.5"},
	{in: `s, t -$ g: a spread`, tag: "CommandSyntaxError", rule: RuleSyntax, atEOF: true},
	{in: `s, t -$ g: a spread fast`, tag: "CommandSyntaxError", rule: RuleSyntax, at: "fast"},
	{in: `s, t -$ g: a spread by`, tag: "CommandSyntaxError", rule: RuleSyntax, atEOF: true},
	{in: `s, t -$ g: a spread "even"`, tag: "CommandSyntaxError", rule: RuleSyntax, at: `"even"`},
	{in: `s -$ g: a >`, tag: "CommandSyntaxError", rule: RuleSyntax, atEOF: true},
	{in: `s -$ g: a > f on`, tag: "CommandSyntaxError", rule: RuleSyntax, atEOF: true},
	{in: `s -$ g: a > on`, tag: "CommandSyntaxError", rule: RuleSyntax, at: "on"},
	{in: `s -$ g: a > f on where`, tag: "CommandSyntaxError", rule: RuleSyntax, at: "where"},
	{in: `s -$ g: a b`, tag: "CommandSyntaxError", rule: RuleSyntax, at: "b"},
	{in: `s -$ g: a )`, tag: "CommandSyntaxError", rule: RuleSyntax, at: ")"},
	{in: `s -$ and: a`, tag: "CommandSyntaxError", rule: RuleSyntax, at: "and"},
	{in: `in -$ g: a`, tag: "CommandSyntaxError", rule: RuleSyntax, at: "in"},
	{in: `s -$ g: x[where: a]`, tag: "CommandSyntaxError", rule: RuleSyntax, at: "where"},
	{in: `s -$ g: a > limit`, tag: "CommandSyntaxError", rule: RuleSyntax, at: "limit"},
	{in: `s -$ g: a with sample=1`, tag: "CommandSyntaxError", rule: RuleSyntax, at: "sample"},
}

func TestParseInvalid(t *testing.T) {
	for _, tt := range invalidCommands {
		t.Run(tt.in, func(t *testing.T) {
			_, err := Parse(tt.in)
			appErr, ok := errors.FromError(err)
			if !ok {
				t.Fatalf("expected *errors.Error, got %T (%v)", err, err)
			}
			if appErr.Tag != tt.tag {
				t.Errorf("tag = %s (%s), want %s", appErr.Tag, appErr.Message, tt.tag)
			}
			// The route runs from Parse to the function that found the problem.
			if !appErr.HasRoute("Parse") || !strings.HasSuffix(appErr.Route(), "."+tt.tag) {
				t.Errorf("unexpected failure path: %s", appErr.Route())
			}
			p, ok := ProblemOf(err)
			if !ok || p.Rule != tt.rule {
				t.Errorf("rule = %q, want %q (%s)", p.Rule, tt.rule, p.Message)
			}
			runes := []rune(tt.in)
			wantOffset, wantLength := len(runes), 0
			if !tt.atEOF {
				nth := max(tt.nth, 1)
				idx, from := -1, 0
				for range nth {
					i := strings.Index(string(runes[from:]), tt.at)
					if i < 0 {
						t.Fatalf("test case: %q does not occur %d times", tt.at, nth)
					}
					idx = from + len([]rune(string(runes[from:])[:i]))
					from = idx + 1
				}
				wantOffset, wantLength = idx, len([]rune(tt.at))
			}
			if p.Position.Offset != wantOffset || p.Position.Length != wantLength {
				t.Errorf("position = %+v, want {%d %d} (%s)", p.Position, wantOffset, wantLength, p.Message)
			}
			if p.Message == "" {
				t.Error("empty message")
			}
		})
	}
}

// The first problem in the text is the one reported, whatever its kind of
// restriction; lexical errors come before everything.
func TestParseReportsFirstByPosition(t *testing.T) {
	tests := []struct {
		in   string
		rule string
	}{
		{`a, a -$ g: x spread even pinned spread`, RuleSyntax}, // syntax error outranks restrictions
		{`a=2, a -$ g: x $`, RuleLex},                          // lexical error outranks everything
		{`a=2, a -$ g: x`, RuleT2},                             // earlier in the text than the duplicate
		{`a, a=2 -$ g: x`, RuleT1},
		{`a, b -$ g: x && y || z spread by x`, RuleS1},
		{`a, b -$ g: x * "" spread dynamic pinned`, RuleS7},
	}
	for _, tt := range tests {
		_, err := Parse(tt.in)
		p, ok := ProblemOf(err)
		if !ok || p.Rule != tt.rule {
			t.Errorf("Parse(%q) rule = %q, want %q", tt.in, p.Rule, tt.rule)
		}
	}
}

func TestParsePositionsAreRunes(t *testing.T) {
	_, err := Parse(`s -$ g: "ééé" && a || b`)
	p, _ := ProblemOf(err)
	if p.Rule != RuleS1 || p.Position.Offset != 19 {
		t.Errorf("problem = %+v, want S1 at rune offset 19", p)
	}
}

func TestProblemOfForeignError(t *testing.T) {
	if _, ok := ProblemOf(errors.New("Other", "x")); ok {
		t.Error("ProblemOf accepted an error without a rule")
	}
	if _, ok := ProblemOf(nil); ok {
		t.Error("ProblemOf accepted nil")
	}
}

func TestCheckName(t *testing.T) {
	tests := []struct {
		name string
		ok   bool
	}{
		{"alice", true}, {"Alice_2", true}, {"12345", true}, {"seed", true}, {"pinned", true},
		{"", false}, {"with", false}, {"in", false}, {"on", false}, {"not", false},
		{"a-b", false}, {"a b", false}, {"é", false}, {"a%", false}, {"a.b", false},
	}
	for _, tt := range tests {
		if got := CheckName(tt.name) == ""; got != tt.ok {
			t.Errorf("CheckName(%q) ok = %v, want %v", tt.name, got, tt.ok)
		}
	}
}
