package scheduler

import (
	"context"
	"sync"
	"time"

	"github.com/rm4n0s/bos/internal/comfy"
	"github.com/rm4n0s/bos/internal/domain"
	"github.com/rm4n0s/bos/internal/events"
	"github.com/rm4n0s/bos/internal/store"
)

// pageSize is how many queued jobs a runner reads at a time while it looks for
// one it may run.
const pageSize = 50

// runner serves one server: a monitor probes it, and a loop gives it jobs up to
// its max_concurrent_jobs (SPEC §8.1).
type runner struct {
	s  *Scheduler
	id domain.ID

	mu        sync.Mutex
	server    domain.Server
	client    comfy.Client // nil while the server is unavailable
	available bool
	since     time.Time // when `available` last changed, or when the runner started
	running   map[domain.ID]context.CancelCauseFunc

	wake   chan struct{}
	cancel context.CancelFunc
	done   sync.WaitGroup
}

func newRunner(s *Scheduler, srv domain.Server) *runner {
	return &runner{
		s: s, id: srv.ID, server: srv, since: s.Now(),
		running: map[domain.ID]context.CancelCauseFunc{}, wake: make(chan struct{}, 1),
	}
}

// start launches the monitor and the loop under ctx.
func (r *runner) start(ctx context.Context) {
	ctx, r.cancel = context.WithCancel(ctx)
	for _, f := range []func(context.Context){r.monitor, r.loop} {
		r.done.Add(1)
		r.s.wg.Add(1)
		go func() {
			defer r.done.Done()
			defer r.s.wg.Done()
			f(ctx)
		}()
	}
}

// stop ends the runner: its jobs lose their server, and the client is released.
// It does not wait for the goroutines; Run does. When the server itself is gone
// (requeue), the jobs of an earlier run that were waiting for it are queued
// again; at shutdown they stay for the next start.
func (r *runner) stop(requeue bool) {
	if r.cancel != nil {
		r.cancel()
	}
	r.lose()
	if requeue {
		r.s.requeueResumable(r.id)
	}
}

func (r *runner) poke() {
	select {
	case r.wake <- struct{}{}:
	default:
	}
}

// update applies an edit of the server. A new address is a new server: the
// connection and the jobs on it are lost.
func (r *runner) update(srv domain.Server) {
	r.mu.Lock()
	changed := srv.Host != r.server.Host
	r.server = srv
	r.mu.Unlock()
	if changed {
		r.lose()
	}
}

func (r *runner) isAvailable() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.available
}

// downSince reports whether the server is unavailable, and since when.
func (r *runner) downSince() (time.Time, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.since, !r.available
}

func (r *runner) currentClient() comfy.Client {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.client
}

// isHealthy reports whether the server's own jobs stay with it: it is
// available, or has been unavailable for no longer than the grace period.
func (r *runner) isHealthy(now time.Time, grace time.Duration) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.available || now.Sub(r.since) <= grace
}

func (r *runner) code() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.server.CodeName
}

// comfyPath is the folder of the server's ComfyUI installation, empty when it
// was not told.
func (r *runner) comfyPath() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.server.ComfyUIPath
}

// ---- monitor ----

// monitor probes the server every interval: reachable means available,
// unreachable means unavailable since now, and the client is released
// (SPEC §8.1).
func (r *runner) monitor(ctx context.Context) {
	ticker := time.NewTicker(r.s.ProbeInterval)
	defer ticker.Stop()
	for {
		r.probe(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (r *runner) probe(ctx context.Context) {
	r.mu.Lock()
	client, srv := r.client, r.server
	r.mu.Unlock()

	if client == nil {
		c, err := r.s.NewClient(srv)
		if err != nil {
			r.markDown(ctx, err)
			return
		}
		client = c
	}
	pctx, cancel := context.WithTimeout(ctx, r.s.ProbeTimeout)
	err := client.Ping(pctx)
	cancel()
	if ctx.Err() != nil {
		r.release(client)
		return
	}
	if err != nil {
		r.release(client)
		r.markDown(ctx, err)
		return
	}
	r.markUp(client)
}

// release closes a client that is not in use any more.
func (r *runner) release(c comfy.Client) {
	r.mu.Lock()
	if r.client == c {
		r.client = nil
	}
	r.mu.Unlock()
	if err := c.Close(); err != nil {
		r.s.Logger.Debug("closing a client failed", "server", r.code(), "error", err.Error())
	}
}

func (r *runner) markUp(client comfy.Client) {
	r.mu.Lock()
	old := r.client
	r.client = client
	was := r.available
	if !was {
		r.available, r.since = true, r.s.Now()
	}
	code := r.server.CodeName
	r.mu.Unlock()
	if old != nil && old != client {
		_ = old.Close() // replaced by a newer connection; nothing to report
	}
	if !was {
		r.s.Events.Append(events.Event{Type: events.ServerOnline, Server: code})
		r.s.Wake() // other servers' jobs may now have a home again
	}
}

func (r *runner) markDown(_ context.Context, cause error) {
	r.mu.Lock()
	was := r.available
	r.mu.Unlock()
	if !was {
		return
	}
	r.s.Logger.Info("server became unavailable", "server", r.code(), "error", cause.Error())
	r.lose()
}

// lose marks the server unavailable: its running jobs lose their server, the
// client is released and an event tells the clients.
func (r *runner) lose() {
	r.mu.Lock()
	was := r.available
	client := r.client
	cancels := make([]context.CancelCauseFunc, 0, len(r.running))
	for _, c := range r.running {
		cancels = append(cancels, c)
	}
	r.client, r.available = nil, false
	if was {
		r.since = r.s.Now()
	}
	code := r.server.CodeName
	r.mu.Unlock()

	for _, cancel := range cancels {
		cancel(serverLostCause{})
	}
	if client != nil {
		if err := client.Close(); err != nil {
			r.s.Logger.Debug("closing a client failed", "server", code, "error", err.Error())
		}
	}
	if was {
		r.s.Events.Append(events.Event{Type: events.ServerOffline, Server: code})
	}
}

// ---- the loop that gives the server work ----

func (r *runner) loop(ctx context.Context) {
	ticker := time.NewTicker(r.s.ProbeInterval)
	defer ticker.Stop()
	for {
		r.fill(ctx)
		select {
		case <-ctx.Done():
			return
		case <-r.wake:
		case <-ticker.C:
		}
	}
}

// fill starts jobs until the server is full or nothing is left for it.
func (r *runner) fill(ctx context.Context) {
	for ctx.Err() == nil {
		r.mu.Lock()
		client, free := r.client, r.available && len(r.running) < r.server.MaxConcurrentJobs
		r.mu.Unlock()
		if client == nil || !free {
			break
		}
		if id, ok := r.s.takeResumable(r.id); ok {
			r.resume(ctx, client, id)
			continue
		}
		job, err := r.pick(ctx)
		if err != nil {
			if ctx.Err() == nil {
				logError(r.s.Logger, "could not look for a job", err, "server", r.code())
			}
			break
		}
		if job == nil {
			break
		}
		r.launch(ctx, client, job, false)
	}
	if since, down := r.downSince(); down {
		r.s.requeueStale(r.id, since)
	}
}

// pick takes the first queued job in queue order that this server may run and
// claims it (SPEC §8.7). Another runner may claim a job first; then the next
// one is tried.
func (r *runner) pick(ctx context.Context) (*domain.Job, error) {
	repos := r.s.DB.Repos()
	active, err := repos.Commands.ListByStatus(ctx, domain.StatusQueued, domain.StatusProcessing)
	if err != nil {
		return nil, err
	}
	r.s.targets.refresh(active)
	code := r.code()
	healthy := r.s.healthy()
	filter := store.QueueFilter{Server: r.id, Commands: r.s.targets.commandsFor(code), Healthy: healthy}
	var cursor store.QueueCursor
	for {
		page, err := repos.Jobs.ListQueuedFor(ctx, cursor, pageSize, filter)
		if err != nil {
			return nil, err
		}
		for i := range page {
			j := &page[i]
			if !r.s.eligible(j, r.id, code, healthy) {
				continue
			}
			claimed, err := repos.Jobs.Claim(ctx, j.ID)
			if err != nil {
				return nil, err
			}
			if claimed {
				return r.begin(ctx, j.ID)
			}
		}
		if len(page) < pageSize {
			return nil, nil
		}
		last := page[len(page)-1]
		cursor = store.QueueCursor{Seq: last.QueueSeq, ID: last.ID}
	}
}

// eligible applies the table of SPEC §8.7 to a queued job. A server that takes
// over a job from another must be one of the targets of its command (§6.9).
func (s *Scheduler) eligible(j *domain.Job, server domain.ID, code string, healthy []domain.ID) bool {
	isHealthy := func(id domain.ID) bool {
		for _, h := range healthy {
			if h == id {
				return true
			}
		}
		return false
	}
	target := s.targets.has(j.CommandID, code)
	if !j.IsGenerated {
		switch {
		case j.PreferredServerID != nil && *j.PreferredServerID == server:
			return true
		case j.PreferredServerID == nil:
			return target
		case j.Pinned:
			return false
		}
		return target && !isHealthy(*j.PreferredServerID)
	}
	if j.FinishedFixers >= len(j.Fixers) {
		return true // nothing left to run; any server can close the job
	}
	step := j.Fixers[j.FinishedFixers]
	if step.ServerID != 0 {
		return step.ServerID == server
	}
	if j.ServerID == nil || *j.ServerID == server {
		return true
	}
	return target && !isHealthy(*j.ServerID)
}

// begin reads a claimed job and prepares it for its next stage.
func (r *runner) begin(ctx context.Context, id domain.ID) (*domain.Job, error) {
	repos := r.s.DB.Repos()
	job, err := repos.Jobs.Get(ctx, id)
	if err != nil {
		// The job was claimed but cannot be read: give it back.
		if _, terr := repos.Jobs.Transition(context.WithoutCancel(ctx), id, domain.StatusProcessing, domain.StatusQueued); terr != nil {
			logError(r.s.Logger, "could not give a job back", terr, "job", id)
		}
		return nil, err
	}
	r.mu.Lock()
	srv := r.server
	r.mu.Unlock()
	if !job.IsGenerated {
		job.ServerID, job.ServerHost = &srv.ID, srv.Host
	}
	job.PromptID, job.Error, job.ErrorTrace = "", "", ""
	if err := repos.Jobs.Update(ctx, job); err != nil {
		return nil, err
	}
	r.s.syncCommand(ctx, job.CommandID)
	return job, nil
}

// launch runs a job on its own goroutine.
func (r *runner) launch(ctx context.Context, client comfy.Client, job *domain.Job, resume bool) {
	jctx, cancel := context.WithCancelCause(ctx)
	r.mu.Lock()
	r.running[job.ID] = cancel
	r.mu.Unlock()
	r.s.wg.Add(1)
	go func() {
		defer r.s.wg.Done()
		defer func() {
			cancel(nil)
			r.mu.Lock()
			delete(r.running, job.ID)
			r.mu.Unlock()
			r.poke()
			r.s.Wake()
		}()
		r.s.execute(jctx, r, client, job, resume)
	}()
}

// resume waits again for the prompt of a job that was running when the
// application stopped.
func (r *runner) resume(ctx context.Context, client comfy.Client, id domain.ID) {
	job, err := r.s.DB.Repos().Jobs.Get(ctx, id)
	if err != nil || job.Status != domain.StatusProcessing || job.IsGenerated || job.PromptID == "" {
		return // it moved on, or was deleted, since the restart
	}
	r.launch(ctx, client, job, true)
}
