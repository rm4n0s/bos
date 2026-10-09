package api

import (
	"mime"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/rm4n0s/errors"
)

func (a *API) fileRoutes() {
	a.add(route{Method: "GET", Path: "/files/{kind}/{name...}", Summary: "Read a stored file: results, thumbnails, controlnet, ipadapter, regions or downloads",
		Response: "file", Errors: []string{"FileNotFound"}, Run: a.serveFile})
}

func (a *API) dirOf(kind string) (string, bool) {
	switch kind {
	case "results":
		return a.Dirs.Results, true
	case "thumbnails":
		return a.Dirs.Thumbnails, true
	case "controlnet":
		return a.Dirs.ControlNet, true
	case "ipadapter":
		return a.Dirs.IPAdapter, true
	case "regions":
		return a.Dirs.Regions, true
	case "downloads":
		return a.Dirs.Downloads, true
	}
	return "", false
}

// cleanName accepts a relative path of plain segments: results and masks live
// in folders of their own. Empty segments, "." and "..", backslashes and
// control characters are refused, so the path cannot leave its directory.
func cleanName(name string) bool {
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

func (a *API) serveFile(w http.ResponseWriter, r *http.Request) error {
	dir, ok := a.dirOf(r.PathValue("kind"))
	name := r.PathValue("name")
	if !ok || !cleanName(name) {
		return errors.New("FileNotFound", "there is no such file", "kind", r.PathValue("kind"), "name", name)
	}
	ctype := mime.TypeByExtension(filepath.Ext(name))
	if ctype == "" {
		ctype = "application/octet-stream"
	}
	return a.serveStored(w, r, filepath.Join(dir, filepath.FromSlash(name)), ctype)
}
