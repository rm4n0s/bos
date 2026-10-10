package main

import (
	"context"
	"log/slog"
	"time"

	"github.com/rm4n0s/bos/internal/api"
	"github.com/rm4n0s/bos/internal/civitai"
	"github.com/rm4n0s/bos/internal/comfy"
	"github.com/rm4n0s/bos/internal/config"
	"github.com/rm4n0s/bos/internal/domain"
	"github.com/rm4n0s/bos/internal/events"
	"github.com/rm4n0s/bos/internal/files"
	"github.com/rm4n0s/bos/internal/scheduler"
	"github.com/rm4n0s/bos/internal/service"
	"github.com/rm4n0s/bos/internal/store"
	"github.com/rm4n0s/bos/internal/ui"
)

// app is the object graph (SPEC §14.3, rule 3): everything is built here, once,
// and handed to whoever needs it.
type app struct {
	cfg      *config.Config
	db       *store.DB
	files    *files.Store
	events   *events.Log
	services *service.Services
	sched    *scheduler.Scheduler
}

// appOption changes how the object graph is built; tests use it to make the
// polling quick.
type appOption func(*appTuning)

type appTuning struct {
	probeInterval time.Duration
	pollInterval  time.Duration
	// logLevel, when set, is changed to the level of the configuration.
	logLevel *slog.LevelVar
}

// withLogLevel lets the configuration set the level of the logger main built.
func withLogLevel(v *slog.LevelVar) appOption { return func(t *appTuning) { t.logLevel = v } }

// newApp builds the services and the scheduler over an open, migrated store
// (SPEC §2, startup step 2). Call run to start the scheduler and close when
// done, which closes the store too.
func newApp(ctx context.Context, cfg *config.Config, db *store.DB, logger *slog.Logger, overridden []string, opts ...appOption) (*app, error) {
	return wire(ctx, cfg, db, logger, overridden, opts)
}

func wire(ctx context.Context, cfg *config.Config, db *store.DB, logger *slog.Logger, overridden []string, opts []appOption) (*app, error) {
	var tune appTuning
	for _, o := range opts {
		o(&tune)
	}
	fs, err := files.NewStore(cfg.Directories())
	if err != nil {
		return nil, err
	}
	log := events.NewLog(cfg.EventLogSize, nil)
	sched := scheduler.New(scheduler.Deps{
		DB: db, Files: fs, Events: log, Logger: logger,
		NewClient: func(s domain.Server) (comfy.Client, error) {
			c, err := comfy.NewServer(s.Host, comfy.Options{Logger: logger, PollInterval: tune.pollInterval})
			if err != nil {
				return nil, err
			}
			return c, nil
		},
		NewSeed:       service.RandomSeed,
		DeleteOutputs: cfg.DeleteComfyOutputs,
		Grace:         time.Duration(cfg.GraceSeconds) * time.Second,
		MaxAttempts:   cfg.MaxJobAttempts,
		ProbeInterval: tune.probeInterval,
	})
	svc := service.New(service.Deps{
		Store: service.NewStore(db), Files: fs, Fleet: sched, Dispatcher: sched, Logger: logger,
		Dirs:    service.Dirs{Results: cfg.ResultsDir, Regions: cfg.RegionsDir, Downloads: cfg.DownloadsDir, CivitaiCache: cfg.CivitaiCacheDir},
		Limits:  service.Limits{PreviewSoftLimit: cfg.PreviewSoftMax},
		Civitai: civitai.New(civitai.Options{Token: cfg.CivitaiToken, Domain: cfg.CivitaiDomain, Logger: logger}),
		Events:  log, Pause: time.Duration(cfg.CivitaiPauseSeconds) * time.Second, Background: ctx,
		DBPath: cfg.Database, Overridden: overridden,
	})
	return &app{cfg: cfg, db: db, files: fs, events: log, services: svc, sched: sched}, nil
}

// apiDeps is what the REST API is built from.
func (a *app) apiDeps(logger *slog.Logger) api.Deps {
	c := a.cfg
	return api.Deps{
		Services: a.services, Events: a.events, Files: a.files,
		Dirs: api.Dirs{
			Results: c.ResultsDir, ControlNet: c.ControlNetDir, IPAdapter: c.IPAdapterDir,
			Regions: c.RegionsDir, Thumbnails: c.ThumbnailsDir, Downloads: c.DownloadsDir,
		},
		AuthToken: c.AuthToken, CORSAllowList: c.CORSAllowList, UploadLimit: c.UploadLimit, Logger: logger,
	}
}

// uiDeps is what the web interface is built from.
func (a *app) uiDeps(logger *slog.Logger) ui.Deps {
	c := a.cfg
	return ui.Deps{
		Services: a.services, Events: a.events, Files: a.files, Dirs: a.apiDeps(logger).Dirs,
		AuthToken: c.AuthToken, UploadLimit: c.UploadLimit, Logger: logger,
	}
}

// run starts the scheduler (SPEC §2, startup step 3) and returns a function that
// stops it and waits for it. The scheduler stops when ctx ends or when the
// returned function is called.
func (a *app) run(ctx context.Context, logger *slog.Logger) (stop func()) {
	ctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		if err := a.sched.Run(ctx); err != nil {
			logError(logger, err)
		}
	}()
	return func() {
		cancel()
		<-done
	}
}

// close waits for the imports that are still running, which stop when the
// program does, and releases the store.
func (a *app) close() error {
	a.services.Civitai.Wait()
	return a.db.Close()
}
