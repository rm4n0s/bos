// Package filesrv sends stored files over HTTP, confined to the configured
// directories (SPEC §15.2, §16.6). The REST API and the web interface both
// use it.
package filesrv

import (
	"io"
	"io/fs"
	"mime"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/rm4n0s/bos/internal/files"
	"github.com/rm4n0s/errors"
)

// Dirs are the storage directories (SPEC §2) by the kind a URL names.
type Dirs struct {
	Results, ControlNet, IPAdapter, Regions, Thumbnails, Downloads string
}

// Dir returns the directory of a kind: results, thumbnails, controlnet,
// ipadapter, regions or downloads.
func (d Dirs) Dir(kind string) (string, bool) {
	switch kind {
	case "results":
		return d.Results, true
	case "thumbnails":
		return d.Thumbnails, true
	case "controlnet":
		return d.ControlNet, true
	case "ipadapter":
		return d.IPAdapter, true
	case "regions":
		return d.Regions, true
	case "downloads":
		return d.Downloads, true
	}
	return "", false
}

// Server sends files from a file store.
type Server struct {
	Files  *files.Store
	Dirs   Dirs
	Logger interface{ Warn(msg string, args ...any) }
}

// CleanName accepts a relative path of plain segments: results and masks live
// in folders of their own. Empty segments, "." and "..", backslashes and
// control characters are refused, so the path cannot leave its directory.
func CleanName(name string) bool {
	if name == "" {
		return false
	}
	for _, seg := range strings.Split(name, "/") {
		if seg == "" || seg == "." || seg == ".." || strings.ContainsAny(seg, `\`) {
			return false
		}
		for _, r := range seg {
			if r < ' ' || r == 0x7f {
				return false
			}
		}
	}
	return true
}

// Serve sends the file name of a kind. The content type follows the extension
// and the browser is told not to guess another.
func (s *Server) Serve(w http.ResponseWriter, r *http.Request, kind, name string) error {
	dir, ok := s.Dirs.Dir(kind)
	if !ok || dir == "" || !CleanName(name) {
		return errors.New("FileNotFound", "there is no such file", "kind", kind, "name", name)
	}
	ctype := mime.TypeByExtension(filepath.Ext(name))
	if ctype == "" {
		ctype = "application/octet-stream"
	}
	return s.Send(w, r, filepath.Join(dir, filepath.FromSlash(name)), ctype)
}

// Send sends a stored file. It supports ranges when the file store hands out
// a seekable file, and refuses folders.
func (s *Server) Send(w http.ResponseWriter, r *http.Request, path, contentType string) error {
	f, err := s.Files.Open(path)
	if err != nil {
		return err
	}
	var modified time.Time
	if st, ok := f.(interface{ Stat() (fs.FileInfo, error) }); ok {
		if info, err := st.Stat(); err == nil {
			if info.IsDir() {
				_ = f.Close() // nothing was read; the missing file is the error to report
				return errors.New("FileNotFound", "there is no such file")
			}
			modified = info.ModTime()
		}
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "sandbox")
	if seeker, ok := f.(io.ReadSeeker); ok {
		http.ServeContent(w, r, filepath.Base(path), modified, seeker)
	} else if _, err := io.Copy(w, f); err != nil && s.Logger != nil {
		s.Logger.Warn("could not send a file", "error", err.Error())
	}
	if err := f.Close(); err != nil {
		return errors.NewErr("FileStoreFailure", err).SetMetadata("operation", "close")
	}
	return nil
}
