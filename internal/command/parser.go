package command

import (
	"math"
	"strconv"
)

// maxCount is the largest count the language accepts.
const maxCount = math.MaxInt32

// Parse lexes and parses text into a Command and applies the restrictions of
// SPEC §6.3 that need no catalogue (S1–S8, T1–T5). It stops at the first
// lexical error, then the first syntax error, then reports the restriction
// violation that comes first in the text. Semantic checks (SPEC §6.11) and
// rule R1 need the catalogue and are not done here.
//
// The returned error is a CommandLexError, CommandSyntaxError or
// CommandRestrictionViolated, each with the metadata keys "rule", "position"
// and "token".
func Parse(text string) (*Command, error) {
	toks, err := lex(text)
	if err != nil {
		return nil, err
	}
	p := &parser{toks: toks}
	cmd, err := p.parseCommand()
	if err != nil {
		return nil, err
	}
	p.violations = append(p.violations, restrictions(cmd, p.emptyAt)...)
	if err := firstViolation(p.violations); err != nil {
		return nil, err
	}
	return cmd, nil
}

type parser struct {
	toks       []Token
	i          int
	violations []violation
	emptyAt    Pos // where the selections should have started (rule S6)
}

func (p *parser) peek() Token { return p.toks[p.i] }

func (p *parser) next() Token {
	t := p.toks[p.i]
	if t.Kind != TokEOF {
		p.i++
	}
	return t
}

func (p *parser) accept(kind TokKind) (Token, bool) {
	if p.peek().Kind == kind {
		return p.next(), true
	}
	return Token{}, false
}

// isWord reports whether t is the (reserved or contextual) word text.
func isWord(t Token, text string) bool { return t.Kind == TokWord && t.Text == text }

func (p *parser) violate(rule, msg string, at Pos, token string) {
	p.violations = append(p.violations, violation{rule: rule, msg: msg, at: at, token: token})
}

func describe(t Token) string {
	switch t.Kind {
	case TokEOF:
		return "the end of the command"
	case TokString:
		return strconv.Quote(t.Text)
	default:
		return "'" + t.Text + "'"
	}
}

func (p *parser) unexpected(want string) error {
	t := p.peek()
	return syntaxError("expected "+want+", found "+describe(t), t.Pos, t.Text)
}

func (p *parser) expect(kind TokKind, want string) (Token, error) {
	if t, ok := p.accept(kind); ok {
		return t, nil
	}
	return Token{}, p.unexpected(want)
}

// parseName reads a code name: a WORD that is not a reserved word.
func (p *parser) parseName(role string) (Name, error) {
	t := p.peek()
	switch {
	case t.Kind == TokPattern:
		return Name{}, syntaxError("wildcards are only allowed in item selectors, not in a "+role, t.Pos, t.Text)
	case t.Kind != TokWord:
		return Name{}, p.unexpected("a " + role)
	case IsReserved(t.Text):
		return Name{}, syntaxError("'"+t.Text+"' is a reserved word and cannot be used as a "+role, t.Pos, t.Text)
	}
	p.next()
	return Name{Text: t.Text, Pos: t.Pos}, nil
}

func (p *parser) parseCommand() (*Command, error) {
	c := &Command{}
	for {
		t, err := p.parseTarget()
		if err != nil {
			return nil, err
		}
		c.Servers = append(c.Servers, t)
		if _, ok := p.accept(TokComma); !ok {
			break
		}
	}
	if _, err := p.expect(TokArrow, "'-$' after the servers"); err != nil {
		return nil, err
	}
	gen, err := p.parseName("generator")
	if err != nil {
		return nil, err
	}
	c.Generator = gen
	if _, err := p.expect(TokColon, "':' after the generator"); err != nil {
		return nil, err
	}
	if c.Selections, err = p.parseSelections(); err != nil {
		return nil, err
	}
	if err := p.parseClauses(c); err != nil {
		return nil, err
	}
	for {
		if _, ok := p.accept(TokFix); !ok {
			break
		}
		step, err := p.parseStep()
		if err != nil {
			return nil, err
		}
		c.Fixers = append(c.Fixers, step)
	}
	if t := p.peek(); t.Kind != TokEOF {
		if clauseRank(t) > 0 {
			return nil, syntaxError("the clause '"+t.Text+"' must come before the fixers", t.Pos, t.Text)
		}
		return nil, p.unexpected("the end of the command")
	}
	return c, nil
}

func (p *parser) parseTarget() (Target, error) {
	name, err := p.parseName("server")
	if err != nil {
		return Target{}, err
	}
	t := Target{Server: name, Weight: 1}
	if _, ok := p.accept(TokEq); ok {
		w, err := p.parseCount("a weight")
		if err != nil {
			return Target{}, err
		}
		t.Weight, t.WeightPos, t.weightSet = w.N, w.Pos, true
	}
	return t, nil
}

func (p *parser) parseStep() (Step, error) {
	fixer, err := p.parseName("fixer")
	if err != nil {
		return Step{}, err
	}
	s := Step{Fixer: fixer}
	if t := p.peek(); isWord(t, "on") {
		p.next()
		on, err := p.parseName("server")
		if err != nil {
			return Step{}, err
		}
		s.On = &on
	}
	return s, nil
}

// ---- selections ----

func (p *parser) startsTerm() bool {
	t := p.peek()
	switch t.Kind {
	case TokString, TokBang, TokPattern:
		return true
	case TokWord:
		return !IsReserved(t.Text)
	}
	return false
}

func (p *parser) parseSelections() ([]Selection, error) {
	if !p.startsTerm() {
		t := p.peek()
		if t.Kind != TokEOF && t.Kind != TokFix && clauseRank(t) == 0 {
			return nil, p.unexpected("a group name or a string literal")
		}
		p.emptyAt = t.Pos
		p.violate(RuleS6, "a command needs at least one selection after ':'", t.Pos, t.Text)
		return nil, nil
	}
	var sels []Selection
	for {
		s, err := p.parseCombo()
		if err != nil {
			return nil, err
		}
		sels = append(sels, s)
		if _, ok := p.accept(TokProduct); !ok {
			return sels, nil
		}
	}
}

// parseBody reads `combo { * combo }`, the body of a template tag or region
// keyword.
func (p *parser) parseBody() ([]Selection, error) {
	var body []Selection
	for {
		s, err := p.parseCombo()
		if err != nil {
			return nil, err
		}
		body = append(body, s)
		if _, ok := p.accept(TokProduct); !ok {
			return body, nil
		}
	}
}

func (p *parser) parseCombo() (Selection, error) {
	first, err := p.parseOperand()
	if err != nil {
		return Selection{}, err
	}
	op := p.peek()
	if op.Kind != TokMerge && op.Kind != TokZip {
		return first, nil
	}
	operands := []Selection{first}
	mixed := false
	for {
		t := p.peek()
		if t.Kind != TokMerge && t.Kind != TokZip {
			break
		}
		if t.Kind != op.Kind && !mixed {
			mixed = true
			p.violate(RuleS1, "'&&' and '||' cannot be mixed in one term; a selection has no grouping parentheses", t.Pos, t.Text)
		}
		p.next()
		o, err := p.parseOperand()
		if err != nil {
			return Selection{}, err
		}
		operands = append(operands, o)
	}
	kind := SelMerge
	if op.Kind == TokZip {
		kind = SelZip
	}
	return Selection{Kind: kind, Operands: operands, Pos: Span(first.Pos, operands[len(operands)-1].Pos)}, nil
}

func (p *parser) parseOperand() (Selection, error) {
	t := p.peek()
	switch t.Kind {
	case TokString:
		p.next()
		return Selection{Kind: SelLiteral, Text: t.Text, Pos: t.Pos}, nil
	case TokBang:
		p.next()
		s, ok := p.accept(TokString)
		if !ok {
			return Selection{}, p.unexpected("a string after '!'")
		}
		return Selection{Kind: SelLiteral, Text: s.Text, Negative: true, Pos: Span(t.Pos, s.Pos)}, nil
	case TokWord, TokPattern:
		return p.parseGroupTerm()
	}
	return Selection{}, p.unexpected("a group name or a string literal")
}

// parseGroupTerm reads `group [filter]`, then an optional template `[…]` or
// region `{…}` suffix.
func (p *parser) parseGroupTerm() (Selection, error) {
	group, err := p.parseName("group")
	if err != nil {
		return Selection{}, err
	}
	sel := Selection{Kind: SelGroup, Group: group, Pos: group.Pos}
	if p.peek().Kind == TokLPar {
		f, err := p.parseFilter()
		if err != nil {
			return Selection{}, err
		}
		sel.Filter = &f
		for p.peek().Kind == TokLPar { // rule S2: a group has at most one filter
			extra, err := p.parseFilter()
			if err != nil {
				return Selection{}, err
			}
			p.violate(RuleS2, "a group has at most one filter; put every selector in one pair of parentheses", extra.Pos, "(")
		}
	}
	switch p.peek().Kind {
	case TokLBrk:
		p.next()
		sel.Kind = SelTemplate
		if sel.Binds, err = p.parseBinds(TokRBrk, "template tag", "']'"); err != nil {
			return Selection{}, err
		}
	case TokLBrc:
		p.next()
		sel.Kind = SelRegion
		if sel.Binds, err = p.parseBinds(TokRBrc, "region keyword", "'}'"); err != nil {
			return Selection{}, err
		}
	}
	return sel, nil
}

func (p *parser) parseBinds(closing TokKind, role, closer string) ([]Bind, error) {
	var binds []Bind
	for {
		key, err := p.parseName(role)
		if err != nil {
			return nil, err
		}
		if _, err := p.expect(TokColon, "':' after the "+role); err != nil {
			return nil, err
		}
		body, err := p.parseBody()
		if err != nil {
			return nil, err
		}
		binds = append(binds, Bind{Key: key, Body: body})
		if _, ok := p.accept(TokComma); ok {
			continue
		}
		if _, err := p.expect(closing, "',' or "+closer); err != nil {
			return nil, err
		}
		return binds, nil
	}
}

// parseFilter reads `( selector, … )` or `( ~selector, … )`. Mixing the two
// forms is rule S2.
func (p *parser) parseFilter() (Filter, error) {
	open, err := p.expect(TokLPar, "'('")
	if err != nil {
		return Filter{}, err
	}
	f := Filter{}
	_, f.Exclude = p.accept(TokTilde)
	mixed := false
	for first := true; ; first = false {
		if !first {
			tilde, hasTilde := p.accept(TokTilde)
			if hasTilde != f.Exclude && !mixed {
				mixed = true
				at := p.peek()
				if hasTilde {
					at = tilde
				}
				p.violate(RuleS2, "a filter is either all-include or all-exclude; write '~' before every excluded selector or none", at.Pos, at.Text)
			}
		}
		sel, err := p.parseSelector()
		if err != nil {
			return Filter{}, err
		}
		f.Selectors = append(f.Selectors, sel)
		if _, ok := p.accept(TokComma); !ok {
			break
		}
	}
	closeTok, err := p.expect(TokRPar, "',' or ')'")
	if err != nil {
		return Filter{}, err
	}
	f.Pos = Span(open.Pos, closeTok.Pos)
	return f, nil
}

func (p *parser) parseSelector() (Selector, error) {
	t := p.peek()
	switch t.Kind {
	case TokHash:
		p.next()
		tag, err := p.parseName("tag")
		if err != nil {
			return Selector{}, err
		}
		return Selector{Kind: SelTag, Text: tag.Text, Pos: Span(t.Pos, tag.Pos)}, nil
	case TokPattern:
		p.next()
		return Selector{Kind: SelPattern, Text: t.Text, Pos: t.Pos}, nil
	case TokWord:
		name, err := p.parseName("item")
		if err != nil {
			return Selector{}, err
		}
		return Selector{Kind: SelItem, Text: name.Text, Pos: name.Pos}, nil
	}
	return Selector{}, p.unexpected("an item name, a pattern or a #tag")
}

// ---- clauses ----

// clauseRank gives the fixed order of the clauses: where, limit/sample,
// repeat, with, spread. It is 0 for tokens that do not start a clause.
func clauseRank(t Token) int {
	if t.Kind != TokWord {
		return 0
	}
	switch t.Text {
	case "where":
		return 1
	case "limit", "sample":
		return 2
	case "repeat":
		return 3
	case "with":
		return 4
	case "spread":
		return 5
	}
	return 0
}

func (p *parser) parseClauses(c *Command) error {
	last := 0
	for {
		t := p.peek()
		rank := clauseRank(t)
		if rank == 0 {
			return nil
		}
		switch {
		case rank == last && rank == 2:
			return syntaxError("only one of 'limit' and 'sample' is allowed", t.Pos, t.Text)
		case rank == last:
			return syntaxError("the clause '"+t.Text+"' appears twice", t.Pos, t.Text)
		case rank < last:
			return syntaxError("the clause '"+t.Text+"' is out of order; the order is where, limit/sample, repeat, with, spread", t.Pos, t.Text)
		}
		last = rank
		p.next()
		var err error
		switch t.Text {
		case "where":
			c.Where, err = p.parseCond()
		case "limit":
			var n Count
			if n, err = p.parseCount("a count after 'limit'"); err == nil {
				n.Pos = Span(t.Pos, n.Pos)
				c.Limit = &n
			}
		case "sample":
			c.Sample, err = p.parseSample(t)
		case "repeat":
			var n Count
			if n, err = p.parseCount("a count after 'repeat'"); err == nil {
				n.Pos = Span(t.Pos, n.Pos)
				c.Repeat = &n
			}
		case "with":
			c.Overrides, err = p.parseOverrides()
		case "spread":
			c.Spread, err = p.parseSpread(t)
		}
		if err != nil {
			return err
		}
	}
}

func (p *parser) parseSample(kw Token) (*Sample, error) {
	n, err := p.parseCount("a count after 'sample'")
	if err != nil {
		return nil, err
	}
	s := &Sample{Count: n, Pos: Span(kw.Pos, n.Pos)}
	if t := p.peek(); isWord(t, "seed") {
		p.next()
		seed, err := p.parseSeed()
		if err != nil {
			return nil, err
		}
		s.Seed = &seed
		s.Pos = Span(s.Pos, seed.Pos)
	}
	return s, nil
}

func (p *parser) parseSeed() (Seed, error) {
	t := p.peek()
	if t.Kind != TokWord && t.Kind != TokDecimal {
		return Seed{}, p.unexpected("a seed number")
	}
	if t.Kind == TokWord && IsReserved(t.Text) {
		return Seed{}, p.unexpected("a seed number")
	}
	p.next()
	if !isDigits(t.Text) {
		p.violate(RuleS8, "a seed must be a whole number made of digits", t.Pos, t.Text)
		return Seed{Pos: t.Pos}, nil
	}
	n, err := strconv.ParseUint(t.Text, 10, 64)
	if err != nil {
		p.violate(RuleS8, "the seed is too large", t.Pos, t.Text)
	}
	return Seed{N: n, Pos: t.Pos}, nil
}

// parseCount reads a count: digits only and at least 1 (rule S8). A bad
// count is recorded as a violation and parsing goes on with the value 1.
func (p *parser) parseCount(want string) (Count, error) {
	t := p.peek()
	if t.Kind != TokWord && t.Kind != TokDecimal || t.Kind == TokWord && IsReserved(t.Text) {
		return Count{}, p.unexpected(want)
	}
	p.next()
	bad := func(msg string) (Count, error) {
		p.violate(RuleS8, msg, t.Pos, t.Text)
		return Count{N: 1, Pos: t.Pos}, nil
	}
	if !isDigits(t.Text) {
		return bad("a count must be a whole number made of digits")
	}
	n, err := strconv.ParseUint(t.Text, 10, 64)
	switch {
	case err != nil || n > maxCount:
		return bad("the count is too large")
	case n < 1:
		return bad("a count must be at least 1")
	}
	return Count{N: int(n), Pos: t.Pos}, nil
}

func (p *parser) parseOverrides() ([]Override, error) {
	var out []Override
	for {
		key, err := p.parseName("setting name")
		if err != nil {
			return nil, err
		}
		if _, err := p.expect(TokEq, "'=' after the setting name"); err != nil {
			return nil, err
		}
		v, err := p.parseValue()
		if err != nil {
			return nil, err
		}
		out = append(out, Override{Key: key, Value: v})
		if _, ok := p.accept(TokComma); !ok {
			return out, nil
		}
	}
}

func (p *parser) parseValue() (Value, error) {
	t := p.peek()
	switch t.Kind {
	case TokString:
		p.next()
		return Value{Kind: ValueText, Text: t.Text, Pos: t.Pos}, nil
	case TokDecimal:
		p.next()
		f, err := strconv.ParseFloat(t.Text, 64)
		if err != nil || math.IsInf(f, 0) {
			return Value{}, syntaxError("the number is out of range", t.Pos, t.Text)
		}
		return Value{Kind: ValueDecimal, Float: f, Pos: t.Pos}, nil
	case TokWord:
		p.next()
		if !isDigits(t.Text) {
			return Value{Kind: ValueText, Text: t.Text, Pos: t.Pos}, nil
		}
		n, err := strconv.ParseUint(t.Text, 10, 64)
		if err != nil {
			p.violate(RuleS8, "the integer is too large", t.Pos, t.Text)
		}
		return Value{Kind: ValueInt, Int: n, Pos: t.Pos}, nil
	}
	return Value{}, p.unexpected("a value")
}

func (p *parser) parseSpread(kw Token) (*Spread, error) {
	t := p.peek()
	if t.Kind != TokWord {
		return nil, p.unexpected("dynamic, even, blocks or by")
	}
	s := &Spread{Mode: SpreadMode(t.Text), Pos: Span(kw.Pos, t.Pos)}
	p.next()
	switch s.Mode {
	case SpreadDynamic, SpreadEven, SpreadBlocks:
	case SpreadBy:
		g, err := p.parseName("group")
		if err != nil {
			return nil, err
		}
		s.ByGroup = g
		s.Pos = Span(s.Pos, g.Pos)
	default:
		return nil, syntaxError("expected dynamic, even, blocks or by, found '"+t.Text+"'", t.Pos, t.Text)
	}
	if pin := p.peek(); isWord(pin, "pinned") {
		p.next()
		s.Pinned, s.PinnedPos = true, pin.Pos
		s.Pos = Span(s.Pos, pin.Pos)
	}
	return s, nil
}

// ---- where ----

func (p *parser) parseCond() (*Cond, error) {
	first, err := p.parseConj()
	if err != nil {
		return nil, err
	}
	args := appendFlat(nil, first, CondOr)
	for isWord(p.peek(), "or") {
		p.next()
		c, err := p.parseConj()
		if err != nil {
			return nil, err
		}
		args = appendFlat(args, c, CondOr)
	}
	if len(args) == 1 {
		return first, nil
	}
	return &Cond{Op: CondOr, Args: args, Pos: Span(args[0].Pos, args[len(args)-1].Pos)}, nil
}

func (p *parser) parseConj() (*Cond, error) {
	first, err := p.parseNeg()
	if err != nil {
		return nil, err
	}
	args := appendFlat(nil, first, CondAnd)
	for isWord(p.peek(), "and") {
		p.next()
		c, err := p.parseNeg()
		if err != nil {
			return nil, err
		}
		args = appendFlat(args, c, CondAnd)
	}
	if len(args) == 1 {
		return first, nil
	}
	return &Cond{Op: CondAnd, Args: args, Pos: Span(args[0].Pos, args[len(args)-1].Pos)}, nil
}

// appendFlat adds c to args, splicing in its operands when it is a group of the
// same associative operator (SPEC §6.3: all binary operators are associative).
func appendFlat(args []*Cond, c *Cond, op CondOp) []*Cond {
	if c.Op == op {
		return append(args, c.Args...)
	}
	return append(args, c)
}

func (p *parser) parseNeg() (*Cond, error) {
	t := p.peek()
	switch {
	case isWord(t, "not"):
		p.next()
		arg, err := p.parseNeg()
		if err != nil {
			return nil, err
		}
		return &Cond{Op: CondNot, Args: []*Cond{arg}, Pos: Span(t.Pos, arg.Pos)}, nil
	case t.Kind == TokLPar:
		p.next()
		c, err := p.parseCond()
		if err != nil {
			return nil, err
		}
		if _, err := p.expect(TokRPar, "')'"); err != nil {
			return nil, err
		}
		return c, nil
	}
	return p.parseAtom()
}

func (p *parser) parseAtom() (*Cond, error) {
	t := p.peek()
	if t.Kind == TokHash {
		p.next()
		tag, err := p.parseName("tag")
		if err != nil {
			return nil, err
		}
		return &Cond{Op: CondTag, Tag: tag, Pos: Span(t.Pos, tag.Pos)}, nil
	}
	group, err := p.parseName("group")
	if err != nil {
		return nil, err
	}
	switch op := p.peek(); {
	case op.Kind == TokEq:
		p.next()
		sel, err := p.parseSelector()
		if err != nil {
			return nil, err
		}
		return &Cond{Op: CondEq, Group: group, Selectors: []Selector{sel}, Pos: Span(group.Pos, sel.Pos)}, nil
	case isWord(op, "in"):
		p.next()
		if _, err := p.expect(TokLPar, "'(' after 'in'"); err != nil {
			return nil, err
		}
		var sels []Selector
		for {
			sel, err := p.parseSelector()
			if err != nil {
				return nil, err
			}
			sels = append(sels, sel)
			if _, ok := p.accept(TokComma); !ok {
				break
			}
		}
		closeTok, err := p.expect(TokRPar, "',' or ')'")
		if err != nil {
			return nil, err
		}
		return &Cond{Op: CondIn, Group: group, Selectors: sels, Pos: Span(group.Pos, closeTok.Pos)}, nil
	}
	return nil, p.unexpected("'=' or 'in' after the group, or a #tag")
}
