package filesrv

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/rm4n0s/bos/internal/files"
	"github.com/rm4n0s/errors"
)

func TestCleanName(t *testing.T) {
	tests := map[string]bool{
		"a.png": true, "7/a.png": true, "item-1f/red.png": true,
		"": false, ".": false, "..": false, "../a": false, "a/..": false, "a//b": false, "/a": false, "a/": false, `a\b`: false, "a\x00": false, "a\nb": false,
	}
	for name, want := range tests {
		if got := CleanName(name); got != want {
			t.Errorf("CleanName(%q) = %v, want %v", name, got, want)
		}
	}
}

func newServer(t *testing.T) (*Server, string) {
	t.Helper()
	root := t.TempDir()
	st, err := files.NewStore([]string{root})
	if err != nil {
		t.Fatal(err)
	}
	return &Server{Files: st, Dirs: Dirs{Results: filepath.Join(root, "results"), Thumbnails: filepath.Join(root, "thumbs")}}, root
}

func TestServe(t *testing.T) {
	s, root := newServer(t)
	if err := os.MkdirAll(filepath.Join(root, "results", "7"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "results", "7", "fox.png"), []byte("0123456789"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "outside.txt"), []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), "GET", "/", nil)
	req.Header.Set("Range", "bytes=2-4")
	if err := s.Serve(rec, req, "results", "7/fox.png"); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusPartialContent || rec.Body.String() != "234" || rec.Header().Get("Content-Type") != "image/png" ||
		rec.Header().Get("X-Content-Type-Options") != "nosniff" || rec.Header().Get("Content-Security-Policy") != "sandbox" {
		t.Errorf("response = %d %q %v", rec.Code, rec.Body, rec.Header())
	}

	for _, tc := range []struct{ kind, name string }{
		{"results", "../outside.txt"}, {"results", "7"}, {"results", "nope.png"}, {"secrets", "x"}, {"thumbnails", "x.png"}, {"controlnet", "x.png"},
	} {
		err := s.Serve(httptest.NewRecorder(), req, tc.kind, tc.name)
		appErr, ok := errors.FromError(err)
		if !ok || appErr.Tag != "FileNotFound" {
			t.Errorf("%s/%s: err = %v, want FileNotFound", tc.kind, tc.name, err)
		}
	}
}
