package main

import (
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
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
	err := serve(t.Context(), filepath.Join(t.TempDir(), "none.json"), testEnv(t, nil), testLogger(t), nil)
	if appErr := routeOf(t, err); !appErr.HasRoute("serve->Load.ConfigNotReadable") {
		t.Errorf("unexpected failure path: %s", appErr.Route())
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
	if appErr := routeOf(t, err); !appErr.HasRoute("serve->Open.StoreFailure") {
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
