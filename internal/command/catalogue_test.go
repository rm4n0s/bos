package command

import "strconv"

// fakeCatalogue implements Catalogue in memory.
type fakeCatalogue struct {
	servers    map[string]bool
	fixers     map[string]bool
	generators map[string]Generator
	groups     map[string][]Item
	nextID     int64
}

func newFakeCatalogue() *fakeCatalogue {
	return &fakeCatalogue{
		servers: map[string]bool{}, fixers: map[string]bool{},
		generators: map[string]Generator{}, groups: map[string][]Item{},
	}
}

func (c *fakeCatalogue) HasServer(code string) bool { return c.servers[code] }
func (c *fakeCatalogue) HasFixer(code string) bool  { return c.fixers[code] }
func (c *fakeCatalogue) HasGroup(code string) bool  { _, ok := c.groups[code]; return ok }
func (c *fakeCatalogue) Generator(code string) (Generator, bool) {
	g, ok := c.generators[code]
	return g, ok
}
func (c *fakeCatalogue) Items(group string) []Item { return c.groups[group] }

// group adds a group with the given items; it returns the catalogue to chain.
func (c *fakeCatalogue) group(name string, items ...Item) *fakeCatalogue {
	out := make([]Item, len(items))
	for i, it := range items {
		c.nextID++
		it.ID, it.Group = c.nextID, name
		out[i] = it
	}
	c.groups[name] = out
	return c
}

// words makes one item per code name, with the code name as its positive prompt.
func words(codes ...string) []Item {
	items := make([]Item, len(codes))
	for i, code := range codes {
		items[i] = Item{Code: code, Positive: code}
	}
	return items
}

// sampleCatalogue is the sample data of SPEC §6.12.
func sampleCatalogue() *fakeCatalogue {
	c := newFakeCatalogue()
	c.servers["gpu1"], c.servers["gpu2"], c.servers["gpu3"] = true, true, true
	c.fixers["upscale"], c.fixers["face"] = true, true
	c.generators["g"] = Generator{OutputType: OutputImage}
	c.generators["gr"] = Generator{OutputType: OutputImage, HasRandomSeed: true}
	c.generators["vid"] = Generator{OutputType: OutputVideo}
	c.generators["mesh"] = Generator{OutputType: Output3D}
	c.generators["odd"] = Generator{OutputType: OutputUnknown}

	c.group("characters", words("alice", "bob", "carol")...)
	poses := words("standing", "jumping", "sitting", "running")
	poses[1].Tags, poses[3].Tags = []string{"acrobatic"}, []string{"acrobatic"}
	c.group("poses", poses...)
	c.group("emotions", words("happy", "sad", "cry")...)
	c.group("anime", words("anime1", "anime2")...)
	c.group("scene",
		Item{Code: "park", Positive: "<tmpl:subject> in a park", Negative: "no <tmpl:subject>"},
		Item{Code: "beach", Positive: "<tmpl:subject> on a beach"},
	)
	c.group("duo", Item{
		Code: "duo1", Positive: "two people",
		MaskFiles: []MaskFile{{Keyword: "red", File: "duo1_red.png"}, {Keyword: "blue", File: "duo1_blue.png"}},
	})
	c.group("style", words("style1", "style2")...)
	c.group("empty")
	return c
}

func ids(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = "i" + strconv.Itoa(i+1)
	}
	return out
}
