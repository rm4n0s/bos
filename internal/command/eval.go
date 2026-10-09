package command

import (
	"strconv"
	"strings"
)

// evalItem is one element of an evaluated term: a catalogue item, a literal, or
// the fusion or template expansion of several (SPEC §6.4).
type evalItem struct {
	group    string // group code name; joined with '+' for a zip; empty for a literal
	code     string
	positive string
	negative string

	loras       []LoRA
	controlNets []ControlNet
	ipAdapters  []IPAdapter
	output      *OutputSpec

	// members are the original catalogue items that contribute (SPEC §6.7); a
	// literal has none.
	members []*Item
	// base is the catalogue item a region term's options stand for, whose masks
	// and coordinates the region prompts attach to.
	base *Item
}

func fromItem(it *Item) evalItem {
	return evalItem{
		group: it.Group, code: it.Code, positive: it.Positive, negative: it.Negative,
		loras: it.LoRAs, controlNets: it.ControlNets, ipAdapters: it.IPAdapters,
		output: it.Output, members: []*Item{it}, base: it,
	}
}

// fuse merges the members of a zipped tuple into one item (SPEC §6.4).
func fuse(parts []evalItem) evalItem {
	var f evalItem
	var groups, codes []string
	var pos, neg []string
	for _, p := range parts {
		if p.group != "" {
			groups = append(groups, p.group)
		}
		codes = append(codes, p.code)
		pos = append(pos, p.positive)
		neg = append(neg, p.negative)
		f.loras = append(f.loras, p.loras...)
		f.controlNets = append(f.controlNets, p.controlNets...)
		f.ipAdapters = append(f.ipAdapters, p.ipAdapters...)
		f.members = append(f.members, p.members...)
		if p.output != nil {
			f.output = p.output
		}
	}
	f.group = strings.Join(groups, "+")
	f.code = strings.Join(codes, "+")
	f.positive, f.negative = joinPrompts(pos...), joinPrompts(neg...)
	return f
}

// fragment is one combination of a template tag's body (SPEC §6.5, step 2).
type fragment struct {
	positive string
	negative string
	loras    []LoRA
	members  []*Item
	label    string
}

func fragmentOf(chosen []evalItem) fragment {
	var f fragment
	var pos, neg, labels []string
	for _, c := range chosen {
		pos = append(pos, c.positive)
		neg = append(neg, c.negative)
		f.loras = append(f.loras, c.loras...)
		f.members = append(f.members, c.members...)
		labels = append(labels, c.code)
	}
	f.positive, f.negative = joinPrompts(pos...), joinPrompts(neg...)
	f.label = strings.Join(labels, "+")
	return f
}

// regionPrompt is the prompt of one region keyword (SPEC §6.6, step 1).
type regionPrompt struct {
	keyword string
	prompt  string
	loras   []LoRA
	members []*Item
}

// regionSet is one complete set of region prompts, one per keyword in the
// order the keywords were written.
type regionSet []regionPrompt

// evaluator turns selections into item lists. Problems it finds are the ones
// only evaluation can see: an empty term and a term that is too large.
type evaluator struct {
	items    func(group string) []Item
	lit      int
	limit    int
	problems []Problem
}

func (e *evaluator) add(tag, msg string, at Pos) {
	e.problems = append(e.problems, semantic(RuleSemantic, tag, msg, at))
}

// mul multiplies two sizes, reporting false when the result exceeds the limit.
func (e *evaluator) mul(a, b int) (int, bool) {
	if a == 0 || b == 0 {
		return 0, true
	}
	if a > e.limit/b {
		return 0, false
	}
	return a * b, true
}

func (e *evaluator) tooLarge(at Pos) {
	e.add(tagTooLarge, "this part of the command expands to more than "+strconv.Itoa(e.limit)+" combinations", at)
}

// selection evaluates a term, or a combo of a template or region body, to its
// ordered item list and reports it when it comes out empty (SPEC §6.11: no term
// evaluates to an empty list).
func (e *evaluator) selection(s Selection) []evalItem {
	before := len(e.problems)
	out := e.eval(s)
	if len(out) == 0 && len(e.problems) == before {
		e.add(tagSelectionEmpty, "this selection evaluates to no items", s.Pos)
	}
	return out
}

// eval evaluates one non-region selection without judging an empty result. An
// operand of a merge may be empty as long as the merge is not; an empty operand
// of a zip empties the zip, which is reported there.
func (e *evaluator) eval(s Selection) []evalItem {
	var out []evalItem
	switch s.Kind {
	case SelLiteral:
		e.lit++
		it := evalItem{code: "lit" + strconv.Itoa(e.lit)}
		if s.Negative {
			it.negative = s.Text
		} else {
			it.positive = s.Text
		}
		out = []evalItem{it}
	case SelMerge:
		for _, op := range s.Operands {
			out = append(out, e.eval(op)...)
			if len(out) > e.limit {
				e.tooLarge(s.Pos)
				return nil
			}
		}
	case SelZip:
		out = e.zip(s)
	case SelTemplate:
		out = e.template(s)
	default:
		items := applyFilter(e.items(s.Group.Text), s.Filter)
		out = make([]evalItem, len(items))
		for i, it := range items {
			out[i] = fromItem(it)
		}
	}
	return out
}

func (e *evaluator) zip(s Selection) []evalItem {
	lists := make([][]evalItem, len(s.Operands))
	shortest := -1
	for i, op := range s.Operands {
		lists[i] = e.eval(op)
		if shortest < 0 || len(lists[i]) < shortest {
			shortest = len(lists[i])
		}
	}
	out := make([]evalItem, 0, max(shortest, 0))
	for k := range max(shortest, 0) {
		tuple := make([]evalItem, len(lists))
		for i := range lists {
			tuple[i] = lists[i][k]
		}
		out = append(out, fuse(tuple))
	}
	return out
}

// bodyLists evaluates the combos of a body; the body is their product.
func (e *evaluator) bodyLists(body []Selection) [][]evalItem {
	lists := make([][]evalItem, len(body))
	for i, c := range body {
		lists[i] = e.selection(c)
	}
	return lists
}

// product calls fn for every element of the Cartesian product of lists, last
// list varying fastest. The chosen slice is reused between calls.
func product(lists [][]evalItem, fn func(chosen []evalItem)) {
	for _, l := range lists {
		if len(l) == 0 {
			return
		}
	}
	idx := make([]int, len(lists))
	chosen := make([]evalItem, len(lists))
	for {
		for i := range lists {
			chosen[i] = lists[i][idx[i]]
		}
		fn(chosen)
		i := len(lists) - 1
		for i >= 0 {
			idx[i]++
			if idx[i] < len(lists[i]) {
				break
			}
			idx[i] = 0
			i--
		}
		if i < 0 {
			return
		}
	}
}

func productSize(e *evaluator, lists [][]evalItem) (int, bool) {
	n := 1
	for _, l := range lists {
		var ok bool
		if n, ok = e.mul(n, len(l)); !ok {
			return 0, false
		}
	}
	return n, true
}

// template expands `base[tag: body, …]` (SPEC §6.5).
func (e *evaluator) template(s Selection) []evalItem {
	base := applyFilter(e.items(s.Group.Text), s.Filter)
	frags := make([][]fragment, len(s.Binds))
	size := len(base)
	for i, b := range s.Binds {
		lists := e.bodyLists(b.Body)
		n, ok := productSize(e, lists)
		if !ok {
			e.tooLarge(b.Key.Pos)
			return nil
		}
		if n == 0 {
			return nil // an empty body was reported where it was found
		}
		frags[i] = make([]fragment, 0, n)
		product(lists, func(chosen []evalItem) { frags[i] = append(frags[i], fragmentOf(chosen)) })
		if size, ok = e.mul(size, n); !ok {
			e.tooLarge(s.Pos)
			return nil
		}
	}

	type key struct{ positive, negative string }
	seen := make(map[key]bool)
	var out []evalItem
	tuple := make([]int, len(frags))
	for _, b := range base {
		for i := range tuple {
			tuple[i] = 0
		}
		for {
			pos, neg := b.Positive, b.Negative
			code := b.Code
			loras := append([]LoRA(nil), b.LoRAs...)
			members := []*Item{b}
			for t, bind := range s.Binds {
				f := frags[t][tuple[t]]
				pos = strings.ReplaceAll(pos, placeholderOpen+bind.Key.Text+">", f.positive)
				neg = strings.ReplaceAll(neg, placeholderOpen+bind.Key.Text+">", f.negative)
				code += "-" + f.label
				loras = append(loras, f.loras...)
				members = append(members, f.members...)
			}
			if k := (key{pos, neg}); !seen[k] {
				seen[k] = true
				out = append(out, evalItem{
					group: b.Group, code: code, positive: pos, negative: neg,
					loras: dedupeLoRAs(loras), controlNets: b.ControlNets, ipAdapters: b.IPAdapters,
					output: b.Output, members: members, base: b,
				})
			}
			t := len(tuple) - 1
			for t >= 0 {
				tuple[t]++
				if tuple[t] < len(frags[t]) {
					break
				}
				tuple[t] = 0
				t--
			}
			if t < 0 {
				break
			}
		}
	}
	return out
}

// regionSets evaluates the keyword bodies of a region term and returns the
// product of their prompts (SPEC §6.6). Only positive prompts count.
func (e *evaluator) regionSets(s Selection) []regionSet {
	perKeyword := make([][]regionPrompt, len(s.Binds))
	for i, b := range s.Binds {
		lists := e.bodyLists(b.Body)
		n, ok := productSize(e, lists)
		if !ok {
			e.tooLarge(b.Key.Pos)
			return nil
		}
		if n == 0 {
			return nil
		}
		keyword := b.Key.Text
		product(lists, func(chosen []evalItem) {
			rp := regionPrompt{keyword: keyword}
			var prompts []string
			for _, c := range chosen {
				prompts = append(prompts, c.positive)
				rp.loras = append(rp.loras, c.loras...)
				rp.members = append(rp.members, c.members...)
			}
			rp.prompt = joinPrompts(prompts...)
			perKeyword[i] = append(perKeyword[i], rp)
		})
	}

	total := 1
	for _, l := range perKeyword {
		var ok bool
		if total, ok = e.mul(total, len(l)); !ok {
			e.tooLarge(s.Pos)
			return nil
		}
	}
	sets := make([]regionSet, 0, total)
	idx := make([]int, len(perKeyword))
	for {
		set := make(regionSet, len(perKeyword))
		for k := range perKeyword {
			set[k] = perKeyword[k][idx[k]]
		}
		sets = append(sets, set)
		k := len(idx) - 1
		for k >= 0 {
			idx[k]++
			if idx[k] < len(perKeyword[k]) {
				break
			}
			idx[k] = 0
			k--
		}
		if k < 0 {
			return sets
		}
	}
}
