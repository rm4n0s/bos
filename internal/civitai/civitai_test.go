package civitai

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rm4n0s/bos/internal/comfy"
	"github.com/rm4n0s/bos/internal/comfy/comfytest"
	"github.com/rm4n0s/errors"
)

// fakeSite serves two models: 7 (a LoRA with two versions) and 8 whose
// download is refused. Any other id is not found.
type fakeSite struct {
	srv       *httptest.Server
	downloads atomic.Int64
	tokens    []string
	content   []byte
	badHash   bool
}

func newSite(t *testing.T) *fakeSite {
	t.Helper()
	s := &fakeSite{content: []byte(strings.Repeat("lora-weights-", 100))}
	mux := http.NewServeMux()
	hash := func() string { h := sha256.Sum256(s.content); return hex.EncodeToString(h[:]) }
	model := func(id int, dl string) map[string]any {
		h := hash()
		if s.badHash {
			h = strings.Repeat("0", 64)
		}
		return map[string]any{
			"id": id, "name": "Dancing Fox", "type": "LORA", "tags": []string{"animal", "dance"}, "description": "<p>A fox that dances</p>",
			"modelVersions": []any{
				map[string]any{"id": 70, "name": "v1", "baseModel": "SDXL 1.0", "trainedWords": []string{"old"}, "files": []any{}},
				map[string]any{"id": 71, "name": "v2", "baseModel": "Illustrious", "trainedWords": []string{"dancing fox", "sparkles"}, "files": []any{
					map[string]any{"name": "fox.safetensors", "sizeKB": float64(len(s.content)) / 1024, "type": "Model", "primary": true, "downloadUrl": s.srv.URL + dl,
						"hashes": map[string]string{"SHA256": strings.ToUpper(h)}},
				}},
			},
		}
	}
	mux.HandleFunc("GET /api/v1/models/{id}", func(w http.ResponseWriter, r *http.Request) {
		s.tokens = append(s.tokens, r.Header.Get("Authorization"))
		switch r.PathValue("id") {
		case "7":
			_ = json.NewEncoder(w).Encode(model(7, "/api/download/models/71"))
		case "8":
			_ = json.NewEncoder(w).Encode(model(8, "/api/download/models/80"))
		case "9":
			_, _ = w.Write([]byte("not json"))
		case "10":
			w.Header().Set("Retry-After", "7")
			w.WriteHeader(http.StatusTooManyRequests)
		case "11":
			_ = json.NewEncoder(w).Encode(map[string]any{"id": 11, "name": "Empty", "modelVersions": []any{}})
		default:
			http.NotFound(w, r)
		}
	})
	mux.HandleFunc("GET /api/download/models/{v}", func(w http.ResponseWriter, r *http.Request) {
		s.downloads.Add(1)
		if r.PathValue("v") == "80" {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		_, _ = w.Write(s.content)
	})
	s.srv = httptest.NewServer(mux)
	t.Cleanup(s.srv.Close)
	return s
}

func (s *fakeSite) adapter(token string) *Adapter {
	return New(Options{Token: token, Domain: "civitai.test", SiteURL: s.srv.URL, MaxRetries: 1, RetryBackoff: time.Millisecond})
}

func wantTag(t *testing.T, err error, tag string) *errors.Error {
	t.Helper()
	appErr, ok := errors.FromError(err)
	if !ok || appErr.Tag != tag {
		t.Fatalf("err = %v, want tag %s", err, tag)
	}
	return appErr
}

func TestDownload(t *testing.T) {
	site := newSite(t)
	a := site.adapter("tok")
	dir := t.TempDir()

	m, err := a.Download(t.Context(), 7, 0, dir)
	if err != nil {
		t.Fatal(err)
	}
	if m.ID != 7 || m.VersionID != 71 || m.Name != "Dancing Fox" || m.BaseModel != "Illustrious" || m.Skipped ||
		len(m.TriggerWords) != 2 || m.TriggerWords[0] != "dancing fox" || len(m.Tags) != 2 || m.Size != int64(len(site.content)) {
		t.Errorf("model = %+v", m)
	}
	if m.FilePath != filepath.Join(dir, "7.safetensors") || m.MetadataPath != filepath.Join(dir, "7.json") {
		t.Errorf("paths = %s %s", m.FilePath, m.MetadataPath)
	}
	if b, _ := os.ReadFile(m.FilePath); string(b) != string(site.content) {
		t.Error("the file content is wrong")
	}
	if b, _ := os.ReadFile(m.MetadataPath); !strings.Contains(string(b), `"triggerWords"`) {
		t.Errorf("metadata = %s", b)
	}
	// The token goes to the model request, never from the environment.
	if len(site.tokens) == 0 || site.tokens[0] != "Bearer tok" {
		t.Errorf("tokens = %v", site.tokens)
	}

	// Again: nothing is downloaded.
	again, err := a.Download(t.Context(), 7, 0, dir)
	if err != nil || !again.Skipped || site.downloads.Load() != 1 {
		t.Errorf("second download: %+v, %v, %d downloads", again, err, site.downloads.Load())
	}
}

func TestDownloadFailures(t *testing.T) {
	site := newSite(t)
	a := site.adapter("tok")
	dir := t.TempDir()
	tests := []struct {
		name string
		id   int
		tag  string
	}{
		{"unknown model", 99, "CivitaiModelNotFound"},
		{"download refused", 8, "CivitaiDownloadFailed"},
		{"not json", 9, "CivitaiMetadataInvalid"},
		{"rate limited", 10, "CivitaiRateLimited"},
		{"no versions", 11, "CivitaiDownloadFailed"},
		{"bad id", -1, "CivitaiDownloadFailed"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := a.Download(t.Context(), tc.id, 0, dir)
			appErr := wantTag(t, err, tc.tag)
			if tc.id > 0 && appErr.Metadata["model_id"] != tc.id {
				t.Errorf("metadata = %v", appErr.Metadata)
			}
		})
	}
	// A failed download leaves nothing in the cache.
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".safetensors") || strings.HasSuffix(e.Name(), ".tmp") {
			t.Errorf("a failed download left %s", e.Name())
		}
	}

	site.badHash = true
	_, err := a.Download(t.Context(), 7, 0, dir)
	_ = wantTag(t, err, "CivitaiDownloadFailed")

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = a.Download(ctx, 7, 0, dir)
	_ = wantTag(t, err, "CivitaiDownloadFailed")
}

func TestStatusErrorsCarryTheStatus(t *testing.T) {
	site := newSite(t)
	_, err := site.adapter("tok").Download(t.Context(), 8, 0, t.TempDir())
	appErr := wantTag(t, err, "CivitaiDownloadFailed")
	if appErr.Metadata["status"] != http.StatusForbidden {
		t.Errorf("metadata = %v", appErr.Metadata)
	}
}

func TestRateLimitedCarriesRetryAfter(t *testing.T) {
	site := newSite(t)
	_, err := site.adapter("tok").Download(t.Context(), 10, 0, t.TempDir())
	appErr := wantTag(t, err, "CivitaiRateLimited")
	if appErr.Metadata["retry_after"] != "7s" {
		t.Errorf("metadata = %v", appErr.Metadata)
	}
}

func TestNotConfigured(t *testing.T) {
	site := newSite(t)
	t.Setenv("CIVITAI_TOKEN", "from-the-environment") // must not be used
	a := site.adapter("")
	if a.Configured() {
		t.Error("an adapter without a token says it is configured")
	}
	_, err := a.Download(t.Context(), 7, 0, t.TempDir())
	_ = wantTag(t, err, "CivitaiNotConfigured")
	if len(site.tokens) != 0 || site.downloads.Load() != 0 {
		t.Error("something was attempted without a token")
	}
	_, err = site.adapter("tok").Download(t.Context(), 7, 0, "")
	_ = wantTag(t, err, "CivitaiNotConfigured")
}

// fakeTarget is a ComfyUI server with the extension: it keeps what it is sent.
type fakeTarget struct {
	files map[string][]byte
	calls int
	err   error
}

func (f *fakeTarget) UploadModel(_ context.Context, folder, subfolder, filename string, size int64, r io.Reader) (string, error) {
	f.calls++
	if f.err != nil {
		return "", f.err
	}
	b, err := io.ReadAll(r)
	if err != nil || int64(len(b)) != size {
		return "", errors.New("ModelCrudRejected", "the size does not match", "size", size, "read", len(b))
	}
	path := folder + "/" + filename
	if subfolder != "" {
		path = folder + "/" + subfolder + "/" + filename
	}
	if f.files == nil {
		f.files = map[string][]byte{}
	}
	f.files[path] = b
	return strings.TrimPrefix(path, folder+"/"), nil
}

func TestInstall(t *testing.T) {
	site := newSite(t)
	m, err := site.adapter("tok").Download(t.Context(), 7, 0, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	a := site.adapter("tok")
	target := &fakeTarget{}

	name, err := a.Install(t.Context(), m, target, "civitai/foxes")
	if err != nil || name != "civitai/foxes/7.safetensors" {
		t.Fatalf("name = %q, err = %v", name, err)
	}
	if got := target.files["loras/civitai/foxes/7.safetensors"]; string(got) != string(site.content) {
		t.Errorf("uploaded %d bytes, want %d", len(got), len(site.content))
	}
	// No sub-folder.
	name, err = a.Install(t.Context(), m, target, "")
	if err != nil || name != "7.safetensors" || target.files["loras/7.safetensors"] == nil {
		t.Errorf("flat install: %q %v", name, err)
	}
	// A version that was asked for has its own name.
	m.Requested = 71
	name, err = a.Install(t.Context(), m, target, "")
	if err != nil || name != "7-71.safetensors" || target.files["loras/7-71.safetensors"] == nil {
		t.Errorf("version install: %q %v", name, err)
	}
}

func TestInstallRefusals(t *testing.T) {
	site := newSite(t)
	m, err := site.adapter("tok").Download(t.Context(), 7, 0, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	a := site.adapter("tok")
	target := &fakeTarget{}
	for _, sub := range []string{"..", "../x", "a/../b", "/abs", "a//b", `a\b`, "C:x", "a/./b", "x\x00y"} {
		_, err := a.Install(t.Context(), m, target, sub)
		_ = wantTag(t, err, "InvalidRequest")
	}
	_, err = a.Install(t.Context(), Model{ID: 7, FilePath: filepath.Join(t.TempDir(), "missing.safetensors")}, target, "x")
	_ = wantTag(t, err, "FileStoreFailure")
	if target.calls != 0 {
		t.Errorf("the server was contacted %d times for a request that was refused", target.calls)
	}
	// What the server says is not hidden.
	target.err = errors.New("ModelCrudMissing", "no extension")
	_, err = a.Install(t.Context(), m, target, "x")
	_ = wantTag(t, err, "ModelCrudMissing")
}

func TestModelName(t *testing.T) {
	if ModelName(7, 0, "") != "7.safetensors" || ModelName(7, 0, "a/b") != "a/b/7.safetensors" ||
		ModelName(7, 71, "") != "7-71.safetensors" || ModelName(7, 71, "a/b") != "a/b/7-71.safetensors" {
		t.Error("model names are wrong")
	}
}

func TestDownloadAVersion(t *testing.T) {
	site := newSite(t)
	a := site.adapter("tok")
	dir := t.TempDir()

	m, err := a.Download(t.Context(), 7, 71, dir)
	if err != nil {
		t.Fatal(err)
	}
	// The version has a folder of its own, so it does not overwrite the newest one.
	if m.ID != 7 || m.VersionID != 71 || m.Requested != 71 || m.FilePath != filepath.Join(dir, "versions", "71", "7.safetensors") {
		t.Errorf("model = %+v", m)
	}
	if _, err := os.Stat(filepath.Join(dir, "7.safetensors")); !os.IsNotExist(err) {
		t.Errorf("the newest version's place was written: %v", err)
	}
	newest, err := a.Download(t.Context(), 7, 0, dir)
	if err != nil || newest.Requested != 0 || newest.Skipped || newest.FilePath != filepath.Join(dir, "7.safetensors") {
		t.Errorf("newest = %+v, %v", newest, err)
	}

	// A version the model does not have, and one without a file.
	for _, version := range []int{99, 70} {
		_, err := a.Download(t.Context(), 7, version, dir)
		appErr := wantTag(t, err, "CivitaiDownloadFailed")
		if appErr.Metadata["model_id"] != 7 || appErr.Metadata["version_id"] != version {
			t.Errorf("version %d: metadata = %v", version, appErr.Metadata)
		}
	}
}

// A model goes from the cache to a ComfyUI server over HTTP, through the
// comfyui-model-crud extension, in the chunks the server asks for.
func TestInstallToAServerWithTheExtension(t *testing.T) {
	ext := comfytest.New(0)
	t.Cleanup(ext.Close)
	server, err := comfy.NewServer(ext.URL, comfy.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := server.Close(); err != nil {
			t.Error(err)
		}
	})
	site := newSite(t)
	m, err := site.adapter("tok").Download(t.Context(), 7, 0, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	name, err := site.adapter("tok").Install(t.Context(), m, server, "civitai")
	if err != nil || name != "civitai/7.safetensors" {
		t.Fatalf("name = %q, err = %v", name, err)
	}
	if got := ext.Models()["loras/civitai/7.safetensors"]; string(got) != string(site.content) {
		t.Errorf("the server holds %d bytes, want %d", len(got), len(site.content))
	}
}
