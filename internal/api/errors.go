package api

import (
	"encoding/json"
	"net/http"

	"github.com/rm4n0s/bos/internal/errtags"
	"github.com/rm4n0s/errors"
)

// internalTag is the tag of an envelope for an error that did not come from the
// application's own error type, or that is withheld from the client.
const internalTag = "InternalError"

// envelope is the error body of SPEC §15.1.
type envelope struct {
	Error envelopeError `json:"error"`
}

type envelopeError struct {
	Tag      string         `json:"tag"`
	Message  string         `json:"message"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

// writeError answers with the envelope of err and the status of its tag. The
// stack and the route are never sent; they are logged once, here (SPEC §17.4).
// A server-side failure is reported to the client without its details.
func (a *API) writeError(w http.ResponseWriter, r *http.Request, err error) {
	appErr, ok := errors.FromError(err)
	if !ok {
		a.Logger.Error("request failed", "method", r.Method, "path", r.URL.Path, "error", err.Error())
		a.writeProblem(w, r, http.StatusInternalServerError, internalTag, "internal error")
		return
	}
	status := errtags.Status(appErr.Tag)
	a.logFailure(r, status, appErr)
	if status == http.StatusInternalServerError {
		a.writeJSON(w, status, envelope{Error: envelopeError{Tag: appErr.Tag, Message: "internal error"}})
		return
	}
	a.writeJSON(w, status, envelope{Error: envelopeError{Tag: appErr.Tag, Message: appErr.Message, Metadata: publicMetadata(appErr.Metadata)}})
}

// writeProblem answers with an envelope for a failure the API itself detects.
func (a *API) writeProblem(w http.ResponseWriter, r *http.Request, status int, tag, message string) {
	if status >= http.StatusInternalServerError {
		a.Logger.Error("request failed", "method", r.Method, "path", r.URL.Path, "tag", tag, "message", message)
	}
	a.writeJSON(w, status, envelope{Error: envelopeError{Tag: tag, Message: message}})
}

// logFailure logs an application error with its replayable JSON form. Client
// mistakes are routine and logged at debug level.
func (a *API) logFailure(r *http.Request, status int, appErr *errors.Error) {
	level := a.Logger.Warn
	if status < http.StatusInternalServerError {
		level = a.Logger.Debug
	}
	if j, err := appErr.ToJson(); err == nil {
		level("request failed", "method", r.Method, "path", r.URL.Path, "status", status, "error", j)
		return
	}
	level("request failed", "method", r.Method, "path", r.URL.Path, "status", status, "tag", appErr.Tag)
}

// publicMetadata keeps the metadata a client may see: what can be written as
// JSON, without the server's file paths.
func publicMetadata(m map[string]any) map[string]any {
	if len(m) == 0 {
		return nil
	}
	out := make(map[string]any, len(m))
	for k, v := range m {
		if k == "path" {
			continue
		}
		if _, err := json.Marshal(v); err != nil {
			continue
		}
		out[k] = v
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
