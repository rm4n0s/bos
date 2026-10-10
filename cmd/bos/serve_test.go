package main

import (
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rm4n0s/errors"
)

func testLogger(t *testing.T) *slog.Logger {
	t.Helper()
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func testEnv(t *testing.T, extra map[string]string) func(string) string {
	t.Helper()
	root := t.TempDir()
	env := map[string]string{
		"BOS_DATABASE":       filepath.Join(root, "bos.db"),
		"BOS_RESULTS_DIR":    filepath.Join(root, "results"),
		"BOS_CONTROLNET_DIR": filepath.Join(root, "controlnet"),
		"BOS_IPADAPTER_DIR":  filepath.Join(root, "ipadapter"),
		"BOS_REGIONS_DIR":    filepath.Join(root, "regions"),
		"BOS_THUMBNAILS_DIR": filepath.Join(root, "thumbnails"),
		"BOS_DOWNLOADS_DIR":  filepath.Join(root, "downloads"),
		"BOS_LISTEN_ADDR":    "127.0.0.1:0",
	}
	for k, v := range extra {
		env[k] = v
	}
	return func(k string) string { return env[k] }
}

func get(t *testing.T, url string) int {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if err := resp.Body.Close(); err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode
}

func TestServeHealthAndGracefulShutdown(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	env := testEnv(t, nil)

	addrCh := make(chan net.Addr, 1)
	done := make(chan error, 1)
	go func() {
		done <- serve(ctx, "", env, testLogger(t), func(a net.Addr) { addrCh <- a })
	}()

	var addr net.Addr
	select {
	case addr = <-addrCh:
	case err := <-done:
		t.Fatalf("serve returned early: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("server did not start")
	}
	if code := get(t, "http://"+addr.String()+"/healthz"); code != http.StatusOK {
		t.Errorf("/healthz status = %d", code)
	}
	if _, err := os.Stat(env("BOS_RESULTS_DIR")); err != nil {
		t.Errorf("results directory was not created: %v", err)
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("shutdown returned %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("server did not stop")
	}
}

func routeOf(t *testing.T, err error) *errors.Error {
	t.Helper()
	appErr, ok := errors.FromError(err)
	if !ok {
		t.Fatalf("expected *errors.Error, got %T (%v)", err, err)
	}
	return appErr
}

func TestServeConfigFailure(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "config.json"), 0o750); err != nil {
		t.Fatal(err)
	}
	// A directory where the file should be: it exists, so it is not created, and it cannot be read.
	err := serve(t.Context(), filepath.Join(dir, "config.json"), testEnv(t, nil), testLogger(t), nil)
	if appErr := routeOf(t, err); !appErr.HasRoute("serve->Load.ConfigNotReadable") {
		t.Errorf("unexpected failure path: %s", appErr.Route())
	}
}

func TestServeConfigNotWritable(t *testing.T) {
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	err := serve(t.Context(), filepath.Join(file, "config.json"), testEnv(t, nil), testLogger(t), nil)
	if appErr := routeOf(t, err); !appErr.HasRoute("serve->EnsureFile.ConfigNotWritable") {
		t.Errorf("unexpected failure path: %s", appErr.Route())
	}
}

func TestServeCreatesMissingConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "conf", "bos.json")
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	ready := make(chan net.Addr, 1)
	done := make(chan error, 1)
	// No BOS_* paths: everything must come from the file that serve creates.
	go func() {
		done <- serve(ctx, path, func(k string) string {
			if k == "BOS_LISTEN_ADDR" {
				return "127.0.0.1:0"
			}
			return ""
		}, testLogger(t), func(a net.Addr) { ready <- a })
	}()
	select {
	case <-ready:
	case err := <-done:
		t.Fatalf("serve returned early: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("server did not start")
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{path, filepath.Join(dir, "conf", "data", "bos.db"), filepath.Join(dir, "conf", "data", "results")} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("expected %s to exist: %v", p, err)
		}
	}
}

func TestServeDirectoryFailure(t *testing.T) {
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	err := serve(t.Context(), "", testEnv(t, map[string]string{"BOS_RESULTS_DIR": filepath.Join(file, "r")}), testLogger(t), nil)
	if appErr := routeOf(t, err); !appErr.HasRoute("serve->EnsureDirs.FileStoreFailure") {
		t.Errorf("unexpected failure path: %s", appErr.Route())
	}
}

func TestServeStoreFailure(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "no", "such", "dir", "bos.db")
	err := serve(t.Context(), "", testEnv(t, map[string]string{"BOS_DATABASE": missing}), testLogger(t), nil)
	if appErr := routeOf(t, err); !appErr.HasRoute("serve->newApp->Open.StoreFailure") {
		t.Errorf("unexpected failure path: %s", appErr.Route())
	}
}

func TestServeListenFailure(t *testing.T) {
	busy, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := busy.Close(); err != nil {
			t.Error(err)
		}
	}()
	env := testEnv(t, map[string]string{"BOS_LISTEN_ADDR": busy.Addr().String()})
	err = serve(t.Context(), "", env, testLogger(t), nil)
	if appErr := routeOf(t, err); !appErr.HasRoute("serve.ListenFailed") {
		t.Errorf("unexpected failure path: %s", appErr.Route())
	}
}

func TestRunServerServeFailure(t *testing.T) {
	lis, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	if err := lis.Close(); err != nil {
		t.Fatal(err)
	}
	err = runServer(t.Context(), &http.Server{ReadHeaderTimeout: time.Second}, lis, time.Second, testLogger(t))
	if appErr := routeOf(t, err); !appErr.HasRoute("runServer.ServeFailed") {
		t.Errorf("unexpected failure path: %s", appErr.Route())
	}
}

func TestRunServerShutdownFailure(t *testing.T) {
	lis, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	release := make(chan struct{})
	defer close(release)
	srv := &http.Server{
		ReadHeaderTimeout: time.Second,
		Handler: http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
			close(started)
			<-release
		}),
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- runServer(ctx, srv, lis, 50*time.Millisecond, testLogger(t)) }()

	go func() {
		req, reqErr := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://"+lis.Addr().String(), nil)
		if reqErr != nil {
			return
		}
		if resp, doErr := http.DefaultClient.Do(req); doErr == nil {
			_ = resp.Body.Close()
		}
	}()
	<-started
	cancel()

	if appErr := routeOf(t, <-done); !appErr.HasRoute("runServer.ShutdownFailed") {
		t.Errorf("unexpected failure path: %s", appErr.Route())
	}
}

func TestRunMainBadFlag(t *testing.T) {
	if err := runMain([]string{"-nope"}); err == nil {
		t.Fatal("expected an error for an unknown flag")
	}
}

// TestServeServesTheAPI starts the whole program and talks to it over HTTP:
// the token is required, a project can be created, and an open event stream
// does not hold up the shutdown (acceptance 28).
func TestServeServesTheAPI(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	env := testEnv(t, map[string]string{"BOS_AUTH_TOKEN": "s3cret"})
	addrCh := make(chan net.Addr, 1)
	done := make(chan error, 1)
	go func() { done <- serve(ctx, "", env, testLogger(t), func(a net.Addr) { addrCh <- a }) }()
	var base string
	select {
	case a := <-addrCh:
		base = "http://" + a.String()
	case err := <-done:
		t.Fatalf("serve returned early: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("server did not start")
	}

	// One connection per request: net/http waits up to five seconds for a spare
	// connection that never sent a request, which would hide what is tested.
	client := &http.Client{Transport: &http.Transport{DisableKeepAlives: true}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	open := func(method, path, body string, headers map[string]string) *http.Response {
		t.Helper()
		// t.Context, not ctx: the stream must outlive the shutdown signal.
		req, err := http.NewRequestWithContext(t.Context(), method, base+path, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return resp
	}
	status := func(method, path, body string, headers map[string]string) int {
		t.Helper()
		resp := open(method, path, body, headers)
		if err := resp.Body.Close(); err != nil {
			t.Error(err)
		}
		return resp.StatusCode
	}

	if code := status("GET", "/api/v1/projects", "", nil); code != http.StatusUnauthorized {
		t.Errorf("without the token: status = %d", code)
	}
	// The web interface shares the port: its pages send a visitor to sign in.
	if code := status("GET", "/servers", "", nil); code != http.StatusSeeOther {
		t.Errorf("web page without the cookie: status = %d", code)
	}
	if code := status("GET", "/static/datastar.js", "", nil); code != http.StatusOK {
		t.Errorf("static script: status = %d", code)
	}
	auth := map[string]string{"Authorization": "Bearer s3cret", "Content-Type": "application/json"}
	if code := status("POST", "/api/v1/projects", `{"name":"Demo"}`, auth); code != http.StatusCreated {
		t.Errorf("create a project: status = %d", code)
	}
	if code := status("GET", "/api/v1/projects", "", auth); code != http.StatusOK {
		t.Errorf("list projects: status = %d", code)
	}

	// An event stream is open when the shutdown starts.
	stream := open("GET", "/api/v1/events", "", map[string]string{"Authorization": "Bearer s3cret", "Accept": "text/event-stream"})
	defer func() {
		if err := stream.Body.Close(); err != nil {
			t.Error(err)
		}
	}()
	if stream.StatusCode != http.StatusOK {
		t.Fatalf("event stream: status = %d", stream.StatusCode)
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("shutdown returned %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the open event stream held up the shutdown")
	}
}

// quick makes the scheduler and the adapter poll every few milliseconds.
func quick(t *appTuning) { t.probeInterval, t.pollInterval = 10*time.Millisecond, 10*time.Millisecond }

func workflowFile(t *testing.T, kind, name string) string {
	t.Helper()
	return filepath.Join("..", "..", "examples", "workflows", kind, name)
}

// syncBuffer is a log destination that several goroutines may write.
type syncBuffer struct {
	mu sync.Mutex
	b  strings.Builder
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string { s.mu.Lock(); defer s.mu.Unlock(); return s.b.String() }

func TestLogLevelFromTheConfiguration(t *testing.T) {
	for level, wantRequests := range map[string]bool{"debug": true, "info": false} {
		t.Run(level, func(t *testing.T) {
			var logs syncBuffer
			lv := &slog.LevelVar{}
			logger := slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: lv}))
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			addr := make(chan net.Addr, 1)
			done := make(chan error, 1)
			go func() {
				done <- serve(ctx, "", testEnv(t, map[string]string{"BOS_LOG_LEVEL": level}), logger, func(a net.Addr) { addr <- a }, withLogLevel(lv))
			}()
			select {
			case a := <-addr:
				if code := get(t, "http://"+a.String()+"/healthz?token=secret"); code != 200 {
					t.Fatalf("status = %d", code)
				}
			case err := <-done:
				t.Fatal(err)
			case <-time.After(5 * time.Second):
				t.Fatal("no start")
			}
			cancel()
			<-done
			out := logs.String()
			if got := strings.Contains(out, `"msg":"request"`) && strings.Contains(out, `"path":"/healthz"`); got != wantRequests {
				t.Errorf("requests logged = %v, want %v:\n%s", got, wantRequests, out)
			}
			if strings.Contains(out, "secret") {
				t.Errorf("the query string reached the log:\n%s", out)
			}
		})
	}
}
