package command

import "strings"

// CompletionKind says which code names fit at the cursor.
type CompletionKind string

// The kinds of name a command can ask for at the cursor. CompleteNone is the
// zero-information answer: the cursor is where no name belongs.
const (
	CompleteNone      CompletionKind = ""
	CompleteServer    CompletionKind = "server"
	CompleteGenerator CompletionKind = "generator"
	CompleteFixer     CompletionKind = "fixer"
	CompleteGroup     CompletionKind = "group"
	CompleteItem      CompletionKind = "item"
	CompleteTag       CompletionKind = "tag"
)

// Completion is what the cursor of a half-written command asks for: the kind
// of code name, the part already typed and the span of the text a chosen name
// replaces (the typed part and the rest of the word under the cursor).
type Completion struct {
	Kind CompletionKind
	// Group is the group whose items fit, for CompleteItem.
	Group  string
	Prefix string
	// Start and End are byte offsets: code[Start:End] is replaced by the name.
	Start, End int
}

// CompletionAt reads the text before the cursor, a byte offset, and says which
// code names fit there. It follows the grammar of SPEC §6.2 loosely enough to
// work on text that is not finished yet: it never fails, and where it cannot
// tell it answers CompleteNone.
func CompletionAt(code string, cursor int) Completion {
	cursor = max(0, min(cursor, len(code)))
	toks, inString := scanForCompletion(code[:cursor])
	if inString {
		return Completion{}
	}

	start := cursor
	if n := len(toks); n > 0 && toks[n-1].word && toks[n-1].end == cursor {
		cur := toks[n-1]
		if cur.wildcard {
			return Completion{}
		}
		start = cur.start
		toks = toks[:n-1]
	}
	end := cursor
	for end < len(code) && isNameByte(code[end]) {
		end++
	}

	kind, group := completionKind(toks)
	if kind == CompleteNone {
		return Completion{}
	}
	return Completion{Kind: kind, Group: group, Prefix: code[start:cursor], Start: start, End: end}
}

// ctoken is a token of the loose scan: a word (names, keywords and numbers) or
// one punctuation mark. Strings are skipped.
type ctoken struct {
	text       string
	word       bool
	wildcard   bool
	start, end int
}

func isNameByte(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_'
}

func scanForCompletion(s string) (toks []ctoken, inString bool) {
	for i := 0; i < len(s); {
		c := s[i]
		switch {
		case c == '"':
			i++
			closed := false
			for i < len(s) {
				if s[i] == '\\' {
					i += 2
					continue
				}
				i++
				if s[i-1] == '"' {
					closed = true
					break
				}
			}
			if !closed {
				return nil, true
			}
		case isNameByte(c) || c == '%' || c == '?':
			j := i
			wild := false
			for j < len(s) && (isNameByte(s[j]) || s[j] == '%' || s[j] == '?') {
				wild = wild || s[j] == '%' || s[j] == '?'
				j++
			}
			toks = append(toks, ctoken{text: s[i:j], word: true, wildcard: wild, start: i, end: j})
			i = j
		case strings.HasPrefix(s[i:], "-$"), strings.HasPrefix(s[i:], "&&"), strings.HasPrefix(s[i:], "||"):
			toks = append(toks, ctoken{text: s[i : i+2], start: i, end: i + 2})
			i += 2
		case strings.IndexByte(":*>()[]{},~!#=", c) >= 0:
			toks = append(toks, ctoken{text: s[i : i+1], start: i, end: i + 1})
			i++
		default: // whitespace, and anything the lexer would reject
			i++
		}
	}
	return toks, false
}

// stage is the clause of the command that the scan is in, outside brackets.
type stage int

const (
	stagePlacement stage = iota
	stageGenerator
	stageSelections
	stageWhere
	stagePick
	stageRepeat
	stageWith
	stageSpread
	stageFixers
)

// frame is an open bracket. group is set for a parenthesis that follows a
// group name, whose content selects items of that group.
type frame struct {
	open  string
	group string
}

var reservedWords = map[string]bool{
	"where": true, "repeat": true, "limit": true, "sample": true, "with": true,
	"spread": true, "on": true, "and": true, "or": true, "not": true, "in": true,
}

// completionKind finds what the tokens before the cursor expect next.
func completionKind(toks []ctoken) (CompletionKind, string) {
	st := stagePlacement
	var stack []frame
	for i, t := range toks {
		switch {
		case t.text == "(" && !t.word:
			f := frame{open: "("}
			if i > 0 && toks[i-1].word {
				switch w := toks[i-1].text; {
				case w == "in" && i > 1 && toks[i-2].word:
					f.group = toks[i-2].text
				case !reservedWords[w]:
					f.group = w
				}
			}
			stack = append(stack, f)
		case (t.text == "[" || t.text == "{") && !t.word:
			stack = append(stack, frame{open: t.text})
		case (t.text == ")" || t.text == "]" || t.text == "}") && !t.word:
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
		case len(stack) > 0:
		case t.word:
			if st >= stageSelections && st != stageFixers {
				switch t.text {
				case "where":
					st = stageWhere
				case "limit", "sample":
					st = stagePick
				case "repeat":
					st = stageRepeat
				case "with":
					st = stageWith
				case "spread":
					st = stageSpread
				}
			}
		case t.text == "-$":
			st = stageGenerator
		case t.text == ":":
			if st == stageGenerator {
				st = stageSelections
			}
		case t.text == ">":
			if st >= stageSelections {
				st = stageFixers
			}
		}
	}

	var prev, before ctoken
	if n := len(toks); n > 0 {
		prev = toks[n-1]
		if n > 1 {
			before = toks[n-2]
		}
	}
	isPunct := func(t ctoken, p string) bool { return !t.word && t.text == p }
	isWord := func(t ctoken, w string) bool { return t.word && t.text == w }
	afterJoiner := isPunct(prev, ":") || isPunct(prev, "*") || isPunct(prev, "&&") || isPunct(prev, "||")

	if isPunct(prev, "#") {
		return CompleteTag, ""
	}
	if len(stack) > 0 {
		top := stack[len(stack)-1]
		switch top.open {
		case "(":
			if top.group != "" {
				if isPunct(prev, "(") || isPunct(prev, ",") || isPunct(prev, "~") {
					return CompleteItem, top.group
				}
				return CompleteNone, ""
			}
			// A grouping parenthesis of a where condition.
			switch {
			case isPunct(prev, "(") || isWord(prev, "and") || isWord(prev, "or") || isWord(prev, "not"):
				return CompleteGroup, ""
			case isPunct(prev, "=") && before.word:
				return CompleteItem, before.text
			}
		case "[", "{":
			if afterJoiner {
				return CompleteGroup, ""
			}
		}
		return CompleteNone, ""
	}

	switch st {
	case stagePlacement:
		if len(toks) == 0 || isPunct(prev, ",") {
			return CompleteServer, ""
		}
	case stageGenerator:
		if isPunct(prev, "-$") {
			return CompleteGenerator, ""
		}
	case stageSelections:
		if afterJoiner {
			return CompleteGroup, ""
		}
	case stageWhere:
		switch {
		case isWord(prev, "where") || isWord(prev, "and") || isWord(prev, "or") || isWord(prev, "not"):
			return CompleteGroup, ""
		case isPunct(prev, "=") && before.word:
			return CompleteItem, before.text
		}
	case stageSpread:
		if isWord(prev, "by") {
			return CompleteGroup, ""
		}
	case stageFixers:
		switch {
		case isPunct(prev, ">"):
			return CompleteFixer, ""
		case isWord(prev, "on"):
			return CompleteServer, ""
		}
	}
	return CompleteNone, ""
}
