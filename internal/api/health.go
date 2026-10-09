// Package api serves the REST API (SPEC §15).
package api

import (
	"net/http"
)

// NewHandler returns the HTTP handler of the application. In this first
// version it serves only the liveness probe, which is always open (SPEC §15.2).
func NewHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", healthz)
	return mux
}

func healthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	// A failed write means the client has gone away; there is nobody to tell.
	_, _ = w.Write([]byte(`{"status":"ok"}` + "\n"))
}
