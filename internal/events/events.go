// Package events is the ordered, bounded event log with subscriptions (SPEC §8.5).
//
// Several servers produce events at once, so a single "latest" slot would lose
// some. The log keeps the most recent ones under increasing ids; a client asks
// for what came after the last id it saw and receives it in order.
package events

import (
	"context"
	"sync"
	"time"

	"github.com/rm4n0s/bos/internal/domain"
)

// Type is the kind of an event.
type Type string

// The event types (SPEC §15.4).
const (
	Processing      Type = "processing"
	Finished        Type = "finished"
	Error           Type = "error"
	ServerOnline    Type = "server_online"
	ServerOffline   Type = "server_offline"
	CommandFinished Type = "command_finished"
	// The import of LoRAs from Civitai (SPEC §9) reports through the same log.
	ImportProgress Type = "import_progress"
	ImportFinished Type = "import_finished"
	ImportError    Type = "import_error"
)

// Event is one entry of the log. Fields that do not apply are zero.
type Event struct {
	ID        domain.ID `json:"id"`
	Type      Type      `json:"type"`
	JobID     domain.ID `json:"job_id"`
	CommandID domain.ID `json:"command_id"`
	ProjectID domain.ID `json:"project_id"`
	// Server is the code name of the server concerned.
	Server string `json:"server"`
	// GroupID and Message are set by the import events.
	GroupID   domain.ID `json:"group_id,omitempty"`
	Message   string    `json:"message,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// DefaultCapacity is the size of the log when none is configured.
const DefaultCapacity = 100

// Log is a bounded, ordered log of events. It is safe for concurrent use.
type Log struct {
	now func() time.Time

	mu     sync.Mutex
	ring   []Event // the retained events, oldest first
	cap    int
	lastID domain.ID
	// changed is closed and replaced whenever an event is appended, so waiters
	// can block on it together with their context.
	changed chan struct{}
}

// NewLog returns a log that retains the last capacity events (at least 1).
func NewLog(capacity int, now func() time.Time) *Log {
	if capacity < 1 {
		capacity = 1
	}
	if now == nil {
		now = time.Now
	}
	return &Log{now: now, cap: capacity, changed: make(chan struct{})}
}

// Append adds an event, assigning its id and time, and returns it.
func (l *Log) Append(e Event) Event {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.lastID++
	e.ID, e.CreatedAt = l.lastID, l.now().UTC()
	if len(l.ring) == l.cap {
		copy(l.ring, l.ring[1:])
		l.ring = l.ring[:l.cap-1]
	}
	l.ring = append(l.ring, e)
	close(l.changed)
	l.changed = make(chan struct{})
	return e
}

// After returns the events with an id above after, in order, and the id of the
// newest event in the log (0 for an empty log). gap is true when the client has
// missed events: after is older than the oldest retained event, or newer than
// the newest, as after a restart (SPEC §15.4). In both cases the retained
// events are returned.
func (l *Log) After(after domain.ID) (events []Event, gap bool, last domain.ID) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.after(after)
}

func (l *Log) after(after domain.ID) ([]Event, bool, domain.ID) {
	if len(l.ring) == 0 {
		return []Event{}, after > l.lastID, l.lastID
	}
	oldest := l.ring[0].ID
	gap := (after > 0 && after < oldest-1) || after > l.lastID
	if gap {
		after = 0
	}
	out := make([]Event, 0, len(l.ring))
	for _, e := range l.ring {
		if e.ID > after {
			out = append(out, e)
		}
	}
	return out, gap, l.lastID
}

// Wait blocks until there is an event after the given id, or ctx ends, and then
// behaves like After. A client that streams events calls it in a loop.
func (l *Log) Wait(ctx context.Context, after domain.ID) (events []Event, gap bool, last domain.ID, err error) {
	for {
		l.mu.Lock()
		events, gap, last = l.after(after)
		changed := l.changed
		l.mu.Unlock()
		if len(events) > 0 || gap {
			return events, gap, last, nil
		}
		select {
		case <-changed:
		case <-ctx.Done():
			return nil, false, last, ctx.Err()
		}
	}
}
