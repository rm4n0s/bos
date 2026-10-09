package main

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/rm4n0s/bos/internal/api"
	"github.com/rm4n0s/bos/internal/config"
	"github.com/rm4n0s/bos/internal/files"
	"github.com/rm4n0s/errors"
)

// shutdownTimeout bounds how long in-flight requests may take to finish after
// a termination signal.
const shutdownTimeout = 10 * time.Second

// serve follows the startup order of SPEC §2: load the configuration and
// create the directories, open the store, then serve until ctx is cancelled.
// ready, if not nil, is called with the address once the listener is open.
func serve(ctx context.Context, configPath string, getenv func(string) string, logger *slog.Logger, ready func(net.Addr)) (err error) {
	cfg, err := config.Load(configPath, getenv)
	if err != nil {
		return err
	}
	if cfg.ExposedWithoutAuth() {
		logger.Warn("listening on a non-loopback address without auth_token", "listen_addr", cfg.ListenAddr)
	}
	if err := files.EnsureDirs(cfg.Directories()); err != nil {
		return err
	}

	a, err := newApp(ctx, cfg, logger)
	if err != nil {
		return err
	}
	defer func() {
		closeErr := a.close()
		switch {
		case closeErr == nil:
		case err == nil:
			err = closeErr
		default:
			logError(logger, closeErr)
		}
	}()

	stopScheduler := a.run(ctx, logger)
	defer stopScheduler()

	lis, err := (&net.ListenConfig{}).Listen(ctx, "tcp", cfg.ListenAddr)
	if err != nil {
		return errors.NewErr("ListenFailed", err).SetMetadata("listen_addr", cfg.ListenAddr)
	}
	logger.Info("listening", "addr", lis.Addr().String())
	if ready != nil {
		ready(lis.Addr())
	}
	srv := &http.Server{
		Handler:           api.NewHandler(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	return runServer(ctx, srv, lis, shutdownTimeout, logger)
}

// runServer serves on lis until ctx is cancelled, then waits up to
// shutdownWait for in-flight requests.
func runServer(ctx context.Context, srv *http.Server, lis net.Listener, shutdownWait time.Duration, logger *slog.Logger) error {
	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.Serve(lis) }()

	select {
	case err := <-serveErr:
		return errors.NewErr("ServeFailed", err)
	case <-ctx.Done():
	}

	logger.Info("shutting down")
	shutCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), shutdownWait)
	defer cancel()
	if err := srv.Shutdown(shutCtx); err != nil {
		return errors.NewErr("ShutdownFailed", err)
	}
	if err := <-serveErr; !errors.Is(err, http.ErrServerClosed) {
		return errors.NewErr("ServeFailed", err)
	}
	return nil
}

// logError logs err once, with its replayable JSON form when it is an
// application error (SPEC §17.4).
func logError(logger *slog.Logger, err error) {
	if appErr, ok := errors.FromError(err); ok {
		if j, jerr := appErr.ToJson(); jerr == nil {
			logger.Error("fatal", "error", j)
			return
		}
	}
	logger.Error("fatal", "error", err.Error())
}
