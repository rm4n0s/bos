package civitai

import (
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/rm4n0s/errors"
)

// Install copies a model into <comfyPath>/models/loras/<subfolder>/ under the
// name <id>.safetensors, or <id>-<version>.safetensors for a version that was
// asked for (SPEC §9). The application needs file access to that
// directory (it runs on the same machine, or the folder is shared). The file is
// copied to a temporary name and renamed, so ComfyUI never sees half a model.
func (a *Adapter) Install(m Model, comfyPath, subfolder string) (string, error) {
	return Install(m, comfyPath, subfolder)
}

// Install is the function behind Adapter.Install.
func Install(m Model, comfyPath, subfolder string) (string, error) {
	loras, err := CheckTarget(comfyPath, subfolder)
	if err != nil {
		return "", err
	}
	dir := filepath.Join(loras, filepath.FromSlash(subfolder))
	if err := os.MkdirAll(dir, 0o755); err != nil { //nolint:gosec // ComfyUI, a program of the same user, must be able to read the folder
		return "", errors.NewErr("FileStoreFailure", err).SetMetadata("path", dir, "operation", "mkdir")
	}
	name := ModelName(m.ID, m.Requested, subfolder)
	target := filepath.Join(dir, filepath.Base(name))
	if st, err := os.Stat(target); err == nil && st.Size() == m.Size && m.Size > 0 {
		return name, nil // already there
	}
	if err := copyAtomically(m.FilePath, target); err != nil {
		return "", err
	}
	return name, nil
}

// CheckTarget checks, before anything is downloaded, that an import can be
// installed: the sub-folder is plain, and <comfyPath>/models/loras exists. It
// returns that folder.
func CheckTarget(comfyPath, subfolder string) (string, error) {
	if err := CheckSubfolder(subfolder); err != nil {
		return "", err
	}
	loras := filepath.Join(comfyPath, "models", "loras")
	if comfyPath == "" {
		return "", errors.New("InvalidRequest", "the path of the ComfyUI installation is required", "field", "comfyui_path")
	}
	if st, err := os.Stat(loras); err != nil || !st.IsDir() {
		return "", errors.New("InvalidRequest", "the folder models/loras was not found in the ComfyUI folder you gave; it must be a folder this program can write to",
			"field", "comfyui_path", "path", loras)
	}
	return loras, nil
}

// CheckSubfolder accepts an empty sub-folder or a relative path of plain
// segments, so the copy cannot leave models/loras.
func CheckSubfolder(subfolder string) error {
	if subfolder == "" {
		return nil
	}
	for _, seg := range strings.Split(subfolder, "/") {
		if seg == "" || seg == "." || seg == ".." || strings.ContainsAny(seg, `\:`) {
			return errors.New("InvalidRequest", "the LoRA sub-folder must be a relative path such as civitai or styles/anime", "subfolder", subfolder)
		}
		for _, r := range seg {
			if r < ' ' || r == 0x7f {
				return errors.New("InvalidRequest", "the LoRA sub-folder must be a relative path such as civitai or styles/anime", "subfolder", subfolder)
			}
		}
	}
	return nil
}

func copyAtomically(from, to string) (err error) {
	src, err := os.Open(from)
	if err != nil {
		return errors.NewErr("FileStoreFailure", err).SetMetadata("path", from, "operation", "open")
	}
	defer func() { _ = src.Close() }() // read side only
	tmp, err := os.CreateTemp(filepath.Dir(to), ".install-*.tmp")
	if err != nil {
		return errors.NewErr("FileStoreFailure", err).SetMetadata("path", to, "operation", "create")
	}
	defer func() {
		if err != nil {
			_ = tmp.Close()
			_ = os.Remove(tmp.Name())
		}
	}()
	if _, err = io.Copy(tmp, src); err != nil {
		return errors.NewErr("FileStoreFailure", err).SetMetadata("path", to, "operation", "copy")
	}
	if err = tmp.Close(); err != nil {
		return errors.NewErr("FileStoreFailure", err).SetMetadata("path", to, "operation", "close")
	}
	if err = os.Chmod(tmp.Name(), 0o644); err != nil { //nolint:gosec // see MkdirAll above
		return errors.NewErr("FileStoreFailure", err).SetMetadata("path", to, "operation", "chmod")
	}
	if err = os.Rename(tmp.Name(), to); err != nil {
		return errors.NewErr("FileStoreFailure", err).SetMetadata("path", to, "operation", "rename")
	}
	return nil
}
