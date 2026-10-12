package civitai

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/rm4n0s/errors"
)

// Target is a ComfyUI server that models can be uploaded to. It is the
// comfyui-model-crud extension of the server, which comfy.Client reaches.
type Target interface {
	// UploadModel stores size bytes read from r as <subfolder>/<filename> in a
	// model folder of the server and returns the path it has there.
	UploadModel(ctx context.Context, folder, subfolder, filename string, size int64, r io.Reader) (path string, err error)
}

// loraFolder is the model folder of ComfyUI that holds LoRAs.
const loraFolder = "loras"

// Install uploads a model to the server, into its `loras` folder below the
// sub-folder, under the name <id>.safetensors, or <id>-<version>.safetensors
// for a version that was asked for (SPEC §9). The server keeps the file out of
// ComfyUI's sight until it is complete, and a model that is already there is
// not sent again.
func (a *Adapter) Install(ctx context.Context, m Model, target Target, subfolder string) (string, error) {
	return Install(ctx, m, target, subfolder)
}

// Install is the function behind Adapter.Install.
func Install(ctx context.Context, m Model, target Target, subfolder string) (string, error) {
	if err := CheckSubfolder(subfolder); err != nil {
		return "", err
	}
	src, err := os.Open(m.FilePath)
	if err != nil {
		return "", errors.NewErr("FileStoreFailure", err).SetMetadata("path", m.FilePath, "operation", "open")
	}
	defer func() { _ = src.Close() }() // read side only
	st, err := src.Stat()
	if err != nil {
		return "", errors.NewErr("FileStoreFailure", err).SetMetadata("path", m.FilePath, "operation", "stat")
	}
	name := ModelName(m.ID, m.Requested, subfolder)
	if _, err := target.UploadModel(ctx, loraFolder, subfolder, filepath.Base(name), st.Size(), src); err != nil {
		return "", err
	}
	return name, nil
}

// CheckSubfolder accepts an empty sub-folder or a relative path of plain
// segments, so the upload cannot leave the `loras` folder.
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
