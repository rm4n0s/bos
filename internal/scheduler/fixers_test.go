package scheduler

import (
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/bos/internal/domain"
	"github.com/rm4n0s/bos/internal/events"
	"github.com/rm4n0s/bos/internal/workflow"
)

func TestOnlyTheTargetsRunACommand(t *testing.T) {
	r := newRig(t)
	gpu1srv, gpu1 := r.server("gpu1", 1)
	gpu2srv, gpu2 := r.server("gpu2", 1)
	w := r.world(false)
	cmd := r.command(w, "gpu1 -$ anime: chars")
	r.start()
	r.eventually("both online", func() bool { return r.sch.Available(gpu1srv.ID) && r.sch.Available(gpu2srv.ID) })
	r.run(cmd)
	r.waitStatus(cmd, domain.StatusFinished)
	if gpu1.submitted() != 3 || gpu2.submitted() != 0 {
		t.Errorf("gpu1 ran %d, gpu2 ran %d: gpu2 is not a target", gpu1.submitted(), gpu2.submitted())
	}
}

func TestAStaticPlanFallsBackOnlyToOtherTargets(t *testing.T) {
	r := newRig(t)
	r.server("gpu1", 1)
	_, gpu2 := r.server("gpu2", 1)
	_, gpu3 := r.server("gpu3", 1)
	gpu2.set(func(f *fakeComfy) { f.down = true })
	w := r.world(false)
	cmd := r.command(w, "gpu1, gpu2 -$ anime: chars spread even")
	r.start()
	r.run(cmd)
	r.eventually("gpu1 to do its jobs", func() bool { return r.statuses(cmd)[0] == domain.StatusFinished })
	r.clock.Advance(time.Minute)
	r.waitStatus(cmd, domain.StatusFinished)
	if gpu3.submitted() != 0 {
		t.Errorf("gpu3 is not a target and ran %d jobs", gpu3.submitted())
	}
}

func TestFixerStepsRunInOrderEachOnItsServer(t *testing.T) {
	r := newRig(t)
	_, gpu1 := r.server("gpu1", 1)
	_, gpu2 := r.server("gpu2", 1)
	w := r.world(false)
	cmd := r.command(w, "gpu1 -$ anime: chars(a) > upscale on gpu2 > upscale")
	r.start()
	r.run(cmd)
	r.waitStatus(cmd, domain.StatusFinished)

	j := r.jobs(cmd)[0]
	if !j.IsGenerated || !j.IsFixed || j.FinishedFixers != 2 || j.Status != domain.StatusFinished {
		t.Fatalf("job = generated %v fixed %v finished fixers %d status %s", j.IsGenerated, j.IsFixed, j.FinishedFixers, j.Status)
	}
	// Generation on gpu1, step 1 on gpu2 (explicit), step 2 where the image was
	// made, on gpu1.
	if gpu1.submitted() != 2 || gpu2.submitted() != 1 {
		t.Fatalf("gpu1 ran %d prompts, gpu2 ran %d", gpu1.submitted(), gpu2.submitted())
	}
	name := j.ResultPath[strings.LastIndex(j.ResultPath, "/")+1:]
	if got := string(gpu2.uploaded[name]); got != "image from gpu1 via 9" {
		t.Errorf("gpu2 was given %q, want the generated image", got)
	}
	if got := string(gpu1.uploaded[name]); got != "image from gpu2 via 9" {
		t.Errorf("gpu1 was given %q for step 2, want the result of step 1", got)
	}
	if got := r.read(j.ResultPath); got != "image from gpu1 via 9" {
		t.Errorf("the final result = %q", got)
	}
	// The fixer's workflow got the uploaded name in its image input.
	fixGraph := gpu2.graphs()[0]
	node, err := workflow.FindByTitle(fixGraph, "Load Image")
	check(t, err)
	if got := string(node.Inputs["image"]); got != `"gpu2/`+name+`"` {
		t.Errorf("image input = %s", got)
	}
	// One `processing` per stage, one `finished` for the job.
	if got := r.eventTypes(events.Processing, events.Finished); len(got) != 4 || got[3] != events.Finished || got[2] != events.Processing {
		t.Errorf("events = %v", got)
	}
	servers := []string{}
	all, _, _ := r.log.After(0)
	for _, e := range all {
		if e.Type == events.Processing {
			servers = append(servers, e.Server)
		}
	}
	if strings.Join(servers, ",") != "gpu1,gpu2,gpu1" {
		t.Errorf("stages ran on %v", servers)
	}
}

func TestAFixerStepWaitsForItsServer(t *testing.T) {
	r := newRig(t)
	r.server("gpu1", 1)
	_, gpu2 := r.server("gpu2", 1)
	gpu2.set(func(f *fakeComfy) { f.down = true })
	w := r.world(false)
	cmd := r.command(w, "gpu1 -$ anime: chars(a) > upscale on gpu2")
	r.start()
	r.run(cmd)
	r.eventually("the generation", func() bool { return r.jobs(cmd)[0].IsGenerated })
	r.clock.Advance(time.Hour) // an explicit server is never replaced
	time.Sleep(100 * time.Millisecond)
	if j := r.jobs(cmd)[0]; j.Status != domain.StatusQueued || j.IsFixed {
		t.Fatalf("job = %s fixed %v: the step must wait for gpu2", j.Status, j.IsFixed)
	}
	gpu2.set(func(f *fakeComfy) { f.down = false })
	r.waitStatus(cmd, domain.StatusFinished)
}

func TestAFixerStepMovesWhenTheGeneratingServerIsGone(t *testing.T) {
	r := newRig(t)
	gpu1srv, gpu1 := r.server("gpu1", 1)
	_, gpu2 := r.server("gpu2", 1)
	_, gpu3 := r.server("gpu3", 1) // up all the time, but not a target of the command
	w := r.world(false)
	cmd := r.command(w, "gpu1, gpu2 -$ anime: chars(a) spread blocks > upscale")
	gpu2.set(func(f *fakeComfy) { f.down = true })
	// One token: gpu1 finishes the generation and then stalls on the fixer step.
	gate := make(chan struct{}, 1)
	gate <- struct{}{}
	gpu1.set(func(f *fakeComfy) { f.gate = gate })
	r.start()
	r.eventually("gpu1 online", func() bool { return r.sch.Available(gpu1srv.ID) })
	r.run(cmd)
	r.eventually("the fixer step to start on gpu1", func() bool { return gpu1.submitted() == 2 })

	// gpu1 dies in the middle of the step; gpu2 comes up. The step does not move
	// at once: gpu1 is given the grace period to return.
	gpu1.set(func(f *fakeComfy) { f.down = true })
	gpu2.set(func(f *fakeComfy) { f.down = false })
	r.eventually("the step to go back to the queue", func() bool { return r.jobs(cmd)[0].Status == domain.StatusQueued && !r.sch.Available(gpu1srv.ID) })
	time.Sleep(60 * time.Millisecond)
	if got := r.jobs(cmd)[0].Status; got != domain.StatusQueued || gpu2.submitted() != 0 {
		t.Fatalf("the step moved at once: %s, gpu2 ran %d", got, gpu2.submitted())
	}
	r.clock.Advance(31 * time.Second)
	r.waitStatus(cmd, domain.StatusFinished)
	if gpu2.submitted() != 1 || gpu3.submitted() != 0 {
		t.Errorf("gpu2 ran %d prompts, gpu3 ran %d: the step goes to another target", gpu2.submitted(), gpu3.submitted())
	}
	j := r.jobs(cmd)[0]
	if got := r.read(j.ResultPath); got != "image from gpu2 via 9" {
		t.Errorf("result = %q", got)
	}
	if j.Attempts != 1 || j.ServerID == nil || *j.ServerID != gpu1srv.ID {
		t.Errorf("attempts %d, generated on %v", j.Attempts, j.ServerID)
	}
}
