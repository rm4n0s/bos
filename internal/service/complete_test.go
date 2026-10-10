package service

import (
	"reflect"
	"strings"
	"testing"

	"github.com/rm4n0s/bos/internal/command"
)

func TestCompleteOffersTheNamesThatFit(t *testing.T) {
	e := newEnv(t)
	e.world(t)
	e.group(t, "emotions")

	tests := []struct {
		name   string
		marked string
		kind   command.CompletionKind
		want   []string
	}{
		{"servers by prefix", "gp^", command.CompleteServer, []string{"gpu1", "gpu2"}},
		{"a complete name is not offered again", "gpu2^", command.CompleteServer, []string{}},
		{"nothing matches", "zz^", command.CompleteServer, []string{}},
		{"the prefix is not case sensitive", "GPU1, G^", command.CompleteServer, []string{"gpu1", "gpu2"}},
		{"generators", "gpu1 -$ ^", command.CompleteGenerator, []string{"anime"}},
		{"groups", "gpu1 -$ anime: ^", command.CompleteGroup, []string{"characters", "emotions", "poses"}},
		{"groups by prefix", "gpu1 -$ anime: characters * p^", command.CompleteGroup, []string{"poses"}},
		{"items of the group before the parenthesis", "gpu1 -$ anime: characters(^", command.CompleteItem, []string{"a", "b", "c"}},
		{"items by prefix", "gpu1 -$ anime: poses(p^", command.CompleteItem, []string{"p1", "p2", "p3"}},
		{"items of a group that does not exist", "gpu1 -$ anime: nowhere(^", command.CompleteItem, []string{}},
		{"item tags", "gpu1 -$ anime: characters(#co^", command.CompleteTag, []string{"combat"}},
		{"all item tags", "gpu1 -$ anime: characters where #^", command.CompleteTag, []string{"calm", "combat"}},
		{"fixers", "gpu1 -$ anime: characters > ^", command.CompleteFixer, []string{"upscale"}},
		{"a fixer's server", "gpu1 -$ anime: characters > upscale on g^", command.CompleteServer, []string{"gpu1", "gpu2"}},
		{"where items", "gpu1 -$ anime: characters * poses where poses = p^", command.CompleteItem, []string{"p1", "p2", "p3"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, cursor := cursorIn(t, tt.marked)
			got, err := e.Commands.Complete(ctx(t), code, cursor)
			check(t, err)
			if got.Kind != tt.kind {
				t.Fatalf("kind = %q, want %q", got.Kind, tt.kind)
			}
			if tt.kind == command.CompleteNone {
				return
			}
			if !reflect.DeepEqual(got.Names, tt.want) {
				t.Errorf("names = %v, want %v", got.Names, tt.want)
			}
			if got.More {
				t.Error("More = true for a short list")
			}
		})
	}
}

func cursorIn(t *testing.T, marked string) (string, int) {
	t.Helper()
	i := strings.IndexByte(marked, '^')
	if i < 0 {
		t.Fatalf("no cursor in %q", marked)
	}
	return marked[:i] + marked[i+1:], i
}

func TestCompleteKeepsTheSpanOfTheWord(t *testing.T) {
	e := newEnv(t)
	e.world(t)
	code, cursor := cursorIn(t, "gpu1 -$ anime: ch^aracters * poses")
	got, err := e.Commands.Complete(ctx(t), code, cursor)
	check(t, err)
	if code[got.Start:got.End] != "characters" || !reflect.DeepEqual(got.Names, []string{"characters"}) {
		t.Errorf("completion = %+v", got)
	}
}

func TestCompleteLimitsTheList(t *testing.T) {
	e := newEnv(t)
	e.world(t)
	for i := range completeLimit + 3 {
		e.server(t, "srv"+string(rune('a'+i)))
	}
	got, err := e.Commands.Complete(ctx(t), "srv", 3)
	check(t, err)
	if len(got.Names) != completeLimit || !got.More {
		t.Errorf("names = %d, More = %v", len(got.Names), got.More)
	}
}

func TestCompleteNeverFailsOnBadText(t *testing.T) {
	e := newEnv(t)
	e.world(t)
	for _, code := range []string{"", "(((", `"`, "gpu1 -$ anime: characters(", "@@ -$"} {
		if _, err := e.Commands.Complete(ctx(t), code, len(code)); err != nil {
			t.Errorf("%q: %v", code, err)
		}
	}
}
