package scheduler

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/bos/internal/comfy"
	"github.com/rm4n0s/bos/internal/domain"
	"github.com/rm4n0s/bos/internal/events"
	"github.com/rm4n0s/bos/internal/workflow"
	"github.com/rm4n0s/errors"
)

// Acceptance: a workflow failure halts the batch but not the jobs that already run.
func TestAWorkflowFailureHaltsTheBatch(t *testing.T) {
	r := newRig(t)
	_, gpu1 := r.server("gpu1", 1)
	w := r.world(false)
	cmd := r.command(w, "gpu1 -$ anime: chars repeat 2 with seed=1")
	jobs := r.jobs(cmd)
	failing := jobs[1].ID
	n := 0
	gpu1.set(func(f *fakeComfy) {
		f.waitErr = func(id string) error {
			n++
			if n == 2 {
				return errors.New("ComfyUIExecutionFailed", "the sampler ran out of memory", "traceback", "line 1\nline 2", "node", "3")
			}
			return nil
		}
	})
	r.start()
	r.run(cmd)
	r.waitStatus(cmd, domain.StatusError)

	got := r.statuses(cmd)
	want := []domain.Status{domain.StatusFinished, domain.StatusError, domain.StatusIdle, domain.StatusIdle, domain.StatusIdle, domain.StatusIdle}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("statuses = %v, want %v", got, want)
		}
	}
	j := r.job(failing)
	if !strings.HasPrefix(j.Error, "ComfyUIExecutionFailed: the sampler ran out of memory") {
		t.Errorf("error = %q", j.Error)
	}
	var trace struct {
		Tag      string         `json:"tag"`
		Metadata map[string]any `json:"metadata"`
	}
	if err := json.Unmarshal([]byte(j.ErrorTrace), &trace); err != nil || trace.Tag != "ComfyUIExecutionFailed" || trace.Metadata["traceback"] != "line 1\nline 2" {
		t.Errorf("trace = %q (%v)", j.ErrorTrace, err)
	}
	if gpu1.submitted() != 2 {
		t.Errorf("%d prompts were submitted: the batch must stop after the failure", gpu1.submitted())
	}
	if got := r.eventTypes(events.Error); len(got) != 1 {
		t.Errorf("error events = %v", got)
	}
	if c := r.command_(cmd.ID); c.Status != domain.StatusError {
		t.Errorf("command = %s", c.Status)
	}

	// Run again: the idle jobs queue and the batch continues; the failed job
	// stays failed until it is run or recreated.
	gpu1.set(func(f *fakeComfy) { f.waitErr = nil })
	r.run(cmd)
	r.waitStatus(cmd, domain.StatusError)
	r.eventually("the rest to finish", func() bool {
		finished := 0
		for _, s := range r.statuses(cmd) {
			if s == domain.StatusFinished {
				finished++
			}
		}
		return finished == 5
	})
}

func TestAFailureDoesNotInterruptJobsOnOtherServers(t *testing.T) {
	r := newRig(t)
	_, gpu1 := r.server("gpu1", 1)
	_, gpu2 := r.server("gpu2", 1)
	gate := make(chan struct{})
	gpu1.set(func(f *fakeComfy) { f.gate = gate })
	gpu2.set(func(f *fakeComfy) {
		f.waitErr = func(string) error { return errors.New("ComfyUIRejected", "no such checkpoint") }
	})
	w := r.world(false)
	cmd := r.command(w, "gpu1, gpu2 -$ anime: chars")
	r.start()
	r.run(cmd)
	r.eventually("gpu2's job to fail", func() bool {
		for _, s := range r.statuses(cmd) {
			if s == domain.StatusError {
				return true
			}
		}
		return false
	})
	// gpu1 is still running its job, and the third one went back to idle.
	count := map[domain.Status]int{}
	for _, st := range r.statuses(cmd) {
		count[st]++
	}
	if count[domain.StatusProcessing] != 1 || count[domain.StatusError] != 1 || count[domain.StatusIdle] != 1 {
		t.Fatalf("statuses = %v", r.statuses(cmd))
	}
	close(gate)
	r.eventually("gpu1's job to finish", func() bool {
		for _, j := range r.jobs(cmd) {
			if j.Status == domain.StatusFinished {
				return true
			}
		}
		return false
	})
	if c := r.command_(cmd.ID); c.Status != domain.StatusError {
		t.Errorf("command = %s", c.Status)
	}
}

func TestWorkflowProblemsAreRecordedOnTheJob(t *testing.T) {
	r := newRig(t)
	_, gpu1 := r.server("gpu1", 1)
	w := r.world(false)
	noSampler := []byte(`{"1":{"class_type":"CLIPTextEncode","inputs":{"text":""},"_meta":{"title":"Positive"}},` +
		`"2":{"class_type":"SaveImage","inputs":{"images":["1",0]},"_meta":{"title":"Save Image"}}}`)
	_, err := r.svc.Generators.Create(ctx(t), domain.Generator{Name: "bare", CodeName: "bare", Workflow: noSampler, PositivePromptTitle: "Positive"})
	check(t, err)
	missing := r.command(w, "gpu1 -$ anime: chars(a)")
	noSamplerCmd := r.command(w, "gpu1 -$ bare: chars(a) with steps=5")
	gone := r.command(w, "gpu1 -$ anime: chars(b)")
	check(t, r.svc.Generators.Delete(ctx(t), w.generator.ID))

	tests := []struct {
		cmd *domain.Command
		tag string
	}{
		{missing, "GeneratorNotFound"},
		{noSamplerCmd, "NoSamplerInWorkflow"},
		{gone, "GeneratorNotFound"},
	}
	r.start()
	for _, tt := range tests {
		r.run(tt.cmd)
		r.waitStatus(tt.cmd, domain.StatusError)
		j := r.jobs(tt.cmd)[0]
		if !strings.HasPrefix(j.Error, tt.tag+":") || j.ErrorTrace == "" {
			t.Errorf("%s: error = %q, trace %q", tt.cmd.Name, j.Error, j.ErrorTrace)
		}
	}
	if gpu1.submitted() != 0 {
		t.Errorf("%d prompts were submitted for jobs that cannot be built", gpu1.submitted())
	}
}

func TestInputFilesMissingFailTheJobWithTheirTag(t *testing.T) {
	r := newRig(t)
	r.server("gpu1", 1)
	w := r.world(false)
	_, err := r.svc.Items.Create(ctx(t), w.group.ID, domain.Item{
		CodeName: "withimage", PositivePrompt: "x",
		ControlNets: []domain.ControlNet{{Type: "canny", ImagePath: r.root + "/controlnet/missing.png", Strength: 1}},
	})
	check(t, err)
	cmd := r.command(w, "gpu1 -$ anime: chars(withimage)")
	r.start()
	r.run(cmd)
	r.waitStatus(cmd, domain.StatusError)
	if j := r.jobs(cmd)[0]; !strings.HasPrefix(j.Error, "FileNotFound:") {
		t.Errorf("error = %q", j.Error)
	}
}

func TestQueueAndWarningsAreRecorded(t *testing.T) {
	r := newRig(t)
	_, gpu1 := r.server("gpu1", 1)
	gpu1.set(func(f *fakeComfy) {
		f.warnings = []comfy.NodeWarning{{NodeID: "12", Message: "missing input"}}
	})
	w := r.world(false)
	cmd := r.command(w, "gpu1 -$ anime: chars(a)")
	r.start()
	r.run(cmd)
	r.waitStatus(cmd, domain.StatusFinished)
	if j := r.jobs(cmd)[0]; len(j.Warnings) != 1 || j.Warnings[0] != "node 12: missing input" {
		t.Errorf("warnings = %v", j.Warnings)
	}

	gpu1.set(func(f *fakeComfy) {
		f.warnings = nil
		f.queueErr = func(workflow.Graph) error {
			return errors.New("ComfyUIRejected", "node errors", "node_errors", map[string]any{"3": "bad"})
		}
	})
	again := r.command(w, "gpu1 -$ anime: chars(b)")
	r.run(again)
	r.waitStatus(again, domain.StatusError)
	if j := r.jobs(again)[0]; !strings.HasPrefix(j.Error, "ComfyUIRejected:") || j.PromptID != "" {
		t.Errorf("job = %q, prompt %q", j.Error, j.PromptID)
	}
}

func TestResultExtensionFollowsTheFileTheServerMade(t *testing.T) {
	r := newRig(t)
	_, gpu1 := r.server("gpu1", 1)
	gpu1.set(func(f *fakeComfy) { f.filename = "out.webp" })
	w := r.world(false)
	cmd := r.command(w, "gpu1 -$ anime: chars(a) > upscale")
	planned := r.jobs(cmd)[0].ResultPath
	r.start()
	r.run(cmd)
	r.waitStatus(cmd, domain.StatusFinished)
	j := r.jobs(cmd)[0]
	if !strings.HasSuffix(j.ResultPath, ".webp") || strings.TrimSuffix(j.ResultPath, ".webp") != strings.TrimSuffix(planned, ".png") {
		t.Errorf("result = %s, planned %s", j.ResultPath, planned)
	}
	if exists(planned) {
		t.Error("the file with the planned extension was left behind")
	}
	if got := r.read(j.ResultPath); !strings.HasPrefix(got, "image from gpu1") {
		t.Errorf("content = %q", got)
	}
}

// ---- losing a server ----

func TestAJobWhoseServerIsLostIsTriedAgain(t *testing.T) {
	r := newRig(t)
	gpu1srv, gpu1 := r.server("gpu1", 1)
	gate := make(chan struct{})
	gpu1.set(func(f *fakeComfy) { f.gate = gate })
	w := r.world(false)
	cmd := r.command(w, "gpu1 -$ anime: chars(a)")
	r.start()
	r.run(cmd)
	r.eventually("the job to run", func() bool { return gpu1.submitted() == 1 })

	gpu1.set(func(f *fakeComfy) { f.down = true })
	r.eventually("the job to go back to the queue", func() bool { return r.jobs(cmd)[0].Status == domain.StatusQueued })
	j := r.jobs(cmd)[0]
	if j.Attempts != 1 || j.Error != "" {
		t.Errorf("attempts = %d, error %q", j.Attempts, j.Error)
	}
	if r.sch.Available(gpu1srv.ID) {
		t.Error("the server is still available")
	}
	if len(gpu1.cancels) == 0 {
		t.Error("the prompt was not cancelled, even as a best effort")
	}
	if got := r.eventTypes(events.ServerOnline, events.ServerOffline); len(got) != 2 || got[1] != events.ServerOffline {
		t.Errorf("events = %v", got)
	}
	if gpu1.closed == 0 {
		t.Error("the client of the lost server was not released")
	}

	// The server returns: a new client is made and the job runs.
	gpu1.set(func(f *fakeComfy) { f.down, f.gate = false, nil })
	r.waitStatus(cmd, domain.StatusFinished)
	if r.jobs(cmd)[0].Attempts != 1 {
		t.Errorf("attempts = %d", r.jobs(cmd)[0].Attempts)
	}
	r.mu.Lock()
	opened := r.opened["gpu1"]
	r.mu.Unlock()
	if opened < 2 {
		t.Errorf("%d clients were made, want a new one after the loss", opened)
	}
}

func TestAnotherServerTakesAJobOfALostOne(t *testing.T) {
	r := newRig(t)
	_, gpu1 := r.server("gpu1", 1)
	_, gpu2 := r.server("gpu2", 1)
	gate := make(chan struct{})
	gpu1.set(func(f *fakeComfy) { f.gate = gate })
	gpu2.set(func(f *fakeComfy) { f.down = true })
	w := r.world(false)
	cmd := r.command(w, "gpu1, gpu2 -$ anime: chars(a)")
	r.start()
	r.run(cmd)
	r.eventually("gpu1 to run it", func() bool { return gpu1.submitted() == 1 })
	gpu1.set(func(f *fakeComfy) { f.down = true })
	gpu2.set(func(f *fakeComfy) { f.down = false })
	r.waitStatus(cmd, domain.StatusFinished)
	j := r.jobs(cmd)[0]
	if j.Attempts != 1 || !strings.HasSuffix(j.ServerHost, "gpu2:8188") {
		t.Errorf("attempts %d on %s", j.Attempts, j.ServerHost)
	}
}

func TestAfterTooManyAttemptsAJobFailsButTheCommandCarriesOn(t *testing.T) {
	r := newRig(t)
	_, gpu1 := r.server("gpu1", 1)
	_, gpu2 := r.server("gpu2", 1)
	w := r.world(false)
	// The job that drops the connection whatever server runs it.
	for _, f := range []*fakeComfy{gpu1, gpu2} {
		f.set(func(f *fakeComfy) {
			f.waitErr = func(id string) error {
				g := f.prompts[id]
				if g != nil && strings.Contains(promptText(t, g), "prompt a") {
					return errors.New("ServerUnreachable", "the connection dropped")
				}
				return nil
			}
		})
	}
	cmd := r.command(w, "gpu1, gpu2 -$ anime: chars")
	r.start()
	r.run(cmd)
	r.waitStatus(cmd, domain.StatusError)
	jobs := r.jobs(cmd)
	if jobs[0].Status != domain.StatusError || jobs[0].Attempts != 3 || !strings.Contains(jobs[0].Error, "became unreachable") || !strings.Contains(jobs[0].Error, "3 attempts") {
		t.Errorf("job a = %s after %d attempts: %q", jobs[0].Status, jobs[0].Attempts, jobs[0].Error)
	}
	// Server loss does not halt the batch.
	if jobs[1].Status != domain.StatusFinished || jobs[2].Status != domain.StatusFinished {
		t.Errorf("the other jobs = %s, %s", jobs[1].Status, jobs[2].Status)
	}
}

func TestAnInterruptionIsAFailureNotALoss(t *testing.T) {
	r := newRig(t)
	_, gpu1 := r.server("gpu1", 1)
	gpu1.set(func(f *fakeComfy) {
		f.waitErr = func(string) error { return errors.New("ComfyUIInterrupted", "someone pressed cancel") }
	})
	w := r.world(false)
	cmd := r.command(w, "gpu1 -$ anime: chars(a)")
	r.start()
	r.run(cmd)
	r.waitStatus(cmd, domain.StatusError)
	if j := r.jobs(cmd)[0]; j.Attempts != 0 || !strings.HasPrefix(j.Error, "ComfyUIInterrupted:") {
		t.Errorf("attempts %d, error %q", j.Attempts, j.Error)
	}
}

func TestAPromptThatVanishedIsTriedAgain(t *testing.T) {
	r := newRig(t)
	_, gpu1 := r.server("gpu1", 1)
	first := true
	gpu1.set(func(f *fakeComfy) {
		f.waitErr = func(string) error {
			if first {
				first = false
				return errors.New("ComfyUIPromptLost", "the server restarted and forgot the prompt")
			}
			return nil
		}
	})
	w := r.world(false)
	cmd := r.command(w, "gpu1 -$ anime: chars(a)")
	r.start()
	r.run(cmd)
	r.waitStatus(cmd, domain.StatusFinished)
	if j := r.jobs(cmd)[0]; j.Attempts != 1 {
		t.Errorf("attempts = %d", j.Attempts)
	}
}

// ---- the server monitor ----

func TestServerEventsAndAvailability(t *testing.T) {
	r := newRig(t)
	srv, gpu1 := r.server("gpu1", 1)
	if r.sch.Available(srv.ID) {
		t.Error("available before Run")
	}
	r.start()
	r.eventually("online", func() bool { return r.sch.Available(srv.ID) })
	if c, err := r.sch.Client(ctx(t), *srv); err != nil || c == nil {
		t.Errorf("Client = %v, %v", c, err)
	}
	gpu1.set(func(f *fakeComfy) { f.down = true })
	r.eventually("offline", func() bool { return !r.sch.Available(srv.ID) })
	_, err := r.sch.Client(ctx(t), *srv)
	wantTag(t, err, "ServerUnreachable")
	gpu1.set(func(f *fakeComfy) { f.down = false })
	r.eventually("online again", func() bool { return r.sch.Available(srv.ID) })

	want := []events.Type{events.ServerOnline, events.ServerOffline, events.ServerOnline}
	got := r.eventTypes()
	if len(got) != 3 || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Errorf("events = %v, want %v", got, want)
	}
	all, _, _ := r.log.After(0)
	if all[0].Server != "gpu1" {
		t.Errorf("event = %+v", all[0])
	}

	if r.sch.Available(999) {
		t.Error("an unknown server is available")
	}
	_, err = r.sch.Client(ctx(t), domain.Server{ID: 999, CodeName: "nope"})
	wantTag(t, err, "ServerUnreachable")
}

func TestAServerWithABadHostIsUnavailable(t *testing.T) {
	r := newRig(t)
	srv, err := r.svc.Servers.Create(ctx(t), domain.Server{Name: "x", Host: "http://nowhere:1", CodeName: "nowhere"})
	check(t, err)
	r.start()
	time.Sleep(60 * time.Millisecond) // several probes, each failing to make a client
	if r.sch.Available(srv.ID) {
		t.Error("a server without a client is available")
	}
}

func TestEditingTheHostMakesItANewServer(t *testing.T) {
	r := newRig(t)
	srv, gpu1 := r.server("gpu1", 1)
	other := newFakeComfy("moved")
	r.mu.Lock()
	r.fakes["http://moved:8188"] = other
	r.mu.Unlock()
	gate := make(chan struct{})
	gpu1.set(func(f *fakeComfy) { f.gate = gate })
	w := r.world(false)
	cmd := r.command(w, "gpu1 -$ anime: chars(a)")
	r.start()
	r.run(cmd)
	r.eventually("the job to run", func() bool { return gpu1.submitted() == 1 })

	_, err := r.svc.Servers.Update(ctx(t), srv.ID, domain.Server{Name: "gpu1", Host: "http://moved:8188", CodeName: "gpu1", MaxConcurrentJobs: 1})
	check(t, err)
	r.waitStatus(cmd, domain.StatusFinished)
	if other.submitted() != 1 {
		t.Errorf("the new address ran %d jobs", other.submitted())
	}
	if j := r.jobs(cmd)[0]; j.Attempts != 1 || j.ServerHost != "http://moved:8188" {
		t.Errorf("attempts %d on %s", j.Attempts, j.ServerHost)
	}
}

func wantTag(t *testing.T, err error, tag string) {
	t.Helper()
	appErr, ok := errors.FromError(err)
	if !ok || appErr.Tag != tag || !appErr.HasRoute("Scheduler.Client") {
		t.Errorf("error = %v, want %s raised by Scheduler.Client", err, tag)
	}
}
