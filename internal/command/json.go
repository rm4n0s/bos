package command

import (
	"bytes"
	"encoding/json"
	"io"
	"strconv"
	"strings"

	"github.com/rm4n0s/errors"
)

// ToJSON returns the parsed form of c (SPEC §6.10). Spans are not part of it.
// Absent clauses are omitted; keyed objects (overrides, template and region
// bodies) keep the order in which they were written, because that order
// decides the order of the product.
//
// A command that repeats a key in one of those objects is semantically invalid
// (SPEC §6.8, §6.11) and has no JSON form: ToJSON fails for it.
func ToJSON(c *Command) ([]byte, error) {
	if key, dup := DuplicateKey(c); dup {
		return nil, jsonError("the key " + strconv.Quote(key) + " appears twice in one object")
	}
	b, err := json.Marshal(toDTO(c))
	if err != nil {
		return nil, errors.NewErr("CommandJSONInvalid", err)
	}
	return b, nil
}

// FromJSON reads the parsed form back. Unknown fields are rejected, and the
// restrictions of SPEC §6.3 that the structure can still violate are checked
// again, so a stored or submitted command is held to the same rules as text.
func FromJSON(data []byte) (*Command, error) {
	var dto commandDTO
	if err := decodeStrict(data, &dto); err != nil {
		return nil, jsonError(err.Error())
	}
	c, err := dto.toCommand()
	if err != nil {
		return nil, err
	}
	if err := firstViolation(restrictions(c, Pos{})); err != nil {
		return nil, err
	}
	return c, nil
}

func jsonError(msg string) *errors.Error {
	return errors.New("CommandJSONInvalid", "the parsed command is not valid: "+msg, "rule", RuleSyntax)
}

func decodeStrict(data []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	dec.UseNumber()
	if err := dec.Decode(v); err != nil {
		return err
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return errors.New("CommandJSONInvalid", "unexpected data after the JSON value")
	}
	return nil
}

// ---- DTOs ----

type commandDTO struct {
	Servers    []targetDTO    `json:"servers"`
	Generator  string         `json:"generator_code_name"`
	Selections []selectionDTO `json:"group_selections"`
	Where      *condDTO       `json:"where,omitempty"`
	Limit      *int           `json:"limit,omitempty"`
	Sample     *sampleDTO     `json:"sample,omitempty"`
	Repeat     *int           `json:"repeat,omitempty"`
	Overrides  *overridesDTO  `json:"overrides,omitempty"`
	Spread     *spreadDTO     `json:"spread,omitempty"`
	Fixers     []stepDTO      `json:"fixers,omitempty"`
}

type targetDTO struct {
	Server string `json:"server_code_name"`
	Weight int    `json:"weight"`
}

type sampleDTO struct {
	Count int     `json:"count"`
	Seed  *uint64 `json:"seed,omitempty"`
}

type spreadDTO struct {
	Mode    string  `json:"mode"`
	ByGroup *string `json:"by_group"`
	Pinned  bool    `json:"pinned"`
}

type stepDTO struct {
	Fixer string `json:"fixer_code_name"`
	On    string `json:"on,omitempty"`
}

// selectionDTO is one entry of group_selections. A literal has only `literal`
// and `negative`; every other entry names its group and always lists
// include_only and exclude (null when absent).
type selectionDTO struct {
	Group       string         `json:"group_code_name"`
	IncludeOnly []string       `json:"include_only"`
	Exclude     []string       `json:"exclude"`
	Literal     *string        `json:"literal,omitempty"`
	Negative    bool           `json:"negative,omitempty"`
	IsMerged    bool           `json:"is_merged,omitempty"`
	Merged      []selectionDTO `json:"merged_groups,omitempty"`
	IsZipped    bool           `json:"is_zipped,omitempty"`
	Zipped      []selectionDTO `json:"zipped_groups,omitempty"`
	IsTemplate  bool           `json:"is_template,omitempty"`
	Template    *bindsDTO      `json:"template_group_selections,omitempty"`
	IsRegioned  bool           `json:"is_regioned,omitempty"`
	Region      *bindsDTO      `json:"region_group_selections,omitempty"`
}

// MarshalJSON writes a literal without the group fields.
func (s selectionDTO) MarshalJSON() ([]byte, error) {
	if s.Literal != nil {
		return json.Marshal(struct {
			Literal  string `json:"literal"`
			Negative bool   `json:"negative"`
		}{*s.Literal, s.Negative})
	}
	type plain selectionDTO
	return json.Marshal(plain(s))
}

// bindsDTO is an ordered JSON object: tag or keyword → list of selections.
type bindsDTO struct {
	Keys   []string
	Values [][]selectionDTO
}

func (b bindsDTO) MarshalJSON() ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteByte('{')
	for i, k := range b.Keys {
		if i > 0 {
			buf.WriteByte(',')
		}
		kb, _ := json.Marshal(k)
		buf.Write(kb)
		buf.WriteByte(':')
		vb, err := json.Marshal(b.Values[i])
		if err != nil {
			return nil, err
		}
		buf.Write(vb)
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}

func (b *bindsDTO) UnmarshalJSON(data []byte) error {
	return readOrdered(data, func(key string, raw json.RawMessage) error {
		var sels []selectionDTO
		if err := decodeStrict(raw, &sels); err != nil {
			return err
		}
		b.Keys = append(b.Keys, key)
		b.Values = append(b.Values, sels)
		return nil
	})
}

// overridesDTO is the ordered `with` object. Integers are written as JSON
// integers, decimals always with a fractional part, text as strings.
type overridesDTO struct {
	Keys   []string
	Values []Value
}

func (o overridesDTO) MarshalJSON() ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteByte('{')
	for i, k := range o.Keys {
		if i > 0 {
			buf.WriteByte(',')
		}
		kb, _ := json.Marshal(k)
		buf.Write(kb)
		buf.WriteByte(':')
		v := o.Values[i]
		switch v.Kind {
		case ValueInt:
			buf.WriteString(strconv.FormatUint(v.Int, 10))
		case ValueDecimal:
			buf.WriteString(formatDecimal(v.Float))
		default:
			tb, _ := json.Marshal(v.Text)
			buf.Write(tb)
		}
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}

func (o *overridesDTO) UnmarshalJSON(data []byte) error {
	return readOrdered(data, func(key string, raw json.RawMessage) error {
		v, err := valueFromJSON(raw)
		if err != nil {
			return errors.New("CommandJSONInvalid", "override "+key+": "+err.Error())
		}
		o.Keys = append(o.Keys, key)
		o.Values = append(o.Values, v)
		return nil
	})
}

func valueFromJSON(raw json.RawMessage) (Value, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) > 0 && raw[0] == '"' {
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return Value{}, err
		}
		return Value{Kind: ValueText, Text: s}, nil
	}
	text := string(raw)
	if text == "" || !strings.ContainsAny(text[:1], "0123456789") {
		return Value{}, errors.New("CommandJSONInvalid", "a value must be a non-negative number or a string")
	}
	if strings.ContainsAny(text, ".eE") {
		f, err := strconv.ParseFloat(text, 64)
		if err != nil {
			return Value{}, err
		}
		return Value{Kind: ValueDecimal, Float: f}, nil
	}
	n, err := strconv.ParseUint(text, 10, 64)
	if err != nil {
		return Value{}, err
	}
	return Value{Kind: ValueInt, Int: n}, nil
}

// readOrdered walks a JSON object in source order and rejects duplicate keys.
func readOrdered(data []byte, each func(key string, raw json.RawMessage) error) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		return errors.New("CommandJSONInvalid", "expected a JSON object")
	}
	seen := map[string]bool{}
	for dec.More() {
		kt, err := dec.Token()
		if err != nil {
			return err
		}
		key, _ := kt.(string)
		if seen[key] {
			return errors.New("CommandJSONInvalid", "duplicate key "+strconv.Quote(key))
		}
		seen[key] = true
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return err
		}
		if err := each(key, raw); err != nil {
			return err
		}
	}
	_, err = dec.Token()
	return err
}

// condDTO is a node of the where tree: op is and, or, not, eq, in or tag.
type condDTO struct {
	Op        string     `json:"op"`
	Args      []*condDTO `json:"args,omitempty"`
	Arg       *condDTO   `json:"arg,omitempty"`
	Group     string     `json:"group,omitempty"`
	Selector  string     `json:"selector,omitempty"`
	Selectors []string   `json:"selectors,omitempty"`
	Tag       string     `json:"tag,omitempty"`
}

// ---- Command → DTO ----

func toDTO(c *Command) commandDTO {
	d := commandDTO{Generator: c.Generator.Text, Selections: selectionsToDTO(c.Selections)}
	d.Servers = make([]targetDTO, len(c.Servers))
	for i, t := range c.Servers {
		d.Servers[i] = targetDTO{Server: t.Server.Text, Weight: t.Weight}
	}
	if c.Where != nil {
		d.Where = condToDTO(c.Where)
	}
	if c.Limit != nil {
		n := c.Limit.N
		d.Limit = &n
	}
	if c.Sample != nil {
		s := &sampleDTO{Count: c.Sample.Count.N}
		if c.Sample.Seed != nil {
			seed := c.Sample.Seed.N
			s.Seed = &seed
		}
		d.Sample = s
	}
	if c.Repeat != nil {
		n := c.Repeat.N
		d.Repeat = &n
	}
	if len(c.Overrides) > 0 {
		o := &overridesDTO{}
		for _, ov := range c.Overrides {
			o.Keys = append(o.Keys, ov.Key.Text)
			o.Values = append(o.Values, ov.Value)
		}
		d.Overrides = o
	}
	if c.Spread != nil {
		sp := &spreadDTO{Mode: string(c.Spread.Mode), Pinned: c.Spread.Pinned}
		if c.Spread.Mode == SpreadBy {
			g := c.Spread.ByGroup.Text
			sp.ByGroup = &g
		}
		d.Spread = sp
	}
	for _, f := range c.Fixers {
		st := stepDTO{Fixer: f.Fixer.Text}
		if f.On != nil {
			st.On = f.On.Text
		}
		d.Fixers = append(d.Fixers, st)
	}
	return d
}

func selectionsToDTO(sels []Selection) []selectionDTO {
	out := make([]selectionDTO, len(sels))
	for i, s := range sels {
		out[i] = selectionToDTO(s)
	}
	return out
}

func selectionToDTO(s Selection) selectionDTO {
	if s.Kind == SelLiteral {
		text := s.Text
		return selectionDTO{Literal: &text, Negative: s.Negative}
	}
	d := selectionDTO{Group: groupLabel(s)}
	if s.Filter != nil {
		names := make([]string, len(s.Filter.Selectors))
		for i, sel := range s.Filter.Selectors {
			names[i] = sel.String()
		}
		if s.Filter.Exclude {
			d.Exclude = names
		} else {
			d.IncludeOnly = names
		}
	}
	switch s.Kind {
	case SelMerge:
		d.IsMerged, d.Merged = true, selectionsToDTO(s.Operands)
	case SelZip:
		d.IsZipped, d.Zipped = true, selectionsToDTO(s.Operands)
	case SelTemplate:
		d.IsTemplate, d.Template = true, bindsToDTO(s.Binds)
	case SelRegion:
		d.IsRegioned, d.Region = true, bindsToDTO(s.Binds)
	}
	return d
}

func bindsToDTO(binds []Bind) *bindsDTO {
	b := &bindsDTO{}
	for _, bind := range binds {
		b.Keys = append(b.Keys, bind.Key.Text)
		b.Values = append(b.Values, selectionsToDTO(bind.Body))
	}
	return b
}

func condToDTO(c *Cond) *condDTO {
	switch c.Op {
	case CondAnd, CondOr:
		d := &condDTO{Op: "and"}
		if c.Op == CondOr {
			d.Op = "or"
		}
		for _, a := range c.Args {
			d.Args = append(d.Args, condToDTO(a))
		}
		return d
	case CondNot:
		return &condDTO{Op: "not", Arg: condToDTO(c.Args[0])}
	case CondEq:
		return &condDTO{Op: "eq", Group: c.Group.Text, Selector: c.Selectors[0].String()}
	case CondIn:
		d := &condDTO{Op: "in", Group: c.Group.Text}
		for _, s := range c.Selectors {
			d.Selectors = append(d.Selectors, s.String())
		}
		return d
	default:
		return &condDTO{Op: "tag", Tag: c.Tag.Text}
	}
}

// ---- DTO → Command ----

func (d *commandDTO) toCommand() (*Command, error) {
	c := &Command{Generator: Name{Text: d.Generator}}
	if msg := CheckName(d.Generator); msg != "" {
		return nil, jsonError("generator_code_name: " + msg)
	}
	if len(d.Servers) == 0 {
		return nil, jsonError("servers must list at least one server")
	}
	for _, t := range d.Servers {
		if msg := CheckName(t.Server); msg != "" {
			return nil, jsonError("server_code_name: " + msg)
		}
		c.Servers = append(c.Servers, Target{Server: Name{Text: t.Server}, Weight: t.Weight})
	}
	sels, err := selectionsFromDTO(d.Selections)
	if err != nil {
		return nil, err
	}
	c.Selections = sels
	if d.Where != nil {
		if c.Where, err = condFromDTO(d.Where); err != nil {
			return nil, err
		}
	}
	if d.Limit != nil {
		c.Limit = &Count{N: *d.Limit}
	}
	if d.Sample != nil {
		c.Sample = &Sample{Count: Count{N: d.Sample.Count}}
		if d.Sample.Seed != nil {
			c.Sample.Seed = &Seed{N: *d.Sample.Seed}
		}
	}
	if d.Repeat != nil {
		c.Repeat = &Count{N: *d.Repeat}
	}
	if d.Overrides != nil {
		for i, k := range d.Overrides.Keys {
			if msg := CheckName(k); msg != "" {
				return nil, jsonError("override key: " + msg)
			}
			c.Overrides = append(c.Overrides, Override{Key: Name{Text: k}, Value: d.Overrides.Values[i]})
		}
	}
	if d.Spread != nil {
		if c.Spread, err = spreadFromDTO(d.Spread); err != nil {
			return nil, err
		}
	}
	for _, f := range d.Fixers {
		if msg := CheckName(f.Fixer); msg != "" {
			return nil, jsonError("fixer_code_name: " + msg)
		}
		st := Step{Fixer: Name{Text: f.Fixer}}
		if f.On != "" {
			if msg := CheckName(f.On); msg != "" {
				return nil, jsonError("on: " + msg)
			}
			st.On = &Name{Text: f.On}
		}
		c.Fixers = append(c.Fixers, st)
	}
	return c, nil
}

func spreadFromDTO(d *spreadDTO) (*Spread, error) {
	s := &Spread{Mode: SpreadMode(d.Mode), Pinned: d.Pinned}
	switch s.Mode {
	case SpreadDynamic, SpreadEven, SpreadBlocks:
		if d.ByGroup != nil {
			return nil, jsonError("spread.by_group is only allowed with mode by")
		}
	case SpreadBy:
		if d.ByGroup == nil {
			return nil, jsonError("spread.by_group is required with mode by")
		}
		if msg := CheckName(*d.ByGroup); msg != "" {
			return nil, jsonError("spread.by_group: " + msg)
		}
		s.ByGroup = Name{Text: *d.ByGroup}
	default:
		return nil, jsonError("spread.mode must be dynamic, even, blocks or by")
	}
	return s, nil
}

func selectionsFromDTO(ds []selectionDTO) ([]Selection, error) {
	out := make([]Selection, 0, len(ds))
	for _, d := range ds {
		s, err := selectionFromDTO(d)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, nil
}

func selectionFromDTO(d selectionDTO) (Selection, error) {
	if d.Literal != nil {
		if d.Group != "" || d.IncludeOnly != nil || d.Exclude != nil || d.hasStructure() {
			return Selection{}, jsonError("a literal selection has only literal and negative")
		}
		return Selection{Kind: SelLiteral, Text: *d.Literal, Negative: d.Negative}, nil
	}
	if d.Negative {
		return Selection{}, jsonError("negative is only valid on a literal")
	}
	if boolInt(d.IsMerged)+boolInt(d.IsZipped)+boolInt(d.IsTemplate)+boolInt(d.IsRegioned) > 1 {
		return Selection{}, jsonError("a selection is only one of merged, zipped, template and region")
	}
	if (len(d.Merged) > 0 && !d.IsMerged) || (len(d.Zipped) > 0 && !d.IsZipped) ||
		(d.Template != nil && !d.IsTemplate) || (d.Region != nil && !d.IsRegioned) {
		return Selection{}, jsonError("a list of operands or bodies needs its is_… flag")
	}

	var s Selection
	switch {
	case d.IsMerged || d.IsZipped:
		ops := d.Merged
		s.Kind = SelMerge
		if d.IsZipped {
			ops, s.Kind = d.Zipped, SelZip
		}
		if len(ops) < 2 {
			return Selection{}, jsonError("a merge or zip needs at least two operands")
		}
		if d.IncludeOnly != nil || d.Exclude != nil {
			return Selection{}, jsonError("a merge or zip has no filter of its own")
		}
		var err error
		if s.Operands, err = selectionsFromDTO(ops); err != nil {
			return Selection{}, err
		}
		return s, nil
	case d.IsTemplate:
		s.Kind = SelTemplate
	case d.IsRegioned:
		s.Kind = SelRegion
	default:
		s.Kind = SelGroup
	}

	if msg := CheckName(d.Group); msg != "" {
		return Selection{}, jsonError("group_code_name: " + msg)
	}
	s.Group = Name{Text: d.Group}
	f, err := filterFromDTO(d)
	if err != nil {
		return Selection{}, err
	}
	s.Filter = f
	if s.Kind == SelTemplate || s.Kind == SelRegion {
		binds := d.Template
		if s.Kind == SelRegion {
			binds = d.Region
		}
		if binds == nil || len(binds.Keys) == 0 {
			return Selection{}, jsonError("a template or region needs at least one tag or keyword")
		}
		for i, key := range binds.Keys {
			if msg := CheckName(key); msg != "" {
				return Selection{}, jsonError("tag or keyword: " + msg)
			}
			if len(binds.Values[i]) == 0 {
				return Selection{}, jsonError("the body of " + key + " is empty")
			}
			body, err := selectionsFromDTO(binds.Values[i])
			if err != nil {
				return Selection{}, err
			}
			s.Binds = append(s.Binds, Bind{Key: Name{Text: key}, Body: body})
		}
	}
	return s, nil
}

func (d selectionDTO) hasStructure() bool {
	return d.IsMerged || d.IsZipped || d.IsTemplate || d.IsRegioned
}

func filterFromDTO(d selectionDTO) (*Filter, error) {
	if d.IncludeOnly != nil && d.Exclude != nil {
		return nil, jsonError("a filter is either include_only or exclude, not both")
	}
	list, exclude := d.IncludeOnly, false
	if d.Exclude != nil {
		list, exclude = d.Exclude, true
	}
	if list == nil {
		return nil, nil
	}
	if len(list) == 0 {
		return nil, jsonError("a filter lists at least one selector")
	}
	f := &Filter{Exclude: exclude}
	for _, text := range list {
		sel, err := selectorFromString(text)
		if err != nil {
			return nil, err
		}
		f.Selectors = append(f.Selectors, sel)
	}
	return f, nil
}

// selectorFromString reads a selector as stored: an item, a pattern, or #tag.
func selectorFromString(text string) (Selector, error) {
	switch {
	case strings.HasPrefix(text, "#"):
		if msg := CheckName(text[1:]); msg != "" {
			return Selector{}, jsonError("tag selector " + strconv.Quote(text) + ": " + msg)
		}
		return Selector{Kind: SelTag, Text: text[1:]}, nil
	case strings.ContainsAny(text, "%?"):
		for _, r := range text {
			if !isWordRune(r) && !isWildcard(r) {
				return Selector{}, jsonError("invalid pattern " + strconv.Quote(text))
			}
		}
		return Selector{Kind: SelPattern, Text: text}, nil
	}
	if msg := CheckName(text); msg != "" {
		return Selector{}, jsonError("selector " + strconv.Quote(text) + ": " + msg)
	}
	return Selector{Kind: SelItem, Text: text}, nil
}

func condFromDTO(d *condDTO) (*Cond, error) {
	switch d.Op {
	case "and", "or":
		if len(d.Args) < 2 || d.Arg != nil {
			return nil, jsonError("where: " + d.Op + " needs at least two args")
		}
		c := &Cond{Op: CondAnd}
		if d.Op == "or" {
			c.Op = CondOr
		}
		for _, a := range d.Args {
			if a == nil {
				return nil, jsonError("where: null arg")
			}
			sub, err := condFromDTO(a)
			if err != nil {
				return nil, err
			}
			c.Args = append(c.Args, sub)
		}
		return c, nil
	case "not":
		if d.Arg == nil || len(d.Args) > 0 {
			return nil, jsonError("where: not needs one arg")
		}
		sub, err := condFromDTO(d.Arg)
		if err != nil {
			return nil, err
		}
		return &Cond{Op: CondNot, Args: []*Cond{sub}}, nil
	case "eq":
		if msg := CheckName(d.Group); msg != "" {
			return nil, jsonError("where group: " + msg)
		}
		sel, err := selectorFromString(d.Selector)
		if err != nil {
			return nil, err
		}
		return &Cond{Op: CondEq, Group: Name{Text: d.Group}, Selectors: []Selector{sel}}, nil
	case "in":
		if msg := CheckName(d.Group); msg != "" {
			return nil, jsonError("where group: " + msg)
		}
		if len(d.Selectors) == 0 {
			return nil, jsonError("where: in needs at least one selector")
		}
		c := &Cond{Op: CondIn, Group: Name{Text: d.Group}}
		for _, text := range d.Selectors {
			sel, err := selectorFromString(text)
			if err != nil {
				return nil, err
			}
			c.Selectors = append(c.Selectors, sel)
		}
		return c, nil
	case "tag":
		if msg := CheckName(d.Tag); msg != "" {
			return nil, jsonError("where tag: " + msg)
		}
		return &Cond{Op: CondTag, Tag: Name{Text: d.Tag}}, nil
	}
	return nil, jsonError("where: unknown op " + strconv.Quote(d.Op))
}

// DuplicateKey reports a setting name, template tag or region keyword that is
// used twice in the same `with` clause, template or region. The validator
// reports it as a semantic error; it also keeps ToJSON from writing an object
// with a repeated key.
func DuplicateKey(c *Command) (key string, found bool) {
	seen := map[string]bool{}
	for _, o := range c.Overrides {
		if seen[o.Key.Text] {
			return o.Key.Text, true
		}
		seen[o.Key.Text] = true
	}
	var walk func(sels []Selection) (string, bool)
	walk = func(sels []Selection) (string, bool) {
		for _, s := range sels {
			keys := map[string]bool{}
			for _, b := range s.Binds {
				if keys[b.Key.Text] {
					return b.Key.Text, true
				}
				keys[b.Key.Text] = true
				if k, dup := walk(b.Body); dup {
					return k, true
				}
			}
			if k, dup := walk(s.Operands); dup {
				return k, true
			}
		}
		return "", false
	}
	return walk(c.Selections)
}
