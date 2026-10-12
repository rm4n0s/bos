package command

// NamePart identifies one chosen item for building the result file name
// (SPEC §7.1): a literal has an empty Group and the code `lit<k>`.
type NamePart struct {
	Group string
	Code  string
}

// Region is one region prompt of a job with the mask or rectangle of the
// chosen base item (SPEC §6.6, §8.2). A keyword the base item does not define
// has neither; the region injector rejects it.
type Region struct {
	Keyword     string
	MaskFile    string
	Coordinates *Coordinates
	Prompt      string
	LoRAs       []LoRA
}

// Job is the plan of one job (SPEC §7.1). Everything in it is a copy, so later
// edits of the catalogue do not change it.
type Job struct {
	// Index is the position in job order, from 0.
	Index int
	// Combination is the index of the combination in the full product.
	Combination int
	// Repeat is the repeat index r, from 0.
	Repeat int
	// RegionIndex is the index of the region-prompt set, or -1 without a region.
	RegionIndex int

	Items []ItemRef
	Parts []NamePart

	Positive    string
	Negative    string
	LoRAs       []LoRA
	ControlNets []ControlNet
	IPAdapters  []IPAdapter
	Regions     []Region

	// Output is the output override of the last item that defines one.
	Output *OutputSpec
	// Seed is set when the command has `with seed=S`: S + r (SPEC §6.8). A
	// random seed is chosen when the job runs, not here.
	Seed *uint64

	// Preferred is the server a static spread assigned; empty under dynamic.
	Preferred string
	Pinned    bool
}

// FixerStep is one fixer of a job with its optional explicit server.
type FixerStep struct {
	Fixer string
	On    string
}

// Expansion is the result of expanding a valid command.
type Expansion struct {
	Counts
	Generator Generator
	// Settings are the validated `with` values, text settings normalised.
	Settings  map[string]Value
	Fixers    []FixerStep
	Jobs      []Job
	Warnings  []Warning
	Dynamic   bool
	PerServer []ServerCount
}

// Expand validates c and creates the plan of its jobs: the product of the
// terms in order, then where, limit or sample, repeat, and the preferred
// servers of a static spread. When c is invalid it returns a
// CommandValidationFailed error that lists every problem.
func Expand(c *Command, cat Catalogue, opts Options) (*Expansion, error) {
	a := analyse(c, cat, opts)
	if err := validationError(a.problems); err != nil {
		return nil, err
	}
	p := a.plan
	x := &Expansion{
		Counts: p.counts, Generator: p.gen, Warnings: p.warnings,
		Dynamic: !p.static(), PerServer: p.perServer,
		Settings: map[string]Value{},
	}
	valid := settingsFor(p.gen.OutputType)
	for _, o := range c.Overrides {
		x.Settings[o.Key.Text] = settingValue(valid[o.Key.Text], o.Value)
	}
	for _, f := range c.Fixers {
		step := FixerStep{Fixer: f.Fixer.Text}
		if f.On != nil {
			step.On = f.On.Text
		}
		x.Fixers = append(x.Fixers, step)
	}
	x.Jobs = p.jobs()
	return x, nil
}

func (p *plan) jobs() []Job {
	jobs := make([]Job, 0, p.counts.Jobs)
	digits := make([]int, len(p.radix))
	repeat := p.repeat()

	var assign func(j, idx int, digits []int) int
	if p.static() {
		assign = p.assigner()
	}
	var seed *uint64
	for _, o := range p.cmd.Overrides {
		if o.Key.Text == "seed" && o.Value.Kind == ValueInt {
			s := o.Value.Int
			seed = &s
		}
	}

	for k, idx := range p.kept {
		p.decode(idx, digits)
		base := p.combine(digits)
		for r := range repeat {
			j := base
			j.Index = len(jobs)
			j.Combination = idx
			j.Repeat = r
			if seed != nil {
				s := *seed + uint64(r)
				j.Seed = &s
			}
			if assign != nil {
				j.Preferred = p.cmd.Servers[assign(k*repeat+r, idx, digits)].Server.Text
				j.Pinned = p.cmd.Spread.Pinned
			}
			jobs = append(jobs, j)
		}
	}
	return jobs
}

// combine builds the job for one combination: prompts joined in term order,
// attachments concatenated, the region prompts attached (SPEC §6.4, §7.1).
func (p *plan) combine(digits []int) Job {
	j := Job{RegionIndex: -1}
	var pos, neg []string
	var baseItem *Item
	for t, term := range p.terms {
		it := term[digits[t]]
		pos = append(pos, it.positive)
		neg = append(neg, it.negative)
		j.LoRAs = dedupeLoRAs(append(j.LoRAs, it.loras...))
		j.ControlNets = append(j.ControlNets, it.controlNets...)
		j.IPAdapters = append(j.IPAdapters, it.ipAdapters...)
		j.Parts = append(j.Parts, NamePart{Group: it.group, Code: it.code})
		for _, m := range it.members {
			j.Items = append(j.Items, ItemRef{ID: m.ID, Group: m.Group, Code: m.Code})
		}
		if it.output != nil {
			j.Output = it.output
		}
		if t == p.regionAt {
			baseItem = it.base
		}
	}
	j.Positive = removePlaceholders(joinPrompts(pos...))
	j.Negative = removePlaceholders(joinPrompts(neg...))

	if p.regionAt >= 0 {
		j.RegionIndex = digits[len(digits)-1]
		for _, rp := range p.sets[j.RegionIndex] {
			j.Regions = append(j.Regions, regionOf(rp, baseItem))
			for _, m := range rp.members {
				j.Items = append(j.Items, ItemRef{ID: m.ID, Group: m.Group, Code: m.Code})
			}
		}
	}
	return j
}

func regionOf(rp regionPrompt, base *Item) Region {
	r := Region{Keyword: rp.keyword, Prompt: removePlaceholders(rp.prompt), LoRAs: dedupeLoRAs(appendLoRAs(rp.loras))}
	if base == nil {
		return r
	}
	for _, m := range base.MaskFiles {
		if m.Keyword == rp.keyword {
			r.MaskFile = m.File
		}
	}
	for i := range base.Coordinates {
		if base.Coordinates[i].Keyword == rp.keyword {
			c := base.Coordinates[i]
			r.Coordinates = &c
		}
	}
	return r
}

// EffectiveOutput resolves the output type and attributes of a job: the
// generator's, overridden by the item's, overridden by the command's `with`
// (SPEC §6.8). A setting that the resulting output type does not support is
// ignored for that job.
func (x *Expansion) EffectiveOutput(j *Job) OutputSpec {
	out := OutputSpec{Type: x.Generator.OutputType, Attrs: map[string]Value{}}
	for k, v := range x.Generator.Attrs {
		out.Attrs[k] = v
	}
	if j.Output != nil {
		if j.Output.Type != out.Type {
			out.Type, out.Attrs = j.Output.Type, map[string]Value{}
		}
		for k, v := range j.Output.Attrs {
			out.Attrs[k] = v
		}
	}
	valid := settingsFor(out.Type)
	for k, v := range x.Settings {
		if _, ok := valid[k]; ok {
			out.Attrs[k] = v
		}
	}
	if j.Seed != nil {
		if _, ok := valid["seed"]; ok {
			out.Attrs["seed"] = Value{Kind: ValueInt, Int: *j.Seed}
		}
	}
	return out
}

// String returns the job's items as `group(item)` for diagnostics.
func (r ItemRef) String() string { return r.Group + "(" + r.Code + ")" }
