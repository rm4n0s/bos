package comfy

import (
	"context"
	"log/slog"
	"sync"
	"time"

	comfyui "github.com/rm4n0s/comfyui-client-go"
)

// event is what the hub hands to a waiter: how its prompt ended.
type event struct {
	finished finished
}

// waiter receives the end of one prompt.
type waiter struct {
	id string
	ch chan event // capacity 1: a prompt ends once
}

// hub routes the events of one server connection to the waiters of the prompts
// they belong to (SPEC §18.3). ComfyUI sends a prompt's events only to the
// client id that queued it, over one WebSocket, so a single reader goroutine
// consumes the stream and dispatches by prompt id.
//
// A prompt's id is known only after QueuePrompt returns, so its end can arrive
// before anyone waits for it. The hub therefore keeps the last events of
// unwatched prompts for a short time and hands them over on registration.
type hub struct {
	mu      sync.Mutex
	waiters map[string]*waiter
	early   map[string]earlyEvent
	max     int
	ttl     time.Duration
	now     func() time.Time
}

type earlyEvent struct {
	ev event
	at time.Time
}

func newHub(maxEarly int, ttl time.Duration, now func() time.Time) *hub {
	return &hub{waiters: map[string]*waiter{}, early: map[string]earlyEvent{}, max: maxEarly, ttl: ttl, now: now}
}

// register starts watching a prompt. If its end has already been seen, the
// waiter has it at once.
func (h *hub) register(id string) *waiter {
	w := &waiter{id: id, ch: make(chan event, 1)}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.waiters[id] = w
	if e, ok := h.early[id]; ok {
		delete(h.early, id)
		w.ch <- e.ev
	}
	return w
}

func (h *hub) unregister(w *waiter) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.waiters[w.id] == w {
		delete(h.waiters, w.id)
	}
}

// deliver gives the end of a prompt to its waiter, or keeps it for a waiter
// that has not registered yet.
func (h *hub) deliver(id string, ev event) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if w, ok := h.waiters[id]; ok {
		select {
		case w.ch <- ev:
		default: // already has its end
		}
		return
	}
	now := h.now()
	for k, e := range h.early {
		if now.Sub(e.at) > h.ttl {
			delete(h.early, k)
		}
	}
	if len(h.early) >= h.max {
		var oldest string
		var oldestAt time.Time
		for k, e := range h.early {
			if oldest == "" || e.at.Before(oldestAt) {
				oldest, oldestAt = k, e.at
			}
		}
		delete(h.early, oldest)
	}
	h.early[id] = earlyEvent{ev: ev, at: now}
}

// route dispatches one message of the stream. Only the end of a prompt matters
// here: progress is not tracked, and a `status` event whose queue is empty is
// deliberately not taken for completion, because with concurrent prompts it is
// unreliable (SPEC §18.3).
func (h *hub) route(msg *comfyui.WebSocketMessage, logger *slog.Logger) {
	decoded, err := msg.Decode()
	if err != nil {
		logger.Warn("comfy: undecodable event", "type", string(msg.Type), "error", err.Error())
		return
	}
	switch v := decoded.(type) {
	case comfyui.ExecutionSuccessData:
		h.deliver(v.PromptID, event{finished{outcome: outcomeSuccess}})
	case comfyui.ExecutionErrorData:
		h.deliver(v.PromptID, event{finished{outcome: outcomeFailed, failure: &v}})
	case comfyui.ExecutionInterruptedData:
		h.deliver(v.PromptID, event{finished{outcome: outcomeInterrupted, interrupted: &v}})
	}
}

// ---- connection ----

// connection holds the hub's current WebSocket client: the one that queues
// prompts, so their events come back over its socket.
type connection struct {
	mu     sync.Mutex
	client *comfyui.Client
}

func (c *connection) current() *comfyui.Client {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.client
}

func (c *connection) set(cl *comfyui.Client) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.client = cl
}

// run keeps a WebSocket client connected until ctx ends: it dials with
// back-off, consumes the stream, and dials a new client (with a new client id)
// when the connection is lost. The library never reconnects by itself.
func (s *Server) run(ctx context.Context) {
	defer s.wg.Done()
	attempt := 0
	for ctx.Err() == nil {
		client, err := comfyui.NewClient(s.baseURL, clientOption(s.opts))
		if err != nil {
			s.logger.Debug("comfy: websocket dial failed", "host", s.baseURL, "error", err.Error())
			if !sleep(ctx, s.opts.Backoff(attempt)) {
				return
			}
			attempt++
			continue
		}
		attempt = 0
		s.conn.set(client)
		s.setConnected(true, nil)
		s.consume(ctx, client)
		s.conn.set(nil)
		cause := client.Err()
		if closeErr := client.Close(); closeErr != nil {
			s.logger.Debug("comfy: closing the lost client", "error", closeErr.Error())
		}
		if ctx.Err() != nil {
			return
		}
		s.logger.Warn("comfy: connection lost", "host", s.baseURL, "error", errString(cause))
		s.setConnected(false, cause)
		s.forgetUploads() // a server that went away may have lost its inputs
		if !sleep(ctx, s.opts.Backoff(0)) {
			return
		}
	}
}

// consume reads the stream of one client until it ends. The hub is the only
// consumer of Listen (SPEC §18.3).
func (s *Server) consume(ctx context.Context, client *comfyui.Client) {
	events := client.Listen(ctx)
	for {
		select {
		case msg, ok := <-events:
			if !ok {
				return
			}
			s.hub.route(msg, s.logger)
		case <-ctx.Done():
			return
		}
	}
}

func sleep(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return true
	case <-ctx.Done():
		return false
	}
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
