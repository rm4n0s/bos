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

func TestSaveUpload(t *testing.T) {
	root := t.TempDir()
	st, err := NewStore([]string{root})
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "thumbs")
	tests := []struct {
		name, original, wantSuffix string
	}{
		{"plain name", "fox.png", "-fox.png"},
		{"path is cut off", "../../etc/passwd", "-passwd"},
		{"windows path", `C:\Users\me\pic.png`, "-pic.png"},
		{"unusual characters", "my fox (1).png", "-my_fox__1_.png"},
		{"leading dots", "..hidden", "-hidden"},
		{"nothing left", "...", "-upload"},
	}
	seen := map[string]bool{}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p, err := st.SaveUpload(dir, tc.original, strings.NewReader("data"), 0)
			if err != nil {
				t.Fatal(err)
			}
			if filepath.Dir(p) != dir || !strings.HasSuffix(p, tc.wantSuffix) {
				t.Errorf("path = %s, want a file in %s ending %s", p, dir, tc.wantSuffix)
			}
			if b, err := os.ReadFile(p); err != nil || string(b) != "data" {
				t.Errorf("content = %q, %v", b, err)
			}
			if seen[p] {
				t.Errorf("%s was handed out twice", p)
			}
			seen[p] = true
		})
	}
	// The same name twice gives two files.
	a, _ := st.SaveUpload(dir, "same.png", strings.NewReader("1"), 0)
	b, _ := st.SaveUpload(dir, "same.png", strings.NewReader("2"), 0)
	if a == b {
		t.Error("two uploads of one name collided")
	}
}

func TestSaveUploadLimit(t *testing.T) {
	root := t.TempDir()
	st, err := NewStore([]string{root})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.SaveUpload(root, "exact.bin", strings.NewReader("12345"), 5); err != nil {
		t.Errorf("a file of exactly the limit was refused: %v", err)
	}
	_, err = st.SaveUpload(root, "big.bin", strings.NewReader("123456"), 5)
	appErr, ok := errors.FromError(err)
	if !ok || !appErr.HasRoute("Store.SaveUpload.UploadTooLarge") {
		t.Fatalf("err = %v", err)
	}
	entries, _ := os.ReadDir(root)
	for _, e := range entries {
		if strings.Contains(e.Name(), "big") || strings.HasPrefix(e.Name(), ".write-") {
			t.Errorf("a refused upload left %s behind", e.Name())
		}
	}
}

func TestSaveUploadOutsideStorage(t *testing.T) {
	st, err := NewStore([]string{t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	_, err = st.SaveUpload(t.TempDir(), "a.png", strings.NewReader("x"), 0)
	appErr, ok := errors.FromError(err)
	if !ok || !appErr.HasRoute("Store.Write.Store.resolve.FileOutsideStorage") && !appErr.HasRoute("FileOutsideStorage") {
		t.Errorf("err = %v", err)
	}
}

func TestRemoveIfEmpty(t *testing.T) {
	root := t.TempDir()
	st, err := NewStore([]string{root})
	if err != nil {
		t.Fatal(err)
	}
	empty, full := filepath.Join(root, "1"), filepath.Join(root, "2")
	for _, d := range []string{empty, full} {
		if err := os.MkdirAll(d, 0o750); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(full, "a.png"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, d := range []string{empty, full, filepath.Join(root, "gone")} {
		if err := st.RemoveIfEmpty(d); err != nil {
			t.Errorf("%s: %v", d, err)
		}
	}
	if _, err := os.Stat(empty); !os.IsNotExist(err) {
		t.Error("the empty folder is still there")
	}
	if _, err := os.Stat(filepath.Join(full, "a.png")); err != nil {
		t.Error("a folder with a file in it was touched")
	}
	appErr, ok := errors.FromError(st.RemoveIfEmpty("/etc"))
	if !ok || appErr.Tag != "FileOutsideStorage" {
		t.Errorf("a folder outside the storage: %v", appErr)
	}
}
