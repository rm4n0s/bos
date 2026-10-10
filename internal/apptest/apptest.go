// Package apptest builds the services over an in-memory database and
// temporary directories, with a fleet of fake ComfyUI servers, for the tests of
// the front ends. It is used by tests only.
package apptest

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/rm4n0s/bos/internal/civitai"
	"github.com/rm4n0s/bos/internal/comfy"
	"github.com/rm4n0s/bos/internal/domain"
	"github.com/rm4n0s/bos/internal/events"
	"github.com/rm4n0s/bos/internal/files"
	"github.com/rm4n0s/bos/internal/filesrv"
	"github.com/rm4n0s/bos/internal/service"
	"github.com/rm4n0s/bos/internal/store"
	"github.com/rm4n0s/bos/internal/workflow"
	"github.com/rm4n0s/errors"
)

// Env is a wired application core.
type Env struct {
	Services *service.Services
	DB       *store.DB
	Events   *events.Log
	Files    *files.Store
	Dirs     filesrv.Dirs
	Root     string
	Logger   *slog.Logger
	Logs     *strings.Builder
	Fleet    *Fleet
	Wakes    *Wakes
}

// New builds an Env; everything is closed when the test ends.
func New(t *testing.T, opts ...func(*service.Deps)) *Env {
	t.Helper()
	db, err := store.Open(t.Context(), store.InMemory)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	if err := db.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	dirs := filesrv.Dirs{
		Results: filepath.Join(root, "results"), ControlNet: filepath.Join(root, "controlnet"), IPAdapter: filepath.Join(root, "ipadapter"),
		Regions: filepath.Join(root, "regions"), Thumbnails: filepath.Join(root, "thumbnails"), Downloads: filepath.Join(root, "downloads"),
	}
	fs, err := files.NewStore([]string{root})
	if err != nil {
		t.Fatal(err)
	}
	logs := &strings.Builder{}
	logger := slog.New(slog.NewTextHandler(&lockedWriter{w: logs}, &slog.HandlerOptions{Level: slog.LevelDebug}))
	fleet := &Fleet{Down: map[domain.ID]bool{}}
	wakes := &Wakes{}
	log := events.NewLog(100, nil)
	deps := service.Deps{
		Store: service.NewStore(db), Files: fs, Fleet: fleet, Dispatcher: wakes, Logger: logger, Events: log,
		Dirs:   service.Dirs{Results: dirs.Results, Regions: dirs.Regions, Downloads: dirs.Downloads},
		DBPath: filepath.Join(root, "bos.db"),
	}
	for _, o := range opts {
		o(&deps)
	}
	svc := service.New(deps)
	return &Env{Services: svc, DB: db, Events: log, Files: fs, Dirs: dirs, Root: root, Logger: logger, Logs: logs, Fleet: fleet, Wakes: wakes}
}

// lockedWriter makes a strings.Builder safe for the logger's goroutines.
type lockedWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func (l *lockedWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(p)
}

// Wakes counts how often the scheduler was woken.
type Wakes struct {
	mu sync.Mutex
	n  int
}

// Wake implements service.Dispatcher.
func (w *Wakes) Wake() { w.mu.Lock(); w.n++; w.mu.Unlock() }

// Count returns the number of wakes so far.
func (w *Wakes) Count() int { w.mu.Lock(); defer w.mu.Unlock(); return w.n }

// Fleet reports every server online unless it is listed in Down, and hands out
// clients that know two LoRAs and answer a ping.
type Fleet struct {
	mu   sync.Mutex
	Down map[domain.ID]bool
}

// Available implements service.Fleet.
func (f *Fleet) Available(id domain.ID) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return !f.Down[id]
}

// SetDown marks a server unreachable or reachable again.
func (f *Fleet) SetDown(id domain.ID, down bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Down[id] = down
}

// Client implements service.Fleet.
func (f *Fleet) Client(_ context.Context, s domain.Server) (comfy.Client, error) {
	return client{down: !f.Available(s.ID)}, nil
}

type client struct{ down bool }

func (c client) Ping(context.Context) error {
	if c.down {
		return errors.New("ServerUnreachable", "the server does not answer")
	}
	return nil
}
func (client) QueuePrompt(context.Context, workflow.Graph) (string, []comfy.NodeWarning, error) {
	return "", nil, errors.New("Unsupported", "test client")
}
func (client) Wait(context.Context, string) (comfy.Result, error) {
	return comfy.Result{}, errors.New("Unsupported", "test client")
}
func (client) Fetch(context.Context, comfy.OutputFile) (io.ReadCloser, error) {
	return nil, errors.New("Unsupported", "test client")
}
func (client) Upload(context.Context, string, io.Reader) (string, error) {
	return "", errors.New("Unsupported", "test client")
}
func (client) ListModels(_ context.Context, folder string) ([]string, error) {
	if folder == "loras" {
		return []string{"dance.safetensors", "fox.safetensors"}, nil
	}
	return nil, nil
}
func (client) Cancel(context.Context, string) error { return nil }
func (client) Close() error                         { return nil }

// FakeCivitai is civitai.Models with a small catalogue: model 7 "Dancing Fox"
// with two trigger words, model 8 "Sleepy Owl" (already cached). Any other id is
// not found.
type FakeCivitai struct{}

// Configured implements civitai.Models.
func (FakeCivitai) Configured() bool { return true }

// Download implements civitai.Models.
func (FakeCivitai) Download(_ context.Context, id int, _ string) (civitai.Model, error) {
	switch id {
	case 7:
		return civitai.Model{ID: 7, Name: "Dancing Fox", TriggerWords: []string{"dancing fox", "sparkles"}}, nil
	case 8:
		return civitai.Model{ID: 8, Name: "Sleepy Owl", Skipped: true}, nil
	}
	return civitai.Model{}, errors.New("CivitaiModelNotFound", "there is no such model on Civitai", "model_id", id)
}

// Install implements civitai.Models.
func (FakeCivitai) Install(m civitai.Model, _, subfolder string) (string, error) {
	return civitai.ModelName(m.ID, subfolder), nil
}
