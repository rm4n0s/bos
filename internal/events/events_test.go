package events

import (
	"context"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/rm4n0s/bos/internal/domain"
)

func ids(events []Event) []domain.ID {
	out := make([]domain.ID, len(events))
	for i, e := range events {
		out[i] = e.ID
	}
	return out
}

func TestAppendNumbersAndStampsEvents(t *testing.T) {
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	l := NewLog(10, func() time.Time { return now })
	a := l.Append(Event{Type: Processing, JobID: 7, CommandID: 3, ProjectID: 1, Server: "gpu1"})
	b := l.Append(Event{Type: Finished, JobID: 7})
	if a.ID != 1 || b.ID != 2 || !a.CreatedAt.Equal(now) || a.JobID != 7 || a.Server != "gpu1" {
		t.Errorf("events = %+v, %+v", a, b)
	}
}

func TestAfterReturnsWhatCameAfter(t *testing.T) {
	l := NewLog(10, nil)
	if got, gap, last := l.After(0); len(got) != 0 || got == nil || gap || last != 0 {
		t.Errorf("empty log: %v, gap %v, last %d", got, gap, last)
	}
	for range 5 {
		l.Append(Event{Type: Processing})
	}
	tests := []struct {
		after domain.ID
		want  []domain.ID
		gap   bool
	}{
		{0, []domain.ID{1, 2, 3, 4, 5}, false},
		{2, []domain.ID{3, 4, 5}, false},
		{5, []domain.ID{}, false},
		{9, []domain.ID{1, 2, 3, 4, 5}, true}, // newer than the log: the application restarted
	}
	for _, tt := range tests {
		got, gap, last := l.After(tt.after)
		if !reflect.DeepEqual(ids(got), tt.want) || gap != tt.gap || last != 5 {
			t.Errorf("After(%d) = %v, gap %v, last %d; want %v, gap %v", tt.after, ids(got), gap, last, tt.want, tt.gap)
		}
	}
}

func TestTheLogIsBoundedAndReportsGaps(t *testing.T) {
	l := NewLog(3, nil)
	for range 7 {
		l.Append(Event{Type: Finished})
	}
	got, gap, last := l.After(0)
	if !reflect.DeepEqual(ids(got), []domain.ID{5, 6, 7}) || gap || last != 7 {
		t.Errorf("After(0) = %v, gap %v, last %d: a new client has seen nothing and misses nothing it knows of", ids(got), gap, last)
	}
	tests := []struct {
		after domain.ID
		want  []domain.ID
		gap   bool
	}{
		{4, []domain.ID{5, 6, 7}, false}, // the next event it needs, 5, is still there
		{3, []domain.ID{5, 6, 7}, true},  // event 4 is gone
		{1, []domain.ID{5, 6, 7}, true},
		{6, []domain.ID{7}, false},
	}
	for _, tt := range tests {
		got, gap, _ := l.After(tt.after)
		if !reflect.DeepEqual(ids(got), tt.want) || gap != tt.gap {
			t.Errorf("After(%d) = %v, gap %v; want %v, gap %v", tt.after, ids(got), gap, tt.want, tt.gap)
		}
	}
}

func TestTinyCapacity(t *testing.T) {
	l := NewLog(0, nil)
	l.Append(Event{})
	l.Append(Event{})
	if got, _, _ := l.After(0); !reflect.DeepEqual(ids(got), []domain.ID{2}) {
		t.Errorf("events = %v", ids(got))
	}
}

func TestWaitBlocksUntilAnEventArrives(t *testing.T) {
	l := NewLog(10, nil)
	l.Append(Event{Type: Processing})

	// What is already there returns at once.
	got, _, _, err := l.Wait(t.Context(), 0)
	if err != nil || len(got) != 1 {
		t.Fatalf("Wait = %v, %v", got, err)
	}

	done := make(chan []Event, 1)
	go func() {
		events, _, _, _ := l.Wait(t.Context(), 1)
		done <- events
	}()
	select {
	case <-done:
		t.Fatal("Wait returned with nothing new")
	case <-time.After(50 * time.Millisecond):
	}
	l.Append(Event{Type: Error})
	select {
	case events := <-done:
		if len(events) != 1 || events[0].Type != Error {
			t.Errorf("events = %+v", events)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Wait did not wake up")
	}
}

func TestWaitEndsWithItsContext(t *testing.T) {
	l := NewLog(10, nil)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
	defer cancel()
	_, _, _, err := l.Wait(ctx, 0)
	if err == nil {
		t.Error("Wait returned without an event or an error")
	}
}

func TestWaitReportsAGapAtOnce(t *testing.T) {
	l := NewLog(2, nil)
	for range 5 {
		l.Append(Event{})
	}
	events, gap, last, err := l.Wait(t.Context(), 1)
	if err != nil || !gap || last != 5 || len(events) != 2 {
		t.Errorf("Wait = %v, gap %v, last %d, %v", ids(events), gap, last, err)
	}
}

func TestConcurrentAppendsAreOrdered(t *testing.T) {
	l := NewLog(1000, nil)
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 100 {
				l.Append(Event{Type: Processing})
			}
		}()
	}
	wg.Wait()
	got, _, last := l.After(0)
	if len(got) != 800 || last != 800 {
		t.Fatalf("%d events, last %d", len(got), last)
	}
	for i, e := range got {
		if e.ID != domain.ID(i+1) {
			t.Fatalf("event %d has id %d", i, e.ID)
		}
	}
}
