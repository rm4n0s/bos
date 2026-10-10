package ui

import (
	"fmt"
	"strings"
	"testing"
)

// shown writes the numbers of a pager the way a person reads them: the current
// page in brackets, a gap as "…".
func shown(p pagerData) string {
	var parts []string
	for _, n := range p.Numbers() {
		switch {
		case n.Gap:
			parts = append(parts, "…")
		case n.Current:
			parts = append(parts, fmt.Sprintf("[%d]", n.N))
		default:
			parts = append(parts, fmt.Sprint(n.N))
		}
	}
	return strings.Join(parts, " ")
}

func TestPagerNumbers(t *testing.T) {
	tests := []struct {
		page, total int
		want        string
	}{
		{1, 1, "[1]"},
		{1, 2, "[1] 2"},
		{2, 2, "1 [2]"},
		{1, 5, "[1] 2 3 4 5"},
		{3, 7, "1 2 [3] 4 5 6 7"},
		{1, 20, "[1] 2 3 … 20"},
		{4, 20, "1 2 3 [4] 5 6 … 20"},
		{5, 20, "1 2 3 4 [5] 6 7 … 20"},
		{6, 20, "1 … 4 5 [6] 7 8 … 20"},
		{10, 20, "1 … 8 9 [10] 11 12 … 20"},
		{16, 20, "1 … 14 15 [16] 17 18 19 20"},
		{20, 20, "1 … 18 19 [20]"},
		{0, 0, ""},
	}
	for _, tt := range tests {
		p := pagerData{Page: tt.page, TotalPages: tt.total}
		if got := shown(p); got != tt.want {
			t.Errorf("page %d of %d: %q, want %q", tt.page, tt.total, got, tt.want)
		}
	}
}
