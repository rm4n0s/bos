package comfy

import (
	"io/fs"
	"os"
	"path/filepath"

	"github.com/rm4n0s/errors"
)

// RemoveOutput deletes a file ComfyUI saved, once this program has its copy
// (SPEC §8.2). ComfyUI has no call for it, so the file is removed from the
// `output` folder of the installation at root, which this program must be able
// to write to. Only files of type `output` are touched: ComfyUI cleans up its
// temporary files itself, and an input file is never ours to delete.
//
// The path is resolved inside `<root>/output` and cannot leave it, whatever the
// sub-folder or the file name say, not even through a link. A file that is
// already gone is not an error.
func RemoveOutput(root string, f OutputFile) error {
	if f.Type != "output" {
		return nil
	}
	dir := filepath.Join(root, "output")
	path := filepath.Join(dir, filepath.FromSlash(f.Subfolder), filepath.FromSlash(f.Filename))
	out, err := os.OpenRoot(dir)
	if err != nil {
		return errors.NewErr("FileStoreFailure", err).SetMetadata("path", path, "operation", "remove")
	}
	defer func() { _ = out.Close() }()
	if err := out.Remove(filepath.Join(filepath.FromSlash(f.Subfolder), filepath.FromSlash(f.Filename))); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return errors.NewErr("FileStoreFailure", err).SetMetadata("path", path, "operation", "remove")
	}
	return nil
}
