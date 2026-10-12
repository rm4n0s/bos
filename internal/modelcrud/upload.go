package modelcrud

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/rm4n0s/errors"
)

// session is the answer to the creation of an upload.
type session struct {
	ID          string `json:"upload_id"`
	Path        string `json:"path"`
	ChunkSize   int64  `json:"chunk_size"`
	TotalChunks int64  `json:"total_chunks"`
}

type modelFile struct {
	Path string `json:"path"`
	Size int64  `json:"size"`
}

// UploadModel stores size bytes read from r as <folder>/<subfolder>/<filename>
// below ComfyUI's models directory, such as the `loras` folder, and returns the
// path the file has there, relative to the folder. The file appears only when
// the upload is complete, so ComfyUI never sees half a model.
//
// A file of the same path and size is already there when an earlier upload
// finished; nothing is sent and r is not read. A file of the same path and
// another size is replaced. Chunks are sent one after the other, and a chunk
// that fails to arrive is sent again; an upload that fails is cancelled on the
// server, which would otherwise keep its disk space for a day.
func (c *Client) UploadModel(ctx context.Context, folder, subfolder, filename string, size int64, r io.Reader) (string, error) {
	sess, skip, err := c.openUpload(ctx, folder, subfolder, filename, size)
	if err != nil || skip != "" {
		return skip, err
	}
	if err := c.sendChunks(ctx, "/model_crud/uploads/"+url.PathEscape(sess.ID), sess, size, r); err != nil {
		c.abort(ctx, sess.ID)
		return "", err
	}
	var done modelFile
	if err := c.send(ctx, request{op: "complete upload", method: http.MethodPost, path: "/model_crud/uploads/" + url.PathEscape(sess.ID) + "/complete", out: &done}); err != nil {
		c.abort(ctx, sess.ID)
		return "", err
	}
	return done.Path, nil
}

// openUpload creates the upload session. When the file is already there with
// the same size it returns its path as skip instead. A file with another size
// is replaced.
func (c *Client) openUpload(ctx context.Context, folder, subfolder, filename string, size int64) (sess session, skip string, err error) {
	body := map[string]any{"folder": folder, "filename": filename, "size": size, "overwrite": false}
	if subfolder != "" {
		body["subfolder"] = subfolder
	}
	err = c.create(ctx, "/model_crud/uploads", body, &sess)
	if !rejectedWith(err, "file_exists") {
		return sess, "", err
	}
	existing, err := c.modelSize(ctx, folder, joinPath(subfolder, filename))
	if err != nil {
		return sess, "", err
	}
	if existing == size {
		return sess, joinPath(subfolder, filename), nil
	}
	body["overwrite"] = true
	return sess, "", c.create(ctx, "/model_crud/uploads", body, &sess)
}

func (c *Client) create(ctx context.Context, path string, body map[string]any, out *session) error {
	data, err := json.Marshal(body)
	if err != nil {
		return errors.NewErr("ModelCrudRejected", err).SetMetadata("operation", "create upload", "host", c.base)
	}
	return c.send(ctx, request{op: "create upload", method: http.MethodPost, path: path, body: data, ctype: "application/json", out: out})
}

// modelSize is the size of a file of a model folder, -1 when it is not listed.
func (c *Client) modelSize(ctx context.Context, folder, path string) (int64, error) {
	var list struct {
		Files []modelFile `json:"files"`
	}
	if err := c.send(ctx, request{op: "list models", method: http.MethodGet, path: "/model_crud/models/" + url.PathEscape(folder), out: &list}); err != nil {
		return 0, err
	}
	for _, f := range list.Files {
		if f.Path == path {
			return f.Size, nil
		}
	}
	return -1, nil
}

// sendChunks reads r in the chunks the server asked for and sends each one.
func (c *Client) sendChunks(ctx context.Context, base string, sess session, size int64, r io.Reader) error {
	if sess.ChunkSize < 1 || sess.TotalChunks < 1 {
		return errors.New("ModelCrudRejected", "the extension gave an upload without chunks", "operation", "create upload", "host", c.base)
	}
	buf := make([]byte, sess.ChunkSize)
	for i := int64(0); i < sess.TotalChunks; i++ {
		want := min(sess.ChunkSize, size-i*sess.ChunkSize)
		if _, err := io.ReadFull(r, buf[:want]); err != nil {
			return errors.NewErr("FileStoreFailure", err).SetMetadata("operation", "read", "chunk", i)
		}
		if err := c.sendChunk(ctx, base, i, buf[:want]); err != nil {
			return err
		}
	}
	return nil
}

// sendChunk sends one chunk, again after a pause when the server did not
// answer or asked to come back later. A chunk sent twice is harmless.
func (c *Client) sendChunk(ctx context.Context, base string, index int64, chunk []byte) error {
	var err error
	for attempt := 0; attempt < c.opts.Attempts; attempt++ {
		if attempt > 0 {
			if err := sleep(ctx, c.opts.Backoff(attempt-1)); err != nil {
				return c.transportError(ctx, err, "send chunk")
			}
		}
		err = c.send(ctx, request{
			op: "send chunk", method: http.MethodPut, path: base + "/chunks/" + strconv.FormatInt(index, 10),
			body: chunk, ctype: "application/octet-stream", timeout: c.opts.ChunkTimeout,
		})
		if err == nil || !retryable(err) {
			return err
		}
	}
	return err
}

// abort cancels an upload that failed. It runs even when the caller's context
// is the reason for the failure; if it fails too, the server's own expiry
// cleans up.
func (c *Client) abort(ctx context.Context, id string) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), c.opts.CallTimeout)
	defer cancel()
	_ = c.send(ctx, request{op: "cancel upload", method: http.MethodDelete, path: "/model_crud/uploads/" + url.PathEscape(id)})
}

func sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func joinPath(subfolder, filename string) string {
	if subfolder == "" {
		return filename
	}
	return subfolder + "/" + filename
}
