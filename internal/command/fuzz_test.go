package command

import (
	"testing"
)

// FuzzParse checks that the parser never panics and that a valid command is
// stable under parse → format → parse and under JSON round trips.
func FuzzParse(f *testing.F) {
	for _, tt := range validCommands {
		f.Add(tt.in)
	}
	for _, tt := range invalidCommands {
		f.Add(tt.in)
	}
	f.Add(specCommand)
	f.Add("")
	f.Add("\xff\x00\"")
	f.Add(`s -$ g: "unterminated`)

	f.Fuzz(func(t *testing.T, text string) {
		c, err := Parse(text)
		if err != nil {
			if _, ok := ProblemOf(err); !ok {
				t.Fatalf("Parse(%q) returned an error without a problem: %v", text, err)
			}
			return
		}
		canonical := Format(c)
		again, err := Parse(canonical)
		if err != nil {
			t.Fatalf("Format(%q) = %q does not parse: %v", text, canonical, err)
		}
		if got := Format(again); got != canonical {
			t.Fatalf("format is not stable for %q:\nfirst  %q\nsecond %q", text, canonical, got)
		}
		j1, err := ToJSON(c)
		if _, dup := DuplicateKey(c); dup {
			if err == nil {
				t.Fatalf("ToJSON(%q) accepted a repeated key", text)
			}
			return
		}
		if err != nil {
			t.Fatalf("ToJSON(%q): %v", text, err)
		}
		j2, err := ToJSON(again)
		if err != nil {
			t.Fatal(err)
		}
		if string(j1) != string(j2) {
			t.Fatalf("parse → format → parse changed the parsed form of %q:\n%s\n%s", text, j1, j2)
		}
		back, err := FromJSON(j1)
		if err != nil {
			t.Fatalf("FromJSON(%s) for %q: %v", j1, text, err)
		}
		j3, err := ToJSON(back)
		if err != nil {
			t.Fatal(err)
		}
		if string(j1) != string(j3) {
			t.Fatalf("JSON round trip changed %q:\n%s\n%s", text, j1, j3)
		}
		if Format(back) != canonical {
			t.Fatalf("FromJSON changed the text of %q: %q vs %q", text, Format(back), canonical)
		}
	})
}
