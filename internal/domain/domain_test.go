package domain

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestAttrJSONRoundTrip(t *testing.T) {
	attrs := OutputAttributes{
		"width":        {Kind: AttrInt, Int: 512},
		"seed":         {Kind: AttrInt, Int: 18446744073709551615},
		"cfg":          {Kind: AttrDecimal, Float: 6.5},
		"denoise":      {Kind: AttrDecimal, Float: 1},
		"sampler_name": {Kind: AttrText, Text: "euler"},
	}
	b, err := json.Marshal(attrs)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"cfg":6.5,"denoise":1.0,"sampler_name":"euler","seed":18446744073709551615,"width":512}`
	if string(b) != want {
		t.Errorf("JSON = %s\nwant   %s", b, want)
	}
	var back OutputAttributes
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(back, attrs) {
		t.Errorf("round trip = %+v", back)
	}
}

func TestAttrRejectsBadJSON(t *testing.T) {
	for _, in := range []string{`true`, `null`, `-1`, `[]`, `{}`} {
		var a Attr
		if err := a.UnmarshalJSON([]byte(in)); err == nil {
			t.Errorf("accepted %s as %+v", in, a)
		}
	}
}

func TestPage(t *testing.T) {
	tests := []struct {
		in     Page
		number int
		size   int
		offset int
	}{
		{Page{}, 1, 20, 0},
		{Page{Number: 3, Size: 10}, 3, 10, 20},
		{Page{Number: -2, Size: -1}, 1, 20, 0},
		{Page{Number: 2, Size: 5000}, 2, 200, 200},
	}
	for _, tt := range tests {
		n := tt.in.Normalised()
		if n.Number != tt.number || n.Size != tt.size || tt.in.Offset() != tt.offset {
			t.Errorf("%+v -> %+v offset %d; want %d, %d, %d", tt.in, n, tt.in.Offset(), tt.number, tt.size, tt.offset)
		}
	}
	p := NewPaged[int](nil, 45, Page{Number: 2, Size: 20})
	if p.Items == nil || p.Total != 45 || p.Page != 2 || p.PageSize != 20 || p.TotalPages != 3 {
		t.Errorf("paged = %+v", p)
	}
	if b, _ := json.Marshal(NewPaged[int](nil, 0, Page{})); string(b) != `{"items":[],"total":0,"page":1,"page_size":20,"total_pages":0}` {
		t.Errorf("empty page JSON = %s", b)
	}
}

func TestEnumsValid(t *testing.T) {
	for _, s := range []Status{StatusIdle, StatusQueued, StatusProcessing, StatusError, StatusFinished} {
		if !s.Valid() {
			t.Errorf("%s should be valid", s)
		}
	}
	if Status("done").Valid() || OutputType("gif").Valid() {
		t.Error("invalid values accepted")
	}
	for _, o := range []OutputType{OutputImage, OutputVideo, Output3D, OutputUnknown} {
		if !o.Valid() {
			t.Errorf("%s should be valid", o)
		}
	}
}

func TestItemFiles(t *testing.T) {
	tests := []struct {
		name string
		item Item
		want []string
	}{
		{"nothing", Item{}, []string{}},
		{"everything, in order", Item{
			Thumbnail: "t.png", IPAdapter: &IPAdapter{ImageFile: "ip.png"},
			MaskRegion:  &MaskRegion{ReferencePath: "ref.png", FolderPath: "masks/1"},
			ControlNets: []ControlNet{{ImagePath: "a.png"}, {ImagePath: "b.png"}},
		}, []string{"t.png", "ip.png", "ref.png", "masks/1", "a.png", "b.png"}},
		{"empty and repeated paths are left out", Item{
			Thumbnail: "x.png", IPAdapter: &IPAdapter{ImageFile: "x.png"}, ControlNets: []ControlNet{{ImagePath: ""}},
		}, []string{"x.png"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.item.Files(); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Files() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCommandStatusOf(t *testing.T) {
	tests := []struct {
		name string
		jobs map[Status]int
		want Status
	}{
		{"no jobs", nil, StatusIdle},
		{"all idle", map[Status]int{StatusIdle: 3}, StatusIdle},
		{"all finished", map[Status]int{StatusFinished: 3}, StatusFinished},
		{"finished and idle", map[Status]int{StatusFinished: 2, StatusIdle: 1}, StatusIdle},
		{"one queued", map[Status]int{StatusFinished: 2, StatusQueued: 1}, StatusQueued},
		{"one processing beats queued", map[Status]int{StatusQueued: 5, StatusProcessing: 1}, StatusProcessing},
		{"an error while others still run", map[Status]int{StatusError: 1, StatusProcessing: 1}, StatusProcessing},
		{"an error while others wait", map[Status]int{StatusError: 1, StatusQueued: 1}, StatusQueued},
		{"an error and nothing moving", map[Status]int{StatusError: 1, StatusFinished: 4, StatusIdle: 2}, StatusError},
		{"counts of zero", map[Status]int{StatusProcessing: 0, StatusFinished: 2}, StatusFinished},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := CommandStatusOf(tt.jobs); got != tt.want {
				t.Errorf("CommandStatusOf(%v) = %q, want %q", tt.jobs, got, tt.want)
			}
		})
	}
}
