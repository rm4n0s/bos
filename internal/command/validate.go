package command

import (
	"sort"
	"strconv"

	"github.com/rm4n0s/errors"
)

// Tags of the semantic problems (SPEC §17.2). Each is the tag of one kind of
// entry in the metadata of a CommandValidationFailed error.
const (
	tagServerNotFound    = "ServerNotFound"
	tagGeneratorNotFound = "GeneratorNotFound"
	tagFixerNotFound     = "FixerNotFound"
	tagGroupNotFound     = "GroupNotFound"
	tagItemNotFound      = "ItemNotFound"
	tagRegionMismatch    = "CommandRegionMismatch"
	tagWhereGroupMissing = "CommandWhereGroupMissing"
	tagSettingInvalid    = "CommandSettingInvalid"
	tagKeyDuplicated     = "CommandKeyDuplicated"
	tagSelectionEmpty    = "CommandSelectionEmpty"
	tagWhereEmpty        = "CommandWhereEmpty"
	tagTooLarge          = "CommandTooLarge"
)

// Validate performs the semantic checks of SPEC §6.11 on a command that
// parsed. It returns nil, or one CommandValidationFailed error whose metadata
// "errors" lists every problem found (they are collected, not first-fail).
func Validate(c *Command, cat Catalogue, opts Options) error {
	ev := analyse(c, cat, opts)
	return validationError(ev.problems)
}

func validationError(problems []Problem) error {
	if len(problems) == 0 {
		return nil
	}
	return errors.New("CommandValidationFailed", "command is not valid", "errors", problems)
}

// checker runs the checks that need only the catalogue, not evaluation.
type checker struct {
	cat      Catalogue
	cmd      *Command
	gen      Generator
	genFound bool
	problems []Problem
	// selected are the groups that occur in the selections at any depth.
	selected map[string]bool
	items    map[string][]Item
}

func newChecker(c *Command, cat Catalogue) *checker {
	return &checker{cat: cat, cmd: c, selected: map[string]bool{}, items: map[string][]Item{}}
}

func (k *checker) add(rule, tag, msg string, at Pos) {
	k.problems = append(k.problems, semantic(rule, tag, msg, at))
}

func (k *checker) groupItems(group string) []Item {
	if its, ok := k.items[group]; ok {
		return its
	}
	its := k.cat.Items(group)
	k.items[group] = its
	return its
}

// check runs every catalogue check in the order of SPEC §6.11.
func (k *checker) check() {
	c := k.cmd
	for _, t := range c.Servers {
		if !k.cat.HasServer(t.Server.Text) {
			k.add(RuleSemantic, tagServerNotFound, "server "+strconv.Quote(t.Server.Text)+" does not exist", t.Server.Pos)
		}
	}
	k.gen, k.genFound = k.cat.Generator(c.Generator.Text)
	if !k.genFound {
		k.add(RuleSemantic, tagGeneratorNotFound, "generator "+strconv.Quote(c.Generator.Text)+" does not exist", c.Generator.Pos)
	}
	for _, f := range c.Fixers {
		if !k.cat.HasFixer(f.Fixer.Text) {
			k.add(RuleSemantic, tagFixerNotFound, "fixer "+strconv.Quote(f.Fixer.Text)+" does not exist", f.Fixer.Pos)
		}
		if f.On != nil && !k.cat.HasServer(f.On.Text) {
			k.add(RuleSemantic, tagServerNotFound, "server "+strconv.Quote(f.On.Text)+" does not exist", f.On.Pos)
		}
	}

	k.checkSelections(c.Selections)
	if c.Where != nil {
		k.checkCond(c.Where)
	}
	if c.Spread != nil && c.Spread.Mode == SpreadBy && !k.cat.HasGroup(c.Spread.ByGroup.Text) {
		k.add(RuleSemantic, tagGroupNotFound, "group "+strconv.Quote(c.Spread.ByGroup.Text)+" does not exist", c.Spread.ByGroup.Pos)
	}
	k.checkSettings()
	k.checkRepeat()
}

func (k *checker) checkSelections(sels []Selection) {
	for _, s := range sels {
		switch s.Kind {
		case SelLiteral:
		case SelMerge, SelZip:
			k.checkSelections(s.Operands)
		default:
			k.checkGroupTerm(s)
		}
	}
}

func (k *checker) checkGroupTerm(s Selection) {
	g := s.Group.Text
	k.selected[g] = true
	if !k.cat.HasGroup(g) {
		k.add(RuleSemantic, tagGroupNotFound, "group "+strconv.Quote(g)+" does not exist", s.Group.Pos)
	} else if s.Filter != nil {
		k.checkSelectors(g, s.Filter.Selectors)
	}
	seen := map[string]bool{}
	for _, b := range s.Binds {
		if seen[b.Key.Text] {
			k.add(RuleSemantic, tagKeyDuplicated, "the key "+strconv.Quote(b.Key.Text)+" is used twice in "+strconv.Quote(g), b.Key.Pos)
		}
		seen[b.Key.Text] = true
		k.checkSelections(b.Body)
	}
	if s.Kind == SelRegion && k.cat.HasGroup(g) {
		k.checkRegionKeywords(s)
	}
}

// checkSelectors reports every selector that matches no item of the group.
func (k *checker) checkSelectors(group string, sels []Selector) {
	items := k.groupItems(group)
	for _, sel := range sels {
		found := false
		for i := range items {
			if sel.matches(&items[i]) {
				found = true
				break
			}
		}
		if !found {
			k.add(RuleSemantic, tagItemNotFound, describeUnmatched(group, sel), sel.Pos)
		}
	}
}

func describeUnmatched(group string, sel Selector) string {
	switch sel.Kind {
	case SelTag:
		return "no item of group " + strconv.Quote(group) + " has the tag #" + sel.Text
	case SelPattern:
		return "the pattern " + strconv.Quote(sel.Text) + " matches no item of group " + strconv.Quote(group)
	default:
		return "group " + strconv.Quote(group) + " has no item " + strconv.Quote(sel.Text)
	}
}

// checkRegionKeywords compares the keywords of the command with those the base
// group's items define (SPEC §6.11) and reports both directions.
func (k *checker) checkRegionKeywords(s Selection) {
	defined := map[string]bool{}
	var order []string
	items := k.groupItems(s.Group.Text)
	for i := range items {
		for _, kw := range items[i].regionKeywords() {
			if !defined[kw] {
				defined[kw] = true
				order = append(order, kw)
			}
		}
	}
	used := map[string]bool{}
	for _, b := range s.Binds {
		used[b.Key.Text] = true
		if !defined[b.Key.Text] {
			k.add(RuleSemantic, tagRegionMismatch, "region keyword "+strconv.Quote(b.Key.Text)+" is missing in group "+strconv.Quote(s.Group.Text), b.Key.Pos)
		}
	}
	for _, kw := range order {
		if !used[kw] {
			k.add(RuleSemantic, tagRegionMismatch, "region keyword "+strconv.Quote(kw)+" of group "+strconv.Quote(s.Group.Text)+" is missing in the command", s.Group.Pos)
		}
	}
}

func (k *checker) checkCond(c *Cond) {
	switch c.Op {
	case CondAnd, CondOr, CondNot:
		for _, a := range c.Args {
			k.checkCond(a)
		}
	case CondEq, CondIn:
		g := c.Group.Text
		switch {
		case !k.cat.HasGroup(g):
			k.add(RuleSemantic, tagGroupNotFound, "group "+strconv.Quote(g)+" does not exist", c.Group.Pos)
		default:
			k.checkSelectors(g, c.Selectors)
		}
		if !k.selected[g] {
			k.add(RuleSemantic, tagWhereGroupMissing, "group "+strconv.Quote(g)+" is used in 'where' but is not part of the command's selections", c.Group.Pos)
		}
	}
}

func (k *checker) checkRepeat() {
	c := k.cmd
	if c.Repeat == nil || c.Repeat.N <= 1 || !k.genFound {
		return
	}
	hasSeed := false
	for _, o := range c.Overrides {
		if o.Key.Text == "seed" {
			hasSeed = true
		}
	}
	if !hasSeed && !k.gen.HasRandomSeed {
		k.add(RuleR1, "CommandRestrictionViolated",
			"'repeat' needs seed variation: add 'with seed=…' or use a generator with a random seed", c.Repeat.Pos)
	}
}

// ---- 'with' settings (SPEC §6.8) ----

type settingKind int

const (
	setPosInt settingKind = iota // integer ≥ 1
	setSeed                      // integer ≥ 0
	setNumber                    // integer or decimal ≥ 0
	setPosNum                    // integer or decimal > 0
	setUnit                      // integer or decimal in 0…1
	setText                      // WORD or STRING
)

var imageSettings = map[string]settingKind{
	"width": setPosInt, "height": setPosInt, "steps": setPosInt, "batch_size": setPosInt, "seed": setSeed,
	"cfg": setNumber, "denoise": setUnit,
	"sampler_name": setText, "scheduler": setText, "file_type": setText,
}

var videoSettings = map[string]settingKind{
	"width": setPosInt, "height": setPosInt, "num_frames": setPosInt,
	"fps": setPosNum, "duration_seconds": setPosNum,
	"file_type": setText,
}

var model3DSettings = map[string]settingKind{"file_type": setText}

// settingsFor returns the keys valid for an output type.
func settingsFor(outputType string) map[string]settingKind {
	switch outputType {
	case OutputImage:
		return imageSettings
	case OutputVideo:
		return videoSettings
	case Output3D:
		return model3DSettings
	}
	return nil
}

func (k *checker) checkSettings() {
	seen := map[string]bool{}
	valid := settingsFor(k.gen.OutputType)
	for _, o := range k.cmd.Overrides {
		key := o.Key.Text
		if seen[key] {
			k.add(RuleSemantic, tagKeyDuplicated, "the setting "+strconv.Quote(key)+" is given twice", o.Key.Pos)
			continue
		}
		seen[key] = true
		if !k.genFound {
			continue
		}
		kind, ok := valid[key]
		if !ok {
			k.add(RuleSemantic, tagSettingInvalid, "the setting "+strconv.Quote(key)+" is not valid for the "+k.gen.OutputType+" generator "+strconv.Quote(k.cmd.Generator.Text)+validKeys(valid), o.Key.Pos)
			continue
		}
		if msg := checkSettingValue(kind, o.Value); msg != "" {
			k.add(RuleSemantic, tagSettingInvalid, "the setting "+strconv.Quote(key)+" "+msg, o.Value.Pos)
		}
	}
}

func validKeys(valid map[string]settingKind) string {
	if len(valid) == 0 {
		return " (it accepts no settings)"
	}
	keys := make([]string, 0, len(valid))
	for k := range valid {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := " (valid: "
	for i, k := range keys {
		if i > 0 {
			out += ", "
		}
		out += k
	}
	return out + ")"
}

// numeric returns the value of an integer or decimal, and whether v is one.
func numeric(v Value) (float64, bool) {
	switch v.Kind {
	case ValueInt:
		return float64(v.Int), true
	case ValueDecimal:
		return v.Float, true
	}
	return 0, false
}

const maxSettingInt = 1<<31 - 1

func checkSettingValue(kind settingKind, v Value) string {
	switch kind {
	case setPosInt:
		switch {
		case v.Kind != ValueInt:
			return "needs a whole number"
		case v.Int < 1 || v.Int > maxSettingInt:
			return "must be between 1 and " + strconv.Itoa(maxSettingInt)
		}
	case setSeed:
		if v.Kind != ValueInt {
			return "needs a whole number"
		}
	case setNumber, setPosNum, setUnit:
		f, ok := numeric(v)
		switch {
		case !ok:
			return "needs a number"
		case kind == setPosNum && f <= 0:
			return "must be greater than 0"
		case kind == setUnit && (f < 0 || f > 1):
			return "must be between 0 and 1"
		}
	case setText:
		if v.Kind == ValueDecimal {
			return "needs a word or a string"
		}
	}
	return ""
}

// settingValue normalises a validated value: a whole number given for a text
// setting becomes its digits as text.
func settingValue(kind settingKind, v Value) Value {
	if kind == setText && v.Kind == ValueInt {
		return Value{Kind: ValueText, Text: strconv.FormatUint(v.Int, 10), Pos: v.Pos}
	}
	return v
}
