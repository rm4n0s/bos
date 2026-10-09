// Package ui serves the web interface (SPEC §16): server-rendered HTML that
// Datastar enhances. Like the REST API it is a thin front end over
// internal/service; it never calls the REST API.
package ui

import (
	"context"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"time"

	"github.com/rm4n0s/bos/internal/events"
	"github.com/rm4n0s/bos/internal/files"
	"github.com/rm4n0s/bos/internal/filesrv"
	"github.com/rm4n0s/bos/internal/service"
)

// defaultKeepAlive is how often a live stream sends a comment (SPEC §16.4).
const defaultKeepAlive = 15 * time.Second

// Deps is everything the interface needs, handed over by main.
type Deps struct {
	Services *service.Services
	Events   *events.Log
	Files    *files.Store
	Dirs     filesrv.Dirs
	// AuthToken, when not empty, makes the interface ask for it once and keep a
	// signed cookie (SPEC §16.6).
	AuthToken string
	// UploadLimit is the largest accepted uploaded file in bytes; 0 means none.
	UploadLimit int64
	Logger      *slog.Logger
	// KeepAlive is the interval of the live streams' comment lines.
	KeepAlive time.Duration
}

// UI is the web interface.
type UI struct {
	Deps
	mux        *http.ServeMux
	patterns   []string
	tpl        *templates
	fileServer *filesrv.Server

	stop     context.Context
	stopFunc context.CancelFunc
}

// New builds the interface: it parses the embedded templates and registers the
// routes.
func New(deps Deps) (*UI, error) {
	if deps.Logger == nil {
		deps.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	if deps.KeepAlive <= 0 {
		deps.KeepAlive = defaultKeepAlive
	}
	u := &UI{Deps: deps, mux: http.NewServeMux(), fileServer: &filesrv.Server{Files: deps.Files, Dirs: deps.Dirs, Logger: deps.Logger}}
	u.stop, u.stopFunc = context.WithCancel(context.Background())
	tpl, err := parseTemplates(u.funcs())
	if err != nil {
		return nil, err
	}
	u.tpl = tpl
	static, err := fs.Sub(staticFS, "static")
	if err != nil {
		return nil, err
	}
	u.mux.Handle("GET /static/", http.StripPrefix("/static/", staticHandler(static)))
	u.registerAll()
	return u, nil
}

// handle registers a route and remembers its pattern.
func (u *UI) handle(pattern string, h http.HandlerFunc) {
	u.patterns = append(u.patterns, pattern)
	u.mux.HandleFunc(pattern, h)
}

// Patterns returns the routing patterns that are registered, for tests.
func (u *UI) Patterns() []string { return append([]string(nil), u.patterns...) }

// Stop ends the live streams so that a graceful shutdown does not wait for
// them.
func (u *UI) Stop() { u.stopFunc() }

// Handler returns the handler to serve: the routes behind the security
// headers, the sign-in check and the request-forgery check.
func (u *UI) Handler() http.Handler {
	return u.recoverer(u.securityHeaders(u.authenticate(u.csrf(u.mux))))
}

// registerAll registers the routes of SPEC §16.2.
func (u *UI) registerAll() {
	u.authRoutes()
	u.homeRoutes()
	u.serverRoutes()
	u.generatorRoutes()
	u.fixerRoutes()
	u.projectRoutes()
	u.groupRoutes()
	u.itemRoutes()
	u.commandRoutes()
	u.fileRoutes()
}

func (u *UI) fileRoutes() {
	u.handle("GET /files/{kind}/{name...}", func(w http.ResponseWriter, r *http.Request) {
		if err := u.fileServer.Serve(w, r, r.PathValue("kind"), r.PathValue("name")); err != nil {
			http.NotFound(w, r)
		}
	})
}

// recoverer turns a panic in a handler into a plain 500.
func (u *UI) recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				if v == http.ErrAbortHandler { //nolint:errorlint // net/http compares the panic value itself
					panic(v)
				}
				u.Logger.Error("panic in a handler", "path", r.URL.Path, "panic", v)
				http.Error(w, "internal error", http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}
