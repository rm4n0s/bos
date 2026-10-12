package comfy

import (
	"strings"
	"testing"

	"github.com/rm4n0s/bos/internal/comfy/comfytest"
)

// The adapter reaches the output and models folders of the server through the
// comfyui-model-crud extension.
func TestDeleteOutputGoesThroughTheExtension(t *testing.T) {
	ext := comfytest.New(0)
	t.Cleanup(ext.Close)
	s, err := NewServer(ext.URL, Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Error(err)
		}
	})

	for _, f := range []OutputFile{
		{Filename: "a.png", Type: "output"},
		{Filename: "b c.png", Subfolder: "day/one", Type: "output"},
		{Filename: "t.png", Type: "temp"},  // ComfyUI cleans these up itself
		{Filename: "i.png", Type: "input"}, // never ours to delete
	} {
		if err := s.DeleteOutput(t.Context(), f); err != nil {
			t.Fatal(err)
		}
	}
	got := strings.Join(ext.DeletedOutputs(), "|")
	if got != "a.png|day/one/b c.png" {
		t.Errorf("deleted %q", got)
	}
}

func TestUploadModelGoesThroughTheExtension(t *testing.T) {
	ext := comfytest.New(0)
	t.Cleanup(ext.Close)
	s, err := NewServer(ext.URL, Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Error(err)
		}
	})

	content := strings.Repeat("lora", 20) // several chunks
	path, err := s.UploadModel(t.Context(), "loras", "civitai", "7.safetensors", int64(len(content)), strings.NewReader(content))
	if err != nil || path != "civitai/7.safetensors" {
		t.Fatalf("UploadModel = %q, %v", path, err)
	}
	if got := string(ext.Models()["loras/civitai/7.safetensors"]); got != content {
		t.Errorf("the server holds %q", got)
	}
	// The model is there now: asking again sends nothing.
	path, err = s.UploadModel(t.Context(), "loras", "civitai", "7.safetensors", int64(len(content)), strings.NewReader("not read"))
	if err != nil || path != "civitai/7.safetensors" {
		t.Errorf("second UploadModel = %q, %v", path, err)
	}
	if got := string(ext.Models()["loras/civitai/7.safetensors"]); got != content {
		t.Errorf("the second upload changed the model: %q", got)
	}
}

// A server without the extension says so with a tag of its own.
func TestServerWithoutTheExtension(t *testing.T) {
	f := newFakeComfy(t)
	s := newServer(t, f)
	wantTag(t, s.DeleteOutput(t.Context(), OutputFile{Filename: "a.png", Type: "output"}), "Server.DeleteOutput", "ModelCrudMissing")
	_, err := s.UploadModel(t.Context(), "loras", "", "a.safetensors", 1, strings.NewReader("a"))
	wantTag(t, err, "Server.UploadModel", "ModelCrudMissing")
}
