package command

import (
	"strconv"
	"strings"
)

// Format renders c as canonical one-line command text: single spaces, one
// spelling per construct. Parsing the result gives back an equal command
// (spans aside), which is what the fuzz test checks.
func Format(c *Command) string {
	var sb strings.Builder
	for i, t := range c.Servers {
		if i > 0 {
			sb.WriteString(", ")
		}
		sb.WriteString(t.Server.Text)
		if t.Weight != 1 {
			sb.WriteString("=" + strconv.Itoa(t.Weight))
		}
	}
	sb.WriteString(" -$ " + c.Generator.Text + ": ")
	sb.WriteString(formatSelections(c.Selections))
	if c.Where != nil {
		sb.WriteString(" where " + formatCond(c.Where, 0))
	}
	if c.Limit != nil {
		sb.WriteString(" limit " + strconv.Itoa(c.Limit.N))
	}
	if c.Sample != nil {
		sb.WriteString(" sample " + strconv.Itoa(c.Sample.Count.N))
		if c.Sample.Seed != nil {
			sb.WriteString(" seed " + strconv.FormatUint(c.Sample.Seed.N, 10))
		}
	}
	if c.Repeat != nil {
		sb.WriteString(" repeat " + strconv.Itoa(c.Repeat.N))
	}
	if len(c.Overrides) > 0 {
		sb.WriteString(" with ")
		for i, o := range c.Overrides {
			if i > 0 {
				sb.WriteString(", ")
			}
			sb.WriteString(o.Key.Text + "=" + formatValue(o.Value))
		}
	}
	if c.Spread != nil {
		sb.WriteString(" spread " + string(c.Spread.Mode))
		if c.Spread.Mode == SpreadBy {
			sb.WriteString(" " + c.Spread.ByGroup.Text)
		}
		if c.Spread.Pinned {
			sb.WriteString(" pinned")
		}
	}
	for _, f := range c.Fixers {
		sb.WriteString(" > " + f.Fixer.Text)
		if f.On != nil {
			sb.WriteString(" on " + f.On.Text)
		}
	}
	return sb.String()
}

func formatSelections(sels []Selection) string {
	parts := make([]string, len(sels))
	for i, s := range sels {
		parts[i] = formatSelection(s)
	}
	return strings.Join(parts, " * ")
}

func formatSelection(s Selection) string {
	switch s.Kind {
	case SelLiteral:
		if s.Negative {
			return "!" + quote(s.Text)
		}
		return quote(s.Text)
	case SelMerge:
		return joinSelections(s.Operands, " && ")
	case SelZip:
		return joinSelections(s.Operands, " || ")
	case SelTemplate:
		return s.Group.Text + formatFilter(s.Filter) + "[" + formatBinds(s.Binds) + "]"
	case SelRegion:
		return s.Group.Text + formatFilter(s.Filter) + "{" + formatBinds(s.Binds) + "}"
	default:
		return s.Group.Text + formatFilter(s.Filter)
	}
}

func joinSelections(sels []Selection, sep string) string {
	parts := make([]string, len(sels))
	for i, s := range sels {
		parts[i] = formatSelection(s)
	}
	return strings.Join(parts, sep)
}

func formatBinds(binds []Bind) string {
	parts := make([]string, len(binds))
	for i, b := range binds {
		parts[i] = b.Key.Text + ": " + formatSelections(b.Body)
	}
	return strings.Join(parts, ", ")
}

func formatFilter(f *Filter) string {
	if f == nil {
		return ""
	}
	parts := make([]string, len(f.Selectors))
	for i, s := range f.Selectors {
		parts[i] = s.String()
		if f.Exclude {
			parts[i] = "~" + parts[i]
		}
	}
	return "(" + strings.Join(parts, ", ") + ")"
}

// formatCond renders a condition. parent is the binding strength of the
// enclosing operator (0 none, 1 or, 2 and, 3 not) and decides parentheses.
func formatCond(c *Cond, parent int) string {
	switch c.Op {
	case CondOr, CondAnd:
		level, word := 1, " or "
		if c.Op == CondAnd {
			level, word = 2, " and "
		}
		parts := make([]string, len(c.Args))
		for i, a := range c.Args {
			// An operand of the same operator needs parentheses too: the parser
			// would otherwise splice it, which is the same meaning but a
			// different tree.
			parts[i] = formatCond(a, level+boolInt(a.Op == c.Op))
		}
		s := strings.Join(parts, word)
		if parent > level {
			return "(" + s + ")"
		}
		return s
	case CondNot:
		return "not " + formatCond(c.Args[0], 3)
	case CondEq:
		return c.Group.Text + " = " + c.Selectors[0].String()
	case CondIn:
		parts := make([]string, len(c.Selectors))
		for i, s := range c.Selectors {
			parts[i] = s.String()
		}
		return c.Group.Text + " in (" + strings.Join(parts, ", ") + ")"
	default:
		return "#" + c.Tag.Text
	}
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func formatValue(v Value) string {
	switch v.Kind {
	case ValueInt:
		return strconv.FormatUint(v.Int, 10)
	case ValueDecimal:
		return formatDecimal(v.Float)
	default:
		if IsWord(v.Text) && !isDigits(v.Text) {
			return v.Text
		}
		return quote(v.Text)
	}
}

// formatDecimal spells f so that it lexes as a DECIMAL again: shortest digits,
// no exponent, and always a fractional part.
func formatDecimal(f float64) string {
	s := strconv.FormatFloat(f, 'f', -1, 64)
	if !strings.Contains(s, ".") {
		s += ".0"
	}
	return s
}

func quote(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return `"` + s + `"`
}
