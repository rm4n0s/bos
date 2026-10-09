package api

import (
	"bufio"
	"bytes"
	"io"
	"mime"
	"net/http"
	"slices"
	"sort"

	"github.com/rm4n0s/errors"
)

// Image types of SPEC §10 and decision D6: thumbnails are PNG, JPEG or GIF;
// reference images may also be WebP. A mask reference must be PNG, because a
// lossy format would add stray colours (SPEC §7.3).
var (
	thumbnailTypes = []string{"image/png", "image/jpeg", "image/gif"}
	referenceTypes = []string{"image/png", "image/jpeg", "image/gif", "image/webp"}
	maskTypes      = []string{"image/png"}
)

// uploadKind says where an uploaded part is stored and what it may be.
type uploadKind struct {
	dir   string
	mimes []string
}

// uploadSet is the files one request stored, by form field.
type uploadSet struct{ files map[string]string }

// paths returns the stored paths, in field order.
func (u *uploadSet) paths() []string {
	names := make([]string, 0, len(u.files))
	for f := range u.files {
		names = append(names, f)
	}
	sort.Strings(names)
	out := make([]string, len(names))
	for i, f := range names {
		out[i] = u.files[f]
	}
	return out
}

// has reports whether path was stored by this request.
func (u *uploadSet) has(path string) bool { return slices.Contains(u.paths(), path) }

// discard removes the files of a request that did not succeed. A file that
// cannot be removed is logged: the request has already failed.
func (a *API) discard(u *uploadSet) {
	if u == nil {
		return
	}
	for _, p := range u.paths() {
		if err := a.Files.Remove(p); err != nil {
			a.Logger.Warn("could not remove an uploaded file", "path", p, "error", err.Error())
		}
	}
}

// readBody reads a request whose body is either JSON, or multipart form data
// with the JSON in a part named "data" and the files in the parts kindOf
// accepts (SPEC §15.2). Files are stored as they arrive; on any error they are
// removed again.
func (a *API) readBody(w http.ResponseWriter, r *http.Request, v any, kindOf func(field string) (uploadKind, bool)) (*uploadSet, error) {
	set := &uploadSet{files: map[string]string{}}
	media, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if media != "multipart/form-data" {
		return set, decodeJSON(w, r, v)
	}
	if a.UploadLimit > 0 {
		r.Body = http.MaxBytesReader(w, r.Body, 32*a.UploadLimit+maxJSONBody)
	}
	if err := a.readParts(r, v, kindOf, set); err != nil {
		a.discard(set)
		return nil, tooLarge(err)
	}
	return set, nil
}

// tooLarge turns the error of a body over the limit into UploadTooLarge.
func tooLarge(err error) error {
	var big *http.MaxBytesError
	if errors.As(err, &big) {
		return errors.New("UploadTooLarge", "the request is larger than the upload limit", "limit_bytes", big.Limit)
	}
	return err
}

func (a *API) readParts(r *http.Request, v any, kindOf func(string) (uploadKind, bool), set *uploadSet) error {
	mr, err := r.MultipartReader()
	if err != nil {
		return badRequest("the multipart body is not valid", "reason", err.Error())
	}
	sawData := false
	for {
		part, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return tooLargeOr(err, badRequest("the multipart body is not valid", "reason", err.Error()))
		}
		field := part.FormName()
		if field == "data" {
			if sawData {
				return badRequest("the part data is given twice")
			}
			sawData = true
			raw, err := io.ReadAll(io.LimitReader(part, maxJSONBody+1))
			if err != nil {
				return tooLargeOr(err, badRequest("the part data cannot be read", "reason", err.Error()))
			}
			if len(raw) > maxJSONBody {
				return errors.New("UploadTooLarge", "the part data is too large", "limit_bytes", maxJSONBody)
			}
			if err := decodeStrict(bytes.NewReader(raw), v); err != nil {
				return err
			}
			continue
		}
		kind, ok := kindOf(field)
		if !ok {
			return badRequest("the form has no file part of this name", "part", field)
		}
		if _, dup := set.files[field]; dup {
			return badRequest("the file part is given twice", "part", field)
		}
		br := bufio.NewReaderSize(part, 512)
		head, _ := br.Peek(512) // a short file is fine; a read error shows in the copy
		if got := detectedType(head); !slices.Contains(kind.mimes, got) {
			return badRequest("the file is not of an accepted type", "part", field, "type", got, "accepted", kind.mimes)
		}
		path, err := a.Files.SaveUpload(kind.dir, part.FileName(), br, a.UploadLimit)
		if err != nil {
			return err
		}
		set.files[field] = path
	}
}

// tooLargeOr returns UploadTooLarge when err comes from the body limit, and
// fallback otherwise.
func tooLargeOr(err, fallback error) error {
	if t := tooLarge(err); t != err { //nolint:errorlint // tooLarge returns its argument unchanged when it is not the limit
		return t
	}
	return fallback
}

// detectedType sniffs the content type of a file, ignoring parameters.
func detectedType(head []byte) string {
	t, _, err := mime.ParseMediaType(http.DetectContentType(head))
	if err != nil {
		return "application/octet-stream"
	}
	return t
}
