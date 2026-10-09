package ui

import (
	"io"
	"mime/multipart"
	"net/http"
	"strconv"
	"strings"

	"github.com/rm4n0s/bos/internal/uploads"
	"github.com/rm4n0s/errors"
)

// maxFormMemory is how much of a form is kept in memory; larger files go to a
// temporary file while they are read.
const maxFormMemory = 8 << 20

// form is a submitted dialog: Datastar sends the fields of the form (as
// multipart, because of the files) when a button asks for contentType 'form'.
type form struct {
	r      *http.Request
	values map[string][]string
	files  map[string][]*multipart.FileHeader
}

// readForm parses the body of an action request. It must run before the event
// stream is opened, because a response that has started closes the request body.
func (u *UI) readForm(w http.ResponseWriter, r *http.Request) (*form, error) {
	if u.UploadLimit > 0 {
		r.Body = http.MaxBytesReader(w, r.Body, 32*u.UploadLimit+maxFormMemory)
	}
	if err := r.ParseMultipartForm(maxFormMemory); err != nil && !errors.Is(err, http.ErrNotMultipart) {
		var big *http.MaxBytesError
		if errors.As(err, &big) {
			return nil, errors.New("UploadTooLarge", "the request is larger than the upload limit", "limit_bytes", big.Limit)
		}
		return nil, errors.New("InvalidRequest", "the form cannot be read", "reason", err.Error())
	}
	if err := r.ParseForm(); err != nil {
		return nil, errors.New("InvalidRequest", "the form cannot be read", "reason", err.Error())
	}
	f := &form{r: r, values: r.PostForm}
	if r.MultipartForm != nil {
		f.files = r.MultipartForm.File
		for k, v := range r.MultipartForm.Value {
			f.values[k] = v
		}
	}
	return f, nil
}

// cleanup removes the temporary files of the form.
func (f *form) cleanup() {
	if f.r.MultipartForm != nil {
		_ = f.r.MultipartForm.RemoveAll() // temporary files; nothing to do if one is already gone
	}
}

func (f *form) str(name string) string {
	if v := f.values[name]; len(v) > 0 {
		return strings.TrimSpace(v[0])
	}
	return ""
}

// raw is str without trimming, for text where the layout matters.
func (f *form) raw(name string) string {
	if v := f.values[name]; len(v) > 0 {
		return v[0]
	}
	return ""
}

// checked reads a checkbox: present when ticked.
func (f *form) checked(name string) bool {
	v := f.str(name)
	return v == "on" || v == "true" || v == "1"
}

func (f *form) integer(name string) (int64, error) {
	v := f.str(name)
	if v == "" {
		return 0, nil
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return 0, errors.New("InvalidRequest", "a whole number is needed", "field", name, "value", v)
	}
	return n, nil
}

func (f *form) decimal(name string) (float64, error) {
	v := f.str(name)
	if v == "" {
		return 0, nil
	}
	n, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return 0, errors.New("InvalidRequest", "a number is needed", "field", name, "value", v)
	}
	return n, nil
}

// id reads the hidden id field; 0 means a new entity.
func (f *form) id() (int64, error) { return f.integer("id") }

// upload stores the file of a field, if the user chose one, after checking its
// type. It reports whether there was a file.
func (u *UI) upload(f *form, field, as string, kind uploads.Kind, set *uploads.Set) (bool, error) {
	headers := f.files[field]
	if len(headers) == 0 || headers[0].Size == 0 && headers[0].Filename == "" {
		return false, nil
	}
	h := headers[0]
	file, err := h.Open()
	if err != nil {
		return false, errors.New("InvalidRequest", "the file cannot be read", "field", field, "reason", err.Error())
	}
	path, saveErr := uploads.Save(u.Files, as, kind, h.Filename, file, u.UploadLimit)
	if closeErr := file.Close(); saveErr == nil && closeErr != nil {
		saveErr = errors.New("InvalidRequest", "the file cannot be read", "field", field, "reason", closeErr.Error())
	}
	if saveErr != nil {
		return false, saveErr
	}
	set.Add(as, path)
	return true, nil
}

// text reads a whole uploaded or pasted text: a pasted value wins over a file.
func (u *UI) textFile(f *form, field string) (string, error) {
	headers := f.files[field]
	if len(headers) == 0 {
		return "", nil
	}
	file, err := headers[0].Open()
	if err != nil {
		return "", errors.New("InvalidRequest", "the file cannot be read", "field", field, "reason", err.Error())
	}
	defer func() { _ = file.Close() }() // reading is done; a close failure cannot matter
	b, err := io.ReadAll(io.LimitReader(file, 8<<20))
	if err != nil {
		return "", errors.New("InvalidRequest", "the file cannot be read", "field", field, "reason", err.Error())
	}
	return string(b), nil
}

// actForm builds a handler for an action that submits a dialog. Like act it
// reads the request first, then opens the stream and runs fn; files stored for
// the request are removed when fn fails.
func (u *UI) actForm(errID string, fn func(r *http.Request, f *form, s *stream) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		f, err := u.readForm(w, r)
		if err != nil {
			s := u.newStream(w, r)
			u.report(s, r, errID, err)
			return
		}
		defer f.cleanup()
		s := u.newStream(w, r)
		if err := fn(r, f, s); err != nil {
			u.report(s, r, errID, err)
		}
	}
}
