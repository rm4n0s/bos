package command

import (
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/rm4n0s/errors"
)

// TokKind is the kind of a token (SPEC §6.1).
type TokKind int

// The token kinds. TokTilde is the spec's NOT token `~`; the word `not` is an
// ordinary reserved WORD.
const (
	TokEOF     TokKind = iota
	TokWord            // letters, digits, underscore
	TokPattern         // a word with % or ?
	TokDecimal         // digits . digits
	TokString          // "…"
	TokArrow           // -$
	TokColon           // :
	TokProduct         // *
	TokMerge           // &&
	TokZip             // ||
	TokFix             // >
	TokLPar
	TokRPar
	TokLBrk
	TokRBrk
	TokLBrc
	TokRBrc
	TokComma
	TokTilde // ~
	TokBang  // !
	TokHash  // #
	TokEq    // =
)

// Token is one lexical token. Text is the source spelling, except for strings,
// where it is the unescaped content.
type Token struct {
	Kind TokKind
	Text string
	Pos  Pos
}

var punct = map[rune]TokKind{
	':': TokColon, '*': TokProduct, '>': TokFix, '(': TokLPar, ')': TokRPar,
	'[': TokLBrk, ']': TokRBrk, '{': TokLBrc, '}': TokRBrc, ',': TokComma,
	'~': TokTilde, '!': TokBang, '#': TokHash, '=': TokEq,
}

// lexer turns the command text into tokens. It works on runes so positions
// count characters.
type lexer struct {
	src  []rune
	i    int
	toks []Token
}

// lex splits text into tokens and appends a final TokEOF. Whitespace outside
// string literals only separates tokens, which is equivalent to the
// normalisation of SPEC §6.1.
func lex(text string) ([]Token, error) {
	if !utf8.ValidString(text) {
		valid := 0
		for valid < len(text) {
			r, size := utf8.DecodeRuneInString(text[valid:])
			if r == utf8.RuneError && size == 1 {
				break
			}
			valid += size
		}
		return nil, lexError("the command is not valid UTF-8", Pos{Offset: utf8.RuneCountInString(text[:valid]), Length: 1}, "")
	}
	lx := &lexer{src: []rune(text)}
	for {
		for lx.i < len(lx.src) && unicode.IsSpace(lx.src[lx.i]) {
			lx.i++
		}
		if lx.i >= len(lx.src) {
			lx.toks = append(lx.toks, Token{Kind: TokEOF, Pos: Pos{Offset: len(lx.src)}})
			return lx.toks, nil
		}
		if err := lx.next(); err != nil {
			return nil, err
		}
	}
}

func (lx *lexer) emit(kind TokKind, text string, start int) {
	lx.toks = append(lx.toks, Token{Kind: kind, Text: text, Pos: Pos{Offset: start, Length: lx.i - start}})
}

func (lx *lexer) peekAt(offset int) rune {
	if lx.i+offset < len(lx.src) {
		return lx.src[lx.i+offset]
	}
	return 0
}

func isWildcard(r rune) bool { return r == '%' || r == '?' }

func (lx *lexer) next() error {
	start := lx.i
	c := lx.src[lx.i]
	switch {
	case isWordRune(c) || isWildcard(c):
		lx.word(start)
		return nil
	case c == '"':
		return lx.str(start)
	case c == '-':
		if lx.peekAt(1) == '$' {
			lx.i += 2
			lx.emit(TokArrow, "-$", start)
			return nil
		}
		return lexError("'-' is only valid as part of '-$'", Pos{Offset: start, Length: 1}, "-")
	case c == '&' || c == '|':
		if lx.peekAt(1) == c {
			lx.i += 2
			kind := TokMerge
			if c == '|' {
				kind = TokZip
			}
			lx.emit(kind, string([]rune{c, c}), start)
			return nil
		}
		return lexError("'"+string(c)+"' is only valid as '"+string(c)+string(c)+"'", Pos{Offset: start, Length: 1}, string(c))
	}
	if kind, ok := punct[c]; ok {
		lx.i++
		lx.emit(kind, string(c), start)
		return nil
	}
	return lexError("unexpected character "+strconv.QuoteRune(c), Pos{Offset: start, Length: 1}, string(c))
}

// word scans a WORD, a PATTERN or a DECIMAL. Wildcards exist only inside a
// PATTERN, so `*` is never part of a word.
func (lx *lexer) word(start int) {
	wild := false
	for lx.i < len(lx.src) && (isWordRune(lx.src[lx.i]) || isWildcard(lx.src[lx.i])) {
		wild = wild || isWildcard(lx.src[lx.i])
		lx.i++
	}
	text := string(lx.src[start:lx.i])
	switch {
	case wild:
		lx.emit(TokPattern, text, start)
	case isDigits(text) && lx.peekAt(0) == '.' && lx.peekAt(1) >= '0' && lx.peekAt(1) <= '9':
		lx.i++
		for lx.i < len(lx.src) && lx.src[lx.i] >= '0' && lx.src[lx.i] <= '9' {
			lx.i++
		}
		lx.emit(TokDecimal, string(lx.src[start:lx.i]), start)
	default:
		lx.emit(TokWord, text, start)
	}
}

// str scans a string literal. Only \" and \\ are escapes; any other backslash
// is kept verbatim. The content is never normalised.
func (lx *lexer) str(start int) error {
	var sb strings.Builder
	lx.i++ // opening quote
	for lx.i < len(lx.src) {
		c := lx.src[lx.i]
		switch {
		case c == '"':
			lx.i++
			lx.emit(TokString, sb.String(), start)
			return nil
		case c == '\n' || c == '\r':
			return lexError("a string literal cannot contain a line break", Pos{Offset: start, Length: lx.i - start}, "")
		case c == '\\' && (lx.peekAt(1) == '"' || lx.peekAt(1) == '\\'):
			sb.WriteRune(lx.src[lx.i+1])
			lx.i += 2
		default:
			sb.WriteRune(c)
			lx.i++
		}
	}
	return lexError("unterminated string literal", Pos{Offset: start, Length: lx.i - start}, "")
}

func lexError(msg string, at Pos, token string) *errors.Error {
	return errors.New("CommandLexError", msg, "rule", RuleLex, "position", at, "token", token)
}
