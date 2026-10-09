package api

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strconv"

	"github.com/rm4n0s/bos/internal/domain"
	"github.com/rm4n0s/errors"
)

// writeJSON writes v as the JSON body of a response.
func (a *API) writeJSON(w http.ResponseWriter, status int, v any) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		a.Logger.Error("could not encode a response", "error", err.Error())
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":{"tag":"` + internalTag + `","message":"internal error"}}` + "\n"))
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	// A failed write means the client has gone away; there is nobody to tell.
	_, _ = w.Write(buf.Bytes())
}

// created answers 201 with the new resource and its Location header.
func (a *API) created(w http.ResponseWriter, path string, id domain.ID, v any) {
	w.Header().Set("Location", BasePath+path+"/"+strconv.FormatInt(id, 10))
	a.writeJSON(w, http.StatusCreated, v)
}

// noContent answers 204.
func noContent(w http.ResponseWriter) { w.WriteHeader(http.StatusNoContent) }

// badRequest is the error of a request that cannot be read.
func badRequest(message string, meta ...any) error {
	return errors.New("InvalidRequest", message, meta...)
}

// decodeJSON reads the JSON body of r into v. Unknown fields, trailing data and
// bodies over 8 MiB are refused (SPEC §15.1).
func decodeJSON(w http.ResponseWriter, r *http.Request, v any) error {
	return decodeStrict(http.MaxBytesReader(w, r.Body, maxJSONBody), v)
}

func decodeStrict(body io.Reader, v any) error {
	dec := json.NewDecoder(body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		var tooBig *http.MaxBytesError
		switch {
		case errors.As(err, &tooBig):
			return errors.New("UploadTooLarge", "the request body is too large", "limit_bytes", tooBig.Limit)
		case errors.Is(err, io.EOF):
			return badRequest("the request body is empty")
		}
		return badRequest("the request body is not valid: "+err.Error(), "reason", err.Error())
	}
	if dec.More() {
		return badRequest("the request body has data after the JSON value")
	}
	return nil
}

// pathID reads the {name} segment of the path as an identifier.
func pathID(r *http.Request, name string) (domain.ID, error) {
	raw := r.PathValue(name)
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id < 1 {
		return 0, badRequest("the "+name+" in the path is not a positive integer", "value", raw)
	}
	return id, nil
}

// pageOf reads the page and page_size query parameters (SPEC §15.1). Missing
// values take the defaults; a size above 200 is cut to 200.
func pageOf(r *http.Request) (domain.Page, error) {
	var p domain.Page
	q := r.URL.Query()
	for name, dst := range map[string]*int{"page": &p.Number, "page_size": &p.Size} {
		raw := q.Get(name)
		if raw == "" {
			continue
		}
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 {
			return p, badRequest("the "+name+" must be a positive integer", "value", raw)
		}
		*dst = n
	}
	return p.Normalised(), nil
}
