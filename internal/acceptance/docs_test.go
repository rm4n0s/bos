package acceptance

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/rm4n0s/bos/internal/config"
	"github.com/rm4n0s/bos/internal/errtags"
)

// TestTroubleshootingNamesEveryTag keeps docs/troubleshooting.md complete: a
// tag that is raised but not explained would leave a user with a bare name.
func TestTroubleshootingNamesEveryTag(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "docs", "troubleshooting.md"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(b)
	for _, tag := range errtags.All() {
		if !strings.Contains(text, "### `"+tag.Name+"`") {
			t.Errorf("docs/troubleshooting.md has no section for %s", tag.Name)
		}
	}
}

var (
	mdLink    = regexp.MustCompile(`\]\(([^)\s]+)\)`)
	mdAnchors = regexp.MustCompile(`(?m)^#{1,6} (.+)$`)
)

// slug is the anchor GitHub makes of a heading.
func slug(h string) string {
	h = strings.ToLower(strings.TrimSpace(strings.ReplaceAll(h, "`", "")))
	var out strings.Builder
	for _, r := range h {
		switch {
		case r == ' ' || r == '-':
			out.WriteRune('-')
		case r == '_' || r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r > 127:
			out.WriteRune(r)
		}
	}
	return out.String()
}

// TestDocumentationLinksResolve checks every relative link of the Markdown
// files: the file exists and, when the link names a heading, so does that.
func TestDocumentationLinksResolve(t *testing.T) {
	root := filepath.Join("..", "..")
	var files []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && (d.Name() == ".git" || d.Name() == "node_modules") {
			return fs.SkipDir
		}
		if !d.IsDir() && strings.HasSuffix(path, ".md") {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	headings := func(path string) map[string]bool {
		b, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		out := map[string]bool{}
		for _, m := range mdAnchors.FindAllStringSubmatch(string(b), -1) {
			out[slug(m[1])] = true
		}
		return out
	}
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range mdLink.FindAllStringSubmatch(string(b), -1) {
			target := m[1]
			if strings.Contains(target, "://") || strings.HasPrefix(target, "mailto:") {
				continue
			}
			file, anchor, _ := strings.Cut(target, "#")
			dest := f
			if file != "" {
				dest = filepath.Join(filepath.Dir(f), file)
			}
			info, err := os.Stat(dest)
			if err != nil {
				t.Errorf("%s links to %s, which does not exist", f, target)
				continue
			}
			if anchor != "" && !info.IsDir() && strings.HasSuffix(dest, ".md") && !headings(dest)[anchor] {
				t.Errorf("%s links to %s, but %s has no such heading", f, target, dest)
			}
		}
	}
}

// TestConfigurationGuideListsEverySetting keeps docs/guide/configuration.md in
// step with the settings of the program.
func TestConfigurationGuideListsEverySetting(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "docs", "guide", "configuration.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range config.Keys() {
		if !strings.Contains(string(b), "| `"+name+"` |") {
			t.Errorf("the configuration guide has no row for %s", name)
		}
	}
}
