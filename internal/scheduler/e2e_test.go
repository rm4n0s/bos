package scheduler

import (
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/bos/internal/comfy"
	"github.com/rm4n0s/bos/internal/comfy/comfytest"
	"github.com/rm4n0s/bos/internal/domain"
	"github.com/rm4n0s/bos/internal/events"
	"github.com/rm4n0s/bos/internal/service"
)

// The services, the scheduler and the real adapter over HTTP, against a stand-in
// for ComfyUI: the same wiring as the application, a command from its text to
// its result files.
func TestEndToEndWithTheRealAdapter(t *testing.T) {
	fake := comfytest.New(20 * time.Millisecond)
	t.Cleanup(fake.Close)
	fake.SetLoRAs("detail.safetensors")

	r := newRig(t, func(d *Deps) {
		d.NewClient = func(srv domain.Server) (comfy.Client, error) {
			c, err := comfy.NewServer(srv.Host, comfy.Options{PollInterval: 10 * time.Millisecond, CallTimeout: 2 * time.Second})
			if err != nil {
				return nil, err
			}
			return c, nil
		}
	})
	srv, err := r.svc.Servers.Create(ctx(t), domain.Server{Name: "fake", Host: fake.URL, CodeName: "fake", MaxConcurrentJobs: 2})
	check(t, err)
	w := r.world(false)
	cmd := r.command(w, "fake -$ anime: chars > upscale")

	r.start()
	r.eventually("the server to be online", func() bool { return r.sch.Available(srv.ID) })
	// The preview sees the server and its LoRAs through the scheduler.
	pv, err := service.New(service.Deps{Store: service.NewStore(r.db), Files: r.files, Fleet: r.sch}).Commands.Preview(ctx(t), "fake -$ anime: chars")
	check(t, err)
	if !pv.Valid || pv.Jobs != 3 || len(pv.Warnings) != 0 {
		t.Errorf("preview = %+v", pv)
	}
	loras, err := service.New(service.Deps{Store: service.NewStore(r.db), Files: r.files, Fleet: r.sch}).Servers.LoRAs(ctx(t), srv.ID)
	check(t, err)
	if len(loras) != 1 || loras[0] != "detail.safetensors" {
		t.Errorf("LoRAs = %v", loras)
	}

	r.run(cmd)
	r.waitStatus(cmd, domain.StatusFinished)

	for i, j := range r.jobs(cmd) {
		if j.Status != domain.StatusFinished || !j.IsFixed || j.FinishedFixers != 1 || j.Error != "" {
			t.Errorf("job %d = %s fixed %v fixers %d error %q", i, j.Status, j.IsFixed, j.FinishedFixers, j.Error)
		}
		// The last stage's file is the result: the fixer's output.
		if got := r.read(j.ResultPath); !strings.HasPrefix(got, "png:fake-") {
			t.Errorf("job %d result = %q", i, got)
		}
	}
	// The fixer steps were given the generated images, which the adapter names
	// by their content (SPEC §18.6).
	uploads := fake.Uploads()
	if len(uploads) != 3 {
		t.Errorf("%d files uploaded, want one image for each fixer step", len(uploads))
	}
	for name, content := range uploads {
		if len(name) != 64+len(".png") && len(name) != 64 || !strings.HasPrefix(string(content), "png:fake-") {
			t.Errorf("upload %q = %q", name, content)
		}
	}
	if n := len(fake.Prompts()); n != 6 {
		t.Errorf("%d prompts, want 3 generations and 3 fixer steps", n)
	}
	if got := r.eventTypes(events.CommandFinished); len(got) != 1 {
		t.Errorf("events = %v", r.eventTypes())
	}
}

func TestEndToEndServerLoss(t *testing.T) {
	fake := comfytest.New(400 * time.Millisecond)
	t.Cleanup(fake.Close)
	r := newRig(t, func(d *Deps) {
		d.NewClient = func(srv domain.Server) (comfy.Client, error) {
			c, err := comfy.NewServer(srv.Host, comfy.Options{PollInterval: 10 * time.Millisecond, CallTimeout: time.Second, PollFailures: 2})
			if err != nil {
				return nil, err
			}
			return c, nil
		}
	})
	srv, err := r.svc.Servers.Create(ctx(t), domain.Server{Name: "fake", Host: fake.URL, CodeName: "fake", MaxConcurrentJobs: 1})
	check(t, err)
	w := r.world(false)
	cmd := r.command(w, "fake -$ anime: chars(a)")
	r.start()
	r.eventually("online", func() bool { return r.sch.Available(srv.ID) })
	r.run(cmd)
	r.eventually("the prompt", func() bool { return len(fake.Prompts()) == 1 })

	fake.SetDown(true) // the server goes away mid-run
	r.eventually("the job to be queued again", func() bool {
		j := r.jobs(cmd)[0]
		return j.Status == domain.StatusQueued && j.Attempts == 1
	})
	fake.SetDown(false)
	r.waitStatus(cmd, domain.StatusFinished)
	if n := len(fake.Prompts()); n != 2 {
		t.Errorf("%d prompts, want the original and the retry", n)
	}
}
