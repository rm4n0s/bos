package domain

import (
	"bytes"
	"encoding/json"
	"strconv"
	"strings"
)

// AttrKind says what an output attribute holds.
type AttrKind int

// The kinds of output attribute value.
const (
	AttrInt AttrKind = iota
	AttrDecimal
	AttrText
)

// Attr is one output attribute value (SPEC §3.2): a whole number, a decimal or
// text. It is written as the natural JSON value, so 64-bit seeds keep their
// precision.
type Attr struct {
	Kind  AttrKind
	Int   uint64
	Float float64
	Text  string
}

// MarshalJSON writes an integer as a JSON integer, a decimal always with a
// fractional part, and text as a string.
func (a Attr) MarshalJSON() ([]byte, error) {
	switch a.Kind {
	case AttrInt:
		return []byte(strconv.FormatUint(a.Int, 10)), nil
	case AttrDecimal:
		s := strconv.FormatFloat(a.Float, 'f', -1, 64)
		if !strings.Contains(s, ".") {
			s += ".0"
		}
		return []byte(s), nil
	}
	return json.Marshal(a.Text)
}

// UnmarshalJSON reads what MarshalJSON writes.
func (a *Attr) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if len(data) > 0 && data[0] == '"' {
		a.Kind = AttrText
		return json.Unmarshal(data, &a.Text)
	}
	text := string(data)
	if strings.ContainsAny(text, ".eE") {
		f, err := strconv.ParseFloat(text, 64)
		if err != nil {
			return err
		}
		*a = Attr{Kind: AttrDecimal, Float: f}
		return nil
	}
	n, err := strconv.ParseUint(text, 10, 64)
	if err != nil {
		return err
	}
	*a = Attr{Kind: AttrInt, Int: n}
	return nil
}

// OutputAttributes are the optional output settings of a generator, an item or
// a job: width, height, steps, cfg, sampler_name, … (SPEC §3.2).
type OutputAttributes map[string]Attr
