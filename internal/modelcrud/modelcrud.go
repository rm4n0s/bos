// Package modelcrud is the client of the ComfyUI extension comfyui-model-crud
// (https://github.com/rm4n0s/comfyui-model-crud), which every ComfyUI server of
// this program runs. ComfyUI itself cannot delete a file or receive a model, so
// the program uses the extension's routes under /model_crud for both: it removes
// the results it has saved from the server's `output` folder, and it uploads
// LoRAs into the server's `loras` folder.
//
// The package speaks plain net/http and knows nothing of the rest of the
// application. Every failure carries a tag of SPEC §17.2: ServerUnreachable for
// a server that does not answer, ComfyUICallCancelled for a cancelled call, and
// ModelCrudMissing, ModelCrudDisabled and ModelCrudRejected for what the
// extension says.
package modelcrud

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/rm4n0s/errors"
)

// Options tune a Client. The zero value is usable.
type Options struct {
	// HTTPClient sends the requests; the default has no overall timeout, each
	// call is bounded by CallTimeout and by the context of the caller.
	HTTPClient *http.Client
	// CallTimeout bounds one small request (default 30 s).
	CallTimeout time.Duration
	// ChunkTimeout bounds the upload of one chunk (default 2 min).
	ChunkTimeout time.Duration
	// Attempts is how many times a chunk is sent before the upload fails
	// (default 4).
	Attempts int
	// Backoff returns the wait before retry n, from 0 (default 250 ms doubling
	// up to 5 s).
	Backoff func(retry int) time.Duration
}

func (o Options) withDefaults() Options {
	if o.HTTPClient == nil {
		o.HTTPClient = &http.Client{}
	}
	if o.CallTimeout <= 0 {
		o.CallTimeout = 30 * time.Second
	}
	if o.ChunkTimeout <= 0 {
		o.ChunkTimeout = 2 * time.Minute
	}
	if o.Attempts < 1 {
		o.Attempts = 4
	}
	if o.Backoff == nil {
		o.Backoff = func(retry int) time.Duration {
			return min(250*time.Millisecond<<min(retry, 5), 5*time.Second)
		}
	}
	return o
}

// Client talks to the extension of one ComfyUI server.
type Client struct {
	base string
	opts Options
}

// New returns the client of the ComfyUI at baseURL, such as
// http://127.0.0.1:8188. It does not contact the server.
func New(baseURL string, o Options) (*Client, error) {
	u, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, errors.New("ServerHostInvalid", "the server address must be an http or https URL", "host", baseURL)
	}
	return &Client{base: strings.TrimRight(u.String(), "/"), opts: o.withDefaults()}, nil
}

// Config is what the extension reports about itself.
type Config struct {
	Version           string   `json:"version"`
	UploadEnabled     bool     `json:"upload_enabled"`
	DeleteEnabled     bool     `json:"delete_enabled"`
	MaxUploadBytes    int64    `json:"max_upload_bytes"`
	ChunkSize         int64    `json:"chunk_size"`
	MaxParallel       int      `json:"max_parallel"`
	AllowedExtensions []string `json:"allowed_extensions"`
	MediaFolders      []string `json:"media_folders"`
	MediaExtensions   []string `json:"media_extensions"`
}

// Config reads GET /model_crud/config.
func (c *Client) Config(ctx context.Context) (Config, error) {
	var cfg Config
	err := c.send(ctx, request{op: "read config", method: http.MethodGet, path: "/model_crud/config", out: &cfg})
	return cfg, err
}

// DeleteOutput removes one file of ComfyUI's `output` folder: path is the
// sub-folder and the file name joined with a slash, as ComfyUI reports them. A
// file that is already gone is not an error.
func (c *Client) DeleteOutput(ctx context.Context, path string) error {
	q := url.Values{"path": {path}}
	err := c.send(ctx, request{op: "delete output", method: http.MethodDelete, path: "/model_crud/output", query: q})
	if rejectedWith(err, "file_not_found") {
		return nil
	}
	return err
}

// request is one call of the extension.
type request struct {
	op      string
	method  string
	path    string
	query   url.Values
	body    []byte
	ctype   string
	timeout time.Duration
	out     any
}

// send makes the call and decodes a JSON answer into out. Every failure is an
// application error.
func (c *Client) send(ctx context.Context, r request) error {
	timeout := r.timeout
	if timeout <= 0 {
		timeout = c.opts.CallTimeout
	}
	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	target := c.base + r.path
	if len(r.query) > 0 {
		target += "?" + r.query.Encode()
	}
	req, err := http.NewRequestWithContext(callCtx, r.method, target, bytes.NewReader(r.body))
	if err != nil {
		return errors.NewErr("ServerHostInvalid", err).SetMetadata("operation", r.op, "host", c.base)
	}
	if r.ctype != "" {
		req.Header.Set("Content-Type", r.ctype)
	}
	resp, err := c.opts.HTTPClient.Do(req)
	if err != nil {
		return c.transportError(ctx, err, r.op)
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return c.transportError(ctx, err, r.op)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return c.statusError(r.op, resp.StatusCode, data)
	}
	if r.out == nil {
		return nil
	}
	if err := json.Unmarshal(data, r.out); err != nil {
		return errors.NewErr("ModelCrudRejected", err).SetMetadata("operation", r.op, "host", c.base, "status", resp.StatusCode, "reason", "the answer is not JSON")
	}
	return nil
}

// transportError is a call that got no answer: the caller gave up, or the
// server cannot be reached.
func (c *Client) transportError(ctx context.Context, err error, op string) error {
	if ctx.Err() != nil {
		return errors.NewErr("ComfyUICallCancelled", ctx.Err()).SetMetadata("operation", op, "host", c.base)
	}
	return errors.NewErr("ServerUnreachable", err).SetMetadata("operation", op, "host", c.base)
}

// apiError is the body of an error answer of the extension.
type apiError struct {
	Code    string `json:"error"`
	Message string `json:"message"`
}

// statusError turns an answer that is not a success into an application error.
// Extension errors carry a code in the body; a 404 without one means that the
// route does not exist, so the extension is not installed. A proxy's 502, 503
// or 504 means that ComfyUI itself is not there.
func (c *Client) statusError(op string, status int, body []byte) error {
	var api apiError
	hasCode := json.Unmarshal(body, &api) == nil && api.Code != ""
	meta := []any{"operation", op, "host", c.base, "status", status}
	switch {
	case !hasCode && status == http.StatusNotFound:
		return errors.New("ModelCrudMissing", "the server does not have the comfyui-model-crud extension (no /model_crud routes); install it in ComfyUI", meta...)
	case !hasCode && (status == http.StatusBadGateway || status == http.StatusServiceUnavailable || status == http.StatusGatewayTimeout):
		return errors.New("ServerUnreachable", "the server is not answering", meta...)
	case api.Code == "uploads_disabled" || api.Code == "delete_disabled":
		return errors.New("ModelCrudDisabled", "the extension on the server does not allow it ("+api.Code+"); see MODEL_CRUD_READ_ONLY, MODEL_CRUD_ALLOW_UPLOAD and MODEL_CRUD_ALLOW_DELETE", append(meta, "code", api.Code)...)
	}
	msg := api.Message
	if msg == "" {
		msg = "the extension answered with status " + strconv.Itoa(status)
	}
	return errors.New("ModelCrudRejected", msg, append(meta, "code", api.Code, "body", truncate(string(body), 300))...)
}

// retryable reports a failure worth another try: no answer, or an answer that
// asks to come back later.
func retryable(err error) bool {
	appErr, ok := errors.FromError(err)
	if !ok {
		return false
	}
	if appErr.Tag == "ServerUnreachable" {
		return true
	}
	status, _ := appErr.Metadata["status"].(int)
	return appErr.Tag == "ModelCrudRejected" && (status == http.StatusTooManyRequests || status >= 500)
}

// rejectedWith reports an extension error with the given code.
func rejectedWith(err error, code string) bool {
	appErr, ok := errors.FromError(err)
	if !ok || appErr.Tag != "ModelCrudRejected" {
		return false
	}
	got, _ := appErr.Metadata["code"].(string)
	return got == code
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
