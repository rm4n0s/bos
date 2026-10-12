package modelcrud

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rm4n0s/errors"
)

// fakeExtension is comfyui-model-crud as far as the client uses it. It keeps
// the files it holds in memory and records every request.
type fakeExtension struct {
	mu        sync.Mutex
	chunkSize int64
	models    map[string][]byte // "folder/path" -> content
	output    map[string]bool   // path -> present
	sessions  map[string]*fakeSession
	calls     []string
	// failChunks makes the next n chunk requests answer with status failStatus.
	failChunks, failStatus int
	// disabled makes uploads and deletes answer with that error code.
	disabled string
	// absent makes every route a plain 404, like a ComfyUI without the extension.
	absent bool
}

type fakeSession struct {
	folder, path string
	size         int64
	chunks       map[int64][]byte
}

func newFake() *fakeExtension {
	return &fakeExtension{chunkSize: 4, models: map[string][]byte{}, output: map[string]bool{}, sessions: map[string]*fakeSession{}}
}

func (f *fakeExtension) fail(w http.ResponseWriter, status int, code, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": code, "message": msg})
}

func (f *fakeExtension) reply(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func (f *fakeExtension) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, r.Method+" "+r.URL.RequestURI())
	if f.absent {
		http.NotFound(w, r)
		return
	}
	p := r.URL.Path
	switch {
	case r.Method == http.MethodGet && p == "/model_crud/config":
		f.reply(w, 200, Config{Version: "0.1.0", UploadEnabled: true, DeleteEnabled: true, ChunkSize: f.chunkSize, MediaFolders: []string{"input", "output"}})
	case r.Method == http.MethodDelete && p == "/model_crud/output":
		if f.disabled != "" {
			f.fail(w, 403, f.disabled, "disabled")
			return
		}
		path := r.URL.Query().Get("path")
		if !f.output[path] {
			f.fail(w, 404, "file_not_found", "no such file")
			return
		}
		delete(f.output, path)
		f.reply(w, 200, map[string]any{"area": "output", "path": path, "deleted": true})
	case r.Method == http.MethodGet && p == "/model_crud/models/loras":
		var files []map[string]any
		for k, v := range f.models {
			files = append(files, map[string]any{"path": strings.TrimPrefix(k, "loras/"), "size": len(v)})
		}
		f.reply(w, 200, map[string]any{"folder": "loras", "files": files})
	case r.Method == http.MethodPost && p == "/model_crud/uploads":
		f.create(w, r)
	case strings.HasPrefix(p, "/model_crud/uploads/"):
		f.session(w, r, strings.TrimPrefix(p, "/model_crud/uploads/"))
	default:
		http.NotFound(w, r)
	}
}

func (f *fakeExtension) create(w http.ResponseWriter, r *http.Request) {
	if f.disabled != "" {
		f.fail(w, 403, f.disabled, "disabled")
		return
	}
	var in struct {
		Folder, Filename, Subfolder string
		Size                        int64
		Overwrite                   bool
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		f.fail(w, 400, "bad_request", err.Error())
		return
	}
	path := in.Filename
	if in.Subfolder != "" {
		path = in.Subfolder + "/" + in.Filename
	}
	if _, there := f.models[in.Folder+"/"+path]; there && !in.Overwrite {
		f.fail(w, 409, "file_exists", "the file exists")
		return
	}
	id := "up" + strconv.Itoa(len(f.sessions)+1) + strings.Repeat("x", 20)
	f.sessions[id] = &fakeSession{folder: in.Folder, path: path, size: in.Size, chunks: map[int64][]byte{}}
	total := (in.Size + f.chunkSize - 1) / f.chunkSize
	f.reply(w, 201, map[string]any{"upload_id": id, "path": path, "size": in.Size, "chunk_size": f.chunkSize, "total_chunks": total})
}

func (f *fakeExtension) session(w http.ResponseWriter, r *http.Request, rest string) {
	id, tail, _ := strings.Cut(rest, "/")
	s, ok := f.sessions[id]
	if !ok {
		f.fail(w, 404, "unknown_upload", "no such upload")
		return
	}
	switch {
	case r.Method == http.MethodDelete && tail == "":
		delete(f.sessions, id)
		f.reply(w, 200, map[string]any{"cancelled": true})
	case r.Method == http.MethodPut && strings.HasPrefix(tail, "chunks/"):
		if f.failChunks > 0 {
			f.failChunks--
			f.fail(w, f.failStatus, "internal_error", "try later")
			return
		}
		i, _ := strconv.ParseInt(strings.TrimPrefix(tail, "chunks/"), 10, 64)
		b, _ := io.ReadAll(r.Body)
		s.chunks[i] = b
		f.reply(w, 200, map[string]any{"index": i, "received_chunks": len(s.chunks)})
	case r.Method == http.MethodPost && tail == "complete":
		var all []byte
		for i := int64(0); i < int64(len(s.chunks)); i++ {
			all = append(all, s.chunks[i]...)
		}
		if int64(len(all)) != s.size {
			f.fail(w, 409, "incomplete_upload", "chunks are missing")
			return
		}
		f.models[s.folder+"/"+s.path] = all
		delete(f.sessions, id)
		f.reply(w, 201, map[string]any{"folder": s.folder, "path": s.path, "size": s.size})
	default:
		http.NotFound(w, r)
	}
}

func (f *fakeExtension) called(prefix string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, c := range f.calls {
		if strings.HasPrefix(c, prefix) {
			n++
		}
	}
	return n
}

func newClient(t *testing.T, f *fakeExtension) *Client {
	t.Helper()
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	c, err := New(srv.URL, Options{Backoff: func(int) time.Duration { return time.Millisecond }})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// wantTag checks that err is an application error with the tag, raised below
// the entry point.
func wantTag(t *testing.T, err error, entry, tag string) {
	t.Helper()
	appErr, ok := errors.FromError(err)
	if !ok {
		t.Fatalf("expected *errors.Error, got %T (%v)", err, err)
	}
	if appErr.Tag != tag || !appErr.HasRoute(entry) || !strings.HasSuffix(appErr.Route(), "."+tag) {
		t.Errorf("unexpected failure path: %s (tag %s)\nwant %s ... %s", appErr.Route(), appErr.Tag, entry, tag)
	}
}

func TestNewRejectsABadAddress(t *testing.T) {
	for _, host := range []string{"", "localhost:8188", "ftp://x", "http://"} {
		_, err := New(host, Options{})
		wantTag(t, err, "New", "ServerHostInvalid")
	}
}

func TestConfig(t *testing.T) {
	c := newClient(t, newFake())
	cfg, err := c.Config(t.Context())
	if err != nil || !cfg.UploadEnabled || !cfg.DeleteEnabled || cfg.Version != "0.1.0" {
		t.Errorf("Config = %+v, %v", cfg, err)
	}
}

func TestDeleteOutput(t *testing.T) {
	tests := []struct {
		name    string
		present bool
		path    string
	}{
		{name: "a file of the output folder", present: true, path: "a.png"},
		{name: "a file in a sub-folder", present: true, path: "day/one/a b&c.png"},
		{name: "a file that is already gone", present: false, path: "gone.png"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newFake()
			f.output[tc.path] = tc.present
			f.output["other.png"] = true
			if err := newClient(t, f).DeleteOutput(t.Context(), tc.path); err != nil {
				t.Fatal(err)
			}
			if f.output[tc.path] || !f.output["other.png"] {
				t.Errorf("output = %v", f.output)
			}
		})
	}
}

func TestDeleteOutputFailures(t *testing.T) {
	tests := []struct {
		name  string
		setup func(*fakeExtension)
		tag   string
	}{
		{name: "no extension on the server", setup: func(f *fakeExtension) { f.absent = true }, tag: "ModelCrudMissing"},
		{name: "deletes are switched off", setup: func(f *fakeExtension) { f.disabled = "delete_disabled" }, tag: "ModelCrudDisabled"},
		{name: "the extension refuses the path", setup: func(f *fakeExtension) { f.disabled = "path_outside_root" }, tag: "ModelCrudRejected"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newFake()
			tc.setup(f)
			err := newClient(t, f).DeleteOutput(t.Context(), "a.png")
			wantTag(t, err, "Client.DeleteOutput", tc.tag)
		})
	}
}

func TestUnreachableAndCancelled(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close()
	c, err := New(url, Options{})
	if err != nil {
		t.Fatal(err)
	}
	wantTag(t, c.DeleteOutput(t.Context(), "a.png"), "Client.DeleteOutput", "ServerUnreachable")

	live := newClient(t, newFake())
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	wantTag(t, live.DeleteOutput(ctx, "a.png"), "Client.DeleteOutput", "ComfyUICallCancelled")
}

func TestGatewayErrorIsAnUnreachableServer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusBadGateway) }))
	t.Cleanup(srv.Close)
	c, err := New(srv.URL, Options{})
	if err != nil {
		t.Fatal(err)
	}
	wantTag(t, c.DeleteOutput(t.Context(), "a.png"), "Client.DeleteOutput", "ServerUnreachable")
}

func TestUploadModel(t *testing.T) {
	tests := []struct {
		name      string
		subfolder string
		content   string
		path      string
	}{
		{name: "several chunks", content: "0123456789", path: "x.safetensors"},
		{name: "exactly one chunk", content: "abcd", path: "x.safetensors"},
		{name: "a sub-folder", subfolder: "civitai/styles", content: "hello world", path: "civitai/styles/x.safetensors"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newFake()
			path, err := newClient(t, f).UploadModel(t.Context(), "loras", tc.subfolder, "x.safetensors", int64(len(tc.content)), strings.NewReader(tc.content))
			if err != nil || path != tc.path {
				t.Fatalf("UploadModel = %q, %v", path, err)
			}
			if got := string(f.models["loras/"+tc.path]); got != tc.content {
				t.Errorf("stored %q, want %q", got, tc.content)
			}
		})
	}
}

func TestUploadModelAlreadyThere(t *testing.T) {
	f := newFake()
	f.models["loras/a/x.safetensors"] = []byte("12345")
	c := newClient(t, f)
	path, err := c.UploadModel(t.Context(), "loras", "a", "x.safetensors", 5, strings.NewReader("12345"))
	if err != nil || path != "a/x.safetensors" {
		t.Fatalf("UploadModel = %q, %v", path, err)
	}
	if n := f.called("PUT"); n != 0 {
		t.Errorf("%d chunks were sent for a file that is there", n)
	}
}

func TestUploadModelReplacesAFileOfAnotherSize(t *testing.T) {
	f := newFake()
	f.models["loras/x.safetensors"] = []byte("old")
	_, err := newClient(t, f).UploadModel(t.Context(), "loras", "", "x.safetensors", 9, strings.NewReader("new bytes"))
	if err != nil || string(f.models["loras/x.safetensors"]) != "new bytes" {
		t.Errorf("UploadModel = %v, stored %q", err, f.models["loras/x.safetensors"])
	}
}

func TestUploadModelRetriesAChunk(t *testing.T) {
	for _, status := range []int{http.StatusTooManyRequests, http.StatusInternalServerError} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			f := newFake()
			f.failChunks, f.failStatus = 2, status
			_, err := newClient(t, f).UploadModel(t.Context(), "loras", "", "x.safetensors", 6, strings.NewReader("abcdef"))
			if err != nil || string(f.models["loras/x.safetensors"]) != "abcdef" {
				t.Errorf("UploadModel = %v, stored %q", err, f.models["loras/x.safetensors"])
			}
		})
	}
}

func TestUploadModelGivesUpAndCancels(t *testing.T) {
	f := newFake()
	f.failChunks, f.failStatus = 100, http.StatusServiceUnavailable
	_, err := newClient(t, f).UploadModel(t.Context(), "loras", "", "x.safetensors", 6, strings.NewReader("abcdef"))
	wantTag(t, err, "Client.UploadModel", "ModelCrudRejected")
	if n := f.called("PUT"); n != 4 {
		t.Errorf("%d attempts, want 4", n)
	}
	if n := f.called("DELETE /model_crud/uploads/"); n != 1 {
		t.Errorf("the failed upload was cancelled %d times, want 1", n)
	}
	if len(f.sessions) != 0 {
		t.Errorf("%d sessions left on the server", len(f.sessions))
	}
}

func TestUploadModelFailures(t *testing.T) {
	t.Run("no extension", func(t *testing.T) {
		f := newFake()
		f.absent = true
		_, err := newClient(t, f).UploadModel(t.Context(), "loras", "", "x.safetensors", 1, strings.NewReader("a"))
		wantTag(t, err, "Client.UploadModel", "ModelCrudMissing")
	})
	t.Run("uploads are switched off", func(t *testing.T) {
		f := newFake()
		f.disabled = "uploads_disabled"
		_, err := newClient(t, f).UploadModel(t.Context(), "loras", "", "x.safetensors", 1, strings.NewReader("a"))
		wantTag(t, err, "Client.UploadModel", "ModelCrudDisabled")
	})
	t.Run("the source ends early", func(t *testing.T) {
		f := newFake()
		_, err := newClient(t, f).UploadModel(t.Context(), "loras", "", "x.safetensors", 10, bytes.NewReader([]byte("abc")))
		wantTag(t, err, "Client.sendChunks", "FileStoreFailure")
		if len(f.sessions) != 0 {
			t.Errorf("%d sessions left on the server", len(f.sessions))
		}
	})
}
