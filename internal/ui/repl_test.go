package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rm4n0s/bos/internal/domain"
	"github.com/rm4n0s/bos/internal/events"
)

func TestReplPage(t *testing.T) {
	u := newTestUI(t)
	page := u.get("/repl").Body
	mustContain(t, "empty", page, "Add a <a href=\"/servers\">server</a>", "Nothing run yet.", `name="items"`)

	u.commandWorld()
	page = u.get("/repl").Body
	mustContain(t, "with a world", page, `<option value="gpu1">GPU (gpu1)</option>`, `<option value="anime">Anime (anime)</option>`)
	mustNotContain(t, "with a world", page, "Add a <a href=")

	u.env.Fleet.SetDown(1, true)
	mustContain(t, "offline", u.get("/repl").Body, "— offline")
}

func TestReplRun(t *testing.T) {
	u := newTestUI(t)
	u.commandWorld()

	r := u.submit("POST", "/ui/repl/run", field("server", "gpu1"), field("generator", "anime"), field("items", "characters(a),poses(p1)"),
		field("positive", "at night"), field("negative", "blurry"), field("loras", "fox.safetensors, 0.5"))
	mustContain(t, "run", r.patches(), `id="repl-result"`, `badge queued`, "a p1 at night")
	job, err := u.env.Services.Repl.Last(t.Context())
	check(t, err)
	if job.NegativePrompt != "blurry" || len(job.LoRAs) != 1 || job.LoRAs[0].StrengthModel != 0.5 || job.LoRAs[0].StrengthClip != 1 || !job.Pinned {
		t.Errorf("job = %+v", job)
	}
	if u.env.Wakes.Count() != 1 {
		t.Errorf("wakes = %d", u.env.Wakes.Count())
	}

	// Failures stay in the form.
	for name, parts := range map[string][]part{
		"bad items":         {field("server", "gpu1"), field("generator", "anime"), field("items", "characters(a) poses(p1)")},
		"unknown server":    {field("server", "nope"), field("generator", "anime")},
		"unknown item":      {field("server", "gpu1"), field("generator", "anime"), field("items", "characters(zzz)")},
		"bad lora":          {field("server", "gpu1"), field("generator", "anime"), field("loras", "x.safetensors, loud")},
		"lora without name": {field("server", "gpu1"), field("generator", "anime"), field("loras", " , 1, 1")},
	} {
		r := u.submit("POST", "/ui/repl/run", parts...)
		if !strings.Contains(r.patches(), "selector #repl-error") {
			t.Errorf("%s: no error in the form:\n%s", name, r.patches())
		}
	}

	// While it runs, another run is refused.
	u.setJob(job.ID, func(j *domain.Job) { j.Status = domain.StatusProcessing })
	r = u.submit("POST", "/ui/repl/run", field("server", "gpu1"), field("generator", "anime"))
	mustContain(t, "busy", r.patches(), "selector #repl-error", "running")
}

func TestReplResultReplacesItself(t *testing.T) {
	u := newTestUI(t, fast)
	u.commandWorld()
	_, err := u.env.Services.Repl.Run(t.Context(), serviceRepl())
	check(t, err)
	job, _ := u.env.Services.Repl.Last(t.Context())

	s := u.live("/ui/stream/toasts", nil)
	defer s.stop()

	// The job ends: the page that shows the REPL gets the picture, once.
	check(t, os.MkdirAll(filepath.Dir(job.ResultPath), 0o750))
	check(t, os.WriteFile(job.ResultPath, png1(), 0o600))
	u.setJob(job.ID, func(j *domain.Job) { j.Status = domain.StatusFinished })
	u.env.Events.Append(events.Event{Type: events.Finished, JobID: job.ID, CommandID: domain.ReplID, ProjectID: domain.ReplID, Server: "gpu1"})
	got := s.until(`id="repl-result"`)
	mustContain(t, "result", got, `<img class="result" src="/files/results/repl/repl.png?v=`, "badge finished")

	// A failure shows its message and trace.
	u.setJob(job.ID, func(j *domain.Job) {
		j.Status, j.Error, j.ErrorTrace = domain.StatusError, "no GPU memory", "trace text"
	})
	u.env.Events.Append(events.Event{Type: events.Error, JobID: job.ID, CommandID: domain.ReplID, ProjectID: domain.ReplID, Server: "gpu1"})
	got = s.until("no GPU memory")
	mustContain(t, "error", got, "trace text", "badge error")
	mustNotContain(t, "error", got, "<img")

	// The page, opened later, shows the same.
	mustContain(t, "page", u.get("/repl").Body, "no GPU memory")
}
