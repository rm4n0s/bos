package api

import (
	"net/http"
	"strings"
)

// route describes one operation: what the router needs and what the OpenAPI
// document says about it (SPEC §15.5).
type route struct {
	Method  string
	Path    string // relative to BasePath, with {name} placeholders
	Summary string
	Success int
	// Errors are the notable error tags (SPEC §15.2).
	Errors []string
	// Paged is true for lists that take page and page_size.
	Paged bool
	// Query lists the query parameters the operation reads.
	Query []string
	// Body names the kind of request body: "", "json", "multipart" or "either".
	Body string
	// Response is "json" (the default), "stream", "file" or "none".
	Response string
	// Run handles the request; an error it returns becomes the error envelope.
	Run func(w http.ResponseWriter, r *http.Request) error
}

// pattern is the net/http routing pattern of the route.
func (r route) pattern() string { return r.Method + " " + BasePath + r.Path }

// add registers a route.
func (a *API) add(r route) {
	if r.Success == 0 {
		r.Success = http.StatusOK
	}
	a.routes = append(a.routes, r)
	run := r.Run
	a.mux.HandleFunc(r.pattern(), func(w http.ResponseWriter, req *http.Request) {
		if err := run(w, req); err != nil {
			a.writeError(w, req, err)
		}
	})
}

// Routes returns the registered operations, in registration order.
func (a *API) Routes() []route { return append([]route(nil), a.routes...) }

// registerAll registers every route of SPEC §15.2.
func (a *API) registerAll() {
	a.serverRoutes()
	a.generatorRoutes()
	a.fixerRoutes()
	a.groupRoutes()
	a.itemRoutes()
	a.tagRoutes()
	a.projectRoutes()
	a.commandRoutes()
	a.jobRoutes()
	a.replRoutes()
	a.eventRoutes()
	a.fileRoutes()
	a.openAPIRoutes()
}

// pathParams lists the {name} placeholders of a path.
func pathParams(path string) []string {
	var out []string
	for _, seg := range strings.Split(path, "/") {
		if strings.HasPrefix(seg, "{") && strings.HasSuffix(seg, "}") {
			out = append(out, strings.TrimSuffix(strings.TrimPrefix(seg, "{"), "}"))
		}
	}
	return out
}
