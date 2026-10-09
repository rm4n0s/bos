package ui

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/rm4n0s/bos/internal/domain"
	"github.com/rm4n0s/bos/internal/events"
)

// defaultCoalesce is how long a burst of events is gathered before it is drawn,
// so a fast batch does not flood the browser: a job is patched at most once in
// that time (SPEC §16.4).
const defaultCoalesce = 250 * time.Millisecond

// lastEventID reads where a reconnecting stream continues. A browser that has
// not seen an event sends none.
func lastEventID(r *http.Request) (domain.ID, bool) {
	raw := r.Header.Get("Last-Event-ID")
	if raw == "" {
		return 0, false
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || n < 0 {
		return 0, false
	}
	return n, true
}

// serveEvents runs a live stream: it waits for events, gathers a burst, and
// hands the batch to draw. A stream without Last-Event-ID starts at the newest
// event, because the page it belongs to was just drawn. It ends when the
// request ends or the interface is stopped.
func (u *UI) serveEvents(w http.ResponseWriter, r *http.Request, draw func(s *stream, batch []events.Event, gap bool) error) {
	after, resumed := lastEventID(r)
	s := u.newStream(w, r)
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	go func() {
		select {
		case <-u.stop.Done():
			cancel()
		case <-ctx.Done():
		}
	}()
	if !resumed {
		_, _, after = u.Events.After(0)
	}
	rc := http.NewResponseController(w)
	for ctx.Err() == nil {
		waitCtx, stop := context.WithTimeout(ctx, u.KeepAlive)
		list, gap, last, err := u.Events.Wait(waitCtx, after)
		stop()
		switch {
		case err == nil:
		case ctx.Err() == nil && waitCtx.Err() != nil:
			if _, werr := fmt.Fprint(w, ": keep-alive\n\n"); werr != nil || rc.Flush() != nil {
				return
			}
			continue
		default:
			return
		}
		// Gather the burst.
		select {
		case <-time.After(u.coalesce()):
		case <-ctx.Done():
			return
		}
		list, gap, last = u.afterGathering(after, list, gap, last)
		s.eventID = strconv.FormatInt(last, 10)
		if err := draw(s, list, gap); err != nil {
			u.Logger.Warn("could not draw a live update", "error", err.Error())
		}
		s.eventID = ""
		after = last
		if s.IsClosed() {
			return
		}
	}
}

// afterGathering returns the events of the whole burst: what Wait returned and
// what came during the pause.
func (u *UI) afterGathering(after domain.ID, list []events.Event, gap bool, last domain.ID) ([]events.Event, bool, domain.ID) {
	more, moreGap, newest := u.Events.After(after)
	if len(more) >= len(list) || moreGap {
		return more, gap || moreGap, newest
	}
	return list, gap, last
}

func (u *UI) coalesce() time.Duration {
	if u.Coalesce > 0 {
		return u.Coalesce
	}
	return defaultCoalesce
}

// isJobEvent reports whether an event is about the state of one job.
func isJobEvent(t events.Type) bool {
	return t == events.Processing || t == events.Finished || t == events.Error
}

func (u *UI) streamRoutes() {
	u.handle("GET /ui/stream/commands/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, err := pathID(r, "id")
		if err != nil {
			http.NotFound(w, r)
			return
		}
		if _, err := u.Services.Commands.Get(r.Context(), id); err != nil {
			http.NotFound(w, r)
			return
		}
		u.serveEvents(w, r, func(s *stream, batch []events.Event, gap bool) error {
			return u.drawCommandUpdate(r, s, id, batch, gap)
		})
	})
	u.handle("GET /ui/stream/toasts", func(w http.ResponseWriter, r *http.Request) {
		u.serveEvents(w, r, func(s *stream, batch []events.Event, gap bool) error {
			return u.drawToasts(r, s, batch, gap)
		})
	})
}

// drawCommandUpdate redraws what a burst of events changed on the page of a
// command: the rows of its jobs and the progress above them.
func (u *UI) drawCommandUpdate(r *http.Request, s *stream, command domain.ID, batch []events.Event, gap bool) error {
	jobs := map[domain.ID]bool{}
	touched := false
	for _, e := range batch {
		if e.CommandID != command {
			continue
		}
		touched = true
		if isJobEvent(e.Type) && e.JobID > 0 {
			jobs[e.JobID] = true
		}
	}
	if gap {
		if err := s.toast("error", "Some updates were missed. Reload the page to see the current state."); err != nil {
			return err
		}
		touched = true
	}
	if !touched {
		return nil
	}
	ids := make([]domain.ID, 0, len(jobs))
	for id := range jobs {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	for _, id := range ids {
		j, err := u.Services.Jobs.Get(r.Context(), id)
		if err != nil {
			continue // the job is gone: its command was edited or deleted
		}
		if err := s.render("job_row", u.jobViewOf(j)); err != nil {
			return err
		}
	}
	h, err := u.commandHeaderOf(r, command)
	if err != nil {
		return nil // the command is gone
	}
	return s.render("command_header", h)
}

// drawToasts tells the user what the burst did: one toast for each kind of
// event, the progress in the command lists and the badge of a server that
// changed.
func (u *UI) drawToasts(r *http.Request, s *stream, batch []events.Event, gap bool) error {
	var started, finished int
	var failed []domain.ID
	commands := map[domain.ID]bool{}
	repl := false
	for _, e := range batch {
		if e.CommandID > 0 {
			commands[e.CommandID] = true
		}
		if e.CommandID == domain.ReplID && isJobEvent(e.Type) {
			repl = true
		}
		switch e.Type {
		case events.Processing:
			started++
		case events.Finished:
			finished++
		case events.Error:
			failed = append(failed, e.JobID)
		case events.CommandFinished:
			if err := s.toast("finished", fmt.Sprintf("Command %d finished.", e.CommandID)); err != nil {
				return err
			}
		case events.ServerOnline, events.ServerOffline:
			if err := u.drawServerChange(r, s, e); err != nil {
				return err
			}
		}
	}
	if gap {
		if err := s.toast("error", "Some updates were missed. Reload the page to see the current state."); err != nil {
			return err
		}
	}
	if finished > 0 {
		if err := s.toast("finished", plural(finished, "job")+" finished."); err != nil {
			return err
		}
	}
	if len(failed) > 0 {
		if err := s.toast("error", plural(len(failed), "job")+" failed: "+idList(failed)+"."); err != nil {
			return err
		}
	}
	if started > 0 && finished == 0 && len(failed) == 0 {
		if err := s.toast("processing", plural(started, "job")+" started."); err != nil {
			return err
		}
	}
	if err := u.drawImportEvents(r, s, batch); err != nil {
		return err
	}
	if repl {
		// On the REPL page the picture replaces itself when the job ends.
		if err := u.drawRepl(r, s); err != nil {
			return err
		}
	}
	ids := make([]domain.ID, 0, len(commands))
	for id := range commands {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	for _, id := range ids {
		sum, err := u.Services.Commands.Summary(r.Context(), id)
		if err != nil {
			continue // the command is gone
		}
		if err := s.render("command_progress", sum); err != nil {
			return err
		}
	}
	return nil
}

// drawServerChange says that a server came or went and redraws its badge where
// the page shows one.
func (u *UI) drawServerChange(r *http.Request, s *stream, e events.Event) error {
	state := "offline"
	if e.Type == events.ServerOnline {
		state = "online"
	}
	kind := "error"
	if e.Type == events.ServerOnline {
		kind = "ok"
	}
	if err := s.toast(kind, "Server "+e.Server+" is "+state+"."); err != nil {
		return err
	}
	list, err := u.Services.Servers.List(r.Context(), domain.Page{Number: 1, Size: domain.MaxPageSize})
	if err != nil {
		return err
	}
	for _, v := range list.Items {
		if v.CodeName == e.Server {
			return s.render("server_status", v)
		}
	}
	return nil
}

func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return strconv.Itoa(n) + " " + noun + "s"
}

func idList(ids []domain.ID) string {
	const shown = 5
	out := ""
	for i, id := range ids {
		if i == shown {
			return out + fmt.Sprintf(" and %d more", len(ids)-shown)
		}
		if i > 0 {
			out += ", "
		}
		out += "#" + strconv.FormatInt(id, 10)
	}
	return out
}
