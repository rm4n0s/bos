package api

import "net/http"

func (a *API) fileRoutes() {
	a.add(route{Method: "GET", Path: "/files/{kind}/{name...}", Summary: "Read a stored file: results, thumbnails, controlnet, ipadapter, regions or downloads",
		Response: "file", Errors: []string{"FileNotFound"}, Run: a.serveFile})
}

func (a *API) serveFile(w http.ResponseWriter, r *http.Request) error {
	return a.fileServer.Serve(w, r, r.PathValue("kind"), r.PathValue("name"))
}
