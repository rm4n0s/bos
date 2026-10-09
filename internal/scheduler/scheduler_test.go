package scheduler

import (
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/bos/internal/domain"
	"github.com/rm4n0s/bos/internal/events"
)

func TestRunsACommandToTheEnd(t *testing.T) {
	r := newRig(t)
	_, gpu1 := r.server("gpu1", 1)
	w := r.world(false)
	cmd := r.command(w, "gpu1 -$ anime: chars")
	r.start()
	r.run(cmd)
	r.waitStatus(cmd, domain.StatusFinished)

	jobs := r.jobs(cmd)
	if len(jobs) != 3 {
		t.Fatalf("%d jobs", len(jobs))
	}
	graphs := gpu1.graphs()
	for i, j := range jobs {
		if j.Status != domain.StatusFinished || !j.IsGenerated || j.IsFixed || j.PromptID == "" || j.Error != "" {
			t.Errorf("job %d = status %s generated %v fixed %v prompt %q error %q", i, j.Status, j.IsGenerated, j.IsFixed, j.PromptID, j.Error)
		}
		if j.ServerID == nil || j.ServerHost != "http://gpu1:8188" {
			t.Errorf("job %d ran on %v %q", i, j.ServerID, j.ServerHost)
		}
		if got := promptText(t, graphs[i]); got != j.PositivePrompt {
			t.Errorf("job %d queued the prompt %q, want %q: jobs run in job order", i, got, j.PositivePrompt)
		}
		if !strings.HasSuffix(j.ResultPath, ".png") {
			t.Errorf("job %d result = %s", i, j.ResultPath)
		}
		if got := r.read(j.ResultPath); got != "image from gpu1 via 9" {
			t.Errorf("job %d result content = %q", i, got)
		}
	}

	types := r.eventTypes()
	want := []events.Type{events.ServerOnline, events.Processing, events.Finished, events.Processing, events.Finished, events.Processing, events.Finished, events.CommandFinished}
	if len(types) != len(want) {
		t.Fatalf("events = %v, want %v", types, want)
	}
	for i := range want {
		if types[i] != want[i] {
			t.Errorf("events = %v, want %v", types, want)
			break
		}
	}
	all, _, _ := r.log.After(0)
	if e := all[len(all)-1]; e.CommandID != cmd.ID || e.ProjectID != w.project.ID {
		t.Errorf("last event = %+v", e)
	}
	if e := all[1]; e.JobID != jobs[0].ID || e.Server != "gpu1" {
		t.Errorf("first processing event = %+v", e)
	}
}

func TestServerCapacityIsRespected(t *testing.T) {
	r := newRig(t)
	_, gpu1 := r.server("gpu1", 2)
	gate := make(chan struct{})
	gpu1.set(func(f *fakeComfy) { f.gate = gate })
	w := r.world(false)
	cmd := r.command(w, "gpu1 -$ anime: chars")
	r.start()
	r.run(cmd)

	r.eventually("two jobs to run", func() bool { return gpu1.submitted() == 2 })
	time.Sleep(50 * time.Millisecond) // a third job would have started by now
	if n := gpu1.submitted(); n != 2 {
		t.Fatalf("%d prompts submitted while the server takes two at a time", n)
	}
	if got := r.statuses(cmd); got[0] != domain.StatusProcessing || got[1] != domain.StatusProcessing || got[2] != domain.StatusQueued {
		t.Errorf("statuses = %v", got)
	}
	if c := r.command_(cmd.ID); c.Status != domain.StatusProcessing {
		t.Errorf("command status = %s", c.Status)
	}
	gate <- struct{}{} // one job ends; the third starts
	r.eventually("the third job to start", func() bool { return gpu1.submitted() == 3 })
	close(gate)
	r.waitStatus(cmd, domain.StatusFinished)
	if gpu1.maxSeen > 2 {
		t.Errorf("%d prompts were waited for at once", gpu1.maxSeen)
	}
}

func TestServersWorkInParallelAndTakeTheNextJob(t *testing.T) {
	r := newRig(t)
	_, gpu1 := r.server("gpu1", 1)
	_, gpu2 := r.server("gpu2", 1)
	for _, f := range []*fakeComfy{gpu1, gpu2} {
		f.set(func(f *fakeComfy) { f.latency = 40 * time.Millisecond })
	}
	w := r.world(false)
	cmd := r.command(w, "gpu1, gpu2 -$ anime: chars")
	r.start()
	r.eventually("both servers to be online", func() bool { return r.sch.Available(1) && r.sch.Available(2) })
	r.run(cmd)
	r.waitStatus(cmd, domain.StatusFinished)

	if gpu1.submitted() == 0 || gpu2.submitted() == 0 || gpu1.submitted()+gpu2.submitted() != 3 {
		t.Errorf("gpu1 ran %d jobs, gpu2 ran %d", gpu1.submitted(), gpu2.submitted())
	}
	jobs := r.jobs(cmd)
	// The first two jobs started together, on different servers.
	if *jobs[0].ServerID == *jobs[1].ServerID {
		t.Errorf("the first two jobs ran on the same server %d", *jobs[0].ServerID)
	}
}

func TestStaticAssignmentStaysUntilTheGraceEnds(t *testing.T) {
	r := newRig(t)
	gpu1srv, gpu1 := r.server("gpu1", 1)
	_, gpu2 := r.server("gpu2", 1)
	gpu2.set(func(f *fakeComfy) { f.down = true })
	w := r.world(false)
	cmd := r.command(w, "gpu1, gpu2 -$ anime: chars spread even")
	jobs := r.jobs(cmd)
	if *jobs[0].PreferredServerID != gpu1srv.ID || jobs[1].PreferredServerID == nil || *jobs[1].PreferredServerID == gpu1srv.ID {
		t.Fatalf("preferred servers: %v %v %v", jobs[0].PreferredServerID, jobs[1].PreferredServerID, jobs[2].PreferredServerID)
	}
	r.start()
	r.eventually("gpu1 online", func() bool { return r.sch.Available(gpu1srv.ID) })
	r.run(cmd)

	// gpu1 does its own jobs (1 and 3); job 2 waits for gpu2.
	r.eventually("gpu1 to finish its two jobs", func() bool {
		s := r.statuses(cmd)
		return s[0] == domain.StatusFinished && s[2] == domain.StatusFinished
	})
	time.Sleep(50 * time.Millisecond)
	if got := r.statuses(cmd)[1]; got != domain.StatusQueued {
		t.Fatalf("job 2 is %s: it must wait for its server within the grace period", got)
	}

	r.clock.Advance(29 * time.Second)
	time.Sleep(50 * time.Millisecond)
	if got := r.statuses(cmd)[1]; got != domain.StatusQueued {
		t.Fatalf("job 2 is %s after 29 s", got)
	}
	r.clock.Advance(2 * time.Second) // gpu2 has now been down for longer than 30 s
	r.waitStatus(cmd, domain.StatusFinished)
	if j := r.job(jobs[1].ID); *j.ServerID != gpu1srv.ID {
		t.Errorf("job 2 ran on server %d, want gpu1", *j.ServerID)
	}
	if gpu2.submitted() != 0 || gpu1.submitted() != 3 {
		t.Errorf("gpu1 ran %d, gpu2 ran %d", gpu1.submitted(), gpu2.submitted())
	}
}

func TestPinnedJobsNeverMove(t *testing.T) {
	r := newRig(t)
	gpu1srv, gpu1 := r.server("gpu1", 1)
	_, gpu2 := r.server("gpu2", 1)
	gpu2.set(func(f *fakeComfy) { f.down = true })
	w := r.world(false)
	cmd := r.command(w, "gpu1, gpu2 -$ anime: chars spread even pinned")
	r.start()
	r.eventually("gpu1 online", func() bool { return r.sch.Available(gpu1srv.ID) })
	r.run(cmd)
	r.eventually("gpu1 to finish its jobs", func() bool { return gpu1.submitted() == 2 })
	r.clock.Advance(time.Hour)
	time.Sleep(100 * time.Millisecond)
	if got := r.statuses(cmd)[1]; got != domain.StatusQueued {
		t.Errorf("a pinned job moved: %s", got)
	}
	// It runs when its server returns.
	gpu2.set(func(f *fakeComfy) { f.down = false })
	r.waitStatus(cmd, domain.StatusFinished)
	if gpu2.submitted() != 1 {
		t.Errorf("gpu2 ran %d jobs, want the pinned one", gpu2.submitted())
	}
}

func TestADeletedServerReleasesItsUnpinnedJobs(t *testing.T) {
	r := newRig(t)
	gpu1srv, gpu1 := r.server("gpu1", 1)
	gpu2srv, _ := r.server("gpu2", 1)
	w := r.world(false)
	cmd := r.command(w, "gpu1, gpu2 -$ anime: chars spread even")
	check(t, r.svc.Servers.Delete(ctx(t), gpu2srv.ID))
	r.start()
	r.eventually("gpu1 online", func() bool { return r.sch.Available(gpu1srv.ID) })
	r.run(cmd)
	r.waitStatus(cmd, domain.StatusFinished)
	if gpu1.submitted() != 3 {
		t.Errorf("gpu1 ran %d jobs, want all three", gpu1.submitted())
	}
}

func TestRandomSeedGeneratorsGetAFreshSeedPerJob(t *testing.T) {
	r := newRig(t)
	_, gpu1 := r.server("gpu1", 1)
	w := r.world(true)
	plain := r.command(w, "gpu1 -$ anime: chars")
	fixed := r.command(w, "gpu1 -$ anime: chars(a) with seed=99")
	r.start()
	r.run(plain)
	r.run(fixed)
	r.waitStatus(plain, domain.StatusFinished)
	r.waitStatus(fixed, domain.StatusFinished)

	seen := map[uint64]bool{}
	for _, j := range r.jobs(plain) {
		if j.Seed == nil || seen[*j.Seed] || *j.Seed <= 1000 {
			t.Errorf("job %d seed = %v", j.ID, j.Seed)
			continue
		}
		seen[*j.Seed] = true
	}
	if j := r.jobs(fixed)[0]; j.Seed == nil || *j.Seed != 99 {
		t.Errorf("the seed of the command was replaced: %v", j.Seed)
	}
	// The sampler of the submitted workflow carries the recorded seed.
	for i, g := range gpu1.graphs() {
		var job domain.Job
		for _, c := range []*domain.Command{plain, fixed} {
			for _, j := range r.jobs(c) {
				if j.PromptID == gpu1.order[i] {
					job = j
				}
			}
		}
		n, err := findSampler(g)
		check(t, err)
		if got := strings.TrimSpace(string(n)); got != itoa(*job.Seed) {
			t.Errorf("prompt %d: sampler seed %s, job seed %d", i, got, *job.Seed)
		}
	}
}
