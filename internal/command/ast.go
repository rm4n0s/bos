// Package command is the command language of SPEC §6: lexer, parser,
// restriction checks, canonical formatter and the parsed JSON form. It is
// pure: no I/O and no project imports.
//
// Evaluation (the validator and the expander) lives in the same package and
// works on the AST defined here.
package command

import (
	"strconv"
	"strings"
)

// Pos is a span of the command text, in runes (not bytes), so a client can
// underline it. A zero-length span marks a position, such as the end of the
// input.
type Pos struct {
	Offset int `json:"offset"`
	Length int `json:"length"`
}

// End is the offset just after the span.
func (p Pos) End() int { return p.Offset + p.Length }

// Span returns the smallest span that covers a and b.
func Span(a, b Pos) Pos {
	start := min(a.Offset, b.Offset)
	end := max(a.End(), b.End())
	return Pos{Offset: start, Length: end - start}
}

// Name is a reference to an entity by code name: a server, generator, fixer,
// group, template tag or region keyword.
type Name struct {
	Text string
	Pos  Pos
}

// Command is a parsed command (SPEC §6.2). Spans are filled in by Parse and
// are empty after FromJSON; they are not part of the JSON form.
type Command struct {
	Servers    []Target
	Generator  Name
	Selections []Selection
	Where      *Cond
	Limit      *Count
	Sample     *Sample
	Repeat     *Count
	Overrides  []Override
	Spread     *Spread
	Fixers     []Step
}

// Target is one server of the placement, with its weight (default 1).
type Target struct {
	Server    Name
	Weight    int
	WeightPos Pos
	weightSet bool // the weight was written in the text (rule T2)
}

// Count is a whole number of at least 1 with the span it was written at.
type Count struct {
	N   int
	Pos Pos
}

// Seed is a non-negative whole number with the span it was written at.
type Seed struct {
	N   uint64
	Pos Pos
}

// Sample is the `sample n [seed s]` clause. Seed is nil when it was omitted;
// the service stores a generated seed in the parsed form (SPEC §6.7).
type Sample struct {
	Count Count
	Seed  *Seed
	Pos   Pos
}

// ValueKind says what a `with` value is.
type ValueKind int

// The kinds of `with` values (SPEC §6.2: WORD | DECIMAL | STRING). A digits-only
// WORD is an integer; a WORD that is not, and a STRING, are text.
const (
	ValueInt ValueKind = iota
	ValueDecimal
	ValueText
)

// Value is the right-hand side of a `with` assignment.
type Value struct {
	Kind  ValueKind
	Int   uint64
	Float float64
	Text  string
	Pos   Pos
}

// Override is one `key=value` assignment of the `with` clause. Duplicate keys
// are kept; they are a semantic error (SPEC §6.8).
type Override struct {
	Key   Name
	Value Value
}

// SpreadMode is how work is divided across servers (SPEC §6.9).
type SpreadMode string

// The spread modes.
const (
	SpreadDynamic SpreadMode = "dynamic"
	SpreadEven    SpreadMode = "even"
	SpreadBlocks  SpreadMode = "blocks"
	SpreadBy      SpreadMode = "by"
)

// Spread is the `spread` clause. When a command has several servers and no
// clause, Command.Spread is nil and the mode is dynamic.
type Spread struct {
	Mode      SpreadMode
	ByGroup   Name
	Pinned    bool
	PinnedPos Pos
	Pos       Pos
}

// Step is one fixer step, optionally on another server.
type Step struct {
	Fixer Name
	On    *Name
}

// SelKind says which construct of the grammar a Selection is.
type SelKind int

// The kinds of selection.
const (
	SelGroup    SelKind = iota // group [filter]
	SelLiteral                 // "text" or !"text"
	SelMerge                   // a && b && …
	SelZip                     // a || b || …
	SelTemplate                // group [filter] [tag: body, …]
	SelRegion                  // group {keyword: body, …}
)

// Selection is one entry of the parsed `group_selections` list (SPEC §6.10),
// at any nesting depth.
type Selection struct {
	Kind SelKind
	// Group is the base group of a group, template or region selection.
	Group Name
	// Filter is the optional item filter of a group or template selection.
	Filter *Filter
	// Text and Negative describe a literal.
	Text     string
	Negative bool
	// Operands are the members of a merge or a zip.
	Operands []Selection
	// Binds are the tag bodies of a template or the keyword bodies of a region.
	Binds []Bind
	Pos   Pos
}

// Bind is `tag: body` of a template or `keyword: body` of a region. The body is
// a product of selections.
type Bind struct {
	Key  Name
	Body []Selection
}

// Filter is the parenthesised item list after a group: either all-include or
// all-exclude (rule S2).
type Filter struct {
	Exclude   bool
	Selectors []Selector
	Pos       Pos
}

// SelectorKind says how a Selector matches items (SPEC §6.4).
type SelectorKind int

// The kinds of selector.
const (
	SelItem    SelectorKind = iota // alice
	SelPattern                     // run%, pose_?
	SelTag                         // #acrobatic
)

// Selector picks items of one group.
type Selector struct {
	Kind SelectorKind
	// Text is the item code name, the pattern, or the tag without its '#'.
	Text string
	Pos  Pos
}

// String returns the selector as written (SPEC §6.10).
func (s Selector) String() string {
	if s.Kind == SelTag {
		return "#" + s.Text
	}
	return s.Text
}

// CondOp is the operator of a node of a `where` condition.
type CondOp int

// The nodes of a condition.
const (
	CondAnd CondOp = iota // Args
	CondOr                // Args
	CondNot               // Args[0]
	CondEq                // Group = Selectors[0]
	CondIn                // Group in (Selectors…)
	CondTag               // #Tag
)

// Cond is a `where` condition tree (SPEC §6.7, §6.10).
type Cond struct {
	Op        CondOp
	Args      []*Cond
	Group     Name
	Selectors []Selector
	Tag       Name
	Pos       Pos
}

// Rule identifiers carried in the error metadata and in API error entries
// (SPEC §15.3).
const (
	RuleLex      = "LEX"
	RuleSyntax   = "SYN"
	RuleSemantic = "SEM"
)

// reserved lists the reserved words of SPEC §6.1. The contextual words (seed,
// dynamic, even, blocks, by, pinned) are ordinary words.
var reserved = map[string]bool{
	"where": true, "repeat": true, "limit": true, "sample": true, "with": true,
	"spread": true, "on": true, "and": true, "or": true, "not": true, "in": true,
}

// IsReserved reports whether word is a reserved word of the command language.
func IsReserved(word string) bool { return reserved[word] }

// IsWord reports whether s is a WORD (SPEC §6.1): one or more of A–Z a–z 0–9 _.
func IsWord(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if !isWordRune(r) {
			return false
		}
	}
	return true
}

func isWordRune(r rune) bool {
	return r == '_' || r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z'
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// CheckName reports why s cannot be used as a code name that a command can
// reference (SPEC §6.1): it must be a WORD and not a reserved word. It returns
// the empty string when s is acceptable.
func CheckName(s string) string {
	switch {
	case !IsWord(s):
		return "a code name may contain only letters, digits and underscore"
	case IsReserved(s):
		return strconv.Quote(s) + " is a reserved word of the command language"
	}
	return ""
}

// groupLabel is the informational group_code_name of a selection in the
// parsed form: a merge or zip joins its operands' names with '+' or '|'.
func groupLabel(s Selection) string {
	switch s.Kind {
	case SelLiteral:
		return "lit"
	case SelMerge:
		return joinLabels(s.Operands, "+")
	case SelZip:
		return joinLabels(s.Operands, "|")
	default:
		return s.Group.Text
	}
}

func joinLabels(ops []Selection, sep string) string {
	names := make([]string, len(ops))
	for i, o := range ops {
		names[i] = groupLabel(o)
	}
	return strings.Join(names, sep)
}
