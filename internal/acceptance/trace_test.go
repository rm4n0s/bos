package acceptance

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var (
	testName = regexp.MustCompile("`(Test[A-Za-z0-9_]+)`")
	rowStart = regexp.MustCompile(`^\| (\d+[a-c]?) \|`)
)

// testFunctions returns the names of the test functions of the module.
func testFunctions(t *testing.T) map[string]bool {
	t.Helper()
	root := filepath.Join("..", "..")
	fn := regexp.MustCompile(`(?m)^func (Test[A-Za-z0-9_]+)\(`)
	out := map[string]bool{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, "_test.go") {
			return err
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, m := range fn.FindAllStringSubmatch(string(b), -1) {
			out[m[1]] = true
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// TestEveryAcceptanceTestIsCovered keeps docs/ACCEPTANCE.md honest: it lists
// every acceptance test of the specification (1–41 and 1a–1c), every row names
// at least one test, and every name is a test that exists.
func TestEveryAcceptanceTestIsCovered(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "docs", "ACCEPTANCE.md"))
	if err != nil {
		t.Fatal(err)
	}
	have := testFunctions(t)
	seen := map[string]bool{}
	for _, line := range strings.Split(string(b), "\n") {
		m := rowStart.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		id := m[1]
		seen[id] = true
		names := testName.FindAllStringSubmatch(line, -1)
		if len(names) == 0 {
			t.Errorf("acceptance %s names no test", id)
		}
		for _, n := range names {
			if !have[n[1]] {
				t.Errorf("acceptance %s names %s, which is not a test function", id, n[1])
			}
		}
	}
	want := []string{"1", "1a", "1b", "1c"}
	for n := 2; n <= 41; n++ {
		want = append(want, itoa(n))
	}
	for _, id := range want {
		if !seen[id] {
			t.Errorf("acceptance %s is not in docs/ACCEPTANCE.md", id)
		}
	}
	if len(seen) != len(want) {
		t.Errorf("the table has %d rows, the specification has %d tests", len(seen), len(want))
	}
}

func itoa(n int) string {
	const digits = "0123456789"
	if n < 10 {
		return digits[n : n+1]
	}
	return itoa(n/10) + digits[n%10:n%10+1]
}
