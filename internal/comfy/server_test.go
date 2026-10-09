package comfy

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rm4n0s/bos/internal/workflow"
	"github.com/rm4n0s/errors"
)

var testGraph = workflow.Graph{"1": []byte(`{"class_type":"KSampler","inputs":{}}`)}

type testLog struct {
	mu   sync.Mutex
	logs []string
}

func (l *testLog) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.logs = append(l.logs, string(p))
	return len(p), nil
}

func (l *testLog) text() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return strings.Join(l.logs, "")
}

// newServer connects an adapter to the fake, with timings short enough for tests.
func newServer(t *testing.T, f *fakeComfy, mod ...func(*Options)) *Server {
	t.Helper()
	opts := Options{
		PollInterval: 20 * time.Millisecond, CallTimeout: 2 * time.Second, RequestTimeout: 2 * time.Second,
		Backoff: func(int) time.Duration { return 10 * time.Millisecond },
	}
	for _, m := range mod {
		m(&opts)
	}
	s, err := NewServer(f.srv.URL, opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Error(err)
		}
	})
	return s
}

// connected waits until the adapter's WebSocket is up and returns its client id.
func connected(t *testing.T, f *fakeComfy, s *Server) string {
	t.Helper()
	id := f.waitSocket()
	deadline := time.Now().Add(5 * time.Second)
	for !s.Connected() {
		if time.Now().After(deadline) {
			t.Fatal("the adapter never reported the connection")
		}
		time.Sleep(2 * time.Millisecond)
	}
	return id
}

// wantTag asserts the tag and the entry method of the failure path.
func wantTag(t *testing.T, err error, entry, tag string) {
	t.Helper()
	_ = wantTagMeta(t, err, entry, tag)
}

// wantTagMeta is wantTag for tests that go on to look at the error.
func wantTagMeta(t *testing.T, err error, entry, tag string) *errors.Error {
	t.Helper()
	appErr, ok := errors.FromError(err)
	if !ok {
		t.Fatalf("expected *errors.Error, got %T (%v)", err, err)
	}
	if appErr.Tag != tag || !appErr.HasRoute(entry) || !strings.HasSuffix(appErr.Route(), "."+tag) {
		t.Errorf("unexpected failure path: %s (tag %s)\nwant %s ... %s", appErr.Route(), appErr.Tag, entry, tag)
	}
	return appErr
}

func queue(t *testing.T, s *Server) string {
	t.Helper()
	id, warnings, err := s.QueuePrompt(t.Context(), testGraph)
	if err != nil || len(warnings) != 0 {
		t.Fatalf("QueuePrompt = %q, %v, %v", id, warnings, err)
	}
	return id
}

type waitResult struct {
	res Result
	err error
}

// waitAsync starts Wait in the background.
func waitAsync(ctx context.Context, s *Server, id string) <-chan waitResult {
	ch := make(chan waitResult, 1)
	go func() {
		res, err := s.Wait(ctx, id)
		ch <- waitResult{res, err}
	}()
	return ch
}

func get(t *testing.T, ch <-chan waitResult) waitResult {
	t.Helper()
	select {
	case r := <-ch:
		return r
	case <-time.After(5 * time.Second):
		t.Fatal("Wait did not return")
		return waitResult{}
	}
}

func notDone(t *testing.T, ch <-chan waitResult, within time.Duration) {
	t.Helper()
	select {
	case r := <-ch:
		t.Fatalf("Wait returned too early: %+v", r)
	case <-time.After(within):
	}
}

func TestPingAndModels(t *testing.T) {
	f := newFakeComfy(t)
	f.set(func() { f.models["loras"] = []string{"a.safetensors", "sub/b.safetensors"} })
	s := newServer(t, f)
	if err := s.Ping(t.Context()); err != nil {
		t.Fatal(err)
	}
	got, err := s.ListModels(t.Context(), "loras")
	if err != nil || len(got) != 2 || got[1] != "sub/b.safetensors" {
		t.Errorf("ListModels = %v, %v", got, err)
	}
	_, err = s.ListModels(t.Context(), "unknown")
	wantTag(t, err, "ListModels", "ComfyUIRejected")
}

func TestServerUnreachable(t *testing.T) {
	f := newFakeComfy(t)
	url := f.srv.URL
	f.srv.Close()
	s, err := NewServer(url, Options{Backoff: func(int) time.Duration { return 5 * time.Millisecond }, CallTimeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })

	wantTag(t, s.Ping(t.Context()), "Ping", "ServerUnreachable")
	_, _, err = s.QueuePrompt(t.Context(), testGraph)
	wantTag(t, err, "QueuePrompt", "ServerUnreachable")
	_, err = s.ListModels(t.Context(), "loras")
	wantTag(t, err, "ListModels", "ServerUnreachable")
	_, err = s.Upload(t.Context(), "a.png", strings.NewReader("x"))
	wantTag(t, err, "Upload", "ServerUnreachable")
	_, err = s.Fetch(t.Context(), OutputFile{Filename: "a.png"})
	wantTag(t, err, "Fetch", "ServerUnreachable")
	wantTag(t, s.Cancel(t.Context(), "p1"), "Cancel", "ServerUnreachable")
	if s.Connected() {
		t.Error("connected to a closed server")
	}
}

func TestInvalidHost(t *testing.T) {
	_, err := NewServer("not a url", Options{})
	wantTag(t, err, "NewServer", "ServerHostInvalid")
	_, err = NewServer("ftp://x", Options{})
	wantTag(t, err, "NewServer", "ServerHostInvalid")
}

func TestCallsHonourTheCallersContext(t *testing.T) {
	f := newFakeComfy(t)
	s := newServer(t, f)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	wantTag(t, s.Ping(ctx), "Ping", "ComfyUICallCancelled")
	_, err := s.ListModels(ctx, "loras")
	wantTag(t, err, "ListModels", "ComfyUICallCancelled")
	_, err = s.Wait(ctx, "p1")
	wantTag(t, err, "Wait", "ComfyUICallCancelled")
}

// Acceptance test 35.
func TestQueuePromptNodeErrors(t *testing.T) {
	f := newFakeComfy(t)
	s := newServer(t, f)

	// Accepted with node errors: queued, the errors become warnings.
	f.set(func() {
		f.acceptNodeErrs = map[string]any{"9": map[string]any{"errors": []any{"missing input"}}, "4": "oops"}
	})
	id, warnings, err := s.QueuePrompt(t.Context(), testGraph)
	if err != nil || id == "" {
		t.Fatalf("QueuePrompt = %q, %v", id, err)
	}
	if len(warnings) != 2 || warnings[0].NodeID != "4" || warnings[1].NodeID != "9" || !strings.Contains(warnings[1].Message, "missing input") {
		t.Errorf("warnings = %+v", warnings)
	}
	// The prompt really is queued.
	f.start(id)
	res := waitAsync(t.Context(), s, id)
	f.succeed(id, outputsOf("9", "a.png"), false)
	if r := get(t, res); r.err != nil {
		t.Errorf("the accepted prompt did not run to the end: %v", r.err)
	}

	// Rejected: no prompt id, ComfyUIRejected with the node errors.
	f.set(func() {
		f.acceptNodeErrs = nil
		f.rejectNodeErrs = map[string]any{"3": map[string]any{"errors": []any{"bad"}}}
	})
	id, warnings, err = s.QueuePrompt(t.Context(), testGraph)
	appErr := wantTagMeta(t, err, "QueuePrompt", "ComfyUIRejected")
	if id != "" || warnings != nil || appErr.Metadata["node_errors"] == nil {
		t.Errorf("id = %q, warnings = %v, metadata = %v", id, warnings, appErr.Metadata)
	}

	// Another failure of the HTTP call: 4xx is a rejection, 5xx an unreachable server.
	f.set(func() { f.rejectNodeErrs = nil; f.promptStatus = 422 })
	_, _, err = s.QueuePrompt(t.Context(), testGraph)
	wantTag(t, err, "QueuePrompt", "ComfyUIRejected")
	f.set(func() { f.promptStatus = 503 })
	_, _, err = s.QueuePrompt(t.Context(), testGraph)
	wantTag(t, err, "QueuePrompt", "ServerUnreachable")
}

// Queued through the event socket's client, so its events come back to the hub.
func TestPromptsAreQueuedByTheSocketsClient(t *testing.T) {
	f := newFakeComfy(t)
	s := newServer(t, f)
	clientID := connected(t, f, s)
	id := queue(t, s)
	if got := f.clientOf(id); got != clientID {
		t.Errorf("queued by %q, the socket belongs to %q", got, clientID)
	}
}

func TestWaitCompletedByEvent(t *testing.T) {
	f := newFakeComfy(t)
	s := newServer(t, f, func(o *Options) { o.PollInterval = time.Hour }) // only events can complete it
	connected(t, f, s)
	id := queue(t, s)
	f.start(id)
	res := waitAsync(t.Context(), s, id)
	notDone(t, res, 50*time.Millisecond)

	f.succeed(id, outputsOf("9", "a.png", "b.png"), true)
	r := get(t, res)
	if r.err != nil {
		t.Fatal(r.err)
	}
	files, err := r.res.Pick("9")
	if err != nil || len(files) != 2 || files[0].Filename != "a.png" || files[0].NodeID != "9" || files[0].Type != "output" {
		t.Errorf("files = %+v, %v", files, err)
	}
	if r.res.PromptID != id || len(r.res.Files("9")) != 2 || len(r.res.Files("other")) != 0 {
		t.Errorf("result = %+v", r.res)
	}
	_, err = r.res.Pick("other")
	wantTag(t, err, "Pick", "ComfyUIResultMissing")
}

// §18.3 step 2: the end can arrive before anyone waits.
func TestEarlyEventsAreBuffered(t *testing.T) {
	f := newFakeComfy(t)
	s := newServer(t, f, func(o *Options) { o.PollInterval = time.Hour })
	connected(t, f, s)
	id := queue(t, s)
	f.start(id)
	f.succeed(id, outputsOf("9", "early.png"), true)
	// Give the hub time to take the event before Wait registers.
	time.Sleep(100 * time.Millisecond)
	s.hub.mu.Lock()
	_, buffered := s.hub.early[id]
	s.hub.mu.Unlock()
	if !buffered {
		t.Fatal("the end of an unwatched prompt was not kept")
	}
	r := get(t, waitAsync(t.Context(), s, id))
	if r.err != nil || len(r.res.Files("9")) != 1 {
		t.Errorf("result = %+v", r)
	}
	s.hub.mu.Lock()
	_, still := s.hub.early[id]
	s.hub.mu.Unlock()
	if still {
		t.Error("the buffered event was not consumed")
	}
}

func TestEarlyBufferIsBounded(t *testing.T) {
	now := time.Now()
	h := newHub(3, time.Minute, func() time.Time { return now })
	for i, id := range []string{"a", "b", "c", "d"} {
		now = now.Add(time.Second)
		h.deliver(id, event{finished{outcome: outcomeSuccess}})
		_ = i
	}
	if len(h.early) != 3 {
		t.Fatalf("%d buffered", len(h.early))
	}
	if _, ok := h.early["a"]; ok {
		t.Error("the oldest entry was kept")
	}
	now = now.Add(2 * time.Minute)
	h.deliver("e", event{finished{outcome: outcomeSuccess}})
	if len(h.early) != 1 {
		t.Errorf("expired entries were kept: %d", len(h.early))
	}
}

// §18.3 step 3: a status event with an empty queue is not the end of a prompt.
func TestStatusEventIsNotCompletion(t *testing.T) {
	f := newFakeComfy(t)
	s := newServer(t, f, func(o *Options) { o.PollInterval = time.Hour })
	client := connected(t, f, s)
	id := queue(t, s)
	f.start(id)
	res := waitAsync(t.Context(), s, id)
	f.send(client, "status", map[string]any{"status": map[string]any{"exec_info": map[string]any{"queue_remaining": 0}}})
	f.send(client, "executing", map[string]any{"node": nil, "prompt_id": id})
	notDone(t, res, 150*time.Millisecond)
	f.succeed(id, outputsOf("9", "a.png"), true)
	if r := get(t, res); r.err != nil {
		t.Error(r.err)
	}
}

// Events are routed by prompt id: two jobs waiting on one server do not mix.
func TestEventsAreRoutedByPromptID(t *testing.T) {
	f := newFakeComfy(t)
	s := newServer(t, f, func(o *Options) { o.PollInterval = time.Hour })
	connected(t, f, s)
	ids := []string{queue(t, s), queue(t, s), queue(t, s)}
	for _, id := range ids {
		f.start(id)
	}
	results := make([]<-chan waitResult, len(ids))
	for i, id := range ids {
		results[i] = waitAsync(t.Context(), s, id)
	}
	// Finish them in another order, one failing.
	f.succeed(ids[2], outputsOf("9", "third.png"), true)
	f.fail(ids[0], "3", true)
	f.succeed(ids[1], outputsOf("9", "second.png"), true)

	if r := get(t, results[2]); r.err != nil || r.res.Files("9")[0].Filename != "third.png" {
		t.Errorf("third = %+v", r)
	}
	if r := get(t, results[1]); r.err != nil || r.res.Files("9")[0].Filename != "second.png" {
		t.Errorf("second = %+v", r)
	}
	r := get(t, results[0])
	appErr := wantTagMeta(t, r.err, "Wait", "ComfyUIExecutionFailed")
	if appErr.Metadata["prompt_id"] != ids[0] {
		t.Errorf("failure of the wrong prompt: %v", appErr.Metadata)
	}
}

// Acceptance test 9 (the ComfyUI side): the failure carries node, exception and traceback.
func TestExecutionFailure(t *testing.T) {
	for name, viaEvent := range map[string]bool{"by event": true, "by history": false} {
		t.Run(name, func(t *testing.T) {
			f := newFakeComfy(t)
			s := newServer(t, f)
			connected(t, f, s)
			id := queue(t, s)
			f.start(id)
			res := waitAsync(t.Context(), s, id)
			f.fail(id, "3", viaEvent)
			appErr := wantTagMeta(t, get(t, res).err, "Wait", "ComfyUIExecutionFailed")
			md := appErr.Metadata
			if md["node_id"] != "3" || md["node_type"] != "KSampler" || md["exception_type"] != "torch.OutOfMemoryError" || !strings.Contains(appErr.Message, "CUDA out of memory") {
				t.Errorf("metadata = %v, message = %q", md, appErr.Message)
			}
			if tb, _ := md["traceback"].([]string); len(tb) != 2 {
				t.Errorf("traceback = %v", md["traceback"])
			}
		})
	}
}

func TestInterrupted(t *testing.T) {
	for name, viaEvent := range map[string]bool{"by event": true, "by history": false} {
		t.Run(name, func(t *testing.T) {
			f := newFakeComfy(t)
			s := newServer(t, f)
			connected(t, f, s)
			id := queue(t, s)
			f.start(id)
			res := waitAsync(t.Context(), s, id)
			f.interrupt(id, viaEvent)
			appErr := wantTagMeta(t, get(t, res).err, "Wait", "ComfyUIInterrupted")
			if appErr.Metadata["node_id"] != "3" {
				t.Errorf("metadata = %v", appErr.Metadata)
			}
		})
	}
}

// Without any event the poll finds a prompt that finished while nobody listened,
// and a prompt of an earlier run of the application (SPEC §18.7).
func TestWaitByPollingOnly(t *testing.T) {
	f := newFakeComfy(t)
	s := newServer(t, f)
	f.set(func() { f.next = 6 })
	id := queue(t, s)

	f.start(id)
	res := waitAsync(t.Context(), s, id)
	notDone(t, res, 80*time.Millisecond) // running: the poll sees it in the queue
	f.succeed(id, outputsOf("9", "polled.png"), false)
	if r := get(t, res); r.err != nil || r.res.Files("9")[0].Filename != "polled.png" {
		t.Errorf("result = %+v", r)
	}

	// A new adapter, as after a restart, completes a prompt it never queued.
	restarted := newServer(t, f)
	r := get(t, waitAsync(t.Context(), restarted, id))
	if r.err != nil || len(r.res.Files("9")) != 1 {
		t.Errorf("recovered result = %+v", r)
	}
}

func TestPromptLost(t *testing.T) {
	f := newFakeComfy(t)
	s := newServer(t, f)
	id := queue(t, s)
	f.start(id)
	res := waitAsync(t.Context(), s, id)
	notDone(t, res, 60*time.Millisecond)
	f.vanish(id)
	wantTag(t, get(t, res).err, "Wait", "ComfyUIPromptLost")

	_, err := s.Wait(t.Context(), "never-existed")
	wantTag(t, err, "Wait", "ComfyUIPromptLost")
}

func TestSuccessWithoutHistoryEntry(t *testing.T) {
	f := newFakeComfy(t)
	s := newServer(t, f, func(o *Options) { o.PollInterval = time.Hour })
	client := connected(t, f, s)
	id := queue(t, s)
	f.start(id)
	// The success event arrives but ComfyUI never writes the history entry.
	res := waitAsync(t.Context(), s, id)
	time.Sleep(30 * time.Millisecond)
	f.send(client, "execution_success", map[string]any{"prompt_id": id})
	wantTag(t, get(t, res).err, "Wait", "ComfyUIResultMissing")
}

// Acceptance test 34.
func TestReconnectsWithANewClient(t *testing.T) {
	f := newFakeComfy(t)
	var ups, downs atomic.Int32
	s := newServer(t, f, func(o *Options) {
		o.OnConnection = func(up bool) {
			if up {
				ups.Add(1)
			} else {
				downs.Add(1)
			}
		}
	})
	first := connected(t, f, s)

	// Two prompts are in flight when the connection drops.
	finishedMeanwhile, stillRunning := queue(t, s), queue(t, s)
	f.start(finishedMeanwhile)
	f.start(stillRunning)
	a := waitAsync(t.Context(), s, finishedMeanwhile)
	b := waitAsync(t.Context(), s, stillRunning)
	f.drop(first)

	// The first finishes while the socket is down: the events are lost, the history has it.
	f.succeed(finishedMeanwhile, outputsOf("9", "a.png"), true)
	if r := get(t, a); r.err != nil || r.res.Files("9")[0].Filename != "a.png" {
		t.Errorf("job finished meanwhile = %+v", r)
	}

	second := f.waitSocket()
	if second == first {
		t.Fatal("the hub reconnected with the same client id")
	}
	deadline := time.Now().Add(5 * time.Second)
	for !s.Connected() && time.Now().Before(deadline) {
		time.Sleep(2 * time.Millisecond)
	}
	if downs.Load() < 1 || ups.Load() < 2 {
		t.Errorf("connection callbacks: %d up, %d down", ups.Load(), downs.Load())
	}

	// The second is still running on the server; it is awaited and completes. Its
	// events would go to the old client, so it finishes through the history.
	notDone(t, b, 60*time.Millisecond)
	f.succeed(stillRunning, outputsOf("9", "b.png"), true)
	if r := get(t, b); r.err != nil || r.res.Files("9")[0].Filename != "b.png" {
		t.Errorf("job still running = %+v", r)
	}

	// New prompts use the new client, and its events reach the hub.
	id := queue(t, s)
	if got := f.clientOf(id); got != second {
		t.Errorf("queued by %q, the live socket is %q", got, second)
	}
	f.start(id)
	res := waitAsync(t.Context(), s, id)
	f.succeed(id, outputsOf("9", "c.png"), true)
	if r := get(t, res); r.err != nil {
		t.Error(r.err)
	}
}

func TestReconnectsUntilTheServerComesBack(t *testing.T) {
	f := newFakeComfy(t)
	url := f.srv.URL
	f.srv.Close()
	var attempts atomic.Int32
	s, err := NewServer(url, Options{Backoff: func(int) time.Duration { attempts.Add(1); return 5 * time.Millisecond }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	time.Sleep(60 * time.Millisecond)
	if attempts.Load() < 3 || s.Connected() {
		t.Errorf("%d attempts, connected = %v", attempts.Load(), s.Connected())
	}
}

func TestWaitGivesUpAfterRepeatedPollFailures(t *testing.T) {
	f := newFakeComfy(t)
	s := newServer(t, f, func(o *Options) { o.PollFailures = 3 })
	id := queue(t, s)
	f.start(id)
	res := waitAsync(t.Context(), s, id)
	notDone(t, res, 50*time.Millisecond)
	f.set(func() { f.historyStatus = 503 })
	wantTag(t, get(t, res).err, "Wait", "ServerUnreachable")

	// A single failed poll is tolerated.
	f.set(func() { f.historyStatus = 0 })
	id2 := queue(t, s)
	f.start(id2)
	res = waitAsync(t.Context(), s, id2)
	f.set(func() { f.historyStatus = 503 })
	time.Sleep(25 * time.Millisecond)
	f.set(func() { f.historyStatus = 0 })
	f.succeed(id2, outputsOf("9", "ok.png"), false)
	if r := get(t, res); r.err != nil {
		t.Errorf("a short failure ended the wait: %v", r.err)
	}
}

func TestWaitStopsWithItsContext(t *testing.T) {
	f := newFakeComfy(t)
	s := newServer(t, f)
	id := queue(t, s)
	f.start(id)
	ctx, cancel := context.WithCancel(t.Context())
	res := waitAsync(ctx, s, id)
	notDone(t, res, 50*time.Millisecond)
	cancel()
	wantTag(t, get(t, res).err, "Wait", "ComfyUICallCancelled")
	s.hub.mu.Lock()
	n := len(s.hub.waiters)
	s.hub.mu.Unlock()
	if n != 0 {
		t.Errorf("%d waiters left behind", n)
	}
}

func TestFetch(t *testing.T) {
	f := newFakeComfy(t)
	f.set(func() { f.files["a.png"] = []byte("PNGDATA") })
	s := newServer(t, f)
	body, err := s.Fetch(t.Context(), OutputFile{Filename: "a.png", Type: "output"})
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(body)
	if cerr := body.Close(); cerr != nil || err != nil || string(data) != "PNGDATA" {
		t.Errorf("data = %q, %v, %v", data, err, cerr)
	}
	_, err = s.Fetch(t.Context(), OutputFile{Filename: "missing.png"})
	wantTag(t, err, "Fetch", "ComfyUIResultMissing")
}

// Acceptance test 36: one upload per server, named by content, and the name
// ComfyUI returns is the one used.
func TestUploadIsDeduplicatedByContent(t *testing.T) {
	f := newFakeComfy(t)
	s := newServer(t, f)
	content := []byte("reference image bytes")

	var wg sync.WaitGroup
	values := make([]string, 100)
	for i := range values {
		wg.Add(1)
		go func() {
			defer wg.Done()
			v, err := s.Upload(t.Context(), "ref.PNG", bytes.NewReader(content))
			if err != nil {
				t.Error(err)
			}
			values[i] = v
		}()
	}
	wg.Wait()
	_, _, posts, folder := f.snapshot()
	if posts != 1 {
		t.Errorf("%d uploads for 100 jobs", posts)
	}
	if folder != "bos" {
		t.Errorf("sub-folder = %q", folder)
	}
	for _, v := range values {
		if v != values[0] || !strings.HasPrefix(v, "bos/") || !strings.HasSuffix(v, ".png") || len(v) != len("bos/")+64+len(".png") {
			t.Fatalf("input value = %q", v)
		}
	}

	// Different content is another file; the same content under another name is the same file.
	other, err := s.Upload(t.Context(), "other.png", strings.NewReader("different"))
	if err != nil || other == values[0] {
		t.Errorf("other = %q, %v", other, err)
	}
	same, err := s.Upload(t.Context(), "renamed.png", bytes.NewReader(content))
	if err != nil || same != values[0] {
		t.Errorf("same content = %q, %v", same, err)
	}
	if _, _, posts, _ = f.snapshot(); posts != 2 {
		t.Errorf("%d uploads", posts)
	}

	// A server that renames the file: the returned name wins.
	f2 := newFakeComfy(t)
	s2 := newServer(t, f2)
	f2.set(func() { f2.uploads["x"] = "x" }) // unrelated entry
	v, err := s2.Upload(t.Context(), "a.png", strings.NewReader("renamed by the server"))
	if err != nil || !strings.HasPrefix(v, "bos/") {
		t.Errorf("value = %q, %v", v, err)
	}
}

func TestUploadFailureIsNotRemembered(t *testing.T) {
	f := newFakeComfy(t)
	s := newServer(t, f)
	f.set(func() { f.uploadStatus = 507 })
	_, err := s.Upload(t.Context(), "a.png", strings.NewReader("x"))
	wantTag(t, err, "Upload", "ComfyUIUploadFailed")
	f.set(func() { f.uploadStatus = 0 })
	v, err := s.Upload(t.Context(), "a.png", strings.NewReader("x"))
	if err != nil || v == "" {
		t.Errorf("retry = %q, %v", v, err)
	}
	f.set(func() { f.uploadStatus = 503 })
	_, err = s.Upload(t.Context(), "b.png", strings.NewReader("y"))
	wantTag(t, err, "Upload", "ServerUnreachable")
	_, err = s.Upload(t.Context(), "c.png", errReader{})
	wantTag(t, err, "Upload", "ComfyUIUploadFailed")
}

type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, io.ErrClosedPipe }

func TestUploadsAreForgottenAfterAReconnect(t *testing.T) {
	f := newFakeComfy(t)
	s := newServer(t, f)
	first := connected(t, f, s)
	if _, err := s.Upload(t.Context(), "a.png", strings.NewReader("x")); err != nil {
		t.Fatal(err)
	}
	f.drop(first)
	f.waitSocket()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := s.Upload(t.Context(), "a.png", strings.NewReader("x")); err != nil {
			t.Fatal(err)
		}
		if _, _, posts, _ := f.snapshot(); posts == 2 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Error("the file was not stored again after the connection was lost")
}

func TestCancel(t *testing.T) {
	f := newFakeComfy(t)
	s := newServer(t, f)
	running, pending := queue(t, s), queue(t, s)
	f.start(running)

	if err := s.Cancel(t.Context(), pending); err != nil {
		t.Fatal(err)
	}
	if err := s.Cancel(t.Context(), running); err != nil {
		t.Fatal(err)
	}
	if err := s.Cancel(t.Context(), "gone"); err != nil {
		t.Fatal(err)
	}
	interrupts, deletes, _, _ := f.snapshot()
	if len(deletes) != 1 || deletes[0] != pending {
		t.Errorf("deletes = %v, want [%s]", deletes, pending)
	}
	if len(interrupts) != 1 || interrupts[0] != running {
		t.Errorf("interrupts = %v, want [%s]", interrupts, running)
	}
	f.set(func() { f.queueStatus = 503 })
	wantTag(t, s.Cancel(t.Context(), running), "Cancel", "ServerUnreachable")
}

func TestOldServersWarnOnceAboutInterrupt(t *testing.T) {
	f := newFakeComfy(t)
	f.set(func() { f.version = "0.3.40" })
	logs := &testLog{}
	s := newServer(t, f, func(o *Options) { o.Logger = slog.New(slog.NewTextHandler(logs, nil)) })
	for range 3 {
		id := queue(t, s)
		f.start(id)
		if err := s.Cancel(t.Context(), id); err != nil {
			t.Fatal(err)
		}
	}
	if n := strings.Count(logs.text(), "cannot interrupt one prompt only"); n != 1 {
		t.Errorf("warned %d times:\n%s", n, logs.text())
	}

	f2 := newFakeComfy(t)
	logs2 := &testLog{}
	s2 := newServer(t, f2, func(o *Options) { o.Logger = slog.New(slog.NewTextHandler(logs2, nil)) })
	id := queue(t, s2)
	f2.start(id)
	if err := s2.Cancel(t.Context(), id); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(logs2.text(), "cannot interrupt") {
		t.Error("a current server was reported as old")
	}
}

func TestVersionParsing(t *testing.T) {
	tests := []struct {
		in   string
		want [3]int
		ok   bool
		old  bool
	}{
		{"0.3.60", [3]int{0, 3, 60}, true, false}, {"0.3.57", [3]int{0, 3, 57}, true, false}, {"0.3.56", [3]int{0, 3, 56}, true, true},
		{"v0.4.0", [3]int{0, 4, 0}, true, false}, {"0.3.60-rc1", [3]int{0, 3, 60}, true, false}, {"0.2", [3]int{0, 2, 0}, true, true},
		{"", [3]int{}, false, false}, {"abc", [3]int{}, false, false}, {"1.x.3", [3]int{}, false, false},
	}
	for _, tt := range tests {
		v, ok := parseVersion(tt.in)
		if ok != tt.ok || (ok && v != tt.want) || (ok && less(v, minInterruptVersion) != tt.old) {
			t.Errorf("parseVersion(%q) = %v, %v", tt.in, v, ok)
		}
	}
}

func TestCloseStopsEverything(t *testing.T) {
	f := newFakeComfy(t)
	s, err := NewServer(f.srv.URL, Options{Backoff: func(int) time.Duration { return time.Millisecond }})
	if err != nil {
		t.Fatal(err)
	}
	connected(t, f, s)
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Errorf("second Close: %v", err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for f.socketCount() != 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if f.socketCount() != 0 {
		t.Error("the WebSocket stayed open after Close")
	}
}

func TestMapErrorKeepsApplicationErrors(t *testing.T) {
	f := newFakeComfy(t)
	s := newServer(t, f)
	orig := errors.New("Other", "x")
	if got := s.mapError(t.Context(), orig, "ComfyUIRejected", "op"); got != error(orig) { //nolint:errorlint // identity is the point
		t.Errorf("application error was wrapped: %v", got)
	}
}
