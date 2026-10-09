package command

import "strings"

// matchPattern reports whether code matches a pattern where '%' is any run of
// characters, possibly empty, and '?' is exactly one character. Matching is
// case-sensitive (SPEC §6.4).
func matchPattern(pattern, code string) bool {
	p, s := []rune(pattern), []rune(code)
	pi, si := 0, 0
	star, mark := -1, 0
	for si < len(s) {
		switch {
		case pi < len(p) && p[pi] == '%':
			star, mark = pi, si
			pi++
		case pi < len(p) && (p[pi] == '?' || p[pi] == s[si]):
			pi++
			si++
		case star >= 0:
			pi = star + 1
			mark++
			si = mark
		default:
			return false
		}
	}
	for pi < len(p) && p[pi] == '%' {
		pi++
	}
	return pi == len(p)
}

// matches reports whether the selector matches the item. Tags compare
// case-insensitively (SPEC §3.4).
func (s Selector) matches(it *Item) bool {
	switch s.Kind {
	case SelItem:
		return it.Code == s.Text
	case SelPattern:
		return matchPattern(s.Text, it.Code)
	default:
		for _, tag := range it.Tags {
			if strings.EqualFold(tag, s.Text) {
				return true
			}
		}
		return false
	}
}

// matchesAny reports whether any selector matches the item.
func matchesAny(sels []Selector, it *Item) bool {
	for _, s := range sels {
		if s.matches(it) {
			return true
		}
	}
	return false
}

// applyFilter returns the items a filter keeps, in group order and without
// duplicates: the union of what the selectors match for an include list, the
// rest of the group for an exclude list (SPEC §6.4). A nil filter keeps all.
func applyFilter(items []Item, f *Filter) []*Item {
	out := make([]*Item, 0, len(items))
	for i := range items {
		it := &items[i]
		if f == nil || matchesAny(f.Selectors, it) != f.Exclude {
			out = append(out, it)
		}
	}
	return out
}
