package api

import (
	"bytes"
	"io"
	"mime"
	"net/http"

	"github.com/rm4n0s/bos/internal/uploads"
	"github.com/rm4n0s/errors"
)

// readBody reads a request whose body is either JSON, or multipart form data
// with the JSON in a part named "data" and the files in the parts kindOf
// accepts (SPEC §15.2). Files are stored as they arrive; on any error they are
// removed again.
func (a *API) readBody(w http.ResponseWriter, r *http.Request, v any, kindOf func(field string) (uploads.Kind, bool)) (*uploads.Set, error) {
	set := uploads.NewSet()
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

// discard removes the files of a request that did not succeed.
func (a *API) discard(s *uploads.Set) { uploads.Discard(a.Files, s, a.Logger) }

// tooLarge turns the error of a body over the limit into UploadTooLarge.
func tooLarge(err error) error {
	var big *http.MaxBytesError
	if errors.As(err, &big) {
		return errors.New("UploadTooLarge", "the request is larger than the upload limit", "limit_bytes", big.Limit)
	}
	return err
}

func (a *API) readParts(r *http.Request, v any, kindOf func(string) (uploads.Kind, bool), set *uploads.Set) error {
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
		if set.Has(field) {
			return badRequest("the file part is given twice", "part", field)
		}
		path, err := uploads.Save(a.Files, field, kind, part.FileName(), part, a.UploadLimit)
		if err != nil {
			return err
		}
		set.Add(field, path)
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
