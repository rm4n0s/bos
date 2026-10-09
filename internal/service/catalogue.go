package service

import (
	"context"
	"slices"
	"sort"
	"strings"

	"github.com/rm4n0s/bos/internal/command"
	"github.com/rm4n0s/bos/internal/domain"
	"github.com/rm4n0s/errors"
)

// catalogue is the command language's read-only view of the stored data
// (SPEC §14.3, rule 6). It reads an entity the first time the command asks for
// it and remembers it, so a command touches only what it names. The command
// package's interface has no error return, so the first read that fails is
// kept in err; the caller checks it before trusting any result.
type catalogue struct {
	ctx context.Context
	r   Repos
	err error

	servers    map[string]*domain.Server
	fixers     map[string]*domain.Fixer
	generators map[string]*domain.Generator
	groups     map[string]*groupEntry
}

type groupEntry struct {
	group *domain.Group
	items []domain.Item
}

func newCatalogue(ctx context.Context, r Repos) *catalogue {
	return &catalogue{
		ctx: ctx, r: r,
		servers: map[string]*domain.Server{}, fixers: map[string]*domain.Fixer{},
		generators: map[string]*domain.Generator{}, groups: map[string]*groupEntry{},
	}
}

// lookup returns the cached entity or reads it; a missing entity is cached as
// nil, any other failure is recorded.
func lookup[T any](c *catalogue, cache map[string]*T, code string, read func() (*T, error), notFoundTag string) *T {
	if v, ok := cache[code]; ok {
		return v
	}
	v, err := read()
	if err != nil {
		if appErr, ok := errors.FromError(err); !ok || appErr.Tag != notFoundTag {
			if c.err == nil {
				c.err = err
			}
			return nil // not cached: the failure may be temporary
		}
		v = nil
	}
	cache[code] = v
	return v
}

func (c *catalogue) server(code string) *domain.Server {
	return lookup(c, c.servers, code, func() (*domain.Server, error) { return c.r.Servers().GetByCode(c.ctx, code) }, "ServerNotFound")
}

func (c *catalogue) fixer(code string) *domain.Fixer {
	return lookup(c, c.fixers, code, func() (*domain.Fixer, error) { return c.r.Fixers().GetByCode(c.ctx, code) }, "FixerNotFound")
}

func (c *catalogue) generator(code string) *domain.Generator {
	return lookup(c, c.generators, code, func() (*domain.Generator, error) { return c.r.Generators().GetByCode(c.ctx, code) }, "GeneratorNotFound")
}

func (c *catalogue) group(code string) *groupEntry {
	return lookup(c, c.groups, code, func() (*groupEntry, error) {
		g, err := c.r.Groups().GetByCode(c.ctx, code)
		if err != nil {
			return nil, err
		}
		items, err := c.r.Items().AllByGroup(c.ctx, g.ID)
		if err != nil {
			return nil, err
		}
		return &groupEntry{group: g, items: items}, nil
	}, "GroupNotFound")
}

// ---- command.Catalogue ----

func (c *catalogue) HasServer(code string) bool { return c.server(code) != nil }
func (c *catalogue) HasFixer(code string) bool  { return c.fixer(code) != nil }
func (c *catalogue) HasGroup(code string) bool  { return c.group(code) != nil }

func (c *catalogue) Generator(code string) (command.Generator, bool) {
	g := c.generator(code)
	if g == nil {
		return command.Generator{}, false
	}
	return command.Generator{OutputType: string(g.OutputType), HasRandomSeed: g.HasRandomSeed, Attrs: valuesOf(g.OutputAttributes)}, true
}

func (c *catalogue) Items(group string) []command.Item {
	g := c.group(group)
	if g == nil {
		return nil
	}
	out := make([]command.Item, len(g.items))
	for i := range g.items {
		out[i] = itemOf(group, &g.items[i])
	}
	return out
}

// ---- conversions ----

func valuesOf(a domain.OutputAttributes) map[string]command.Value {
	out := make(map[string]command.Value, len(a))
	for k, v := range a {
		out[k] = valueOf(v)
	}
	return out
}

func valueOf(a domain.Attr) command.Value {
	switch a.Kind {
	case domain.AttrInt:
		return command.Value{Kind: command.ValueInt, Int: a.Int}
	case domain.AttrDecimal:
		return command.Value{Kind: command.ValueDecimal, Float: a.Float}
	}
	return command.Value{Kind: command.ValueText, Text: a.Text}
}

func attrOf(v command.Value) domain.Attr {
	switch v.Kind {
	case command.ValueInt:
		return domain.Attr{Kind: domain.AttrInt, Int: v.Int}
	case command.ValueDecimal:
		return domain.Attr{Kind: domain.AttrDecimal, Float: v.Float}
	}
	return domain.Attr{Kind: domain.AttrText, Text: v.Text}
}

func attributesOf(a map[string]command.Value) domain.OutputAttributes {
	out := make(domain.OutputAttributes, len(a))
	for k, v := range a {
		out[k] = attrOf(v)
	}
	return out
}

// itemOf maps a stored item to the view the command language reads. An item
// overrides the generator's output only when it defines both a type and
// attributes (SPEC §7.1).
func itemOf(group string, it *domain.Item) command.Item {
	out := command.Item{
		ID: it.ID, Group: group, Code: it.CodeName, Tags: it.Tags,
		Positive: it.PositivePrompt, Negative: it.NegativePrompt,
	}
	for _, l := range it.LoRAs {
		out.LoRAs = append(out.LoRAs, command.LoRA{Name: l.Name, StrengthModel: l.StrengthModel, StrengthClip: l.StrengthClip})
	}
	for _, c := range it.ControlNets {
		out.ControlNets = append(out.ControlNets, command.ControlNet{
			Type: c.Type, ImagePath: c.ImagePath, IsReference: c.IsReference, ModelPattern: c.ModelPattern, Strength: c.Strength,
		})
	}
	if ip := it.IPAdapter; ip != nil {
		out.IPAdapters = []command.IPAdapter{{
			ImageFile: ip.ImageFile, Weight: ip.Weight, WeightType: ip.WeightType, StartAt: ip.StartAt, EndAt: ip.EndAt,
			ClipVisionModel: ip.ClipVisionModel, ModelName: ip.ModelName,
		}}
	}
	if m := it.MaskRegion; m != nil {
		for _, k := range sortedKeys(m.MaskFiles) {
			out.MaskFiles = append(out.MaskFiles, command.MaskFile{Keyword: k, File: m.MaskFiles[k]})
		}
	}
	for _, r := range it.CoordinatedRegions {
		out.Coordinates = append(out.Coordinates, command.Coordinates{Keyword: r.Keyword, Width: r.Width, Height: r.Height, X: r.X, Y: r.Y})
	}
	if it.OutputType != "" && len(it.OutputTypeAttributes) > 0 {
		out.Output = &command.OutputSpec{Type: string(it.OutputType), Attrs: valuesOf(it.OutputTypeAttributes)}
	}
	return out
}

// ---- LoRA names ----

// loraFile is the file name ComfyUI knows a LoRA by: a name without an
// extension gets .safetensors, as the injector does (SPEC §8.2).
func loraFile(name string) string {
	name = strings.ReplaceAll(name, `\`, "/")
	if i := strings.LastIndex(name, "."); i < 0 || strings.Contains(name[i:], "/") {
		name += ".safetensors"
	}
	return name
}

// missingLoRAs returns the names, sorted, of the used LoRAs that the installed
// list does not contain.
func missingLoRAs(used []string, installed []string) []string {
	have := make(map[string]bool, len(installed))
	for _, n := range installed {
		have[strings.ReplaceAll(n, `\`, "/")] = true
	}
	var missing []string
	for _, n := range used {
		if !have[loraFile(n)] && !slices.Contains(missing, n) {
			missing = append(missing, n)
		}
	}
	sort.Strings(missing)
	return missing
}
