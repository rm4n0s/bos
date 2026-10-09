package comfy

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// fakeComfy is a stand-in for a ComfyUI server: the HTTP endpoints the adapter
// uses and a WebSocket endpoint, with controls to make prompts start, finish,
// fail or vanish.
type fakeComfy struct {
	t   *testing.T
	srv *httptest.Server

	mu       sync.Mutex
	next     int
	prompts  map[string]*fakePrompt
	pending  []string
	running  []string
	sockets  map[string]*fakeSocket
	arrivals chan string // client ids of new WebSocket connections

	// behaviour switches
	version        string
	rejectNodeErrs map[string]any // POST /prompt answers 400 with these
	acceptNodeErrs map[string]any // POST /prompt answers 200 with these and queues
	promptStatus   int            // POST /prompt fails with this status
	queueStatus    int            // GET /queue fails with this status
	historyStatus  int            // GET /history fails with this status
	files          map[string][]byte
	models         map[string][]string
	uploadStatus   int

	// observations
	uploads      map[string]string // sha256 of the content → stored name
	uploadPosts  int
	uploadFolder string
	queuedBy     map[string]string // prompt id → client id
	interrupts   []string
	deletes      []string
}

type fakePrompt struct {
	id      string
	client  string
	graph   map[string]json.RawMessage
	number  int
	done    bool
	entry   map[string]any
	started bool
}

type fakeSocket struct {
	c  *websocket.Conn
	mu sync.Mutex
}

func newFakeComfy(t *testing.T) *fakeComfy {
	t.Helper()
	f := &fakeComfy{
		t: t, prompts: map[string]*fakePrompt{}, sockets: map[string]*fakeSocket{}, arrivals: make(chan string, 64),
		version: "0.3.60", files: map[string][]byte{}, models: map[string][]string{}, uploads: map[string]string{},
		queuedBy: map[string]string{},
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /system_stats", f.handleStats)
	mux.HandleFunc("POST /prompt", f.handlePrompt)
	mux.HandleFunc("GET /history/{id}", f.handleHistory)
	mux.HandleFunc("GET /queue", f.handleQueue)
	mux.HandleFunc("POST /queue", f.handleDeleteQueued)
	mux.HandleFunc("POST /interrupt", f.handleInterrupt)
	mux.HandleFunc("GET /view", f.handleView)
	mux.HandleFunc("POST /upload/image", f.handleUpload)
	mux.HandleFunc("GET /models/{folder}", f.handleModels)
	mux.HandleFunc("GET /ws", f.handleWS)
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.close)
	return f
}

func (f *fakeComfy) close() {
	f.srv.Close()
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		panic(err)
	}
}

func (f *fakeComfy) handleStats(w http.ResponseWriter, _ *http.Request) {
	f.mu.Lock()
	v := f.version
	f.mu.Unlock()
	writeJSON(w, 200, map[string]any{"system": map[string]any{"comfyui_version": v}, "devices": []any{}})
}

func (f *fakeComfy) handlePrompt(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ClientID string                     `json:"client_id"`
		Prompt   map[string]json.RawMessage `json:"prompt"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, 400, map[string]any{"error": map[string]any{"message": "bad body"}})
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.promptStatus != 0 {
		writeJSON(w, f.promptStatus, map[string]any{"error": "unavailable"})
		return
	}
	if f.rejectNodeErrs != nil {
		writeJSON(w, 400, map[string]any{"error": map[string]any{"message": "Prompt outputs failed validation"}, "node_errors": f.rejectNodeErrs})
		return
	}
	f.next++
	id := "p" + strconv.Itoa(f.next)
	f.prompts[id] = &fakePrompt{id: id, client: body.ClientID, graph: body.Prompt, number: f.next}
	f.pending = append(f.pending, id)
	f.queuedBy[id] = body.ClientID
	resp := map[string]any{"prompt_id": id, "number": f.next, "node_errors": map[string]any{}}
	if f.acceptNodeErrs != nil {
		resp["node_errors"] = f.acceptNodeErrs
	}
	writeJSON(w, 200, resp)
}

func (f *fakeComfy) handleHistory(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.historyStatus != 0 {
		writeJSON(w, f.historyStatus, map[string]any{})
		return
	}
	id := r.PathValue("id")
	if p, ok := f.prompts[id]; ok && p.done {
		writeJSON(w, 200, map[string]any{id: p.entry})
		return
	}
	writeJSON(w, 200, map[string]any{})
}

func (f *fakeComfy) handleQueue(w http.ResponseWriter, _ *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.queueStatus != 0 {
		writeJSON(w, f.queueStatus, map[string]any{})
		return
	}
	item := func(id string) []any {
		return []any{f.prompts[id].number, id, map[string]any{}, map[string]any{}, []any{}}
	}
	running, pending := []any{}, []any{}
	for _, id := range f.running {
		running = append(running, item(id))
	}
	for _, id := range f.pending {
		pending = append(pending, item(id))
	}
	writeJSON(w, 200, map[string]any{"queue_running": running, "queue_pending": pending})
}

func (f *fakeComfy) handleDeleteQueued(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Delete []string `json:"delete"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, 400, map[string]any{})
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, id := range body.Delete {
		f.deletes = append(f.deletes, id)
		f.pending = removeID(f.pending, id)
	}
	writeJSON(w, 200, map[string]any{})
}

func (f *fakeComfy) handleInterrupt(w http.ResponseWriter, r *http.Request) {
	var body struct {
		PromptID string `json:"prompt_id"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	f.mu.Lock()
	f.interrupts = append(f.interrupts, body.PromptID)
	f.mu.Unlock()
	writeJSON(w, 200, map[string]any{})
}

func (f *fakeComfy) handleView(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	data, ok := f.files[r.URL.Query().Get("filename")]
	f.mu.Unlock()
	if !ok {
		http.NotFound(w, r)
		return
	}
	if _, err := w.Write(data); err != nil {
		panic(err)
	}
}

func (f *fakeComfy) handleModels(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	m, ok := f.models[r.PathValue("folder")]
	f.mu.Unlock()
	if !ok {
		http.NotFound(w, r)
		return
	}
	writeJSON(w, 200, m)
}

func (f *fakeComfy) handleUpload(w http.ResponseWriter, r *http.Request) {
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
	key := hex.EncodeToString(sum[:])
	f.mu.Lock()
	defer f.mu.Unlock()
	f.uploadPosts++
	f.uploadFolder = r.FormValue("subfolder")
	if f.uploadStatus != 0 {
		writeJSON(w, f.uploadStatus, map[string]any{"error": "no space"})
		return
	}
	name, seen := f.uploads[key]
	if !seen {
		name = header.Filename
		f.uploads[key] = name
	}
	writeJSON(w, 200, map[string]any{"name": name, "subfolder": r.FormValue("subfolder"), "type": r.FormValue("type")})
}

func (f *fakeComfy) handleWS(w http.ResponseWriter, r *http.Request) {
	up := websocket.Upgrader{}
	c, err := up.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	id := r.URL.Query().Get("clientId")
	sock := &fakeSocket{c: c}
	f.mu.Lock()
	f.sockets[id] = sock
	f.mu.Unlock()
	f.arrivals <- id
	defer func() {
		f.mu.Lock()
		if f.sockets[id] == sock {
			delete(f.sockets, id)
		}
		f.mu.Unlock()
		_ = c.Close()
	}()
	for {
		if _, _, err := c.ReadMessage(); err != nil {
			return
		}
	}
}

func removeID(ids []string, id string) []string {
	out := ids[:0]
	for _, x := range ids {
		if x != id {
			out = append(out, x)
		}
	}
	return out
}

// ---- controls ----

// waitSocket waits for a WebSocket connection and returns its client id.
func (f *fakeComfy) waitSocket() string {
	f.t.Helper()
	select {
	case id := <-f.arrivals:
		return id
	case <-time.After(5 * time.Second):
		f.t.Fatal("no WebSocket connection arrived")
		return ""
	}
}

func (f *fakeComfy) send(client, typ string, data any) bool {
	f.mu.Lock()
	sock := f.sockets[client]
	f.mu.Unlock()
	if sock == nil {
		return false
	}
	b, err := json.Marshal(map[string]any{"type": typ, "data": data})
	if err != nil {
		panic(err)
	}
	sock.mu.Lock()
	defer sock.mu.Unlock()
	return sock.c.WriteMessage(websocket.TextMessage, b) == nil
}

// drop closes the TCP connection of a client's socket without a close handshake.
func (f *fakeComfy) drop(client string) {
	f.mu.Lock()
	sock := f.sockets[client]
	f.mu.Unlock()
	if sock != nil {
		_ = sock.c.UnderlyingConn().Close()
	}
}

func (f *fakeComfy) socketCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.sockets)
}

func (f *fakeComfy) clientOf(promptID string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.queuedBy[promptID]
}

// start moves a pending prompt to running.
func (f *fakeComfy) start(id string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pending = removeID(f.pending, id)
	f.running = append(f.running, id)
	f.prompts[id].started = true
}

// finish ends a prompt: it leaves the queue and enters the history. When
// events is true the end is also sent to the client that queued it.
func (f *fakeComfy) finish(id string, entry map[string]any, event string, data map[string]any, events bool) {
	f.mu.Lock()
	p := f.prompts[id]
	p.done, p.entry = true, entry
	f.pending = removeID(f.pending, id)
	f.running = removeID(f.running, id)
	client := p.client
	f.mu.Unlock()
	if events {
		data["prompt_id"] = id
		f.send(client, event, data)
	}
}

func outputsOf(node string, names ...string) map[string]any {
	imgs := []any{}
	for _, n := range names {
		imgs = append(imgs, map[string]any{"filename": n, "subfolder": "", "type": "output"})
	}
	return map[string]any{node: map[string]any{"images": imgs}}
}

func successEntry(outputs map[string]any) map[string]any {
	return map[string]any{
		"prompt":  []any{},
		"outputs": outputs,
		"status":  map[string]any{"status_str": "success", "completed": true, "messages": []any{[]any{"execution_success", map[string]any{}}}},
	}
}

// succeed finishes a prompt successfully. With events false only the history
// knows (the events went to a client that is gone).
func (f *fakeComfy) succeed(id string, outputs map[string]any, events bool) {
	f.finish(id, successEntry(outputs), "execution_success", map[string]any{"timestamp": 1}, events)
}

func (f *fakeComfy) fail(id, node string, events bool) {
	data := map[string]any{
		"node_id": node, "node_type": "KSampler", "exception_message": "CUDA out of memory", "exception_type": "torch.OutOfMemoryError",
		"traceback": []any{"line 1", "line 2"},
	}
	msg := map[string]any{"prompt_id": id}
	for k, v := range data {
		msg[k] = v
	}
	entry := map[string]any{
		"prompt": []any{}, "outputs": map[string]any{},
		"status": map[string]any{"status_str": "error", "completed": false, "messages": []any{[]any{"execution_error", msg}}},
	}
	f.finish(id, entry, "execution_error", data, events)
}

func (f *fakeComfy) interrupt(id string, events bool) {
	data := map[string]any{"node_id": "3", "node_type": "KSampler", "executed": []any{}}
	msg := map[string]any{"prompt_id": id, "node_id": "3", "node_type": "KSampler", "executed": []any{}}
	entry := map[string]any{
		"prompt": []any{}, "outputs": map[string]any{},
		"status": map[string]any{"status_str": "error", "completed": false, "messages": []any{[]any{"execution_interrupted", msg}}},
	}
	f.finish(id, entry, "execution_interrupted", data, events)
}

// vanish removes a prompt from the queue and the history, as a restarted server would.
func (f *fakeComfy) vanish(id string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pending = removeID(f.pending, id)
	f.running = removeID(f.running, id)
	delete(f.prompts, id)
}

func (f *fakeComfy) set(fn func()) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fn()
}

func (f *fakeComfy) snapshot() (interrupts, deletes []string, uploadPosts int, folder string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.interrupts...), append([]string(nil), f.deletes...), f.uploadPosts, f.uploadFolder
}
