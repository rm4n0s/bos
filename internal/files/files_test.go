package files

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rm4n0s/errors"
)

func TestEnsureDirs(t *testing.T) {
	root := t.TempDir()
	dirs := []string{filepath.Join(root, "a", "b"), filepath.Join(root, "c")}
	for range 2 { // the second call proves that existing directories are fine
		if err := EnsureDirs(dirs); err != nil {
			t.Fatal(err)
		}
	}
	for _, d := range dirs {
		if st, err := os.Stat(d); err != nil || !st.IsDir() {
			t.Errorf("%s was not created: %v", d, err)
		}
	}
}

func TestEnsureDirsFailure(t *testing.T) {
	root := t.TempDir()
	blocker := filepath.Join(root, "file")
	if err := os.WriteFile(blocker, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	err := EnsureDirs([]string{filepath.Join(blocker, "sub")})
	appErr, ok := errors.FromError(err)
	if !ok {
		t.Fatalf("expected *errors.Error, got %T", err)
	}
	if !appErr.HasRoute("EnsureDirs.FileStoreFailure") {
		t.Errorf("unexpected failure path: %s", appErr.Route())
	}
}

func TestStoreRemoveAndOpen(t *testing.T) {
	root := t.TempDir()
	st, err := NewStore([]string{root})
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(root, "a.png")
	folder := filepath.Join(root, "masks")
	if err := os.MkdirAll(folder, 0o750); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{file, filepath.Join(folder, "red.png")} {
		if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	r, err := st.Open(file)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}

	for _, p := range []string{file, folder, file} { // a file, a folder with content, and a path already gone
		if err := st.Remove(p); err != nil {
			t.Errorf("Remove(%s): %v", p, err)
		}
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("%s still exists: %v", p, err)
		}
	}

	_, err = st.Open(file)
	wantTag(t, err, "Store.Open", "FileNotFound")
}

func TestStoreStaysInsideItsDirectories(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "keep.txt")
	if err := os.WriteFile(outside, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	st, err := NewStore([]string{root})
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct{ name, path string }{
		{"another directory", outside},
		{"parent traversal", filepath.Join(root, "..", filepath.Base(filepath.Dir(outside)), "keep.txt")},
		{"the directory itself", root},
		{"the parent of the directory", filepath.Dir(root)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wantTag(t, st.Remove(tt.path), "Store.Remove", "FileOutsideStorage")
			_, err := st.Open(tt.path)
			wantTag(t, err, "Store.Open", "FileOutsideStorage")
		})
	}
	if _, err := os.Stat(outside); err != nil {
		t.Errorf("a file outside the storage directories was touched: %v", err)
	}
}

func TestStoreOpenFailure(t *testing.T) {
	root := t.TempDir()
	st, err := NewStore([]string{root})
	if err != nil {
		t.Fatal(err)
	}
	locked := filepath.Join(root, "locked")
	if err := os.WriteFile(locked, []byte("x"), 0o000); err != nil {
		t.Fatal(err)
	}
	if f, err := os.Open(locked); err == nil { // running as root: permissions do not apply
		_ = f.Close()
		t.Skip("file permissions are not enforced")
	}
	_, err = st.Open(locked)
	wantTag(t, err, "Store.Open", "FileStoreFailure")
}

func wantTag(t *testing.T, err error, entry, tag string) {
	t.Helper()
	appErr, ok := errors.FromError(err)
	if !ok {
		t.Fatalf("expected *errors.Error, got %T (%v)", err, err)
	}
	if appErr.Tag != tag || !appErr.HasRoute(entry) {
		t.Errorf("unexpected failure path: %s (want %s ... %s)", appErr.Route(), entry, tag)
	}
}

type failingReader struct{ after int }

func (f *failingReader) Read(p []byte) (int, error) {
	if f.after == 0 {
		return 0, os.ErrDeadlineExceeded
	}
	f.after--
	p[0] = 'x'
	return 1, nil
}

func TestStoreWrite(t *testing.T) {
	root := t.TempDir()
	st, err := NewStore([]string{root})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "results", "7", "a.png")
	if err := st.Write(path, strings.NewReader("first")); err != nil {
		t.Fatal(err)
	}
	if err := st.Write(path, strings.NewReader("second")); err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(path); err != nil || string(b) != "second" {
		t.Errorf("content = %q, %v", b, err)
	}

	// A copy that fails halfway leaves the old file and no stray file.
	err = st.Write(path, &failingReader{after: 3})
	wantTag(t, err, "Store.Write", "FileStoreFailure")
	if b, _ := os.ReadFile(path); string(b) != "second" {
		t.Errorf("a failed write changed the file: %q", b)
	}
	if entries, _ := os.ReadDir(filepath.Dir(path)); len(entries) != 1 {
		t.Errorf("%d files in the folder, want only the result", len(entries))
	}

	wantTag(t, st.Write(filepath.Join(t.TempDir(), "x"), strings.NewReader("x")), "Store.Write", "FileOutsideStorage")
	// A file in the way of a folder.
	wantTag(t, st.Write(filepath.Join(path, "below"), strings.NewReader("x")), "Store.Write", "FileStoreFailure")
}
