package command

import (
	"sort"
	"strconv"
)

// Default hard limits that keep a mistaken command from exhausting memory.
// Both are far above any realistic batch; the soft limit of SPEC §6.11 is the
// service's business.
const (
	DefaultMaxCombinations = 5_000_000
	DefaultMaxJobs         = 1_000_000
)

// Options tunes evaluation.
type Options struct {
	// MaxCombinations bounds the size of the full product and of any one term.
	MaxCombinations int
	// MaxJobs bounds the number of jobs a command may create.
	MaxJobs int
}

func (o Options) maxCombinations() int {
	if o.MaxCombinations > 0 {
		return o.MaxCombinations
	}
	return DefaultMaxCombinations
}

func (o Options) maxJobs() int {
	if o.MaxJobs > 0 {
		return o.MaxJobs
	}
	return DefaultMaxJobs
}

// Warning is a non-blocking remark returned by the preview (SPEC §6.11).
type Warning struct {
	Code     string `json:"code"`
	Message  string `json:"message"`
	Position Pos    `json:"position"`
}

// Counts are the numbers of SPEC §6.7 and §7.2.
type Counts struct {
	Combinations int `json:"combinations"` // N, the full product
	AfterWhere   int `json:"after_where"`  // N after `where`
	Kept         int `json:"kept"`         // after limit or sample
	Jobs         int `json:"jobs"`         // kept × repeat
}

// ServerCount is the number of jobs a static plan gives a server.
type ServerCount struct {
	Server string `json:"server"`
	Jobs   int    `json:"jobs"`
}

// plan is a validated command with its terms evaluated.
type plan struct {
	cmd *Command
	gen Generator

	terms    [][]evalItem // one option list per top-level selection, in order
	regionAt int          // index of the region term in terms, or -1
	sets     []regionSet  // the region-prompt sets (the last dimension), nil without a region
	radix    []int        // size of each dimension; the last is the number of region sets (1 if none)
	total    int          // N

	// kept are the indices of the combinations that remain after where and
	// limit/sample, in product order.
	kept       []int
	afterWhere int
	counts     Counts
	warnings   []Warning
	byTerm     int // index of the term spread-by partitions on, or -1
	perServer  []ServerCount
}

type analysis struct {
	problems []Problem
	plan     *plan
}

// analyse validates c and, when it is valid, evaluates it as far as the
// numbers of the preview. Jobs are not built.
func analyse(c *Command, cat Catalogue, opts Options) *analysis {
	k := newChecker(c, cat)
	k.check()
	if len(k.problems) > 0 {
		return &analysis{problems: k.problems}
	}
	p, problems := newPlan(c, k, opts)
	return &analysis{problems: problems, plan: p}
}

func newPlan(c *Command, k *checker, opts Options) (*plan, []Problem) {
	maxN := opts.maxCombinations()
	e := &evaluator{items: k.groupItems, limit: maxN}
	p := &plan{cmd: c, gen: k.gen, regionAt: -1, byTerm: -1}

	for i, s := range c.Selections {
		if s.Kind == SelRegion {
			p.regionAt = i
			p.terms = append(p.terms, e.selection(s)) // the base group's items
			p.sets = e.regionSets(s)
			if len(p.sets) == 0 && len(e.problems) == 0 {
				e.add(tagSelectionEmpty, "this region evaluates to no prompts", s.Pos)
			}
			continue
		}
		p.terms = append(p.terms, e.selection(s))
	}
	if len(e.problems) > 0 {
		return nil, e.problems
	}

	p.radix = make([]int, 0, len(p.terms)+1)
	total := 1
	for _, t := range p.terms {
		p.radix = append(p.radix, len(t))
		var ok bool
		if total, ok = e.mul(total, len(t)); !ok {
			return nil, []Problem{semantic(RuleSemantic, tagTooLarge,
				"the command has more than "+strconv.Itoa(maxN)+" combinations", c.Generator.Pos)}
		}
	}
	regions := max(len(p.sets), 1)
	p.radix = append(p.radix, regions)
	var ok bool
	if total, ok = e.mul(total, regions); !ok {
		return nil, []Problem{semantic(RuleSemantic, tagTooLarge,
			"the command has more than "+strconv.Itoa(maxN)+" combinations", c.Generator.Pos)}
	}
	p.total = total

	if c.Spread != nil && c.Spread.Mode == SpreadBy {
		for i, s := range c.Selections {
			if s.Kind == SelGroup && s.Group.Text == c.Spread.ByGroup.Text {
				p.byTerm = i
			}
		}
	}

	if problems := p.pick(opts); len(problems) > 0 {
		return nil, problems
	}
	p.assignCounts()
	return p, nil
}

// decode writes the option chosen in every dimension for combination idx.
// The last dimension varies fastest (SPEC §6.4).
func (p *plan) decode(idx int, digits []int) {
	for d := len(p.radix) - 1; d >= 0; d-- {
		digits[d] = idx % p.radix[d]
		idx /= p.radix[d]
	}
}

// members lists, per dimension and option, the original items that option
// contributes; the where conditions are compiled against it.
func (p *plan) members() [][][]*Item {
	out := make([][][]*Item, 0, len(p.radix))
	for _, t := range p.terms {
		opts := make([][]*Item, len(t))
		for i, it := range t {
			opts[i] = it.members
		}
		out = append(out, opts)
	}
	sets := make([][]*Item, max(len(p.sets), 1))
	for i, set := range p.sets {
		for _, rp := range set {
			sets[i] = append(sets[i], rp.members...)
		}
	}
	return append(out, sets)
}

// pick applies where, then limit or sample, in product order (SPEC §6.7).
func (p *plan) pick(opts Options) []Problem {
	c := p.cmd
	repeat := 1
	if c.Repeat != nil {
		repeat = c.Repeat.N
	}
	maxJobs := opts.maxJobs()
	tooMany := func() []Problem {
		return []Problem{semantic(RuleSemantic, tagTooLarge,
			"the command would create more than "+strconv.Itoa(maxJobs)+" jobs", c.Generator.Pos)}
	}

	var limitN, sampleN int
	var sampleSeed uint64
	switch {
	case c.Limit != nil:
		limitN = c.Limit.N
	case c.Sample != nil:
		sampleN = c.Sample.Count.N
		if c.Sample.Seed != nil {
			sampleSeed = c.Sample.Seed.N
		}
	}

	// passing holds the combinations that survive where, but only when a
	// sample must choose among all of them.
	var passing []int
	if c.Where == nil {
		p.afterWhere = p.total
		keep := p.total
		if limitN > 0 {
			keep = min(limitN, p.total)
		}
		if sampleN > 0 {
			keep = min(sampleN, p.total)
		}
		if keep > maxJobs/repeat {
			return tooMany()
		}
		switch {
		case sampleN > 0:
			p.kept = sampleIndices(p.total, sampleN, sampleSeed)
		default:
			p.kept = make([]int, keep)
			for i := range p.kept {
				p.kept[i] = i
			}
		}
	} else {
		cond := compileCond(c.Where, p.members())
		digits := make([]int, len(p.radix))
		for idx := range p.total {
			p.decode(idx, digits)
			if !cond.eval(digits) {
				continue
			}
			p.afterWhere++
			switch {
			case sampleN > 0:
				passing = append(passing, idx)
			case limitN == 0 || len(p.kept) < limitN:
				if len(p.kept) >= maxJobs/repeat {
					return tooMany()
				}
				p.kept = append(p.kept, idx)
			}
		}
		if p.afterWhere == 0 {
			return []Problem{semantic(RuleSemantic, tagWhereEmpty, "'where' removed every combination", c.Where.Pos)}
		}
		if sampleN > 0 {
			pos := sampleIndices(len(passing), sampleN, sampleSeed)
			if len(pos) > maxJobs/repeat {
				return tooMany()
			}
			p.kept = make([]int, len(pos))
			for i, at := range pos {
				p.kept[i] = passing[at]
			}
		}
	}

	switch {
	case limitN > 0 && limitN >= p.afterWhere:
		p.warn("LimitNotSmaller", "limit "+strconv.Itoa(limitN)+" is not smaller than the "+strconv.Itoa(p.afterWhere)+" available combinations; all are kept", c.Limit.Pos)
	case sampleN > 0 && sampleN >= p.afterWhere:
		p.warn("SampleNotSmaller", "sample "+strconv.Itoa(sampleN)+" is not smaller than the "+strconv.Itoa(p.afterWhere)+" available combinations; all are kept", c.Sample.Pos)
	}
	p.counts = Counts{Combinations: p.total, AfterWhere: p.afterWhere, Kept: len(p.kept), Jobs: len(p.kept) * repeat}
	if p.counts.Jobs > maxJobs {
		return tooMany()
	}
	return nil
}

func (p *plan) warn(code, msg string, at Pos) {
	p.warnings = append(p.warnings, Warning{Code: code, Message: msg, Position: at})
}

func (p *plan) repeat() int {
	if p.cmd.Repeat != nil {
		return p.cmd.Repeat.N
	}
	return 1
}

// ---- spread (SPEC §6.9) ----

// static reports whether jobs get a preferred server when they are created.
func (p *plan) static() bool {
	return p.cmd.Spread != nil && p.cmd.Spread.Mode != SpreadDynamic
}

// weightSequence repeats each target as often as its weight, in listed order.
func (p *plan) weightSequence() []int {
	var seq []int
	for i, t := range p.cmd.Servers {
		for range t.Weight {
			seq = append(seq, i)
		}
	}
	return seq
}

// blockEnds returns, per target, the index after its last job under `blocks`:
// target t gets ⌊N·w_t/W⌋ consecutive jobs and the remaining jobs go one each
// to the first targets in listed order.
func (p *plan) blockEnds(jobs int) []int {
	servers := p.cmd.Servers
	w := 0
	for _, t := range servers {
		w += t.Weight
	}
	sizes := make([]int, len(servers))
	given := 0
	for i, t := range servers {
		sizes[i] = jobs * t.Weight / w
		given += sizes[i]
	}
	for i := 0; given < jobs; i = (i + 1) % len(servers) {
		sizes[i]++
		given++
	}
	ends := make([]int, len(servers))
	acc := 0
	for i, s := range sizes {
		acc += s
		ends[i] = acc
	}
	return ends
}

// assigner returns the preferred-server index of job j, which repeats
// combination idx; digits is scratch space.
func (p *plan) assigner() func(j, idx int, digits []int) int {
	seq := p.weightSequence()
	switch p.cmd.Spread.Mode {
	case SpreadEven:
		return func(j, _ int, _ []int) int { return seq[j%len(seq)] }
	case SpreadBlocks:
		ends := p.blockEnds(p.counts.Jobs)
		return func(j, _ int, _ []int) int {
			return sort.Search(len(ends), func(t int) bool { return j < ends[t] })
		}
	default: // by
		return func(_, idx int, digits []int) int {
			p.decode(idx, digits)
			return seq[digits[p.byTerm]%len(seq)]
		}
	}
}

// assignCounts fills the per-server job counts of a static plan and warns about
// servers that would get no work.
func (p *plan) assignCounts() {
	if !p.static() {
		return
	}
	counts := make([]int, len(p.cmd.Servers))
	assign := p.assigner()
	digits := make([]int, len(p.radix))
	repeat := p.repeat()
	for j := range p.counts.Jobs {
		counts[assign(j, p.kept[j/repeat], digits)]++
	}
	for i, t := range p.cmd.Servers {
		p.perServer = append(p.perServer, ServerCount{Server: t.Server.Text, Jobs: counts[i]})
		if counts[i] == 0 {
			p.warn("ServerWithoutJobs", "server "+strconv.Quote(t.Server.Text)+" would get no jobs", t.Server.Pos)
		}
	}
}

// Preview is the dry run of SPEC §7.2: nothing is created.
type Preview struct {
	Valid    bool      `json:"valid"`
	Problems []Problem `json:"errors"`
	Warnings []Warning `json:"warnings"`
	Counts
	// Dynamic is true when jobs are not pre-assigned; PerServer is then empty.
	Dynamic   bool          `json:"dynamic"`
	PerServer []ServerCount `json:"per_server"`
}

// PreviewOf validates c and reports what adding it would create. Validation
// problems are part of the result, never an error.
func PreviewOf(c *Command, cat Catalogue, opts Options) Preview {
	a := analyse(c, cat, opts)
	if len(a.problems) > 0 {
		return Preview{Problems: a.problems, Warnings: []Warning{}, PerServer: []ServerCount{}}
	}
	p := a.plan
	pv := Preview{
		Valid: true, Problems: []Problem{}, Warnings: append([]Warning{}, p.warnings...),
		Counts: p.counts, Dynamic: !p.static(), PerServer: append([]ServerCount{}, p.perServer...),
	}
	return pv
}

// EnsureSampleSeed stores a seed in the `sample` clause when the user left it
// out, so that re-creating the jobs reproduces the same selection (SPEC §6.7,
// §6.10). It reports whether it set one.
func EnsureSampleSeed(c *Command, newSeed func() uint64) bool {
	if c.Sample == nil || c.Sample.Seed != nil {
		return false
	}
	c.Sample.Seed = &Seed{N: newSeed()}
	return true
}
