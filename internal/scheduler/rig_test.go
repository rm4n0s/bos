package scheduler

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
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

func ctx(t *testing.T) context.Context { return t.Context() }

func check(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// ---- a clock the tests move ----

type manualClock struct{ ns atomic.Int64 }

var epoch = time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

func (c *manualClock) Now() time.Time          { return epoch.Add(time.Duration(c.ns.Load())) }
func (c *manualClock) Advance(d time.Duration) { c.ns.Add(int64(d)) }

// ---- a ComfyUI server that does what the test says ----

// fakeComfy is a comfy.Client with a script: how long a prompt takes, when it
// is reachable, and which calls fail.
type fakeComfy struct {
	name string

	mu       sync.Mutex
	down     bool
	latency  time.Duration
	gate     chan struct{} // when set, Wait blocks until a value arrives or the channel closes
	queueErr func(g workflow.Graph) error
	waitErr  func(promptID string) error
	filename string // the name of the file a prompt produces; default out.png
	// comfyRoot, when set, makes the fake behave like an installation on disk: a
	// prompt writes output/<prompt id>.png below it, and Fetch fails when that
	// file is not there.
	comfyRoot string
	// deleteErr is what DeleteOutput answers with, when set.
	deleteErr error
	deleted   []string // the files DeleteOutput was asked to remove
	warnings  []comfy.NodeWarning

	next     int
	prompts  map[string]workflow.Graph
	order    []string // prompt ids in submission order
	uploads  []string // the names given to Upload
	cancels  []string
	closed   int
	active   int
	maxSeen  int
	started  chan string // receives the id of every prompt that starts to wait
	uploaded map[string][]byte
}

func newFakeComfy(name string) *fakeComfy {
	return &fakeComfy{name: name, prompts: map[string]workflow.Graph{}, started: make(chan string, 1000), uploaded: map[string][]byte{}}
}

func (f *fakeComfy) set(fn func(f *fakeComfy)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fn(f)
}

func (f *fakeComfy) Ping(context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.down {
		return errors.New("ServerUnreachable", "the fake server is down", "server", f.name)
	}
	return nil
}

func (f *fakeComfy) QueuePrompt(_ context.Context, g workflow.Graph) (string, []comfy.NodeWarning, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.down {
		return "", nil, errors.New("ServerUnreachable", "the fake server is down", "server", f.name)
	}
	if f.queueErr != nil {
		if err := f.queueErr(g); err != nil {
			return "", nil, err
		}
	}
	f.next++
	id := f.name + "-p" + strconv.Itoa(f.next)
	f.prompts[id] = g.Clone()
	f.order = append(f.order, id)
	return id, f.warnings, nil
}

func (f *fakeComfy) Wait(ctx context.Context, id string) (comfy.Result, error) {
	f.mu.Lock()
	f.active++
	f.maxSeen = max(f.maxSeen, f.active)
	gate, latency, waitErr := f.gate, f.latency, f.waitErr
	filename, root := f.filename, f.comfyRoot
	g := f.prompts[id]
	f.mu.Unlock()
	defer func() {
		f.mu.Lock()
		f.active--
		f.mu.Unlock()
	}()

	select {
	case f.started <- id:
	default:
	}
	if gate != nil {
		select {
		case <-gate:
		case <-ctx.Done():
			return comfy.Result{}, errors.NewErr("ComfyUICallCancelled", ctx.Err())
		}
	}
	if latency > 0 {
		select {
		case <-time.After(latency):
		case <-ctx.Done():
			return comfy.Result{}, errors.NewErr("ComfyUICallCancelled", ctx.Err())
		}
	}
	if err := ctx.Err(); err != nil {
		return comfy.Result{}, errors.NewErr("ComfyUICallCancelled", err)
	}
	f.mu.Lock()
	down := f.down
	f.mu.Unlock()
	if down {
		return comfy.Result{}, errors.New("ServerUnreachable", "the fake server went down", "server", f.name)
	}
	if waitErr != nil {
		if err := waitErr(id); err != nil {
			return comfy.Result{}, err
		}
	}
	if filename == "" {
		filename = "out.png"
	}
	if root != "" {
		filename = id + ".png"
		if err := os.MkdirAll(filepath.Join(root, "output"), 0o755); err != nil {
			return comfy.Result{}, errors.NewErr("FileStoreFailure", err)
		}
		made := filepath.Join(root, "output", filename)
		if err := os.WriteFile(made, []byte("made by "+id), 0o600); err != nil {
			return comfy.Result{}, errors.NewErr("FileStoreFailure", err)
		}
	}
	res := comfy.Result{PromptID: id, Outputs: map[string][]comfy.OutputFile{}}
	if g == nil { // a prompt of an earlier run: ComfyUI still has it in its history
		res.Outputs["9"] = []comfy.OutputFile{{NodeID: "9", Key: "images", Filename: filename, Type: "output"}}
		return res, nil
	}
	for _, n := range g.Nodes() {
		if n.ClassType == "SaveImage" {
			res.Outputs[n.ID] = []comfy.OutputFile{{NodeID: n.ID, Key: "images", Filename: filename, Type: "output"}}
		}
	}
	return res, nil
}

func (f *fakeComfy) Fetch(_ context.Context, o comfy.OutputFile) (io.ReadCloser, error) {
	f.mu.Lock()
	root := f.comfyRoot
	f.mu.Unlock()
	if root != "" {
		if _, err := os.Stat(filepath.Join(root, "output", o.Subfolder, o.Filename)); err != nil {
			return nil, errors.NewErr("ComfyUICallFailed", err)
		}
	}
	return io.NopCloser(strings.NewReader("image from " + f.name + " via " + o.NodeID)), nil
}

func (f *fakeComfy) Upload(_ context.Context, name string, r io.Reader) (string, error) {
	b, err := io.ReadAll(r)
	if err != nil {
		return "", err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.uploads = append(f.uploads, name)
	f.uploaded[name] = b
	return f.name + "/" + name, nil
}

// DeleteOutput removes the file below comfyRoot, like the extension does on the
// server.
func (f *fakeComfy) DeleteOutput(_ context.Context, o comfy.OutputFile) error {
	f.mu.Lock()
	root, err := f.comfyRoot, f.deleteErr
	f.deleted = append(f.deleted, o.Filename)
	f.mu.Unlock()
	if err != nil {
		return err
	}
	if root == "" || o.Type != "output" {
		return nil
	}
	if err := os.Remove(filepath.Join(root, "output", o.Subfolder, o.Filename)); err != nil && !os.IsNotExist(err) {
		return errors.NewErr("FileStoreFailure", err)
	}
	return nil
}

func (f *fakeComfy) UploadModel(context.Context, string, string, string, int64, io.Reader) (string, error) {
	return "", nil
}

func (f *fakeComfy) ListModels(context.Context, string) ([]string, error) { return nil, nil }

func (f *fakeComfy) Cancel(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cancels = append(f.cancels, id)
	return nil
}

func (f *fakeComfy) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed++
	return nil
}

// graphs returns the submitted workflows in submission order.
func (f *fakeComfy) graphs() []workflow.Graph {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]workflow.Graph, len(f.order))
	for i, id := range f.order {
		out[i] = f.prompts[id]
	}
	return out
}

func (f *fakeComfy) submitted() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.order)
}

var _ comfy.Client = (*fakeComfy)(nil)

// ---- the rig ----

type rig struct {
	t      *testing.T
	db     *store.DB
	svc    *service.Services
	sch    *Scheduler
	clock  *manualClock
	log    *events.Log
	files  *files.Store
	root   string
	fakes  map[string]*fakeComfy // by host
	byCode map[string]*fakeComfy
	mu     sync.Mutex
	opened map[string]int

	deps   Deps
	cancel context.CancelFunc
	done   chan error
	seed   atomic.Uint64
}

type rigOption func(*Deps)

func newRig(t *testing.T, opts ...rigOption) *rig {
	t.Helper()
	db, err := store.Open(ctx(t), store.InMemory)
	check(t, err)
	t.Cleanup(func() { check(t, db.Close()) })
	check(t, db.Migrate(ctx(t)))

	root := t.TempDir()
	fs, err := files.NewStore([]string{root})
	check(t, err)
	r := &rig{t: t, db: db, clock: &manualClock{}, files: fs, root: root, fakes: map[string]*fakeComfy{}, byCode: map[string]*fakeComfy{}, opened: map[string]int{}}
	r.seed.Store(1000)
	r.log = events.NewLog(1000, r.clock.Now)
	r.svc = service.New(service.Deps{
		Store: service.NewStore(db), Files: fs, Dirs: service.Dirs{Results: filepath.Join(root, "results"), Regions: filepath.Join(root, "regions")},
		NewSeed: func() uint64 { return 7 },
	})
	deps := Deps{
		DB: db, Files: fs, Events: r.log, Now: r.clock.Now,
		NewClient: func(srv domain.Server) (comfy.Client, error) {
			r.mu.Lock()
			defer r.mu.Unlock()
			f := r.fakes[srv.Host]
			if f == nil {
				return nil, errors.New("ServerHostInvalid", "no fake for this host", "host", srv.Host)
			}
			r.opened[srv.CodeName]++
			return f, nil
		},
		NewSeed:       func() uint64 { return r.seed.Add(1) },
		Grace:         30 * time.Second,
		MaxAttempts:   3,
		ProbeInterval: 5 * time.Millisecond,
		ProbeTimeout:  time.Second,
	}
	for _, o := range opts {
		o(&deps)
	}
	r.deps = deps
	r.sch = New(deps)
	t.Cleanup(r.stop)
	return r
}

// server registers a server and the fake that answers for it.
func (r *rig) server(code string, concurrent int) (*domain.Server, *fakeComfy) {
	r.t.Helper()
	return r.serverAt(code, concurrent, "")
}

// serverAt registers a server whose fake works on disk below root (see
// comfyRoot).
func (r *rig) serverAt(code string, concurrent int, root string) (*domain.Server, *fakeComfy) {
	r.t.Helper()
	f := newFakeComfy(code)
	f.comfyRoot = root
	srv, err := r.svc.Servers.Create(ctx(r.t), domain.Server{Name: code, Host: "http://" + code + ":8188", CodeName: code, MaxConcurrentJobs: concurrent})
	check(r.t, err)
	r.mu.Lock()
	r.fakes[srv.Host] = f
	r.byCode[code] = f
	r.mu.Unlock()
	return &srv.Server, f
}

func (r *rig) start() {
	r.t.Helper()
	c, cancel := context.WithCancel(context.Background())
	r.cancel = cancel
	r.done = make(chan error, 1)
	go func() { r.done <- r.sch.Run(c) }()
}

// stop ends the scheduler and waits for it.
func (r *rig) stop() {
	if r.cancel == nil {
		return
	}
	r.cancel()
	select {
	case err := <-r.done:
		if err != nil {
			r.t.Errorf("Run: %v", err)
		}
	case <-time.After(5 * time.Second):
		r.t.Error("the scheduler did not stop")
	}
	r.cancel = nil
}

// world is the catalogue of a typical test: a generator, a fixer, one group of
// three items and a project.
type world struct {
	generator *domain.Generator
	fixer     *domain.Fixer
	group     *domain.Group
	project   *domain.Project
}

func (r *rig) world(randomSeed bool) *world {
	r.t.Helper()
	read := func(kind, name string) []byte {
		b, err := os.ReadFile(filepath.Join("..", "..", "examples", "workflows", kind, name))
		check(r.t, err)
		return b
	}
	gen, err := r.svc.Generators.Create(ctx(r.t), domain.Generator{
		Name: "anime", CodeName: "anime", Workflow: read("creators", "illustrious_xl.json"),
		PositivePromptTitle: "Positive", NegativePromptTitle: "Negative", HasRandomSeed: randomSeed,
	})
	check(r.t, err)
	fix, err := r.svc.Fixers.Create(ctx(r.t), domain.Fixer{
		Name: "upscale", CodeName: "upscale", Workflow: read("fixers", "simple_upscale.json"),
		LoadImageTitle: "Load Image", SaveImageTitle: "Save Image",
	})
	check(r.t, err)
	grp, err := r.svc.Groups.Create(ctx(r.t), domain.Group{Name: "chars", CodeName: "chars"})
	check(r.t, err)
	for _, c := range []string{"a", "b", "c"} {
		_, err := r.svc.Items.Create(ctx(r.t), grp.ID, domain.Item{CodeName: c, PositivePrompt: "prompt " + c, NegativePrompt: "bad " + c})
		check(r.t, err)
	}
	prj, err := r.svc.Projects.Create(ctx(r.t), domain.Project{Name: "p"})
	check(r.t, err)
	return &world{generator: gen, fixer: fix, group: grp, project: prj}
}

// command adds a command to the project and queues all its jobs, as Run does.
func (r *rig) command(w *world, code string) *domain.Command {
	r.t.Helper()
	cmd, err := r.svc.Commands.Add(ctx(r.t), w.project.ID, service.CommandInput{Name: code, Code: code})
	check(r.t, err)
	return cmd
}

// run queues a command's jobs and wakes the scheduler.
func (r *rig) run(cmd *domain.Command) {
	r.t.Helper()
	repos := r.db.Repos()
	_, err := repos.Jobs.QueueCommand(ctx(r.t), cmd.ID)
	check(r.t, err)
	check(r.t, repos.Commands.SetStatus(ctx(r.t), cmd.ID, domain.StatusQueued))
	r.sch.Wake()
}

func (r *rig) jobs(cmd *domain.Command) []domain.Job {
	r.t.Helper()
	jobs, err := r.db.Repos().Jobs.AllByCommand(ctx(r.t), cmd.ID)
	check(r.t, err)
	return jobs
}

func (r *rig) job(id domain.ID) *domain.Job {
	r.t.Helper()
	j, err := r.db.Repos().Jobs.Get(ctx(r.t), id)
	check(r.t, err)
	return j
}

func (r *rig) command_(id domain.ID) *domain.Command {
	r.t.Helper()
	c, err := r.db.Repos().Commands.Get(ctx(r.t), id)
	check(r.t, err)
	return c
}

// eventually waits for cond, polling.
func (r *rig) eventually(what string, cond func() bool) {
	r.t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	r.t.Fatalf("timed out waiting for %s", what)
}

// statuses returns the status of every job of a command, in job order.
func (r *rig) statuses(cmd *domain.Command) []domain.Status {
	jobs := r.jobs(cmd)
	out := make([]domain.Status, len(jobs))
	for i, j := range jobs {
		out[i] = j.Status
	}
	return out
}

func (r *rig) waitStatus(cmd *domain.Command, want domain.Status) {
	r.t.Helper()
	r.eventually("command "+cmd.Name+" to be "+string(want), func() bool { return r.command_(cmd.ID).Status == want })
}

func (r *rig) read(path string) string {
	r.t.Helper()
	b, err := os.ReadFile(path)
	check(r.t, err)
	return string(b)
}

// eventTypes lists the types of the logged events, optionally only of some.
func (r *rig) eventTypes(only ...events.Type) []events.Type {
	all, _, _ := r.log.After(0)
	var out []events.Type
	for _, e := range all {
		if len(only) == 0 || containsType(only, e.Type) {
			out = append(out, e.Type)
		}
	}
	return out
}

func containsType(list []events.Type, t events.Type) bool {
	for _, x := range list {
		if x == t {
			return true
		}
	}
	return false
}

// promptText reads the positive prompt a submitted graph carries.
func promptText(t *testing.T, g workflow.Graph) string {
	t.Helper()
	n, err := workflow.FindByTitle(g, "Positive")
	check(t, err)
	var text string
	if raw, ok := n.Inputs["text"]; ok {
		text = strings.Trim(string(bytes.TrimSpace(raw)), `"`)
	}
	return text
}

func itoa(n uint64) string { return strconv.FormatUint(n, 10) }

// findSampler returns the seed input of the first KSampler of a graph.
func findSampler(g workflow.Graph) ([]byte, error) {
	for _, n := range g.Nodes() {
		if n.ClassType == "KSampler" {
			return n.Inputs["seed"], nil
		}
	}
	return nil, errors.New("Test", "no sampler")
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// restart stops the scheduler and starts a new one on the same database, as a
// restart of the application does.
func (r *rig) restart() {
	r.t.Helper()
	r.stop()
	r.sch = New(r.deps)
	r.start()
}

func stringsReader(s string) io.Reader { return strings.NewReader(s) }
