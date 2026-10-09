package command

import "strings"

// cnode is a compiled `where` condition. Whether an atom holds for a
// combination depends only on the options chosen in each dimension, so each
// atom is reduced to a table of booleans per dimension and option.
type cnode struct {
	op   CondOp
	kids []*cnode
	hits []dimHits // atoms: the dimensions in which some option satisfies it
}

type dimHits struct {
	dim int
	hit []bool // per option
}

// compileCond builds the evaluation tree. members[d][o] are the original items
// that option o of dimension d contributes to a combination (SPEC §6.7).
func compileCond(c *Cond, members [][][]*Item) *cnode {
	n := &cnode{op: c.Op}
	switch c.Op {
	case CondAnd, CondOr, CondNot:
		for _, a := range c.Args {
			n.kids = append(n.kids, compileCond(a, members))
		}
		return n
	}
	holds := func(it *Item) bool {
		switch c.Op {
		case CondEq:
			return it.Group == c.Group.Text && c.Selectors[0].matches(it)
		case CondIn:
			return it.Group == c.Group.Text && matchesAny(c.Selectors, it)
		default:
			for _, tag := range it.Tags {
				if strings.EqualFold(tag, c.Tag.Text) {
					return true
				}
			}
			return false
		}
	}
	for d, options := range members {
		hit := make([]bool, len(options))
		any := false
		for o, its := range options {
			for _, it := range its {
				if holds(it) {
					hit[o], any = true, true
					break
				}
			}
		}
		if any {
			n.hits = append(n.hits, dimHits{dim: d, hit: hit})
		}
	}
	return n
}

// eval reports whether the condition holds for the combination whose chosen
// option in each dimension is digits[d].
func (n *cnode) eval(digits []int) bool {
	switch n.op {
	case CondAnd:
		for _, k := range n.kids {
			if !k.eval(digits) {
				return false
			}
		}
		return true
	case CondOr:
		for _, k := range n.kids {
			if k.eval(digits) {
				return true
			}
		}
		return false
	case CondNot:
		return !n.kids[0].eval(digits)
	}
	for _, h := range n.hits {
		if h.hit[digits[h.dim]] {
			return true
		}
	}
	return false
}
