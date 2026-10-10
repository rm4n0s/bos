package comfy

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/rm4n0s/errors"
)

func TestRemoveOutput(t *testing.T) {
	tests := []struct {
		name string
		file OutputFile
		// kept lists files, relative to the installation, that must survive.
		kept []string
		gone string
	}{
		{name: "a file of the output folder", file: OutputFile{Filename: "a.png", Type: "output"}, gone: "output/a.png"},
		{name: "a file in a sub-folder", file: OutputFile{Filename: "a.png", Subfolder: "day/one", Type: "output"}, gone: "output/day/one/a.png"},
		{name: "a temporary file is left to ComfyUI", file: OutputFile{Filename: "t.png", Type: "temp"}, kept: []string{"output/t.png"}},
		{name: "an input file is never removed", file: OutputFile{Filename: "i.png", Type: "input"}, kept: []string{"output/i.png"}},
		{name: "a file that is not there", file: OutputFile{Filename: "missing.png", Type: "output"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			for _, p := range []string{"output/a.png", "output/day/one/a.png", "output/t.png", "output/i.png", "output/other.png"} {
				writeFile(t, filepath.Join(root, p))
			}
			if err := RemoveOutput(root, tc.file); err != nil {
				t.Fatal(err)
			}
			if tc.gone != "" {
				if _, err := os.Stat(filepath.Join(root, tc.gone)); !os.IsNotExist(err) {
					t.Errorf("%s was not removed (%v)", tc.gone, err)
				}
			}
			for _, p := range append(tc.kept, "output/other.png") {
				if _, err := os.Stat(filepath.Join(root, p)); err != nil {
					t.Errorf("%s was lost: %v", p, err)
				}
			}
		})
	}
}

// A name from ComfyUI must never reach a file outside the output folder.
func TestRemoveOutputStaysInsideTheOutputFolder(t *testing.T) {
	tests := []struct {
		name string
		file OutputFile
		link bool
	}{
		{name: "dot dot in the file name", file: OutputFile{Filename: "../precious.txt", Type: "output"}},
		{name: "dot dot in the sub-folder", file: OutputFile{Filename: "precious.txt", Subfolder: "..", Type: "output"}},
		{name: "an absolute file name", file: OutputFile{Filename: "/precious.txt", Type: "output"}},
		{name: "a link out of the folder", file: OutputFile{Filename: "precious.txt", Subfolder: "link", Type: "output"}, link: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			install := filepath.Join(root, "comfy")
			writeFile(t, filepath.Join(install, "output", "keep.png"))
			writeFile(t, filepath.Join(install, "precious.txt"))
			writeFile(t, filepath.Join(root, "precious.txt"))
			if tc.link {
				if err := os.Symlink(root, filepath.Join(install, "output", "link")); err != nil {
					t.Skip("no symbolic links here:", err)
				}
			}
			err := RemoveOutput(install, tc.file)
			for _, p := range []string{"comfy/precious.txt", "precious.txt", "comfy/output/keep.png"} {
				if _, statErr := os.Stat(filepath.Join(root, p)); statErr != nil {
					t.Errorf("%s was removed: %v", p, statErr)
				}
			}
			if err == nil {
				return // a name that stays inside the folder and is not there is not an error
			}
			appErr, ok := errors.FromError(err)
			if !ok {
				t.Fatalf("expected *errors.Error, got %T", err)
			}
			if !appErr.HasRoute("RemoveOutput.FileStoreFailure") {
				t.Errorf("unexpected failure path: %s", appErr.Route())
			}
		})
	}
}

func TestRemoveOutputWithoutAnOutputFolder(t *testing.T) {
	err := RemoveOutput(t.TempDir(), OutputFile{Filename: "a.png", Type: "output"})
	appErr, ok := errors.FromError(err)
	if !ok {
		t.Fatalf("expected *errors.Error, got %T", err)
	}
	if !appErr.HasRoute("RemoveOutput.FileStoreFailure") {
		t.Errorf("unexpected failure path: %s", appErr.Route())
	}
}

func writeFile(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
}
