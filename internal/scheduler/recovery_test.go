package scheduler

import (
	"testing"
	"time"

	"github.com/rm4n0s/bos/internal/domain"
	"github.com/rm4n0s/bos/internal/events"
)

// leave puts a job in the state a crash would have left it in.
func (r *rig) leave(job domain.ID, change func(j *domain.Job)) {
	r.t.Helper()
	repos := r.db.Repos()
	j, err := repos.Jobs.Get(ctx(r.t), job)
	check(r.t, err)
	change(j)
	check(r.t, repos.Jobs.Update(ctx(r.t), j))
}

func TestRecoveryResumesWhatWasSubmittedAndQueuesTheRest(t *testing.T) {
	r := newRig(t)
	srv, gpu1 := r.server("gpu1", 2)
	w := r.world(false)
	plain := r.command(w, "gpu1 -$ anime: chars")
	fixed := r.command(w, "gpu1 -$ anime: chars(a) > upscale")
	repos := r.db.Repos()
	_, err := repos.Jobs.QueueCommand(ctx(t), plain.ID)
	check(t, err)
	_, err = repos.Jobs.QueueCommand(ctx(t), fixed.ID)
	check(t, err)
	for _, c := range []*domain.Command{plain, fixed} {
		check(t, repos.Commands.SetStatus(ctx(t), c.ID, domain.StatusQueued))
	}

	pj := r.jobs(plain)
	fj := r.jobs(fixed)[0]
	// Job a was running and had submitted its prompt; job b was running but had
	// not submitted; the fixer job had finished its generation and was running
	// the fixer step.
	r.leave(pj[0].ID, func(j *domain.Job) {
		j.Status, j.ServerID, j.ServerHost, j.PromptID = domain.StatusProcessing, &srv.ID, srv.Host, "gpu1-old"
	})
	r.leave(pj[1].ID, func(j *domain.Job) { j.Status, j.ServerID = domain.StatusProcessing, &srv.ID })
	check(t, r.files.Write(fj.ResultPath, stringsReader("generated before the crash")))
	r.leave(fj.ID, func(j *domain.Job) {
		j.Status, j.ServerID, j.IsGenerated, j.PromptID = domain.StatusProcessing, &srv.ID, true, "gpu1-oldfix"
	})
	check(t, repos.Commands.SetStatus(ctx(t), plain.ID, domain.StatusProcessing))
	check(t, repos.Commands.SetStatus(ctx(t), fixed.ID, domain.StatusProcessing))

	r.start()
	r.waitStatus(plain, domain.StatusFinished)
	r.waitStatus(fixed, domain.StatusFinished)

	// Job a was not submitted again; its result came from the old prompt.
	graphs := gpu1.submitted()
	if graphs != 3 { // job b, job c, and the fixer step
		t.Errorf("%d prompts were submitted, want 3: the old prompt of job a must be waited for, not run again", graphs)
	}
	a := r.job(pj[0].ID)
	if a.Status != domain.StatusFinished || a.PromptID != "gpu1-old" || a.Attempts != 0 {
		t.Errorf("job a = %s prompt %q attempts %d", a.Status, a.PromptID, a.Attempts)
	}
	if got := r.read(a.ResultPath); got != "image from gpu1 via 9" {
		t.Errorf("job a result = %q", got)
	}
	// The fixer step started over, on the image of the generation.
	f := r.job(fj.ID)
	if f.FinishedFixers != 1 || !f.IsFixed {
		t.Errorf("fixer job = %+v", f)
	}
	name := fj.ResultPath[len(fj.ResultPath)-len("anime_c2_chars-a.png"):]
	if got := string(gpu1.uploaded[name]); got != "generated before the crash" {
		t.Errorf("the step was given %q", got)
	}
}

func TestRecoveryRepairsCommandStatuses(t *testing.T) {
	r := newRig(t)
	r.server("gpu1", 1)
	w := r.world(false)
	// A crash between the end of the last job and the update of its command.
	stuck := r.command(w, "gpu1 -$ anime: chars(a)")
	repos := r.db.Repos()
	r.leave(r.jobs(stuck)[0].ID, func(j *domain.Job) { j.Status, j.IsGenerated = domain.StatusFinished, true })
	check(t, repos.Commands.SetStatus(ctx(t), stuck.ID, domain.StatusProcessing))
	r.start()
	r.eventually("the command to be repaired", func() bool { return r.command_(stuck.ID).Status == domain.StatusFinished })
	if got := r.eventTypes(events.CommandFinished); len(got) != 1 {
		t.Errorf("events = %v", got)
	}
}

func TestRecoveryGivesUpOnAServerThatIsGone(t *testing.T) {
	r := newRig(t)
	gone, _ := r.server("old", 1)
	r.server("gpu1", 1)
	w := r.world(false)
	cmd := r.command(w, "gpu1 -$ anime: chars(a)")
	repos := r.db.Repos()
	_, err := repos.Jobs.QueueCommand(ctx(t), cmd.ID)
	check(t, err)
	r.leave(r.jobs(cmd)[0].ID, func(j *domain.Job) {
		j.Status, j.ServerID, j.PromptID = domain.StatusProcessing, &gone.ID, "old-1"
	})
	check(t, repos.Commands.SetStatus(ctx(t), cmd.ID, domain.StatusProcessing))
	check(t, r.svc.Servers.Delete(ctx(t), gone.ID))

	r.start()
	r.waitStatus(cmd, domain.StatusFinished)
	if j := r.jobs(cmd)[0]; j.Attempts != 1 || j.PromptID == "old-1" {
		t.Errorf("attempts %d, prompt %q: a job whose server is gone is tried again", j.Attempts, j.PromptID)
	}
}

func TestRecoveryGivesUpAfterTheGracePeriod(t *testing.T) {
	r := newRig(t)
	srv, gpu1 := r.server("gpu1", 1)
	_, gpu2 := r.server("gpu2", 1)
	gpu1.set(func(f *fakeComfy) { f.down = true })
	w := r.world(false)
	cmd := r.command(w, "gpu1, gpu2 -$ anime: chars(a)")
	repos := r.db.Repos()
	_, err := repos.Jobs.QueueCommand(ctx(t), cmd.ID)
	check(t, err)
	r.leave(r.jobs(cmd)[0].ID, func(j *domain.Job) {
		j.Status, j.ServerID, j.PromptID = domain.StatusProcessing, &srv.ID, "old-1"
	})
	check(t, repos.Commands.SetStatus(ctx(t), cmd.ID, domain.StatusProcessing))

	r.start()
	time.Sleep(80 * time.Millisecond)
	if got := r.jobs(cmd)[0].Status; got != domain.StatusProcessing {
		t.Fatalf("the job is %s: its server may still return within the grace period", got)
	}
	r.clock.Advance(31 * time.Second)
	r.waitStatus(cmd, domain.StatusFinished)
	if gpu2.submitted() != 1 {
		t.Errorf("gpu2 ran %d prompts", gpu2.submitted())
	}
}

func TestShutdownLeavesRunningJobsForTheNextStart(t *testing.T) {
	r := newRig(t)
	_, gpu1 := r.server("gpu1", 1)
	gate := make(chan struct{})
	gpu1.set(func(f *fakeComfy) { f.gate = gate })
	w := r.world(false)
	cmd := r.command(w, "gpu1 -$ anime: chars")
	r.start()
	r.run(cmd)
	r.eventually("the first job to run", func() bool { return gpu1.submitted() == 1 })

	r.stop()
	got := r.statuses(cmd)
	if got[0] != domain.StatusProcessing || got[1] != domain.StatusQueued || got[2] != domain.StatusQueued {
		t.Fatalf("statuses after the shutdown = %v: the running job must stay processing", got)
	}
	if j := r.jobs(cmd)[0]; j.Attempts != 0 || j.Error != "" || j.PromptID == "" {
		t.Errorf("job = attempts %d error %q prompt %q", j.Attempts, j.Error, j.PromptID)
	}
	if c := r.command_(cmd.ID); c.Status != domain.StatusProcessing {
		t.Errorf("command = %s", c.Status)
	}

	gpu1.set(func(f *fakeComfy) { f.gate = nil })
	r.restart()
	r.waitStatus(cmd, domain.StatusFinished)
	if gpu1.submitted() != 3 {
		t.Errorf("%d prompts: the interrupted job must be waited for, not run again", gpu1.submitted())
	}
}
