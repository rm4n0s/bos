package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/rm4n0s/bos/internal/domain"
	"github.com/rm4n0s/bos/internal/events"
	"github.com/rm4n0s/errors"
)

func (a *API) eventRoutes() {
	a.add(route{Method: "GET", Path: "/events", Summary: "The event log, as JSON or as a server-sent event stream", Query: []string{"after"},
		Response: "stream", Errors: []string{"InvalidRequest"}, Run: a.listEvents})
}

// cursor reads where a client wants to continue: the Last-Event-ID header of a
// reconnecting stream, or the after parameter.
func cursor(r *http.Request) (after domain.ID, given bool, err error) {
	raw := r.URL.Query().Get("after")
	if raw == "" {
		raw = r.Header.Get("Last-Event-ID")
	}
	if raw == "" {
		return 0, false, nil
	}
	n, perr := strconv.ParseInt(raw, 10, 64)
	if perr != nil || n < 0 {
		return 0, false, badRequest("the event cursor must be a non-negative integer", "value", raw)
	}
	return n, true, nil
}

func (a *API) listEvents(w http.ResponseWriter, r *http.Request) error {
	after, given, err := cursor(r)
	if err != nil {
		return err
	}
	if strings.Contains(r.Header.Get("Accept"), "text/event-stream") {
		return a.streamEvents(w, r, after, given)
	}
	list, gap, last := a.Events.After(after)
	a.writeJSON(w, http.StatusOK, map[string]any{"events": list, "last_id": last, "gap": gap})
	return nil
}

// streamEvents sends the events as they arrive (SPEC §15.4). Without a cursor
// the stream starts at the newest event; with one it continues after it. A
// comment line goes out at the keep-alive interval.
func (a *API) streamEvents(w http.ResponseWriter, r *http.Request, after domain.ID, given bool) error {
	rc := http.NewResponseController(w)
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	if err := rc.Flush(); err != nil {
		return nil // the client cannot stream; nothing more to say to it
	}
	if !given {
		_, _, after = a.Events.After(0)
	}
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	go func() {
		select {
		case <-a.stop.Done():
			cancel()
		case <-ctx.Done():
		}
	}()

	for ctx.Err() == nil {
		waitCtx, stop := context.WithTimeout(ctx, a.KeepAlive)
		list, gap, last, err := a.Events.Wait(waitCtx, after)
		stop()
		switch {
		case err == nil:
		case errors.Is(err, context.DeadlineExceeded) && ctx.Err() == nil:
			if _, werr := fmt.Fprint(w, ": keep-alive\n\n"); werr != nil || rc.Flush() != nil {
				return nil
			}
			continue
		default:
			return nil
		}
		if gap {
			if _, werr := fmt.Fprint(w, "event: gap\ndata: {\"gap\":true}\n\n"); werr != nil {
				return nil
			}
		}
		for _, e := range list {
			if err := writeEvent(w, e); err != nil {
				return nil
			}
		}
		after = last
		if rc.Flush() != nil {
			return nil
		}
	}
	return nil
}

// writeEvent writes one event in the server-sent event format.
func writeEvent(w http.ResponseWriter, e events.Event) error {
	data, err := json.Marshal(struct {
		JobID     domain.ID `json:"job_id"`
		CommandID domain.ID `json:"command_id"`
		ProjectID domain.ID `json:"project_id"`
		Server    string    `json:"server"`
		CreatedAt string    `json:"created_at"`
	}{e.JobID, e.CommandID, e.ProjectID, e.Server, e.CreatedAt.Format("2006-01-02T15:04:05.999999999Z07:00")})
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n", e.ID, e.Type, data)
	return err
}
