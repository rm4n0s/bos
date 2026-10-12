package comfytest

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
)

// The fake also runs the part of the comfyui-model-crud extension that the
// program uses: it deletes files of the output folder and receives models in
// chunks. It keeps what it is given in memory.

type modelUpload struct {
	folder, path string
	size         int64
	chunks       map[int64][]byte
}

// extensionChunk is the chunk size the fake asks for; small, so that even a
// short test model needs several chunks.
const extensionChunk = 16

func (s *Server) routeExtension(mux *http.ServeMux) {
	mux.HandleFunc("DELETE /model_crud/output", s.handleDeleteOutput)
	mux.HandleFunc("GET /model_crud/models/{folder}", s.handleListModels)
	mux.HandleFunc("DELETE /model_crud/models/{folder}", s.handleDeleteModel)
	mux.HandleFunc("POST /model_crud/uploads", s.handleCreateUpload)
	mux.HandleFunc("PUT /model_crud/uploads/{id}/chunks/{index}", s.handleChunk)
	mux.HandleFunc("POST /model_crud/uploads/{id}/complete", s.handleComplete)
	mux.HandleFunc("DELETE /model_crud/uploads/{id}", s.handleCancelUpload)
}

func extensionError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]string{"error": code, "message": message})
}

// DeletedOutputs returns the paths the program asked to delete from the output
// folder, in order.
func (s *Server) DeletedOutputs() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.deleted...)
}

// DeletedModels returns the models the program asked to delete, as
// "folder/path", in order.
func (s *Server) DeletedModels() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.deletedModels...)
}

// Models returns the models received, by "folder/path".
func (s *Server) Models() map[string][]byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string][]byte, len(s.models))
	for name, b := range s.models {
		out[name] = b
	}
	return out
}

func (s *Server) handleDeleteOutput(w http.ResponseWriter, r *http.Request) {
	if s.refuse(w) {
		return
	}
	path := r.URL.Query().Get("path")
	s.mu.Lock()
	s.deleted = append(s.deleted, path)
	s.mu.Unlock()
	writeJSON(w, 200, map[string]any{"area": "output", "path": path, "deleted": true})
}

// handleDeleteModel removes a model from what the server lists and holds. A file
// the server does not have answers file_not_found, as the extension does.
func (s *Server) handleDeleteModel(w http.ResponseWriter, r *http.Request) {
	if s.refuse(w) {
		return
	}
	folder, path := r.PathValue("folder"), r.URL.Query().Get("path")
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.deleteRefused != "" {
		extensionError(w, http.StatusForbidden, s.deleteRefused, "deleting is not allowed")
		return
	}
	found := false
	if _, ok := s.models[folder+"/"+path]; ok {
		delete(s.models, folder+"/"+path)
		found = true
	}
	if folder == "loras" {
		kept := s.loras[:0:0]
		for _, n := range s.loras {
			if strings.ReplaceAll(n, `\`, "/") == path {
				found = true
				continue
			}
			kept = append(kept, n)
		}
		s.loras = kept
	}
	if !found {
		extensionError(w, http.StatusNotFound, "file_not_found", "no such file")
		return
	}
	s.deletedModels = append(s.deletedModels, folder+"/"+path)
	writeJSON(w, 200, map[string]any{"folder": folder, "path": path, "deleted": true})
}

// RefuseDeletes makes the deletion of models answer with the error code, such as
// delete_disabled; an empty code allows it again.
func (s *Server) RefuseDeletes(code string) {
	s.mu.Lock()
	s.deleteRefused = code
	s.mu.Unlock()
}

func (s *Server) handleListModels(w http.ResponseWriter, r *http.Request) {
	if s.refuse(w) {
		return
	}
	folder := r.PathValue("folder")
	s.mu.Lock()
	files := []map[string]any{}
	for name, b := range s.models {
		if path, ok := strings.CutPrefix(name, folder+"/"); ok {
			files = append(files, map[string]any{"path": path, "size": len(b)})
		}
	}
	s.mu.Unlock()
	writeJSON(w, 200, map[string]any{"folder": folder, "files": files})
}

func (s *Server) handleCreateUpload(w http.ResponseWriter, r *http.Request) {
	if s.refuse(w) {
		return
	}
	var in struct {
		Folder, Filename, Subfolder string
		Size                        int64
		Overwrite                   bool
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.Size < 1 || in.Filename == "" {
		extensionError(w, 400, "bad_request", "the upload needs a file name and a size")
		return
	}
	path := in.Filename
	if in.Subfolder != "" {
		path = in.Subfolder + "/" + in.Filename
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, there := s.models[in.Folder+"/"+path]; there && !in.Overwrite {
		extensionError(w, 409, "file_exists", "the file exists")
		return
	}
	s.nextUpload++
	id := "upload" + strconv.Itoa(s.nextUpload) + strings.Repeat("x", 16)
	s.sessions[id] = &modelUpload{folder: in.Folder, path: path, size: in.Size, chunks: map[int64][]byte{}}
	writeJSON(w, 201, map[string]any{
		"upload_id": id, "path": path, "size": in.Size, "chunk_size": extensionChunk,
		"total_chunks": (in.Size + extensionChunk - 1) / extensionChunk,
	})
}

func (s *Server) handleChunk(w http.ResponseWriter, r *http.Request) {
	if s.refuse(w) {
		return
	}
	index, err := strconv.ParseInt(r.PathValue("index"), 10, 64)
	body, readErr := io.ReadAll(r.Body)
	if err != nil || readErr != nil {
		extensionError(w, 400, "bad_chunk", "the chunk is not readable")
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	up, ok := s.sessions[r.PathValue("id")]
	if !ok {
		extensionError(w, 404, "unknown_upload", "no such upload")
		return
	}
	up.chunks[index] = body
	writeJSON(w, 200, map[string]any{"index": index, "received_chunks": len(up.chunks)})
}

func (s *Server) handleComplete(w http.ResponseWriter, r *http.Request) {
	if s.refuse(w) {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	id := r.PathValue("id")
	up, ok := s.sessions[id]
	if !ok {
		extensionError(w, 404, "unknown_upload", "no such upload")
		return
	}
	var all []byte
	for i := int64(0); i < int64(len(up.chunks)); i++ {
		all = append(all, up.chunks[i]...)
	}
	if int64(len(all)) != up.size {
		extensionError(w, 409, "incomplete_upload", "chunks are missing")
		return
	}
	s.models[up.folder+"/"+up.path] = all
	delete(s.sessions, id)
	writeJSON(w, 201, map[string]any{"folder": up.folder, "path": up.path, "size": up.size})
}

func (s *Server) handleCancelUpload(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	delete(s.sessions, r.PathValue("id"))
	s.mu.Unlock()
	writeJSON(w, 200, map[string]any{"cancelled": true})
}
