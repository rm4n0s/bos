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

	"github.com/rm4n0s/bos/internal/store"
	"github.com/rm4n0s/errors"
)

func testLogger(t *testing.T) *slog.Logger {
	t.Helper()
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// dbOf is the database file a test environment names. BOS_DATABASE is not a
// setting of the program (the database is given with -db): the tests carry the
// path in their environment so that one value serves serve and its settings.
func dbOf(env func(string) string) string { return env("BOS_DATABASE") }

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
		done <- serve(ctx, dbOf(env), env, testLogger(t), func(a net.Addr) { addrCh <- a })
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

func TestServeDatabaseFolderFailure(t *testing.T) {
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	// The folder of the database cannot be made: its parent is a file.
	err := serve(t.Context(), filepath.Join(file, "sub", "bos.db"), testEnv(t, nil), testLogger(t), nil)
	if appErr := routeOf(t, err); !appErr.HasRoute("serve->openDatabase.FileStoreFailure") {
		t.Errorf("unexpected failure path: %s", appErr.Route())
	}
}

// A database that does not exist is created with its folder, its schema and
// default settings: every folder next to it.
func TestServeCreatesTheDatabaseWithDefaultSettings(t *testing.T) {
	root := t.TempDir()
	dbPath := filepath.Join(root, "new", "bos.db")
	listenOnly := func(k string) string { // no BOS_* folders: everything comes from the database
		if k == "BOS_LISTEN_ADDR" {
			return "127.0.0.1:0"
		}
		return ""
	}
	run := func() {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		ready := make(chan net.Addr, 1)
		done := make(chan error, 1)
		go func() { done <- serve(ctx, dbPath, listenOnly, testLogger(t), func(a net.Addr) { ready <- a }) }()
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
	}
	run()
	for _, name := range []string{"results", "controlnet", "ipadapter", "regions", "thumbnails", "downloads", "civitai"} {
		if info, err := os.Stat(filepath.Join(root, "new", name)); err != nil || !info.IsDir() {
			t.Errorf("expected the folder %s next to the database: %v", name, err)
		}
	}
	settings := storedSettings(t, dbPath)
	if settings["results_dir"] != filepath.Join(root, "new", "results") || settings["civitai_cache_dir"] != filepath.Join(root, "new", "civitai") {
		t.Errorf("stored settings = %v", settings)
	}

	// The next start reads the settings from the database: a folder changed
	// there is the one that is made.
	other := filepath.Join(root, "elsewhere")
	db, err := store.Open(t.Context(), dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Repos().Settings.Save(t.Context(), map[string]string{"results_dir": other}); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	run()
	if info, err := os.Stat(other); err != nil || !info.IsDir() {
		t.Errorf("the folder stored in the database was not made: %v", err)
	}
}

// storedSettings reads the settings of a database file that is not in use.
func storedSettings(t *testing.T, dbPath string) map[string]string {
	t.Helper()
	db, err := store.Open(t.Context(), dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	}()
	got, err := db.Repos().Settings.All(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func TestServeDirectoryFailure(t *testing.T) {
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	env := testEnv(t, map[string]string{"BOS_RESULTS_DIR": filepath.Join(file, "r")})
	err := serve(t.Context(), dbOf(env), env, testLogger(t), nil)
	if appErr := routeOf(t, err); !appErr.HasRoute("serve->EnsureDirs.FileStoreFailure") {
		t.Errorf("unexpected failure path: %s", appErr.Route())
	}
}

func TestServeStoreFailure(t *testing.T) {
	// A folder where the database file should be: it exists, so it is opened, and it is not a database.
	err := serve(t.Context(), t.TempDir(), testEnv(t, nil), testLogger(t), nil)
	if appErr := routeOf(t, err); !appErr.HasRoute("serve->openDatabase") || appErr.Tag != "StoreFailure" {
		t.Errorf("unexpected failure path: %s (tag %s)", appErr.Route(), appErr.Tag)
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
	err = serve(t.Context(), dbOf(env), env, testLogger(t), nil)
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
	go func() { done <- serve(ctx, dbOf(env), env, testLogger(t), func(a net.Addr) { addrCh <- a }) }()
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
				env := testEnv(t, map[string]string{"BOS_LOG_LEVEL": level})
				done <- serve(ctx, dbOf(env), env, logger, func(a net.Addr) { addr <- a }, withLogLevel(lv))
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

func TestOverrideEnv(t *testing.T) {
	base := func(k string) string { return map[string]string{"BOS_LOG_LEVEL": "info", "BOS_DATABASE": "d"}[k] }

	if got := overrideEnv(base, "BOS_LOG_LEVEL", ""); got("BOS_LOG_LEVEL") != "info" {
		t.Error("an empty value must leave the environment alone")
	}
	got := overrideEnv(base, "BOS_LOG_LEVEL", "debug")
	if got("BOS_LOG_LEVEL") != "debug" || got("BOS_DATABASE") != "d" {
		t.Errorf("override = %q, database = %q", got("BOS_LOG_LEVEL"), got("BOS_DATABASE"))
	}
}

func TestLogLevelFlagBeatsTheConfiguration(t *testing.T) {
	level := &slog.LevelVar{}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	ready := make(chan net.Addr, 1)
	done := make(chan error, 1)
	// The environment (and so the file) says warn; the flag says debug.
	env := overrideEnv(testEnv(t, map[string]string{"BOS_LOG_LEVEL": "warn"}), "BOS_LOG_LEVEL", "debug")
	go func() {
		done <- serve(ctx, dbOf(env), env, testLogger(t), func(a net.Addr) { ready <- a }, withLogLevel(level))
	}()
	select {
	case <-ready:
	case err := <-done:
		t.Fatalf("serve returned early: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("server did not start")
	}
	if level.Level() != slog.LevelDebug {
		t.Errorf("level = %v, want debug", level.Level())
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestLogLevelFlagMustBeALevel(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bos.db")
	err := runMain([]string{"-db", path, "-log-level", "loud"})
	if appErr := routeOf(t, err); !appErr.HasRoute("serve->openDatabase->Resolve->Config.Validate.ConfigInvalid") {
		t.Errorf("unexpected failure path: %s", appErr.Route())
	}
}

// The settings page saves into the database and its restart button makes the
// program read the database again: a folder changed in the page is made by the
// next run, in the same process.
func TestSettingsPageRestartsTheProgram(t *testing.T) {
	root := t.TempDir()
	dbPath := filepath.Join(root, "bos.db")
	listenOnly := func(k string) string {
		if k == "BOS_LISTEN_ADDR" {
			return "127.0.0.1:0"
		}
		return ""
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	starts := make(chan net.Addr, 4)
	done := make(chan error, 1)
	go func() {
		done <- serveAndRestart(ctx, dbPath, listenOnly, testLogger(t), func(a net.Addr) { starts <- a }, quick)
	}()
	wait := func() string {
		select {
		case a := <-starts:
			return "http://" + a.String()
		case err := <-done:
			t.Fatalf("the program ended: %v", err)
		case <-time.After(10 * time.Second):
			t.Fatal("the program did not start")
		}
		return ""
	}
	post := func(base, path, contentType, body string) int {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+path, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Datastar-Request", "true")
		req.Header.Set("Content-Type", contentType)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = io.Copy(io.Discard, res.Body)
		if err := res.Body.Close(); err != nil {
			t.Fatal(err)
		}
		return res.StatusCode
	}

	first := wait()
	newFolder := filepath.Join(root, "moved-results")
	if _, err := os.Stat(newFolder); err == nil {
		t.Fatal("the folder is there before it is asked for")
	}
	if status := post(first, "/ui/settings", "application/x-www-form-urlencoded", "results_dir="+newFolder); status != 200 {
		t.Fatalf("save: status %d", status)
	}
	if got := storedSettings(t, dbPath)["results_dir"]; got != newFolder {
		t.Fatalf("stored results_dir = %q", got)
	}
	// Saved, but not applied until the restart.
	if _, err := os.Stat(newFolder); err == nil {
		t.Error("the folder was made before the restart")
	}
	if status := post(first, "/ui/settings/restart", "application/json", "{}"); status != 200 {
		t.Fatalf("restart: status %d", status)
	}
	second := wait()
	if info, err := os.Stat(newFolder); err != nil || !info.IsDir() {
		t.Errorf("the restarted program did not use the saved setting: %v", err)
	}
	if status := get(t, second+"/healthz"); status != 200 {
		t.Errorf("healthz after the restart = %d", status)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
