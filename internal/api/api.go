// Package api serves the REST API (SPEC §15). It is a thin front end over
// internal/service: handlers read a request, call a service and write the
// answer or the error envelope; no business rule lives here.
package api

import (
	"context"
	"crypto/subtle"
	"io"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/rm4n0s/bos/internal/events"
	"github.com/rm4n0s/bos/internal/files"
	"github.com/rm4n0s/bos/internal/filesrv"
	"github.com/rm4n0s/bos/internal/service"
)

// BasePath is the prefix of every versioned route (SPEC §15.1).
const BasePath = "/api/v1"

// maxJSONBody bounds a JSON request body (SPEC §15.1): workflows are large.
const maxJSONBody = 8 << 20

// defaultKeepAlive is how often the event stream sends a comment (SPEC §15.4).
const defaultKeepAlive = 15 * time.Second

// Dirs are the storage directories the API writes uploads to and serves files
// from (SPEC §2).
type Dirs = filesrv.Dirs

// Deps is everything the API needs, handed over by main (SPEC §14.3, rule 3).
type Deps struct {
	Services *service.Services
	Events   *events.Log
	Files    *files.Store
	Dirs     Dirs
	// AuthToken, when not empty, is the bearer token every /api/v1 request needs.
	AuthToken string
	// CORSAllowList names the origins that may call the API cross-origin.
	CORSAllowList []string
	// UploadLimit is the largest accepted uploaded file in bytes; 0 means none.
	UploadLimit int64
	Logger      *slog.Logger
	// KeepAlive is the interval of the event stream's comment lines; the default
	// is 15 seconds.
	KeepAlive time.Duration
}

// API is the REST front end.
type API struct {
	Deps
	mux        *http.ServeMux
	fileServer *filesrv.Server
	routes     []route

	stop     context.Context
	stopFunc context.CancelFunc
}

// New builds the API and registers its routes.
func New(deps Deps) *API {
	if deps.Logger == nil {
		deps.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	if deps.KeepAlive <= 0 {
		deps.KeepAlive = defaultKeepAlive
	}
	a := &API{Deps: deps, mux: http.NewServeMux(), fileServer: &filesrv.Server{Files: deps.Files, Dirs: deps.Dirs, Logger: deps.Logger}}
	a.stop, a.stopFunc = context.WithCancel(context.Background())
	a.mux.HandleFunc("GET /healthz", healthz)
	a.registerAll()
	return a
}

// Stop ends the long-lived responses (the event stream) so that a graceful
// shutdown does not wait for them. Call it when the HTTP server starts to shut
// down.
func (a *API) Stop() { a.stopFunc() }

// Handler returns the handler to serve: the routes behind the recovery, CORS
// and authentication layers.
func (a *API) Handler() http.Handler {
	return a.recoverer(a.cors(a.authenticate(a.mux)))
}

func healthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	// A failed write means the client has gone away; there is nobody to tell.
	_, _ = w.Write([]byte(`{"status":"ok"}` + "\n"))
}

// authenticate requires the bearer token on every /api/v1 request when one is
// configured (SPEC §15.1). /healthz stays open.
func (a *API) authenticate(next http.Handler) http.Handler {
	if a.AuthToken == "" {
		return next
	}
	want := []byte(a.AuthToken)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, BasePath+"/") && r.Method != http.MethodOptions {
			token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
			if !ok || subtle.ConstantTimeCompare([]byte(token), want) != 1 {
				w.Header().Set("WWW-Authenticate", "Bearer")
				a.writeProblem(w, r, http.StatusUnauthorized, "Unauthorized", "a valid bearer token is required")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// cors is off unless an allow-list is configured (SPEC §15.1).
func (a *API) cors(next http.Handler) http.Handler {
	if len(a.CORSAllowList) == 0 {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" && slices.Contains(a.CORSAllowList, origin) {
			h := w.Header()
			h.Set("Access-Control-Allow-Origin", origin)
			h.Add("Vary", "Origin")
			if r.Method == http.MethodOptions {
				h.Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
				h.Set("Access-Control-Allow-Headers", "Authorization, Content-Type, Last-Event-ID")
				h.Set("Access-Control-Max-Age", "600")
				w.WriteHeader(http.StatusNoContent)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// recoverer turns a panic in a handler into a 500 envelope, so one bad request
// does not take the connection down silently.
func (a *API) recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				if v == http.ErrAbortHandler { //nolint:errorlint // net/http compares the panic value itself
					panic(v)
				}
				a.Logger.Error("panic in a handler", "path", r.URL.Path, "panic", v)
				a.writeProblem(w, r, http.StatusInternalServerError, "InternalError", "internal error")
			}
		}()
		next.ServeHTTP(w, r)
	})
}
