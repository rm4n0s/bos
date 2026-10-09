package files

import (
	"os"
	"path/filepath"
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
