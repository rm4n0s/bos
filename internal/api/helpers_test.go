package api

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rm4n0s/bos/internal/comfy"
	"github.com/rm4n0s/bos/internal/domain"
	"github.com/rm4n0s/bos/internal/events"
	"github.com/rm4n0s/bos/internal/files"
	"github.com/rm4n0s/bos/internal/service"
	"github.com/rm4n0s/bos/internal/store"
	"github.com/rm4n0s/bos/internal/workflow"
	"github.com/rm4n0s/errors"
)

func check(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// pngBytes is a valid 1x1 white PNG.
var pngBytes = encodePNG(image.NewUniform(color.White), 1, 1)

// encodePNG draws an image of the given size and encodes it.
func encodePNG(src image.Image, w, h int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(img, img.Bounds(), src, image.Point{}, draw.Src)
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		panic(err)
	}
	return buf.Bytes()
}

// twoColourPNG is white with a red left half and a blue right half.
func twoColourPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	draw.Draw(img, img.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	draw.Draw(img, image.Rect(0, 0, 2, 4), image.NewUniform(color.RGBA{R: 255, A: 255}), image.Point{}, draw.Src)
	draw.Draw(img, image.Rect(2, 0, 4, 4), image.NewUniform(color.RGBA{B: 255, A: 255}), image.Point{}, draw.Src)
	var buf bytes.Buffer
	check(t, png.Encode(&buf, img))
	return buf.Bytes()
}

// testFleet reports every server online and knows two LoRAs.
type testFleet struct{}

func (testFleet) Available(domain.ID) bool { return true }
func (testFleet) Client(context.Context, domain.Server) (comfy.Client, error) {
	return loraClient{}, nil
}

type loraClient struct{}

func (loraClient) Ping(context.Context) error { return nil }
func (loraClient) QueuePrompt(context.Context, workflow.Graph) (string, []comfy.NodeWarning, error) {
	return "", nil, errors.New("Unsupported", "test client")
}
func (loraClient) Wait(context.Context, string) (comfy.Result, error) {
	return comfy.Result{}, errors.New("Unsupported", "test client")
}
func (loraClient) Fetch(context.Context, comfy.OutputFile) (io.ReadCloser, error) {
	return nil, errors.New("Unsupported", "test client")
}
func (loraClient) Upload(context.Context, string, io.Reader) (string, error) {
	return "", errors.New("Unsupported", "test client")
}
func (loraClient) ListModels(_ context.Context, folder string) ([]string, error) {
	if folder == "loras" {
		return []string{"dance.safetensors", "fox.safetensors"}, nil
	}
	return nil, nil
}
func (loraClient) Cancel(context.Context, string) error { return nil }
func (loraClient) Close() error                         { return nil }

// wakeLog counts wakes of the scheduler.
type wakeLog struct {
	mu sync.Mutex
	n  int
}

func (w *wakeLog) Wake() { w.mu.Lock(); w.n++; w.mu.Unlock() }

// testAPI is the REST API over a real in-memory store and temporary
// directories, with a fake fleet.
type testAPI struct {
	t      *testing.T
	api    *API
	srv    *httptest.Server
	db     *store.DB
	dirs   Dirs
	root   string
	events *events.Log
	logs   *strings.Builder
	probe  *http.ServeMux // resolves a request to the pattern of its route
	hit    map[string]bool
	mu     sync.Mutex
}

type option func(*Deps)

func newTestAPI(t *testing.T, opts ...option) *testAPI {
	t.Helper()
	db, err := store.Open(t.Context(), store.InMemory)
	check(t, err)
	t.Cleanup(func() { check(t, db.Close()) })
	check(t, db.Migrate(t.Context()))

	root := t.TempDir()
	dirs := Dirs{
		Results: filepath.Join(root, "results"), ControlNet: filepath.Join(root, "controlnet"), IPAdapter: filepath.Join(root, "ipadapter"),
		Regions: filepath.Join(root, "regions"), Thumbnails: filepath.Join(root, "thumbnails"), Downloads: filepath.Join(root, "downloads"),
	}
	fs, err := files.NewStore([]string{root})
	check(t, err)
	logs := &strings.Builder{}
	logger := slog.New(slog.NewTextHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	log := events.NewLog(100, nil)
	svc := service.New(service.Deps{
		Store: service.NewStore(db), Files: fs, Fleet: testFleet{}, Dispatcher: &wakeLog{}, Logger: logger,
		Dirs: service.Dirs{Results: dirs.Results, Regions: dirs.Regions, Downloads: dirs.Downloads},
	})
	deps := Deps{Services: svc, Events: log, Files: fs, Dirs: dirs, UploadLimit: 1 << 20, Logger: logger, KeepAlive: 50 * time.Millisecond}
	for _, o := range opts {
		o(&deps)
	}
	a := New(deps)
	srv := httptest.NewServer(a.Handler())
	t.Cleanup(srv.Close)
	t.Cleanup(a.Stop)

	probe := http.NewServeMux()
	for _, r := range a.Routes() {
		probe.HandleFunc(r.pattern(), func(http.ResponseWriter, *http.Request) {})
	}
	return &testAPI{t: t, api: a, srv: srv, db: db, dirs: dirs, root: root, events: log, logs: logs, probe: probe, hit: map[string]bool{}}
}

// reply is a decoded response.
type reply struct {
	Status int
	Header http.Header
	Body   []byte
	t      *testing.T
}

// json decodes the body into a generic value.
func (r reply) json() map[string]any {
	r.t.Helper()
	var m map[string]any
	if err := json.Unmarshal(r.Body, &m); err != nil {
		r.t.Fatalf("body is not a JSON object: %v\n%s", err, r.Body)
	}
	return m
}

// tag returns the tag of the error envelope.
func (r reply) tag() string {
	r.t.Helper()
	e, _ := r.json()["error"].(map[string]any)
	tag, _ := e["tag"].(string)
	return tag
}

func jsonNumber(v float64) string {
	b, _ := json.Marshal(int64(v))
	return string(b)
}

// request sends a request and records which route it hit.
func (a *testAPI) request(method, path string, body io.Reader, headers map[string]string) reply {
	a.t.Helper()
	req, err := http.NewRequestWithContext(a.t.Context(), method, a.srv.URL+path, body)
	check(a.t, err)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	if _, pattern := a.probe.Handler(req); pattern != "" {
		a.mu.Lock()
		a.hit[pattern] = true
		a.mu.Unlock()
	}
	res, err := http.DefaultClient.Do(req)
	check(a.t, err)
	defer func() { check(a.t, res.Body.Close()) }()
	b, err := io.ReadAll(res.Body)
	check(a.t, err)
	return reply{Status: res.StatusCode, Header: res.Header, Body: b, t: a.t}
}

// do sends a JSON request; a nil body sends none.
func (a *testAPI) do(method, path string, body any) reply {
	a.t.Helper()
	var r io.Reader
	headers := map[string]string{}
	switch v := body.(type) {
	case nil:
	case string:
		r = strings.NewReader(v)
		headers["Content-Type"] = "application/json"
	default:
		b, err := json.Marshal(v)
		check(a.t, err)
		r = bytes.NewReader(b)
		headers["Content-Type"] = "application/json"
	}
	return a.request(method, path, r, headers)
}

// expect fails the test unless the reply has the status, and the tag when the
// status is an error.
func (a *testAPI) expect(r reply, status int, tag string) reply {
	a.t.Helper()
	if r.Status != status || tag != "" && r.tag() != tag {
		a.t.Fatalf("status = %d (tag %q), want %d (tag %q)\n%s", r.Status, errTag(r), status, tag, r.Body)
	}
	return r
}

func errTag(r reply) string {
	var e envelope
	if json.Unmarshal(r.Body, &e) != nil {
		return ""
	}
	return e.Error.Tag
}

// part is one part of a multipart form.
type part struct {
	field, filename, contentType string
	data                         []byte
}

// form sends a multipart request: data is the JSON part, parts are the files.
func (a *testAPI) form(method, path string, data any, parts ...part) reply {
	a.t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	if data != nil {
		b, err := json.Marshal(data)
		check(a.t, err)
		check(a.t, mw.WriteField("data", string(b)))
	}
	for _, p := range parts {
		h := textproto.MIMEHeader{}
		h.Set("Content-Disposition", `form-data; name="`+p.field+`"; filename="`+p.filename+`"`)
		h.Set("Content-Type", p.contentType)
		w, err := mw.CreatePart(h)
		check(a.t, err)
		_, err = w.Write(p.data)
		check(a.t, err)
	}
	check(a.t, mw.Close())
	return a.request(method, path, &buf, map[string]string{"Content-Type": mw.FormDataContentType()})
}

// example reads an example workflow shipped with the project.
func example(t *testing.T, kind, name string) json.RawMessage {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "examples", "workflows", kind, name))
	check(t, err)
	return b
}

// storedFiles lists the files below a directory.
func storedFiles(t *testing.T, dir string) []string {
	t.Helper()
	var out []string
	_ = filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			out = append(out, p)
		}
		return nil
	})
	return out
}

func eventOf(kind string) events.Event {
	return events.Event{Type: events.Type(kind), JobID: 1, CommandID: 1, ProjectID: 1, Server: "gpu1"}
}
