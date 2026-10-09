package command

import (
	"fmt"
	"math/rand/v2"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// randomCatalogue builds groups g0…g(n-1) of 1–5 items whose tags are drawn
// from t0…t2.
func randomCatalogue(r *rand.Rand, groups int) *fakeCatalogue {
	cat := newFakeCatalogue()
	cat.servers["a"], cat.servers["b"], cat.servers["c"] = true, true, true
	cat.generators["g"] = Generator{OutputType: OutputImage}
	cat.generators["gr"] = Generator{OutputType: OutputImage, HasRandomSeed: true}
	for gi := range groups {
		items := make([]Item, 1+r.IntN(5))
		for i := range items {
			items[i] = Item{Code: fmt.Sprintf("g%di%d", gi, i), Positive: fmt.Sprintf("p%d%d", gi, i)}
			for tg := range 3 {
				if r.IntN(3) == 0 {
					items[i].Tags = append(items[i].Tags, "t"+strconv.Itoa(tg))
				}
			}
		}
		cat.group("g"+strconv.Itoa(gi), items...)
	}
	return cat
}

// term is a plain top-level term as the oracle sees it.
type term struct {
	group string
	text  string
	items []Item
}

// randomTerm picks a group (not used before) and an optional filter, and
// evaluates the filter by hand.
func randomTerm(r *rand.Rand, cat *fakeCatalogue, group string) term {
	items := cat.groups[group]
	t := term{group: group, text: group, items: items}
	switch r.IntN(4) {
	case 0: // include a few items by name
		var names []string
		var kept []Item
		for _, it := range items {
			if r.IntN(2) == 0 {
				names = append(names, it.Code)
				kept = append(kept, it)
			}
		}
		if len(kept) > 0 {
			t.text, t.items = group+"("+strings.Join(names, ", ")+")", kept
		}
	case 1: // exclude one item
		skip := r.IntN(len(items))
		if len(items) > 1 {
			var kept []Item
			for i, it := range items {
				if i != skip {
					kept = append(kept, it)
				}
			}
			t.text, t.items = group+"(~"+items[skip].Code+")", kept
		}
	case 2: // include by tag, if some item has it
		tag := "t" + strconv.Itoa(r.IntN(3))
		var kept []Item
		for _, it := range items {
			if slices.Contains(it.Tags, tag) {
				kept = append(kept, it)
			}
		}
		if len(kept) > 0 {
			t.text, t.items = group+"(#"+tag+")", kept
		}
	}
	return t
}

// cond is a where condition with its own evaluator, written independently of
// the compiled tree in where.go.
type cond struct {
	text string
	eval func(chosen []Item) bool
}

func randomCond(r *rand.Rand, terms []term, depth int) cond {
	if depth > 0 && r.IntN(3) > 0 {
		switch r.IntN(3) {
		case 0:
			a, b := randomCond(r, terms, depth-1), randomCond(r, terms, depth-1)
			return cond{"(" + a.text + " and " + b.text + ")", func(c []Item) bool { return a.eval(c) && b.eval(c) }}
		case 1:
			a, b := randomCond(r, terms, depth-1), randomCond(r, terms, depth-1)
			return cond{"(" + a.text + " or " + b.text + ")", func(c []Item) bool { return a.eval(c) || b.eval(c) }}
		default:
			a := randomCond(r, terms, depth-1)
			return cond{"not " + a.text, func(c []Item) bool { return !a.eval(c) }}
		}
	}
	t := terms[r.IntN(len(terms))]
	switch r.IntN(3) {
	case 0: // group = item
		it := t.items[r.IntN(len(t.items))]
		return cond{t.group + " = " + it.Code, func(c []Item) bool {
			return slices.ContainsFunc(c, func(x Item) bool { return x.Group == t.group && x.Code == it.Code })
		}}
	case 1: // group in (…)
		a, b := t.items[r.IntN(len(t.items))], t.items[r.IntN(len(t.items))]
		return cond{t.group + " in (" + a.Code + ", " + b.Code + ")", func(c []Item) bool {
			return slices.ContainsFunc(c, func(x Item) bool { return x.Group == t.group && (x.Code == a.Code || x.Code == b.Code) })
		}}
	default: // #tag, any group; only tags that exist somewhere in the term list
		tag := "t0"
		for _, tm := range terms {
			for _, it := range tm.items {
				if len(it.Tags) > 0 {
					tag = it.Tags[0]
				}
			}
		}
		return cond{"#" + tag, func(c []Item) bool {
			return slices.ContainsFunc(c, func(x Item) bool { return slices.Contains(x.Tags, tag) })
		}}
	}
}

// product enumerates the combinations by nested loops, last term fastest.
func brute(terms []term) [][]Item {
	out := [][]Item{{}}
	for _, t := range terms {
		var next [][]Item
		for _, prefix := range out {
			for _, it := range t.items {
				next = append(next, append(slices.Clone(prefix), it))
			}
		}
		out = next
	}
	return out
}

func TestPropertyCountsOrderAndWhere(t *testing.T) {
	r := rand.New(rand.NewPCG(1, 2))
	for n := range 400 {
		cat := randomCatalogue(r, 4)
		var terms []term
		var parts []string
		for _, gi := range r.Perm(4)[:1+r.IntN(3)] {
			tm := randomTerm(r, cat, "g"+strconv.Itoa(gi))
			terms = append(terms, tm)
			parts = append(parts, tm.text)
		}
		text := "a -$ g: " + strings.Join(parts, " * ")
		all := brute(terms)
		want := all

		var wc *cond
		if r.IntN(2) == 0 {
			c := randomCond(r, terms, 2)
			wc = &c
			text += " where " + c.text
			want = nil
			for _, combo := range all {
				if c.eval(combo) {
					want = append(want, combo)
				}
			}
			if len(want) == 0 {
				_, err := Expand(mustParse(t, text), cat, Options{})
				if !hasProblem(t, err, "CommandWhereEmpty") {
					t.Fatalf("case %d: %s: expected CommandWhereEmpty, got %v", n, text, err)
				}
				continue
			}
		}
		afterWhere := len(want)

		keep := len(want)
		switch r.IntN(3) {
		case 0:
			k := 1 + r.IntN(2*len(want))
			text += " limit " + strconv.Itoa(k)
			keep = min(k, len(want))
			want = want[:keep]
		case 1:
			k := 1 + r.IntN(2*len(want))
			text += fmt.Sprintf(" sample %d seed %d", k, r.Uint64()>>1)
			keep = min(k, len(want))
		}
		repeat := 1
		gen := "g"
		if r.IntN(3) == 0 {
			repeat = 2 + r.IntN(3)
			text += " repeat " + strconv.Itoa(repeat)
			gen = "gr"
		}
		text = strings.Replace(text, "-$ g:", "-$ "+gen+":", 1)

		x, err := Expand(mustParse(t, text), cat, Options{})
		if err != nil {
			t.Fatalf("case %d: Expand(%q): %v", n, text, err)
		}
		if x.AfterWhere != afterWhere || x.Kept != keep || x.Counts.Jobs != keep*repeat || len(x.Jobs) != keep*repeat {
			t.Fatalf("case %d: %s\ncounts = %+v, want after where %d, kept %d, jobs %d", n, text, x.Counts, afterWhere, keep, keep*repeat)
		}
		wantTotal := 1
		for _, tm := range terms {
			wantTotal *= len(tm.items)
		}
		if x.Combinations != wantTotal {
			t.Fatalf("case %d: N = %d, want %d", n, x.Combinations, wantTotal)
		}

		// Deterministic: a second expansion is identical.
		again, err := Expand(mustParse(t, text), cat, Options{})
		if err != nil || !reflect.DeepEqual(codes(x), codes(again)) {
			t.Fatalf("case %d: expansion is not deterministic for %s", n, text)
		}

		got := make([]string, 0, len(x.Jobs))
		for _, j := range x.Jobs {
			got = append(got, strings.Join(partCodes(j), "/"))
		}
		if !strings.Contains(text, "sample") {
			var expect []string
			for _, combo := range want {
				var cs []string
				for _, it := range combo {
					cs = append(cs, it.Code)
				}
				for range repeat {
					expect = append(expect, strings.Join(cs, "/"))
				}
			}
			if !reflect.DeepEqual(got, expect) {
				t.Fatalf("case %d: %s\norder = %v\nwant    %v", n, text, got, expect)
			}
		} else {
			// A sample is a subset of the surviving combinations, in product order, without repeats.
			surviving := map[string]int{}
			for i, combo := range want {
				var cs []string
				for _, it := range combo {
					cs = append(cs, it.Code)
				}
				surviving[strings.Join(cs, "/")] = i
			}
			last := -1
			for i := 0; i < len(got); i += repeat {
				pos, ok := surviving[got[i]]
				if !ok || pos <= last {
					t.Fatalf("case %d: %s\nsample %v is not an ascending subset of the combinations", n, text, got)
				}
				last = pos
			}
		}
		_ = wc
	}
}

func partCodes(j Job) []string {
	out := make([]string, len(j.Parts))
	for i, p := range j.Parts {
		out[i] = p.Code
	}
	return out
}

// Counts of the other constructs follow SPEC §6.4 and §6.5.
func TestPropertyConstructCounts(t *testing.T) {
	r := rand.New(rand.NewPCG(3, 4))
	for n := range 200 {
		cat := randomCatalogue(r, 5)
		size := func(g string) int { return len(cat.groups[g]) }
		a, b, c := "g0", "g1", "g2"

		// merge: sum of sizes
		x := expandOK(t, cat, fmt.Sprintf("a -$ g: %s && %s && %s", a, b, c))
		if len(x.Jobs) != size(a)+size(b)+size(c) {
			t.Fatalf("case %d: merge gave %d jobs", n, len(x.Jobs))
		}
		// zip: the shortest list
		x = expandOK(t, cat, fmt.Sprintf("a -$ g: %s || %s || %s", a, b, c))
		if len(x.Jobs) != min(size(a), size(b), size(c)) {
			t.Fatalf("case %d: zip gave %d jobs", n, len(x.Jobs))
		}
		// merge times product
		x = expandOK(t, cat, fmt.Sprintf("a -$ g: %s && %s * %s", a, b, c))
		if len(x.Jobs) != (size(a)+size(b))*size(c) {
			t.Fatalf("case %d: merge * term gave %d jobs", n, len(x.Jobs))
		}
		// zip times product
		x = expandOK(t, cat, fmt.Sprintf("a -$ g: %s || %s * %s", a, b, c))
		if len(x.Jobs) != min(size(a), size(b))*size(c) {
			t.Fatalf("case %d: zip * term gave %d jobs", n, len(x.Jobs))
		}
		// repeat multiplies, limit counts combinations
		x = expandOK(t, cat, fmt.Sprintf("a -$ gr: %s * %s limit 3 repeat 5", a, b))
		if len(x.Jobs) != min(3, size(a)*size(b))*5 {
			t.Fatalf("case %d: limit/repeat gave %d jobs", n, len(x.Jobs))
		}
		// template: at most |base| × Π fragments, and never fewer than |base| when the prompts differ
		cat.group("tm", Item{Code: "t1", Positive: "<tmpl:x> and <tmpl:y>"}, Item{Code: "t2", Positive: "<tmpl:x> only"})
		x = expandOK(t, cat, fmt.Sprintf("a -$ g: tm[x: %s, y: %s]", a, b))
		if bound := 2 * size(a) * size(b); len(x.Jobs) > bound || len(x.Jobs) < 2 {
			t.Fatalf("case %d: template gave %d jobs, bound %d", n, len(x.Jobs), bound)
		}
		for _, j := range x.Jobs {
			if strings.Contains(j.Positive, "<tmpl:") {
				t.Fatalf("case %d: placeholder left in %q", n, j.Positive)
			}
		}
	}
}

func expandOK(t *testing.T, cat Catalogue, text string) *Expansion {
	t.Helper()
	x, err := Expand(mustParse(t, text), cat, Options{})
	if err != nil {
		t.Fatalf("Expand(%q): %v", text, err)
	}
	return x
}

// Static spread modes keep their promises for any number of jobs and weights.
func TestPropertySpread(t *testing.T) {
	r := rand.New(rand.NewPCG(5, 6))
	for n := range 300 {
		cat := randomCatalogue(r, 3)
		names := []string{"a", "b", "c"}[:2+r.IntN(2)]
		weights := make([]int, len(names))
		var targets []string
		totalW := 0
		for i, nm := range names {
			weights[i] = 1 + r.IntN(4)
			totalW += weights[i]
			targets = append(targets, fmt.Sprintf("%s=%d", nm, weights[i]))
		}
		head := strings.Join(targets, ", ")
		jobsText := "g0 * g1"

		x := expandOK(t, cat, fmt.Sprintf("%s -$ g: %s spread blocks", head, jobsText))
		jobs := len(x.Jobs)
		counts := map[string]int{}
		var order []string
		for _, j := range x.Jobs {
			counts[j.Preferred]++
			if len(order) == 0 || order[len(order)-1] != j.Preferred {
				order = append(order, j.Preferred)
			}
		}
		// Blocks are contiguous and in listed order.
		if !slices.IsSorted(order) || len(order) > len(names) {
			t.Fatalf("case %d: blocks are not contiguous: %v", n, order)
		}
		// Each target gets ⌊N·w/W⌋ jobs, plus at most one.
		given := 0
		for i, nm := range names {
			floor := jobs * weights[i] / totalW
			if counts[nm] < floor || counts[nm] > floor+1 {
				t.Fatalf("case %d: %s got %d of %d jobs, weights %v", n, nm, counts[nm], jobs, weights)
			}
			given += counts[nm]
		}
		if given != jobs {
			t.Fatalf("case %d: %d of %d jobs assigned", n, given, jobs)
		}

		x = expandOK(t, cat, fmt.Sprintf("%s -$ g: %s spread even", head, jobsText))
		var seq []string
		for i, nm := range names {
			for range weights[i] {
				seq = append(seq, nm)
			}
		}
		for i, j := range x.Jobs {
			if j.Preferred != seq[i%len(seq)] {
				t.Fatalf("case %d: job %d on %s, want %s", n, i, j.Preferred, seq[i%len(seq)])
			}
		}

		x = expandOK(t, cat, fmt.Sprintf("%s -$ g: g0 * g1 spread by g0", head))
		home := map[string]string{}
		for _, j := range x.Jobs {
			if prev, ok := home[j.Parts[0].Code]; ok && prev != j.Preferred {
				t.Fatalf("case %d: %s runs on %s and %s", n, j.Parts[0].Code, prev, j.Preferred)
			}
			home[j.Parts[0].Code] = j.Preferred
		}
		total := 0
		for _, sc := range x.PerServer {
			total += sc.Jobs
		}
		if total != len(x.Jobs) {
			t.Fatalf("case %d: per-server counts add up to %d, not %d", n, total, len(x.Jobs))
		}
	}
}
