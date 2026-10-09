package service

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rm4n0s/bos/internal/comfy"
	"github.com/rm4n0s/bos/internal/domain"
	"github.com/rm4n0s/bos/internal/files"
	"github.com/rm4n0s/bos/internal/store"
	"github.com/rm4n0s/bos/internal/workflow"
	"github.com/rm4n0s/errors"
)

func ctx(t *testing.T) context.Context { return t.Context() }

func check(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// fakeClock moves one second with every call, so timestamps are deterministic.
func fakeClock() func() time.Time {
	var n atomic.Int64
	start := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	return func() time.Time { return start.Add(time.Duration(n.Add(1)) * time.Second) }
}

// wantRoute asserts that err is an application error that carries tag, raised
// under the method entry.
func wantRoute(t *testing.T, err error, entry, tag string) {
	t.Helper()
	_ = wantRouteErr(t, err, entry, tag)
}

// wantRouteErr is wantRoute that also returns the error, for its metadata.
func wantRouteErr(t *testing.T, err error, entry, tag string) *errors.Error {
	t.Helper()
	appErr, ok := errors.FromError(err)
	if !ok {
		t.Fatalf("expected *errors.Error, got %T (%v)", err, err)
	}
	if appErr.Tag != tag || !appErr.HasRoute(entry) || !strings.HasSuffix(appErr.Route(), "."+tag) {
		t.Errorf("unexpected failure path: %s (tag %s)\nwant %s ... %s", appErr.Route(), appErr.Tag, entry, tag)
	}
	return appErr
}

// env is a wired set of services over an in-memory store and temporary
// directories.
type env struct {
	*Services
	db    *store.DB
	repos Repos
	root  string
	dirs  Dirs
	fleet *fakeFleet
	files *files.Store
	logs  *strings.Builder
}

type envOption func(*Deps)

func withLimits(l Limits) envOption { return func(d *Deps) { d.Limits = l } }

func withStore(wrap func(Store) Store) envOption {
	return func(d *Deps) { d.Store = wrap(d.Store) }
}

func newEnv(t *testing.T, opts ...envOption) *env {
	t.Helper()
	db, err := store.Open(ctx(t), store.InMemory, store.WithClock(fakeClock()))
	check(t, err)
	t.Cleanup(func() { check(t, db.Close()) })
	check(t, db.Migrate(ctx(t)))

	root := t.TempDir()
	dirs := Dirs{Results: filepath.Join(root, "results"), Regions: filepath.Join(root, "regions")}
	fs, err := files.NewStore([]string{root})
	check(t, err)
	fleet := newFakeFleet()
	logs := &strings.Builder{}
	deps := Deps{
		Store: NewStore(db), Files: fs, Fleet: fleet, Dirs: dirs, Logger: newLogger(logs),
		NewSeed: counter(100),
	}
	for _, o := range opts {
		o(&deps)
	}
	return &env{Services: New(deps), db: db, repos: deps.Store.Repos(), root: root, dirs: dirs, fleet: fleet, files: fs, logs: logs}
}

// write creates a file under the environment's root and returns its path.
func (e *env) write(t *testing.T, name, content string) string {
	t.Helper()
	p := filepath.Join(e.root, name)
	check(t, os.MkdirAll(filepath.Dir(p), 0o750))
	check(t, os.WriteFile(p, []byte(content), 0o600))
	return p
}

// counter returns a seed source that gives 100, 101, 102, … so that every call
// differs and the sequence is known.
func counter(first uint64) func() uint64 {
	var n atomic.Uint64
	n.Store(first - 1)
	return func() uint64 { return n.Add(1) }
}

func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// ---- fake ComfyUI fleet ----

type fakeFleet struct {
	online  map[domain.ID]bool
	clients map[string]*fakeClient // by host
	err     error
}

func newFakeFleet() *fakeFleet {
	return &fakeFleet{online: map[domain.ID]bool{}, clients: map[string]*fakeClient{}}
}

func (f *fakeFleet) Available(id domain.ID) bool { return f.online[id] }

func (f *fakeFleet) Client(_ context.Context, s domain.Server) (comfy.Client, error) {
	if f.err != nil {
		return nil, f.err
	}
	c, ok := f.clients[s.Host]
	if !ok {
		c = &fakeClient{}
		f.clients[s.Host] = c
	}
	return c, nil
}

// fakeClient is a comfy.Client that only knows which LoRAs are installed.
type fakeClient struct {
	loras []string
	err   error
}

func (c *fakeClient) Ping(context.Context) error { return nil }
func (c *fakeClient) QueuePrompt(context.Context, workflow.Graph) (string, []comfy.NodeWarning, error) {
	return "", nil, errors.New("Unsupported", "the fake client only lists models")
}
func (c *fakeClient) Wait(context.Context, string) (comfy.Result, error) {
	return comfy.Result{}, errors.New("Unsupported", "the fake client only lists models")
}
func (c *fakeClient) Fetch(context.Context, comfy.OutputFile) (io.ReadCloser, error) {
	return nil, errors.New("Unsupported", "the fake client only lists models")
}
func (c *fakeClient) Upload(context.Context, string, io.Reader) (string, error) {
	return "", errors.New("Unsupported", "the fake client only lists models")
}
func (c *fakeClient) ListModels(_ context.Context, folder string) ([]string, error) {
	if c.err != nil {
		return nil, c.err
	}
	if folder != "loras" {
		return nil, nil
	}
	return c.loras, nil
}
func (c *fakeClient) Cancel(context.Context, string) error { return nil }
func (c *fakeClient) Close() error                         { return nil }

var _ comfy.Client = (*fakeClient)(nil)

func cancellable(t *testing.T) (context.Context, context.CancelFunc) {
	t.Helper()
	return context.WithCancel(t.Context())
}
