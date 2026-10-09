package command

import "strings"

// restrictions applies the rules of SPEC §6.3 that can be decided on the AST:
// S3–S7, the lower bounds of S8 and T1–T5. S1, S2 and the digit rules of S8
// are decided while parsing because the AST cannot express them. emptyAt is
// where the selections should have started; the parser has recorded rule S6
// itself, so it is only reported here when the span is unknown.
//
// Rule R1 needs the generator and is checked by the validator.
func restrictions(c *Command, emptyAt Pos) []violation {
	var vs []violation
	add := func(rule, msg string, at Pos, token string) {
		vs = append(vs, violation{rule: rule, msg: msg, at: at, token: token})
	}

	if len(c.Selections) == 0 && emptyAt == (Pos{}) {
		add(RuleS6, "a command needs at least one selection after ':'", Pos{}, "")
	}

	regions := 0
	for _, s := range c.Selections {
		if s.Kind == SelRegion {
			regions++
			if regions > 1 {
				add(RuleS5, "a command can have only one region", s.Group.Pos, s.Group.Text)
			}
		}
	}
	var walk func(sels []Selection, topLevel bool)
	walk = func(sels []Selection, topLevel bool) {
		for _, s := range sels {
			switch s.Kind {
			case SelRegion:
				if !topLevel {
					add(RuleS3, "a region is only allowed as a top-level term, not inside a merge, a zip or a body", s.Group.Pos, s.Group.Text)
				}
				if s.Filter != nil {
					add(RuleS4, "a region group cannot have an item filter; its items define the region keywords", s.Filter.Pos, "(")
				}
				walk(bodies(s.Binds), false)
			case SelTemplate:
				walk(bodies(s.Binds), false)
			case SelMerge, SelZip:
				walk(s.Operands, false)
			case SelLiteral:
				if strings.TrimSpace(s.Text) == "" {
					add(RuleS7, "a string literal cannot be empty", s.Pos, `""`)
				}
			}
		}
	}
	walk(c.Selections, true)

	countRules(c, add)
	placementRules(c, add)
	return vs
}

func bodies(binds []Bind) []Selection {
	var out []Selection
	for _, b := range binds {
		out = append(out, b.Body...)
	}
	return out
}

// countRules covers the lower bounds of S8 for commands that did not come from
// text (FromJSON), where the parser never saw the numbers.
func countRules(c *Command, add func(rule, msg string, at Pos, token string)) {
	if c.Limit != nil && c.Limit.N < 1 {
		add(RuleS8, "a count must be at least 1", c.Limit.Pos, "limit")
	}
	if c.Repeat != nil && c.Repeat.N < 1 {
		add(RuleS8, "a count must be at least 1", c.Repeat.Pos, "repeat")
	}
	if c.Sample != nil && c.Sample.Count.N < 1 {
		add(RuleS8, "a count must be at least 1", c.Sample.Pos, "sample")
	}
}

func placementRules(c *Command, add func(rule, msg string, at Pos, token string)) {
	seen := map[string]bool{}
	for _, t := range c.Servers {
		if seen[t.Server.Text] {
			add(RuleT1, "each server can appear only once in the placement", t.Server.Pos, t.Server.Text)
		}
		seen[t.Server.Text] = true
		if t.Weight < 1 {
			add(RuleS8, "a weight must be at least 1", t.WeightPos, t.Server.Text)
		}
	}

	dynamic := c.Spread == nil || c.Spread.Mode == SpreadDynamic
	if dynamic {
		for _, t := range c.Servers {
			if t.weightSet || t.Weight != 1 {
				add(RuleT2, "a weight is only allowed with spread even, blocks or by; in dynamic mode capacity is the server's max_concurrent_jobs", t.WeightPos, t.Server.Text)
			}
		}
	}
	if c.Spread == nil {
		return
	}
	if len(c.Servers) < 2 {
		add(RuleT3, "a spread clause needs at least two servers", c.Spread.Pos, "spread")
	}
	if dynamic && c.Spread.Pinned {
		add(RuleT4, "'pinned' is not allowed with spread dynamic: a dynamic job has no preferred server", c.Spread.PinnedPos, "pinned")
	}
	if c.Spread.Mode == SpreadBy {
		plain := 0
		for _, s := range c.Selections {
			if s.Kind == SelGroup && s.Group.Text == c.Spread.ByGroup.Text {
				plain++
			}
		}
		if plain != 1 {
			add(RuleT5, "'spread by "+c.Spread.ByGroup.Text+"' needs exactly one plain top-level term of that group", c.Spread.ByGroup.Pos, c.Spread.ByGroup.Text)
		}
	}
}
