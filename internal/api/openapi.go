package api

import (
	"encoding/json"
	"net/http"
	"sort"
	"strconv"
	"strings"
)

func (a *API) openAPIRoutes() {
	a.add(route{Method: "GET", Path: "/openapi.json", Summary: "The OpenAPI 3.1 description of this API", Run: a.serveOpenAPI})
}

func (a *API) serveOpenAPI(w http.ResponseWriter, _ *http.Request) error {
	b, err := json.MarshalIndent(a.OpenAPI(), "", "  ")
	if err != nil {
		return err
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_, _ = w.Write(append(b, '\n')) // a failed write means the client has gone away
	return nil
}

// OpenAPI describes the registered routes as an OpenAPI 3.1 document
// (SPEC §15.5). It is built from the routes themselves, so a route cannot be
// registered without being described.
func (a *API) OpenAPI() map[string]any {
	paths := map[string]any{}
	for _, r := range a.routes {
		item, _ := paths[r.Path].(map[string]any)
		if item == nil {
			item = map[string]any{}
			paths[r.Path] = item
		}
		item[strings.ToLower(r.Method)] = operation(r)
	}
	return map[string]any{
		"openapi": "3.1.0",
		"info": map[string]any{
			"title":       "Bowl of Scenes API",
			"version":     "1",
			"description": "Bulk image, video and 3D generation on ComfyUI servers. Every error has the envelope of #/components/schemas/Error.",
		},
		"servers": []any{map[string]any{"url": BasePath}},
		"paths":   paths,
		"components": map[string]any{
			"securitySchemes": map[string]any{"bearer": map[string]any{"type": "http", "scheme": "bearer"}},
			"schemas": map[string]any{
				"Error": map[string]any{
					"type": "object",
					"properties": map[string]any{"error": map[string]any{
						"type":     "object",
						"required": []string{"tag", "message"},
						"properties": map[string]any{
							"tag":      map[string]any{"type": "string"},
							"message":  map[string]any{"type": "string"},
							"metadata": map[string]any{"type": "object"},
						},
					}},
				},
			},
		},
		"security": []any{map[string]any{"bearer": []any{}}, map[string]any{}},
	}
}

// operation describes one route.
func operation(r route) map[string]any {
	op := map[string]any{
		"summary":     r.Summary,
		"operationId": operationID(r),
	}
	var params []any
	for _, name := range pathParams(r.Path) {
		schema := map[string]any{"type": "integer", "minimum": 1}
		if name == "kind" || name == "name..." || name == "name" {
			schema = map[string]any{"type": "string"}
		}
		params = append(params, map[string]any{"name": strings.TrimSuffix(name, "..."), "in": "path", "required": true, "schema": schema})
	}
	for _, q := range r.Query {
		params = append(params, map[string]any{"name": q, "in": "query", "schema": map[string]any{"type": "string"}})
	}
	if r.Paged {
		params = append(params,
			map[string]any{"name": "page", "in": "query", "schema": map[string]any{"type": "integer", "minimum": 1}},
			map[string]any{"name": "page_size", "in": "query", "schema": map[string]any{"type": "integer", "minimum": 1, "maximum": 200}})
	}
	if len(params) > 0 {
		op["parameters"] = params
	}
	object := map[string]any{"schema": map[string]any{"type": "object"}}
	switch r.Body {
	case "json":
		op["requestBody"] = map[string]any{"required": true, "content": map[string]any{"application/json": object}}
	case "multipart":
		op["requestBody"] = map[string]any{"required": true, "content": map[string]any{"multipart/form-data": object}}
	case "either":
		op["requestBody"] = map[string]any{"required": true, "content": map[string]any{"application/json": object, "multipart/form-data": object}}
	}

	responses := map[string]any{}
	success := map[string]any{"description": "Success"}
	switch r.Response {
	case "none":
	case "file":
		success["content"] = map[string]any{"*/*": map[string]any{"schema": map[string]any{"type": "string", "format": "binary"}}}
	case "stream":
		success["content"] = map[string]any{
			"application/json":  object,
			"text/event-stream": map[string]any{"schema": map[string]any{"type": "string"}},
		}
	default:
		success["content"] = map[string]any{"application/json": object}
	}
	responses[strconv.Itoa(r.Success)] = success

	byStatus := map[int][]string{}
	if r.Body != "" || len(pathParams(r.Path)) > 0 || len(r.Query) > 0 {
		byStatus[http.StatusBadRequest] = []string{"InvalidRequest"}
	}
	for _, tag := range r.Errors {
		status, ok := statusOf[tag]
		if !ok {
			status = http.StatusInternalServerError
		}
		byStatus[status] = appendUnique(byStatus[status], tag)
	}
	statuses := make([]int, 0, len(byStatus))
	for s := range byStatus {
		statuses = append(statuses, s)
	}
	sort.Ints(statuses)
	for _, s := range statuses {
		tags := byStatus[s]
		sort.Strings(tags)
		responses[strconv.Itoa(s)] = map[string]any{
			"description": "Error tags: " + strings.Join(tags, ", "),
			"content":     map[string]any{"application/json": map[string]any{"schema": map[string]any{"$ref": "#/components/schemas/Error"}}},
		}
	}
	op["responses"] = responses
	return op
}

func appendUnique(list []string, s string) []string {
	for _, v := range list {
		if v == s {
			return list
		}
	}
	return append(list, s)
}

// operationID is the method and the path words: GET /servers/{id}/loras is
// getServersIdLoras.
func operationID(r route) string {
	var b strings.Builder
	b.WriteString(strings.ToLower(r.Method))
	for _, seg := range strings.Split(r.Path, "/") {
		seg = strings.Trim(seg, "{}.")
		if seg == "" {
			continue
		}
		seg = strings.NewReplacer("-", " ", ".", " ").Replace(seg)
		for _, w := range strings.Fields(seg) {
			b.WriteString(strings.ToUpper(w[:1]) + w[1:])
		}
	}
	return b.String()
}
