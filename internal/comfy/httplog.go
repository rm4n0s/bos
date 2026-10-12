package comfy

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	comfyui "github.com/rm4n0s/comfyui-client-go"
)

// maxLoggedBody bounds how much of one request or response body is logged.
const maxLoggedBody = 4 << 20

// clientOption tells the library how to make its HTTP calls. At the debug
// level every call is logged, with the body that goes over the wire (the
// request to POST /prompt is `{"client_id": …, "prompt": {the workflow}}`) and
// ComfyUI's answer; at any other level the library's own client is used.
func clientOption(opts Options) comfyui.Option {
	if !opts.Logger.Enabled(context.Background(), slog.LevelDebug) {
		return comfyui.WithTimeout(opts.RequestTimeout)
	}
	base, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return comfyui.WithTimeout(opts.RequestTimeout)
	}
	tr := base.Clone()
	tr.ResponseHeaderTimeout = opts.RequestTimeout
	tr.MaxIdleConnsPerHost = 16
	return comfyui.WithHTTPClient(&http.Client{Transport: &loggingTransport{next: tr, logger: opts.Logger}})
}

// loggingTransport logs the calls it carries. The checks of the queue and the
// history that Wait repeats every few seconds are not logged: they would bury
// the calls that matter.
type loggingTransport struct {
	next   http.RoundTripper
	logger *slog.Logger
	n      atomic.Uint64
}

func quietPoll(req *http.Request) bool {
	if req.Method != http.MethodGet {
		return false
	}
	p := req.URL.Path
	return p == "/queue" || p == "/system_stats" || strings.HasPrefix(p, "/history")
}

// RoundTrip implements http.RoundTripper.
func (t *loggingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if quietPoll(req) {
		return t.next.RoundTrip(req)
	}
	id := t.n.Add(1)
	attrs := []any{"id", id, "method", req.Method, "url", req.URL.String()}
	if ct := req.Header.Get("Content-Type"); ct != "" {
		attrs = append(attrs, "content_type", ct)
	}
	attrs = append(attrs, requestBody(req)...)
	t.logger.Debug("HTTP request to ComfyUI", attrs...)

	start := time.Now()
	resp, err := t.next.RoundTrip(req)
	took := time.Since(start).Round(time.Millisecond).String()
	if err != nil {
		t.logger.Debug("HTTP answer from ComfyUI", "id", id, "error", err.Error(), "took", took)
		return nil, err
	}
	out := []any{"id", id, "status", resp.StatusCode, "took", took}
	if req.Method != http.MethodGet && isJSON(resp.Header.Get("Content-Type")) {
		out = append(out, t.responseBody(resp)...)
	}
	t.logger.Debug("HTTP answer from ComfyUI", out...)
	return resp, nil
}

func isJSON(contentType string) bool { return strings.Contains(contentType, "json") }

// requestBody describes a request's body: a JSON body whole, as an object; a
// streamed one (an uploaded picture or model) by its type only.
func requestBody(req *http.Request) []any {
	if req.Body == nil || req.Body == http.NoBody {
		return nil
	}
	if req.GetBody == nil || !isJSON(req.Header.Get("Content-Type")) {
		return []any{"body", "(not logged: sent as a stream)"}
	}
	rc, err := req.GetBody()
	if err != nil {
		return []any{"body", "(not logged: " + err.Error() + ")"}
	}
	defer func() { _ = rc.Close() }() // a copy of an in-memory body
	return bodyAttrs(rc)
}

// responseBody logs a JSON answer and puts the bytes back for the caller.
func (t *loggingTransport) responseBody(resp *http.Response) []any {
	head, err := io.ReadAll(io.LimitReader(resp.Body, maxLoggedBody+1))
	resp.Body = &replayBody{Reader: io.MultiReader(bytes.NewReader(head), resp.Body), closer: resp.Body}
	if err != nil {
		return []any{"body", "(not logged: " + err.Error() + ")"}
	}
	return bodyAttrs(bytes.NewReader(head))
}

type replayBody struct {
	io.Reader
	closer io.Closer
}

func (b *replayBody) Close() error { return b.closer.Close() }

// bodyAttrs reads at most maxLoggedBody bytes and returns them as a JSON value
// (so the log line holds an object, not an escaped string), or as text when
// they are not JSON or were cut.
func bodyAttrs(r io.Reader) []any {
	raw, err := io.ReadAll(io.LimitReader(r, maxLoggedBody+1))
	if err != nil {
		return []any{"body", "(not logged: " + err.Error() + ")"}
	}
	if len(raw) > maxLoggedBody {
		return []any{"body", string(raw[:maxLoggedBody]), "body_truncated", true}
	}
	if json.Valid(raw) {
		return []any{"body", json.RawMessage(raw)}
	}
	return []any{"body", string(raw)}
}
