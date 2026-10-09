// Package scheduler runs the work of SPEC §8: a monitor and a runner for every
// ComfyUI server, the choice of jobs between servers (§8.7), the generation and
// fixing of a job (§8.2, §8.3), the reaction to failures and to the loss of a
// server, and the recovery of jobs after a restart.
//
// The queue is the database. A command that is run turns its idle jobs into
// queued ones under one queue number (store.JobRepo.QueueCommand); a runner
// takes the first queued job it may run and claims it with a single conditional
// update, so a job is held by exactly one runner (SPEC §14.3, rule 5).
package scheduler

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"time"

	"github.com/rm4n0s/bos/internal/comfy"
	"github.com/rm4n0s/bos/internal/domain"
	"github.com/rm4n0s/bos/internal/events"
	"github.com/rm4n0s/bos/internal/store"
	"github.com/rm4n0s/errors"
)

// Files is the file storage the scheduler reads job inputs from and writes
// results to.
type Files interface {
	Open(path string) (io.ReadCloser, error)
	Write(path string, r io.Reader) error
	Remove(path string) error
}

// Deps is everything the scheduler needs.
type Deps struct {
	DB    *store.DB
	Files Files
	// NewClient connects to a server. The scheduler asks for a new client every
	// time a server comes back, because a lost one is released (SPEC §8.1).
	NewClient func(domain.Server) (comfy.Client, error)
	Events    *events.Log
	Logger    *slog.Logger
	Now       func() time.Time
	// NewSeed returns a random 64-bit number for generators with a random seed.
	NewSeed func() uint64

	// Grace is how long a server may be unavailable before unpinned jobs that
	// prefer it go elsewhere (SPEC §8.7); default 30 s.
	Grace time.Duration
	// MaxAttempts is how many times a job is tried after its server was lost;
	// default 3.
	MaxAttempts int
	// ProbeInterval is how often each server is probed and its runner looks for
	// work (SPEC §8.1); default 1 s. ProbeTimeout bounds one probe; default 5 s.
	ProbeInterval time.Duration
	ProbeTimeout  time.Duration
}

func (d Deps) withDefaults() Deps {
	if d.Logger == nil {
		d.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	if d.Now == nil {
		d.Now = time.Now
	}
	if d.Events == nil {
		d.Events = events.NewLog(events.DefaultCapacity, d.Now)
	}
	if d.Grace <= 0 {
		d.Grace = 30 * time.Second
	}
	if d.MaxAttempts < 1 {
		d.MaxAttempts = 3
	}
	if d.ProbeInterval <= 0 {
		d.ProbeInterval = time.Second
	}
	if d.ProbeTimeout <= 0 {
		d.ProbeTimeout = 5 * time.Second
	}
	return d
}

// serverLostCause is why a job's context ended when its server was lost. A job
// whose context ends for another reason was stopped by the shutdown of the
// application.
type serverLostCause struct{}

func (serverLostCause) Error() string { return "the server became unreachable" }

// Scheduler owns the runners. Build it with New and start it with Run.
type Scheduler struct {
	Deps

	mu      sync.Mutex
	runners map[domain.ID]*runner
	wg      sync.WaitGroup

	targets *targetCache

	// resumable are the jobs that were running when the application stopped and
	// whose prompt can be waited for again, by the server that ran them.
	resMu     sync.Mutex
	resumable map[domain.ID][]domain.ID
	root      context.Context // the context of Run, for work that outlives a runner
}

// New returns a scheduler. It does nothing until Run.
func New(d Deps) *Scheduler {
	return &Scheduler{Deps: d.withDefaults(), runners: map[domain.ID]*runner{}, resumable: map[domain.ID][]domain.ID{}, targets: newTargetCache()}
}

// Run serves until ctx ends: it repairs the state left by a previous run, then
// keeps a runner for every server that is registered. It returns after every
// runner and every running job has stopped; jobs that were running stay
// `processing` in the database for the next start to recover.
func (s *Scheduler) Run(ctx context.Context) error {
	ctx, cancel := context.WithCancelCause(ctx)
	s.resMu.Lock()
	s.root = ctx
	s.resMu.Unlock()
	defer func() {
		cancel(context.Canceled)
		s.wg.Wait()
		s.closeRunners()
	}()

	if err := s.recoverJobs(ctx); err != nil {
		return err
	}
	ticker := time.NewTicker(s.ProbeInterval)
	defer ticker.Stop()
	for {
		if err := s.reconcile(ctx); err != nil && ctx.Err() == nil {
			logError(s.Logger, "could not read the servers", err)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

// Wake tells the runners that there may be work: a command was run, a job
// stopped, a server changed.
func (s *Scheduler) Wake() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, r := range s.runners {
		r.poke()
	}
}

// reconcile makes the set of runners match the registered servers. Runners are
// stopped after the lock is released: stopping one writes to the database and
// wakes the others.
func (s *Scheduler) reconcile(ctx context.Context) error {
	servers, err := s.DB.Repos().Servers.All(ctx)
	if err != nil {
		return err
	}
	var gone []*runner
	s.mu.Lock()
	known := make(map[domain.ID]bool, len(servers))
	for _, srv := range servers {
		known[srv.ID] = true
		if r, ok := s.runners[srv.ID]; ok {
			r.update(srv)
			continue
		}
		r := newRunner(s, srv)
		s.runners[srv.ID] = r
		r.start(ctx)
	}
	for id, r := range s.runners {
		if !known[id] {
			gone = append(gone, r)
			delete(s.runners, id)
		}
	}
	s.mu.Unlock()
	for _, r := range gone {
		r.stop(true)
	}

	// Jobs left over from a previous run whose server no longer exists.
	s.resMu.Lock()
	var orphans []domain.ID
	s.mu.Lock()
	for id := range s.resumable {
		if _, ok := s.runners[id]; !ok {
			orphans = append(orphans, id)
		}
	}
	s.mu.Unlock()
	s.resMu.Unlock()
	for _, id := range orphans {
		s.requeueResumable(id)
	}
	return nil
}

func (s *Scheduler) closeRunners() {
	s.mu.Lock()
	all := make([]*runner, 0, len(s.runners))
	for id, r := range s.runners {
		all = append(all, r)
		delete(s.runners, id)
	}
	s.mu.Unlock()
	for _, r := range all {
		r.stop(false)
	}
}

// Available reports whether the last probe of a server succeeded. With Client
// it makes the scheduler the fleet of the services.
func (s *Scheduler) Available(id domain.ID) bool {
	s.mu.Lock()
	r := s.runners[id]
	s.mu.Unlock()
	return r != nil && r.isAvailable()
}

// Client returns the connection to a server, or ServerUnreachable while the
// server is not available.
func (s *Scheduler) Client(_ context.Context, srv domain.Server) (comfy.Client, error) {
	s.mu.Lock()
	r := s.runners[srv.ID]
	s.mu.Unlock()
	if r != nil {
		if c := r.currentClient(); c != nil {
			return c, nil
		}
	}
	return nil, errors.New("ServerUnreachable", "the server is not available", "server", srv.CodeName)
}

// healthy returns the servers whose own jobs stay with them: the available
// ones and those unavailable for less than the grace period (SPEC §8.7).
func (s *Scheduler) healthy() []domain.ID {
	now := s.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	ids := make([]domain.ID, 0, len(s.runners))
	for id, r := range s.runners {
		if r.isHealthy(now, s.Grace) {
			ids = append(ids, id)
		}
	}
	return ids
}

// serverLost reports whether a job's context ended because its server was
// lost, as opposed to the application shutting down.
func serverLost(ctx context.Context) bool {
	var lost serverLostCause
	return ctx.Err() != nil && errors.As(context.Cause(ctx), &lost)
}

// logError logs err once, with the replayable form of an application error
// (SPEC §17.4).
func logError(l *slog.Logger, msg string, err error, attrs ...any) {
	if appErr, ok := errors.FromError(err); ok {
		if j, jerr := appErr.ToJson(); jerr == nil {
			l.Warn(msg, append(attrs, "error", j)...)
			return
		}
	}
	l.Warn(msg, append(attrs, "error", err.Error())...)
}
