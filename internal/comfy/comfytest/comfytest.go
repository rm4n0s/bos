// Package comfytest is a small stand-in for a ComfyUI server, for tests of the
// code that talks to one. It speaks the HTTP endpoints the adapter uses and
// completes every prompt it is given after a delay, with one PNG for each save
// node of the workflow. It has no WebSocket endpoint, so a client falls back to
// polling the history, as it does when the socket is down (SPEC §18.3). It also
// runs the routes of the comfyui-model-crud extension that the program uses.
package comfytest

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"time"
)

// Server is a fake ComfyUI.
type Server struct {
	// URL is the address to give the client.
	URL string

	srv *httptest.Server

	mu       sync.Mutex
	latency  time.Duration
	next     int
	prompts  map[string]*prompt
	uploads  map[string][]byte // stored name → content
	loras    []string
	down     bool
	closing  chan struct{}
	timers   sync.WaitGroup
	received []map[string]json.RawMessage

	// The extension (extension.go).
	deleted    []string
	models     map[string][]byte // "folder/path" → content
	sessions   map[string]*modelUpload
	nextUpload int
}

type prompt struct {
	id    string
	graph map[string]json.RawMessage
	done  bool
	files map[string]string // node id → file name
}

// New starts a server. Prompts complete after latency.
func New(latency time.Duration) *Server {
	s := &Server{latency: latency, prompts: map[string]*prompt{}, uploads: map[string][]byte{}, models: map[string][]byte{}, sessions: map[string]*modelUpload{}, closing: make(chan struct{})}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /system_stats", s.handleStats)
	mux.HandleFunc("POST /prompt", s.handlePrompt)
	mux.HandleFunc("GET /history/{id}", s.handleHistory)
	mux.HandleFunc("GET /queue", s.handleQueue)
	mux.HandleFunc("POST /queue", ok)
	mux.HandleFunc("POST /interrupt", ok)
	mux.HandleFunc("GET /view", s.handleView)
	mux.HandleFunc("POST /upload/image", s.handleUpload)
	mux.HandleFunc("GET /models/{folder}", s.handleModels)
	s.routeExtension(mux)
	s.srv = httptest.NewServer(mux)
	s.URL = s.srv.URL
	return s
}

// Close stops the server.
func (s *Server) Close() {
	close(s.closing)
	s.timers.Wait()
	s.srv.Close()
}

// SetDown makes every endpoint answer 503, as a ComfyUI behind a proxy does
// while it is starting.
func (s *Server) SetDown(down bool) {
	s.mu.Lock()
	s.down = down
	s.mu.Unlock()
}

// SetLoRAs sets the LoRAs the server lists.
func (s *Server) SetLoRAs(names ...string) {
	s.mu.Lock()
	s.loras = names
	s.mu.Unlock()
}

// Prompts returns the workflows received, in order.
func (s *Server) Prompts() []map[string]json.RawMessage {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]map[string]json.RawMessage(nil), s.received...)
}

// Uploads returns the files received, by the name they were stored under.
func (s *Server) Uploads() map[string][]byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string][]byte, len(s.uploads))
	for name, b := range s.uploads {
		out[name] = b
	}
	return out
}

func ok(w http.ResponseWriter, _ *http.Request) { writeJSON(w, 200, map[string]any{}) }

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		panic(err) // the response is a value we built
	}
}

// refuse answers 503 while the server is down.
func (s *Server) refuse(w http.ResponseWriter) bool {
	s.mu.Lock()
	down := s.down
	s.mu.Unlock()
	if down {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "starting"})
	}
	return down
}

func (s *Server) handleStats(w http.ResponseWriter, _ *http.Request) {
	if s.refuse(w) {
		return
	}
	writeJSON(w, 200, map[string]any{"system": map[string]any{"comfyui_version": "0.3.60"}, "devices": []any{}})
}

func (s *Server) handlePrompt(w http.ResponseWriter, r *http.Request) {
	if s.refuse(w) {
		return
	}
	var body struct {
		Prompt map[string]json.RawMessage `json:"prompt"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, 400, map[string]any{"error": map[string]any{"message": "bad body"}})
		return
	}
	s.mu.Lock()
	s.next++
	id := "fake-" + strconv.Itoa(s.next)
	p := &prompt{id: id, graph: body.Prompt, files: map[string]string{}}
	for nodeID, raw := range body.Prompt {
		var node struct {
			ClassType string `json:"class_type"`
		}
		if json.Unmarshal(raw, &node) == nil && node.ClassType == "SaveImage" {
			p.files[nodeID] = id + "_" + nodeID + ".png"
		}
	}
	s.prompts[id] = p
	s.received = append(s.received, body.Prompt)
	number, latency := s.next, s.latency
	s.timers.Add(1)
	s.mu.Unlock()

	go func() {
		defer s.timers.Done()
		select {
		case <-time.After(latency):
		case <-s.closing:
			return
		}
		s.mu.Lock()
		p.done = true
		s.mu.Unlock()
	}()
	writeJSON(w, 200, map[string]any{"prompt_id": id, "number": number, "node_errors": map[string]any{}})
}

func (s *Server) handleHistory(w http.ResponseWriter, r *http.Request) {
	if s.refuse(w) {
		return
	}
	id := r.PathValue("id")
	s.mu.Lock()
	defer s.mu.Unlock()
	p, found := s.prompts[id]
	if !found || !p.done {
		writeJSON(w, 200, map[string]any{})
		return
	}
	outputs := map[string]any{}
	for node, name := range p.files {
		outputs[node] = map[string]any{"images": []any{map[string]any{"filename": name, "subfolder": "", "type": "output"}}}
	}
	writeJSON(w, 200, map[string]any{id: map[string]any{
		"prompt":  []any{},
		"outputs": outputs,
		"status":  map[string]any{"status_str": "success", "completed": true, "messages": []any{[]any{"execution_success", map[string]any{}}}},
	}})
}

func (s *Server) handleQueue(w http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	running := []any{}
	for _, p := range s.prompts {
		if !p.done {
			running = append(running, []any{0, p.id, map[string]any{}, map[string]any{}, []any{}})
		}
	}
	writeJSON(w, 200, map[string]any{"queue_running": running, "queue_pending": []any{}})
}

func (s *Server) handleView(w http.ResponseWriter, r *http.Request) {
	if s.refuse(w) {
		return
	}
	if _, err := w.Write([]byte("png:" + r.URL.Query().Get("filename"))); err != nil {
		panic(err) // the client went away; the test will notice
	}
}

func (s *Server) handleUpload(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		writeJSON(w, 400, map[string]any{"error": err.Error()})
		return
	}
	file, header, err := r.FormFile("image")
	if err != nil {
		writeJSON(w, 400, map[string]any{"error": err.Error()})
		return
	}
	data, err := io.ReadAll(file)
	if err != nil {
		writeJSON(w, 500, map[string]any{"error": err.Error()})
		return
	}
	sum := sha256.Sum256(data)
	name := header.Filename
	if name == "" {
		name = hex.EncodeToString(sum[:])
	}
	s.mu.Lock()
	s.uploads[name] = data
	s.mu.Unlock()
	writeJSON(w, 200, map[string]any{"name": name, "subfolder": r.FormValue("subfolder"), "type": r.FormValue("type")})
}

func (s *Server) handleModels(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	loras := s.loras
	s.mu.Unlock()
	if r.PathValue("folder") != "loras" {
		http.NotFound(w, r)
		return
	}
	writeJSON(w, 200, loras)
}
