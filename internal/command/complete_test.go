package command

import "testing"

// splitCursor removes the cursor mark "^" from a command and returns its offset.
func splitCursor(t *testing.T, marked string) (string, int) {
	t.Helper()
	for i := range len(marked) {
		if marked[i] == '^' {
			return marked[:i] + marked[i+1:], i
		}
	}
	t.Fatalf("no cursor in %q", marked)
	return "", 0
}

func TestCompletionAt(t *testing.T) {
	tests := []struct {
		name   string
		marked string
		kind   CompletionKind
		group  string
		prefix string
		// replaced is the text of the word the suggestion replaces.
		replaced string
	}{
		{"nothing typed", "^", CompleteServer, "", "", ""},
		{"first server", "gp^", CompleteServer, "", "gp", "gp"},
		{"after a comma", "gpu1, g^", CompleteServer, "", "g", "g"},
		{"after a comma, nothing yet", "gpu1,^", CompleteServer, "", "", ""},
		{"a finished server expects nothing", "gpu1 ^", CompleteNone, "", "", ""},
		{"a weight", "gpu1=^", CompleteNone, "", "", ""},
		{"generator", "gpu1 -$ an^", CompleteGenerator, "", "an", "an"},
		{"generator, nothing yet", "gpu1 -$ ^", CompleteGenerator, "", "", ""},
		{"first group", "gpu1 -$ anime: ch^", CompleteGroup, "", "ch", "ch"},
		{"group right after the colon", "gpu1 -$ anime:^", CompleteGroup, "", "", ""},
		{"group after a product", "gpu1 -$ anime: characters * po^", CompleteGroup, "", "po", "po"},
		{"group after a merge", "gpu1 -$ anime: characters && po^", CompleteGroup, "", "po", "po"},
		{"group after a zip", "gpu1 -$ anime: characters || po^", CompleteGroup, "", "po", "po"},
		{"line breaks do not matter", "gpu1 -$ anime:\n  characters\n  * po^", CompleteGroup, "", "po", "po"},
		{"item of a group", "gpu1 -$ anime: characters(al^", CompleteItem, "characters", "al", "al"},
		{"second item", "gpu1 -$ anime: characters(alice, b^", CompleteItem, "characters", "b", "b"},
		{"excluded item", "gpu1 -$ anime: characters(~b^", CompleteItem, "characters", "b", "b"},
		{"second excluded item", "gpu1 -$ anime: characters(~alice, ~b^", CompleteItem, "characters", "b", "b"},
		{"item tag", "gpu1 -$ anime: characters(#co^", CompleteTag, "", "co", "co"},
		{"a pattern is left alone", "gpu1 -$ anime: characters(al%^", CompleteNone, "", "", ""},
		{"inside a string", `gpu1 -$ anime: "mast^`, CompleteNone, "", "", ""},
		{"after a closed string", `gpu1 -$ anime: "masterpiece" * po^`, CompleteGroup, "", "po", "po"},
		{"a string holding punctuation", `gpu1 -$ anime: "a (b: c" * po^`, CompleteGroup, "", "po", "po"},
		{"template tag is free", "gpu1 -$ anime: scene[sub^", CompleteNone, "", "", ""},
		{"template body", "gpu1 -$ anime: scene[subject: ch^", CompleteGroup, "", "ch", "ch"},
		{"template body, second term", "gpu1 -$ anime: scene[subject: characters * po^", CompleteGroup, "", "po", "po"},
		{"template body item", "gpu1 -$ anime: scene[subject: characters(al^", CompleteItem, "characters", "al", "al"},
		{"template second bind is free", "gpu1 -$ anime: scene[subject: characters, mo^", CompleteNone, "", "", ""},
		{"after the template", "gpu1 -$ anime: scene[subject: characters] * po^", CompleteGroup, "", "po", "po"},
		{"region keyword is free", "gpu1 -$ anime: duo{re^", CompleteNone, "", "", ""},
		{"region body", "gpu1 -$ anime: duo{red: ch^", CompleteGroup, "", "ch", "ch"},
		{"after the region", "gpu1 -$ anime: duo{red: characters} * st^", CompleteGroup, "", "st", "st"},
		{"where group", "gpu1 -$ anime: characters * poses where po^", CompleteGroup, "", "po", "po"},
		{"where tag", "gpu1 -$ anime: characters where #ac^", CompleteTag, "", "ac", "ac"},
		{"where not tag", "gpu1 -$ anime: characters where not #ac^", CompleteTag, "", "ac", "ac"},
		{"where not group", "gpu1 -$ anime: characters where not po^", CompleteGroup, "", "po", "po"},
		{"where and", "gpu1 -$ anime: characters where #a and po^", CompleteGroup, "", "po", "po"},
		{"where equals item", "gpu1 -$ anime: characters where poses = ru^", CompleteItem, "poses", "ru", "ru"},
		{"where in item", "gpu1 -$ anime: characters where poses in (run, wa^", CompleteItem, "poses", "wa", "wa"},
		{"where grouping parenthesis", "gpu1 -$ anime: characters where not (po^", CompleteGroup, "", "po", "po"},
		{"where tag in a grouping parenthesis", "gpu1 -$ anime: characters where (#a or #b^", CompleteTag, "", "b", "b"},
		{"limit takes a number", "gpu1 -$ anime: characters limit 1^", CompleteNone, "", "", ""},
		{"with takes settings", "gpu1 -$ anime: characters with st^", CompleteNone, "", "", ""},
		{"spread by group", "gpu1 -$ anime: characters spread by ch^", CompleteGroup, "", "ch", "ch"},
		{"spread mode is not a name", "gpu1 -$ anime: characters spread bl^", CompleteNone, "", "", ""},
		{"fixer", "gpu1 -$ anime: characters > up^", CompleteFixer, "", "up", "up"},
		{"fixer, nothing yet", "gpu1 -$ anime: characters >^", CompleteFixer, "", "", ""},
		{"second fixer", "gpu1 -$ anime: characters > upscale > fa^", CompleteFixer, "", "fa", "fa"},
		{"fixer server", "gpu1 -$ anime: characters > upscale on gp^", CompleteServer, "", "gp", "gp"},
		{"after a fixer name", "gpu1 -$ anime: characters > upscale ^", CompleteNone, "", "", ""},
		{"cursor inside a word replaces all of it", "gpu1 -$ anime: ch^aracters", CompleteGroup, "", "ch", "characters"},
		{"cursor before text", "^gpu1 -$ anime: characters", CompleteServer, "", "", "gpu1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, cursor := splitCursor(t, tt.marked)
			got := CompletionAt(code, cursor)
			if got.Kind != tt.kind {
				t.Fatalf("kind = %q, want %q", got.Kind, tt.kind)
			}
			if tt.kind == CompleteNone {
				return
			}
			if got.Group != tt.group || got.Prefix != tt.prefix {
				t.Errorf("group %q prefix %q, want group %q prefix %q", got.Group, got.Prefix, tt.group, tt.prefix)
			}
			if got.Start < 0 || got.End > len(code) || got.Start > cursor || cursor > got.End {
				t.Fatalf("range [%d,%d) does not hold the cursor %d", got.Start, got.End, cursor)
			}
			if word := code[got.Start:got.End]; word != tt.replaced {
				t.Errorf("replaces %q, want %q", word, tt.replaced)
			}
		})
	}
}

func TestCompletionAtCursorOutsideTheText(t *testing.T) {
	for _, cursor := range []int{-5, 99} {
		if got := CompletionAt("gpu1", cursor); got.Kind == CompleteNone {
			t.Errorf("cursor %d: a cursor past the ends is clamped, got kind %q", cursor, got.Kind)
		}
	}
}

// The completion never panics, whatever the text and the cursor.
func FuzzCompletionAt(f *testing.F) {
	for _, s := range []string{"", "gpu1 -$ anime: a(b, ~c) * d[e: f] where #g", `"`, "(((", "}{", "-", "&", "gpu1 -$ x: y > z on"} {
		f.Add(s, 3)
	}
	f.Fuzz(func(t *testing.T, code string, cursor int) {
		got := CompletionAt(code, cursor)
		if got.Kind == CompleteNone {
			return
		}
		if got.Start < 0 || got.Start > got.End || got.End > len(code) {
			t.Fatalf("range [%d,%d) outside %q", got.Start, got.End, code)
		}
	})
}
