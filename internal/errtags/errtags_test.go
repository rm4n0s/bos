package errtags

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

var pascalCase = regexp.MustCompile(`^[A-Z][a-z0-9]*([A-Z][a-z0-9]*|[A-Z]+[a-z0-9]*)*$`)

// sources reads every Go file of the module below internal/ and cmd/, split
// into code and tests.
func sources(t *testing.T) (code, tests map[string]string) {
	t.Helper()
	code, tests = map[string]string{}, map[string]string{}
	for _, root := range []string{"..", filepath.Join("..", "..", "cmd")} {
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") {
				return err
			}
			abs, _ := filepath.Abs(path)
			if strings.Contains(abs, string(filepath.Separator)+"errtags"+string(filepath.Separator)) {
				return nil // the catalogue and its test name every tag
			}
			if strings.HasSuffix(abs, filepath.Join("internal", "api", "errors.go")) {
				return nil // the HTTP status table names every tag without raising any
			}
			b, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			if strings.HasSuffix(path, "_test.go") {
				tests[path] = string(b)
			} else {
				code[path] = string(b)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	return code, tests
}

// anywhere reports whether a file contains text.
func anywhere(files map[string]string, text string) bool {
	for _, content := range files {
		if strings.Contains(content, text) {
			return true
		}
	}
	return false
}

// raised returns the tags that production code gives to an error: the first
// argument of errors.New, NewErr and NewErrf, of the store's notFound helper,
// and the string constants of the command language named tag….
func raised(t *testing.T, code map[string]string) map[string]string {
	t.Helper()
	out := map[string]string{}
	fset := token.NewFileSet()
	for path, content := range code {
		f, err := parser.ParseFile(fset, path, content, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.CallExpr:
				name := ""
				switch fn := n.Fun.(type) {
				case *ast.SelectorExpr:
					if x, ok := fn.X.(*ast.Ident); ok && x.Name == "errors" {
						name = fn.Sel.Name
					}
				case *ast.Ident:
					name = fn.Name
				}
				if (name == "New" || name == "NewErr" || name == "NewErrf" || name == "notFound") && len(n.Args) > 0 {
					if lit, ok := n.Args[0].(*ast.BasicLit); ok && lit.Kind == token.STRING {
						if s, err := strconv.Unquote(lit.Value); err == nil {
							out[s] = path
						}
					}
				}
			case *ast.ValueSpec:
				for i, id := range n.Names {
					if strings.HasPrefix(id.Name, "tag") && i < len(n.Values) {
						if lit, ok := n.Values[i].(*ast.BasicLit); ok && lit.Kind == token.STRING {
							if s, err := strconv.Unquote(lit.Value); err == nil {
								out[s] = path
							}
						}
					}
				}
			}
			return true
		})
	}
	return out
}

func TestCatalogueIsWellFormed(t *testing.T) {
	seen := map[string]bool{}
	for _, tag := range All() {
		if !pascalCase.MatchString(tag.Name) {
			t.Errorf("%q is not PascalCase", tag.Name)
		}
		if seen[tag.Name] {
			t.Errorf("%q is listed twice", tag.Name)
		}
		seen[tag.Name] = true
		if tag.Group == "" {
			t.Errorf("%q has no group", tag.Name)
		}
	}
}

// Every tag the code raises is in the catalogue.
func TestEveryRaisedTagIsCatalogued(t *testing.T) {
	code, _ := sources(t)
	known := map[string]bool{}
	for _, tag := range All() {
		known[tag.Name] = true
	}
	for tag, file := range raised(t, code) {
		if !known[tag] {
			t.Errorf("%s raises %q, which is not in the catalogue", file, tag)
		}
	}
}

// Every catalogued tag is raised somewhere and has a test that names it (a
// route test reads "….Tag", so the name is searched as plain text); a pending
// tag is not raised yet.
func TestEveryTagIsRaisedAndTested(t *testing.T) {
	code, tests := sources(t)
	for _, tag := range All() {
		raisedIn := anywhere(code, strconv.Quote(tag.Name))
		switch {
		case tag.Pending && raisedIn:
			t.Errorf("%s is raised: remove its Pending flag", tag.Name)
		case tag.Pending:
		case !raisedIn:
			t.Errorf("%s is in the catalogue but no code raises it", tag.Name)
		case !anywhere(tests, tag.Name):
			t.Errorf("%s has no route test", tag.Name)
		}
	}
}
