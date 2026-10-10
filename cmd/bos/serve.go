package main

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/rm4n0s/bos/internal/api"
	"github.com/rm4n0s/bos/internal/config"
	"github.com/rm4n0s/bos/internal/files"
	"github.com/rm4n0s/bos/internal/store"
	"github.com/rm4n0s/bos/internal/ui"
	"github.com/rm4n0s/errors"
)

// shutdownTimeout bounds how long in-flight requests may take to finish after
// a termination signal.
const shutdownTimeout = 10 * time.Second

// errRestart is what serve returns when the settings page asked for a restart:
// the program stops serving as it does for a termination signal, and main
// starts it again, so that the settings it reads are the new ones.
//
// It is not a failure, so it is not an application error with a tag.
var errRestart error = restartRequest{}

type restartRequest struct{}

func (restartRequest) Error() string { return "the program was asked to restart" }

// serve follows the startup order of SPEC §2: open the database (creating it,
// and its schema, if it does not exist), read the settings stored in it, create
// the folders they name, then serve until ctx is cancelled or the settings page
// asks for a restart (errRestart). ready, if not nil, is called with the
// address once the listener is open.
func serve(ctx context.Context, dbPath string, getenv func(string) string, logger *slog.Logger, ready func(net.Addr), opts ...appOption) (err error) {
	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)

	db, cfg, overridden, err := openDatabase(ctx, dbPath, getenv, logger)
	if err != nil {
		return err
	}
	var tune appTuning
	for _, o := range opts {
		o(&tune)
	}
	if tune.logLevel != nil {
		tune.logLevel.Set(cfg.Level())
	}
	if cfg.ExposedWithoutAuth() {
		logger.Warn("listening on a non-loopback address without auth_token", "listen_addr", cfg.ListenAddr)
	}
	if err := files.EnsureDirs(cfg.Directories()); err != nil {
		return err
	}

	a, err := newApp(ctx, cfg, db, logger, overridden, opts...)
	if err != nil {
		_ = db.Close() // the failure to wire is the error to report
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
	logger.Info("listening", "addr", lis.Addr().String(), "log_level", cfg.LogLevel)
	if ready != nil {
		ready(lis.Addr())
	}
	rest := api.New(a.apiDeps(logger))
	webDeps := a.uiDeps(logger)
	webDeps.Restart = func() { cancel(errRestart) }
	web, err := ui.New(webDeps)
	if err != nil {
		return err
	}
	// The REST API owns /api/v1 and /healthz; the web interface owns the rest.
	root := http.NewServeMux()
	root.Handle("/api/v1/", rest.Handler())
	root.Handle("/healthz", rest.Handler())
	root.Handle("/", web.Handler())
	srv := &http.Server{
		Handler:           accessLog(logger, root),
		ReadHeaderTimeout: 10 * time.Second,
	}
	// The live streams never end by themselves; stop them when the shutdown starts.
	srv.RegisterOnShutdown(rest.Stop)
	srv.RegisterOnShutdown(web.Stop)
	if err := runServer(ctx, srv, lis, shutdownTimeout, logger); err != nil {
		return err
	}
	if errors.Is(context.Cause(ctx), errRestart) {
		return errRestart
	}
	return nil
}

// openDatabase opens the database file, creating it, its folder and its schema
// when they do not exist, gives a new database (or one from before the settings
// were stored in it) the default folders next to it, and resolves the
// configuration of the run from what is stored and the environment.
func openDatabase(ctx context.Context, dbPath string, getenv func(string) string, logger *slog.Logger) (*store.DB, *config.Config, []string, error) {
	isNew := false
	if dbPath != store.InMemory {
		if _, statErr := os.Stat(dbPath); statErr != nil {
			isNew = os.IsNotExist(statErr)
			if err := os.MkdirAll(filepath.Dir(dbPath), 0o750); err != nil {
				return nil, nil, nil, errors.NewErr("FileStoreFailure", err).SetMetadata("path", dbPath, "operation", "mkdir")
			}
		}
	}
	db, err := store.Open(ctx, dbPath)
	if err != nil {
		return nil, nil, nil, err
	}
	fail := func(err error) (*store.DB, *config.Config, []string, error) {
		_ = db.Close() // the failure is the error to report
		return nil, nil, nil, err
	}
	if err := db.Migrate(ctx); err != nil {
		return fail(err)
	}
	settings := db.Repos().Settings
	stored, err := settings.All(ctx)
	if err != nil {
		return fail(err)
	}
	if missing := config.MissingDefaults(stored, dbPath); len(missing) > 0 {
		if err := settings.Save(ctx, missing); err != nil {
			return fail(err)
		}
		for key, value := range missing {
			stored[key] = value
		}
	}
	if isNew {
		logger.Info("created the database", "path", dbPath, "results_dir", stored["results_dir"])
	}
	cfg, overridden, err := config.Resolve(stored, dbPath, getenv)
	if err != nil {
		return fail(err)
	}
	return db, cfg, overridden, nil
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

// accessLog logs every request at debug level: method, path, status, size and
// time. Passwords and tokens are never in a path; the query is left out.
func accessLog(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !logger.Enabled(r.Context(), slog.LevelDebug) {
			next.ServeHTTP(w, r)
			return
		}
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		start := time.Now()
		next.ServeHTTP(rec, r)
		logger.Debug("request", "method", r.Method, "path", r.URL.Path, "status", rec.status, "bytes", rec.bytes,
			"duration_ms", time.Since(start).Milliseconds(), "remote", r.RemoteAddr)
	})
}

// statusRecorder remembers what a handler answered. Unwrap keeps flushing and
// the other optional behaviours of the real writer available.
type statusRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusRecorder) Write(b []byte) (int, error) {
	n, err := s.ResponseWriter.Write(b)
	s.bytes += n
	return n, err
}

func (s *statusRecorder) Unwrap() http.ResponseWriter { return s.ResponseWriter }
