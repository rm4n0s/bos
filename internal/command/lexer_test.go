package command

import (
	"strings"
	"testing"

	"github.com/rm4n0s/errors"
)

var kindNames = map[TokKind]string{
	TokEOF: "EOF", TokWord: "WORD", TokPattern: "PATTERN", TokDecimal: "DECIMAL", TokString: "STRING",
	TokArrow: "ARROW", TokColon: "COLON", TokProduct: "PRODUCT", TokMerge: "MERGE", TokZip: "ZIP",
	TokFix: "FIX", TokLPar: "LPAR", TokRPar: "RPAR", TokLBrk: "LBRK", TokRBrk: "RBRK", TokLBrc: "LBRC",
	TokRBrc: "RBRC", TokComma: "COMMA", TokTilde: "NOT", TokBang: "BANG", TokHash: "HASH", TokEq: "EQ",
}

func lexed(t *testing.T, text string) string {
	t.Helper()
	toks, err := lex(text)
	if err != nil {
		t.Fatalf("lex(%q): %v", text, err)
	}
	parts := make([]string, 0, len(toks))
	for _, tok := range toks {
		if tok.Kind == TokEOF {
			continue
		}
		parts = append(parts, kindNames[tok.Kind]+":"+tok.Text)
	}
	return strings.Join(parts, " ")
}

func TestLexTokens(t *testing.T) {
	tests := []struct{ in, want string }{
		{"", ""},
		{"  \n\t ", ""},
		{"gpu1 -$ g:", "WORD:gpu1 ARROW:-$ WORD:g COLON::"},
		{"gpu1-$g:", "WORD:gpu1 ARROW:-$ WORD:g COLON::"},
		{"a*b", "WORD:a PRODUCT:* WORD:b"},
		{"a * b", "WORD:a PRODUCT:* WORD:b"},
		{"a   *\n  b", "WORD:a PRODUCT:* WORD:b"},
		{"a&&b||c", "WORD:a MERGE:&& WORD:b ZIP:|| WORD:c"},
		{"a>b", "WORD:a FIX:> WORD:b"},
		{"run% %_alt pose_? %", "PATTERN:run% PATTERN:%_alt PATTERN:pose_? PATTERN:%"},
		{"6.5 42 0.25 3d", "DECIMAL:6.5 WORD:42 DECIMAL:0.25 WORD:3d"},
		{"(~a,#b)", "LPAR:( NOT:~ WORD:a COMMA:, HASH:# WORD:b RPAR:)"},
		{"[a:b]{c:d}", "LBRK:[ WORD:a COLON:: WORD:b RBRK:] LBRC:{ WORD:c COLON:: WORD:d RBRC:}"},
		{"a=1", "WORD:a EQ:= WORD:1"},
		{`!"x"`, "BANG:! STRING:x"},
		{`"a  b"`, "STRING:a  b"},
		{`"say \"hi\" \\ \n"`, `STRING:say "hi" \ \n`},
		{`"a * b > c -$"`, "STRING:a * b > c -$"},
		{"where not in", "WORD:where WORD:not WORD:in"},
		{`"é" a`, "STRING:é WORD:a"},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			if got := lexed(t, tt.in); got != tt.want {
				t.Errorf("got  %s\nwant %s", got, tt.want)
			}
		})
	}
}

func TestLexPositions(t *testing.T) {
	toks, err := lex(`ab  "é"  -$`)
	if err != nil {
		t.Fatal(err)
	}
	want := []Pos{{0, 2}, {4, 3}, {9, 2}, {11, 0}}
	for i, w := range want {
		if toks[i].Pos != w {
			t.Errorf("token %d at %+v, want %+v", i, toks[i].Pos, w)
		}
	}
}

func TestLexErrors(t *testing.T) {
	tests := []struct {
		in     string
		offset int
		length int
	}{
		{"a $ b", 2, 1},
		{"a - b", 2, 1},
		{"a & b", 2, 1},
		{"a | b", 2, 1},
		{"a . b", 2, 1},
		{"a 1.2.3", 5, 1},
		{"a 6. b", 3, 1},
		{`a "abc`, 2, 4},
		{"a \"abc\ndef\"", 2, 4},
		{`a "abc\"`, 2, 6},
		{"a é b", 2, 1},
		{"a\xffb", 1, 1},
		{"ok \"é\" $", 7, 1},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			_, err := lex(tt.in)
			appErr, ok := errors.FromError(err)
			if !ok {
				t.Fatalf("expected *errors.Error, got %T (%v)", err, err)
			}
			if appErr.Tag != "CommandLexError" {
				t.Errorf("tag = %s", appErr.Tag)
			}
			if !appErr.HasRoute("lex") {
				t.Errorf("unexpected failure path: %s", appErr.Route())
			}
			p, _ := ProblemOf(err)
			if p.Rule != RuleLex || p.Position.Offset != tt.offset || p.Position.Length != tt.length {
				t.Errorf("problem = %+v, want offset %d length %d", p, tt.offset, tt.length)
			}
		})
	}
}
