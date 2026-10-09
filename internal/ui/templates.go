package ui

import (
	"bytes"
	"embed"
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/rm4n0s/errors"
)

//go:embed templates/*.html
var templateFS embed.FS

//go:embed static
var staticFS embed.FS

// staticHandler serves the embedded scripts and styles. They never change
// while the program runs.
func staticHandler(dir fs.FS) http.Handler {
	files := http.FileServerFS(dir)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=3600")
		files.ServeHTTP(w, r)
	})
}

// templates holds one parsed set per page, each with the layout and the shared
// fragments, and one set of fragments alone.
type templates struct {
	pages     map[string]*template.Template
	fragments *template.Template
}

// page is what a full page is rendered from.
type page struct {
	Title string
	Nav   string
	Error string
	Data  any
	// SignedIn shows the sign-out button; the interface sets it when a token is
	// configured.
	SignedIn bool
}

func parseTemplates(funcs template.FuncMap) (*templates, error) {
	shared, err := fs.Glob(templateFS, "templates/_*.html")
	if err != nil {
		return nil, errors.NewErr("TemplateInvalid", err)
	}
	base := template.New("").Funcs(funcs)
	if base, err = base.ParseFS(templateFS, append([]string{"templates/layout.html"}, shared...)...); err != nil {
		return nil, errors.NewErr("TemplateInvalid", err)
	}
	allFiles, err := fs.Glob(templateFS, "templates/*.html")
	if err != nil {
		return nil, errors.NewErr("TemplateInvalid", err)
	}
	var pageFiles []string
	for _, f := range allFiles {
		if name := path.Base(f); name != "layout.html" && !strings.HasPrefix(name, "_") {
			pageFiles = append(pageFiles, f)
		}
	}
	t := &templates{pages: map[string]*template.Template{}}
	for _, f := range pageFiles {
		name := path.Base(f)
		clone, err := base.Clone()
		if err != nil {
			return nil, errors.NewErr("TemplateInvalid", err)
		}
		if clone, err = clone.ParseFS(templateFS, f); err != nil {
			return nil, errors.NewErr("TemplateInvalid", err)
		}
		t.pages[strings.TrimSuffix(name, ".html")] = clone
	}
	// The fragments a handler patches into a page are defined in the page
	// files, next to the markup that first draws them. Their names are unique
	// across pages; the "content" blocks that all pages define are never run
	// from this set.
	frag, err := base.Clone()
	if err != nil {
		return nil, errors.NewErr("TemplateInvalid", err)
	}
	if frag, err = frag.ParseFS(templateFS, pageFiles...); err != nil {
		return nil, errors.NewErr("TemplateInvalid", err)
	}
	t.fragments = frag
	return t, nil
}

// renderPage writes a complete HTML document.
func (u *UI) renderPage(w http.ResponseWriter, r *http.Request, name string, p page) {
	tpl, ok := u.tpl.pages[name]
	if !ok {
		u.Logger.Error("no such page template", "name", name)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	p.SignedIn = u.AuthToken != "" && u.signedIn(r)
	var buf bytes.Buffer
	if err := tpl.ExecuteTemplate(&buf, "layout", p); err != nil {
		u.Logger.Error("could not render a page", "name", name, "error", err.Error())
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	// A failed write means the client has gone away; there is nobody to tell.
	_, _ = w.Write(buf.Bytes())
}

// fragment renders a named shared fragment to a string.
func (u *UI) fragment(name string, data any) (string, error) {
	var buf bytes.Buffer
	if err := u.tpl.fragments.ExecuteTemplate(&buf, name, data); err != nil {
		return "", errors.NewErr("TemplateInvalid", err).SetMetadata("template", name)
	}
	return buf.String(), nil
}

// funcs are the helpers the templates use.
func (u *UI) funcs() template.FuncMap {
	return template.FuncMap{
		"fileURL":  u.fileURL,
		"statusOf": func(s any) string { return strings.ToLower(strings.TrimSpace(fmt.Sprint(s))) },
		"add":      func(a, b int) int { return a + b },
		"sub":      func(a, b int) int { return a - b },
		"when":     formatTime,
		"join":     strings.Join,
		"seq": func(n int) []int {
			out := make([]int, n)
			for i := range out {
				out[i] = i
			}
			return out
		},
		"dict": dict,
		"itoa": func(n int64) string { return strconv.FormatInt(n, 10) },
	}
}

func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format("2006-01-02 15:04")
}

// fileURL turns the stored path of a file into its URL under /files, or "" if
// the path is empty or outside the directory of that kind.
func (u *UI) fileURL(kind, stored string) string {
	if stored == "" {
		return ""
	}
	dir, ok := u.Dirs.Dir(kind)
	if !ok || dir == "" {
		return ""
	}
	rel, err := filepath.Rel(dir, stored)
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
		return ""
	}
	return "/files/" + kind + "/" + filepath.ToSlash(rel)
}

// dict builds a map for passing several values to a template.
func dict(pairs ...any) map[string]any {
	m := make(map[string]any, len(pairs)/2)
	for i := 0; i+1 < len(pairs); i += 2 {
		if k, ok := pairs[i].(string); ok {
			m[k] = pairs[i+1]
		}
	}
	return m
}
