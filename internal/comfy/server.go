package comfy

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"path"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rm4n0s/bos/internal/modelcrud"
	"github.com/rm4n0s/bos/internal/workflow"
	comfyui "github.com/rm4n0s/comfyui-client-go"
	"github.com/rm4n0s/errors"
)

// Options tunes a Server. The zero value is usable.
type Options struct {
	// Logger receives connection events; the default discards them.
	Logger *slog.Logger
	// RequestTimeout is how long an HTTP call waits for response headers
	// (default 60 s). It does not limit downloads; use the call's context.
	RequestTimeout time.Duration
	// PollInterval is how often Wait checks the history and the queue as a
	// safety net next to the event stream (default 2 s).
	PollInterval time.Duration
	// PollFailures is how many consecutive failed polls Wait tolerates before it
	// reports the server unreachable (default 3).
	PollFailures int
	// CallTimeout bounds one poll or small request (default 30 s).
	CallTimeout time.Duration
	// Backoff returns the wait before reconnect attempt n, from 0 (default
	// 250 ms doubling up to 10 s).
	Backoff func(attempt int) time.Duration
	// EarlyEvents and EarlyTTL bound the memory of prompt ends nobody waits for
	// yet (defaults 2048 and 5 minutes).
	EarlyEvents int
	EarlyTTL    time.Duration
	// UploadSubfolder is the input sub-folder for uploaded files (default "bos").
	UploadSubfolder string
	// OnConnection is called when the event connection goes up or down.
	OnConnection func(up bool)
	// Now is the clock (default time.Now).
	Now func() time.Time
}

func (o Options) withDefaults() Options {
	if o.Logger == nil {
		o.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	if o.RequestTimeout == 0 {
		o.RequestTimeout = 60 * time.Second
	}
	if o.PollInterval <= 0 {
		o.PollInterval = 2 * time.Second
	}
	if o.PollFailures <= 0 {
		o.PollFailures = 3
	}
	if o.CallTimeout <= 0 {
		o.CallTimeout = 30 * time.Second
	}
	if o.Backoff == nil {
		o.Backoff = func(attempt int) time.Duration {
			d := 250 * time.Millisecond << min(attempt, 6)
			return min(d, 10*time.Second)
		}
	}
	if o.EarlyEvents <= 0 {
		o.EarlyEvents = 2048
	}
	if o.EarlyTTL <= 0 {
		o.EarlyTTL = 5 * time.Minute
	}
	if o.UploadSubfolder == "" {
		o.UploadSubfolder = "bos"
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	return o
}

// minInterruptVersion is the first ComfyUI that interrupts one prompt only.
var minInterruptVersion = [3]int{0, 3, 57}

// Server is the adapter over comfyui-client-go for one ComfyUI server. It
// implements Client.
type Server struct {
	baseURL string
	opts    Options
	logger  *slog.Logger

	// http answers every call but QueuePrompt: it has no WebSocket, so it needs
	// no connection to exist.
	http *comfyui.Client
	hub  *hub
	conn connection
	// crud is the client of the comfyui-model-crud extension of the server.
	crud *modelcrud.Client

	cancel context.CancelFunc
	wg     sync.WaitGroup
	closed atomic.Bool

	connected atomic.Bool

	uploadMu sync.Mutex
	uploads  map[string]*upload

	versionOnce sync.Once
}

var _ Client = (*Server)(nil)

// NewServer starts the adapter for the ComfyUI at baseURL. It returns at once:
// the event connection is opened in the background, with back-off, and kept up
// until Close. Prompts can be queued and awaited before it is up; Wait then
// relies on polling.
func NewServer(baseURL string, opts Options) (*Server, error) {
	opts = opts.withDefaults()
	httpClient, err := comfyui.NewClient(baseURL, comfyui.WithoutWebSocket(), clientOption(opts))
	if err != nil {
		return nil, errors.NewErr("ServerHostInvalid", err).SetMetadata("host", baseURL)
	}
	crud, err := modelcrud.New(baseURL, modelcrud.Options{CallTimeout: opts.CallTimeout})
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	s := &Server{
		baseURL: strings.TrimRight(baseURL, "/"), opts: opts, logger: opts.Logger, http: httpClient, crud: crud,
		hub:     newHub(opts.EarlyEvents, opts.EarlyTTL, opts.Now),
		cancel:  cancel,
		uploads: map[string]*upload{},
	}
	s.wg.Add(1)
	go s.run(ctx)
	return s, nil
}

// Connected reports whether the event connection is up.
func (s *Server) Connected() bool { return s.connected.Load() }

func (s *Server) setConnected(up bool, _ error) {
	if s.connected.Swap(up) != up && s.opts.OnConnection != nil {
		s.opts.OnConnection(up)
	}
}

// Close stops the background connection and releases the clients. It is safe to
// call more than once.
func (s *Server) Close() error {
	if !s.closed.CompareAndSwap(false, true) {
		return nil
	}
	s.cancel()
	s.wg.Wait()
	if c := s.conn.current(); c != nil {
		if err := c.Close(); err != nil {
			return errors.NewErr("ServerUnreachable", err).SetMetadata("operation", "close", "host", s.baseURL)
		}
	}
	return s.http.Close()
}

// call bounds one small request.
func (s *Server) call(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, s.opts.CallTimeout)
}

// ---- Client ----

// Ping implements Client.
func (s *Server) Ping(ctx context.Context) error {
	ctx, cancel := s.call(ctx)
	defer cancel()
	if err := s.http.Ping(ctx); err != nil {
		return s.pingError(ctx, err)
	}
	return nil
}

// pingError is every failure of a probe: whatever the cause, ComfyUI is not
// answering properly.
func (s *Server) pingError(ctx context.Context, err error) error {
	if ctx.Err() != nil && !errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return errors.NewErr("ComfyUICallCancelled", ctx.Err()).SetMetadata("operation", "ping", "host", s.baseURL)
	}
	return errors.NewErr("ServerUnreachable", err).SetMetadata("operation", "ping", "host", s.baseURL)
}

// QueuePrompt implements Client. It queues through the client that owns the
// event socket, so the prompt's events come back to the hub; without one it
// queues over HTTP and Wait polls.
func (s *Server) QueuePrompt(ctx context.Context, wf workflow.Graph) (string, []NodeWarning, error) {
	client := s.conn.current()
	if client == nil {
		client = s.http
	}
	ctx, cancel := s.call(ctx)
	defer cancel()
	id, err := client.QueuePrompt(ctx, map[string]json.RawMessage(wf))
	var nodeErrs *comfyui.NodeErrorsError
	if errors.As(err, &nodeErrs) {
		if id != "" {
			return id, warningsOf(nodeErrs.NodeErrors), nil // accepted: queued with some outputs skipped
		}
		return "", nil, errors.NewErr("ComfyUIRejected", err).SetMetadata("host", s.baseURL, "message", nodeErrs.Message, "node_errors", nodeErrs.NodeErrors)
	}
	if err != nil {
		return "", nil, s.mapError(ctx, err, "ComfyUIRejected", "queue prompt")
	}
	if id == "" {
		return "", nil, errors.New("ComfyUIRejected", "ComfyUI accepted the prompt without an id", "host", s.baseURL)
	}
	return id, nil, nil
}

// ListModels implements Client.
func (s *Server) ListModels(ctx context.Context, folder string) ([]string, error) {
	ctx, cancel := s.call(ctx)
	defer cancel()
	models, err := s.http.ListModels(ctx, folder)
	if err != nil {
		return nil, s.mapError(ctx, err, "ComfyUIRejected", "list models")
	}
	return models, nil
}

// Fetch implements Client.
func (s *Server) Fetch(ctx context.Context, f OutputFile) (io.ReadCloser, error) {
	body, err := s.http.ViewImage(ctx, comfyui.ViewParams{Filename: f.Filename, Subfolder: f.Subfolder, Type: f.Type})
	if err != nil {
		return nil, s.mapError(ctx, err, "ComfyUIResultMissing", "fetch output")
	}
	return body, nil
}

// DeleteOutput implements Client.
func (s *Server) DeleteOutput(ctx context.Context, f OutputFile) error {
	if f.Type != "output" {
		return nil
	}
	return s.crud.DeleteOutput(ctx, path.Join(f.Subfolder, f.Filename))
}

// UploadModel implements Client.
func (s *Server) UploadModel(ctx context.Context, folder, subfolder, filename string, size int64, r io.Reader) (string, error) {
	return s.crud.UploadModel(ctx, folder, subfolder, filename, size, r)
}

// DeleteModel implements Client.
func (s *Server) DeleteModel(ctx context.Context, folder, path string) error {
	return s.crud.DeleteModel(ctx, folder, path)
}

// Cancel implements Client: a pending prompt is removed from the queue, a
// running one is interrupted. A prompt that is in neither is already over.
func (s *Server) Cancel(ctx context.Context, promptID string) error {
	ctx, cancel := s.call(ctx)
	defer cancel()
	q, err := s.http.Queue(ctx)
	if err != nil {
		return s.mapError(ctx, err, "ComfyUIRejected", "read queue")
	}
	switch {
	case contains(q.Pending, promptID):
		if err := s.http.DeleteQueued(ctx, promptID); err != nil {
			return s.mapError(ctx, err, "ComfyUIRejected", "remove from queue")
		}
	case contains(q.Running, promptID):
		s.warnOldInterrupt(ctx)
		if err := s.http.Interrupt(ctx, promptID); err != nil {
			return s.mapError(ctx, err, "ComfyUIRejected", "interrupt")
		}
	}
	return nil
}

func contains(ids []string, id string) bool {
	for _, x := range ids {
		if x == id {
			return true
		}
	}
	return false
}

// warnOldInterrupt logs once if the server is too old to interrupt a single
// prompt: there, a prompt that starts between the check and the interrupt can
// be stopped by mistake (SPEC §18.7).
func (s *Server) warnOldInterrupt(ctx context.Context) {
	s.versionOnce.Do(func() {
		stats, err := s.http.SystemStats(ctx)
		if err != nil {
			s.logger.Debug("comfy: cannot read the server version", "host", s.baseURL, "error", err.Error())
			return
		}
		if v, ok := parseVersion(stats.System.ComfyUIVersion); ok && less(v, minInterruptVersion) {
			s.logger.Warn("comfy: this ComfyUI cannot interrupt one prompt only; a prompt that starts at the same moment may be stopped too",
				"host", s.baseURL, "version", stats.System.ComfyUIVersion)
		}
	})
}

func parseVersion(s string) ([3]int, bool) {
	var v [3]int
	parts := strings.SplitN(strings.TrimPrefix(strings.TrimSpace(s), "v"), ".", 3)
	if len(parts) < 2 {
		return v, false
	}
	for i, p := range parts {
		digits := p
		if cut := strings.IndexFunc(p, func(r rune) bool { return r < '0' || r > '9' }); cut >= 0 {
			digits = p[:cut]
		}
		n, err := strconv.Atoi(digits)
		if err != nil {
			return v, false
		}
		v[i] = n
	}
	return v, true
}

func less(a, b [3]int) bool {
	for i := range a {
		if a[i] != b[i] {
			return a[i] < b[i]
		}
	}
	return false
}

// ---- Upload ----

// upload is one file being or already stored on the server.
type upload struct {
	ready chan struct{}
	value string
	err   error
}

// maxUploadBytes bounds one input file held in memory.
const maxUploadBytes = 256 << 20

// Upload implements Client. The file is named by the SHA-256 of its content,
// plus the extension of name, in the application's input sub-folder, so one
// reference image shared by many jobs is stored once per server; ComfyUI answers
// with the name it used, and that name is always the one returned (SPEC §18.6).
func (s *Server) Upload(ctx context.Context, name string, r io.Reader) (string, error) {
	data, err := io.ReadAll(io.LimitReader(r, maxUploadBytes+1))
	if err != nil {
		return "", errors.NewErr("ComfyUIUploadFailed", err).SetMetadata("host", s.baseURL, "file", name, "reason", "cannot read the file")
	}
	if len(data) > maxUploadBytes {
		return "", errors.New("ComfyUIUploadFailed", "the file is too large to upload", "host", s.baseURL, "file", name, "limit", maxUploadBytes)
	}
	sum := sha256.Sum256(data)
	remote := hex.EncodeToString(sum[:]) + strings.ToLower(path.Ext(name))

	s.uploadMu.Lock()
	if u, ok := s.uploads[remote]; ok {
		s.uploadMu.Unlock()
		select {
		case <-u.ready:
			return u.value, u.err
		case <-ctx.Done():
			return "", errors.NewErr("ComfyUICallCancelled", ctx.Err()).SetMetadata("operation", "upload", "host", s.baseURL)
		}
	}
	u := &upload{ready: make(chan struct{})}
	s.uploads[remote] = u
	s.uploadMu.Unlock()

	u.value, u.err = s.store(ctx, remote, data)
	if u.err != nil {
		s.uploadMu.Lock()
		if s.uploads[remote] == u {
			delete(s.uploads, remote) // a failure is not remembered: the next call tries again
		}
		s.uploadMu.Unlock()
	}
	close(u.ready)
	return u.value, u.err
}

func (s *Server) store(ctx context.Context, remote string, data []byte) (string, error) {
	res, err := s.http.UploadImage(ctx, remote, bytes.NewReader(data), comfyui.UploadOptions{Type: "input", Subfolder: s.opts.UploadSubfolder})
	if err != nil {
		return "", s.mapError(ctx, err, "ComfyUIUploadFailed", "upload")
	}
	if res.Name == "" {
		return "", errors.New("ComfyUIUploadFailed", "ComfyUI did not say what it called the file", "host", s.baseURL, "file", remote)
	}
	return res.InputValue(), nil
}

// forgetUploads drops what the adapter remembers about stored files, after the
// server was away: it may have lost them, and storing one again is cheap
// because ComfyUI keeps identical content under the same name.
func (s *Server) forgetUploads() {
	s.uploadMu.Lock()
	defer s.uploadMu.Unlock()
	s.uploads = map[string]*upload{}
}
