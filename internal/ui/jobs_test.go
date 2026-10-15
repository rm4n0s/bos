package ui

import (
	"bufio"
	"encoding/json"
	"html/template"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/bos/internal/domain"
	"github.com/rm4n0s/bos/internal/events"
)

// jobWorld is the command world plus one command of three jobs.
func (u *testUI) jobWorld() (project, command domain.ID, jobs []domain.Job) {
	u.t.Helper()
	project = u.commandWorld()
	cmd, err := u.env.Services.Commands.Add(u.t.Context(), project, serviceCommand("gpu1 -$ anime: characters"))
	check(u.t, err)
	list, err := u.env.DB.Repos().Jobs.AllByCommand(u.t.Context(), cmd.ID)
	check(u.t, err)
	return project, cmd.ID, list
}

func (u *testUI) setJob(id domain.ID, change func(*domain.Job)) {
	u.t.Helper()
	j, err := u.env.DB.Repos().Jobs.Get(u.t.Context(), id)
	check(u.t, err)
	change(j)
	check(u.t, u.env.DB.Repos().Jobs.Update(u.t.Context(), j))
}

func TestJobGrid(t *testing.T) {
	u := newTestUI(t)
	_, cmd, jobs := u.jobWorld()
	seed := uint64(18446744073709551615)
	u.setJob(jobs[0].ID, func(j *domain.Job) {
		j.Status, j.ServerHost, j.Seed = domain.StatusFinished, "http://gpu1:8188", &seed
		j.ResultPath = u.env.Dirs.Results + "/1/alice.png"
	})
	u.setJob(jobs[1].ID, func(j *domain.Job) {
		j.Status, j.Error, j.ErrorTrace, j.Warnings = domain.StatusError, "the workflow is wrong", "goroutine trace here", []string{"node 5: missing"}
	})

	page := u.get("/commands/" + itoa(cmd) + "/jobs")
	if page.Status != 200 {
		t.Fatalf("status = %d", page.Status)
	}
	mustContain(t, "page", page.Body, "Demo", "gpu1 -$ anime: characters", `id="command-progress"`, "1/3", "1 error", "1 idle",
		`id="job-1"`, `id="job-2"`, `id="job-3"`, "18446744073709551615", "the workflow is wrong",
		`<img class="thumb" src="/files/results/1/alice.png"`, "/ui/stream/commands/"+itoa(cmd), "retry: 'always'")
	mustNotContain(t, "page", page.Body, `src="/files/results/1/`+"bob")
	// A picture opens in the viewer on the same page (the link still works in a new tab with a modifier key).
	mustContain(t, "viewer", page.Body, `<a href="/files/results/1/alice.png" data-viewer="Job 1"`, `/static/viewer.js`)
	mustNotContain(t, "viewer", page.Body, `data-viewer="Job 2"`)
	if r := u.get("/static/viewer.js"); r.Status != 200 {
		t.Errorf("viewer.js status = %d", r.Status)
	}
	if u.get("/commands/99/jobs").Status != 404 || u.get("/commands/x/jobs").Status != 404 {
		t.Error("a missing command must be a 404")
	}

	// Filters.
	r := u.datastar("GET", "/ui/commands/"+itoa(cmd)+"/jobs/table?status=error", struct{}{})
	mustContain(t, "error filter", r.patches(), `id="job-2"`)
	mustNotContain(t, "error filter", r.patches(), `id="job-1"`, `id="job-3"`)
	mustContain(t, "no match", u.datastar("GET", "/ui/commands/"+itoa(cmd)+"/jobs/table?status=processing", struct{}{}).patches(), "No job is processing.")
	mustContain(t, "bad status", u.datastar("GET", "/ui/commands/"+itoa(cmd)+"/jobs/table?status=nope", struct{}{}).patches(), `class="toast error"`)

	// The detail dialog: prompts, trace, warnings.
	r = u.datastar("GET", "/ui/jobs/2/open", struct{}{})
	mustContain(t, "detail", r.patches(), "selector #job-detail-body", "the workflow is wrong", "goroutine trace here", "node 5: missing",
		`<textarea name="positive_prompt" rows="4">b</textarea>`, "Changing a prompt starts the job over",
		`<li>created <time datetime="20`, `<li>updated <time datetime="20`)
	if r.signals()["jobs"].(map[string]any)["open"] != true {
		t.Errorf("signals = %v", r.signals())
	}
	mustContain(t, "result image", u.datastar("GET", "/ui/jobs/1/open", struct{}{}).patches(), `<img class="result" src="/files/results/1/alice.png"`, `data-viewer="Job 1"`)

	// Editing prompts resets the job; the row and the header are redrawn.
	r = u.submit("POST", "/ui/jobs/save", field("id", "2"), field("positive_prompt", "a better prompt"), field("negative_prompt", "ugly"))
	mustContain(t, "save", r.patches(), `id="job-2"`, "a better prompt", `badge idle`, `id="command-progress"`)
	mustNotContain(t, "save", r.patches(), "the workflow is wrong")
	if r.signals()["jobs"].(map[string]any)["open"] != false {
		t.Errorf("the dialog must close: %v", r.signals())
	}

	// A running job cannot be edited, and shows read-only.
	u.setJob(3, func(j *domain.Job) { j.Status = domain.StatusProcessing })
	mustContain(t, "running", u.datastar("GET", "/ui/jobs/3/open", struct{}{}).patches(), "readonly", "cannot be changed now")
	r = u.submit("POST", "/ui/jobs/save", field("id", "3"), field("positive_prompt", "x"))
	mustContain(t, "busy", r.patches(), "selector #jobs-error", "running")

	// Run and stop one job.
	r = u.datastar("POST", "/ui/jobs/2/run", struct{}{})
	mustContain(t, "run", r.patches(), `id="job-2"`, `badge queued`, "1 queued")
	r = u.datastar("POST", "/ui/jobs/2/stop", struct{}{})
	mustContain(t, "stop", r.patches(), `badge idle`)
	r = u.datastar("POST", "/ui/jobs/1/run", struct{}{})
	mustContain(t, "not runnable", r.patches(), `class="toast error"`, "only an idle job")
	mustContain(t, "unknown job", u.datastar("GET", "/ui/jobs/99/open", struct{}{}).patches(), `class="toast error"`, "not found")
}

// liveStream is an open event stream of the interface.
type liveStream struct {
	t    *testing.T
	body *bufio.Scanner
	stop func()
}

func (u *testUI) live(path string, headers map[string]string) *liveStream {
	u.t.Helper()
	req, err := http.NewRequestWithContext(u.t.Context(), "GET", u.srv.URL+path, nil)
	check(u.t, err)
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Datastar-Request", "true")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	res, err := u.client.Do(req) //nolint:bodyclose // closed by stop
	check(u.t, err)
	if res.StatusCode != 200 || !strings.HasPrefix(res.Header.Get("Content-Type"), "text/event-stream") {
		u.t.Fatalf("stream: status = %d, type = %q", res.StatusCode, res.Header.Get("Content-Type"))
	}
	sc := bufio.NewScanner(res.Body)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	return &liveStream{t: u.t, body: sc, stop: func() { _ = res.Body.Close() }}
}

// next returns the next block, up to a blank line, skipping keep-alives.
func (s *liveStream) next() string {
	s.t.Helper()
	var lines []string
	for s.body.Scan() {
		line := s.body.Text()
		if line == "" {
			if len(lines) == 0 {
				continue
			}
			block := strings.Join(lines, "\n")
			if block == ": keep-alive" {
				lines = nil
				continue
			}
			return block
		}
		lines = append(lines, line)
	}
	s.t.Fatalf("the stream ended: %v", s.body.Err())
	return ""
}

// until reads blocks until one contains want, and returns all it read.
func (s *liveStream) until(want string) string {
	s.t.Helper()
	var all []string
	done := make(chan struct{})
	timer := time.AfterFunc(5*time.Second, func() { s.stop(); close(done) })
	defer timer.Stop()
	for {
		block := s.next()
		all = append(all, block)
		if strings.Contains(block, want) {
			return strings.Join(all, "\n\n")
		}
		select {
		case <-done:
			s.t.Fatalf("never saw %q:\n%s", want, strings.Join(all, "\n\n"))
		default:
		}
	}
}

func fast(d *Deps) { d.Coalesce = 10 * time.Millisecond }

func TestCommandStreamUpdatesTheRowsAndTheHeader(t *testing.T) {
	u := newTestUI(t, fast)
	_, cmd, jobs := u.jobWorld()
	s := u.live("/ui/stream/commands/"+itoa(cmd), nil)
	defer s.stop()

	// An event of another command changes nothing for this page.
	u.env.Events.Append(events.Event{Type: events.Processing, JobID: 99, CommandID: 77, ProjectID: 1, Server: "gpu1"})
	u.setJob(jobs[0].ID, func(j *domain.Job) { j.Status, j.ServerHost = domain.StatusProcessing, "http://gpu1:8188" })
	u.env.Events.Append(events.Event{Type: events.Processing, JobID: jobs[0].ID, CommandID: cmd, ProjectID: 1, Server: "gpu1"})

	got := s.until(`id="command-progress"`)
	mustContain(t, "first update", got, "event: datastar-patch-elements", `id="job-1"`, `badge processing`, "http://gpu1:8188", "1 processing", "id: ")
	mustNotContain(t, "first update", got, `id="job-99"`)

	// A burst of finishes is drawn once per job, with the newest state.
	for _, j := range jobs {
		u.setJob(j.ID, func(j *domain.Job) { j.Status = domain.StatusFinished })
	}
	for range 5 {
		for _, j := range jobs {
			u.env.Events.Append(events.Event{Type: events.Finished, JobID: j.ID, CommandID: cmd, ProjectID: 1, Server: "gpu1"})
		}
	}
	got = s.until("3/3")
	for _, j := range jobs {
		if n := strings.Count(got, `id="job-`+itoa(j.ID)+`"`); n > 1 {
			t.Errorf("job %d was drawn %d times in one burst", j.ID, n)
		}
	}
}

func TestCommandStreamResumes(t *testing.T) {
	u := newTestUI(t, fast)
	_, cmd, jobs := u.jobWorld()
	for _, j := range jobs {
		u.env.Events.Append(events.Event{Type: events.Processing, JobID: j.ID, CommandID: cmd, ProjectID: 1, Server: "gpu1"})
	}
	// The browser saw event 1 and reconnects.
	s := u.live("/ui/stream/commands/"+itoa(cmd), map[string]string{"Last-Event-ID": "1"})
	defer s.stop()
	got := s.until(`id="command-progress"`)
	mustContain(t, "resumed", got, `id="job-2"`, `id="job-3"`, "id: 3")
	mustNotContain(t, "resumed", got, `id="job-1"`)
}

func TestCommandStreamAnnouncesAGap(t *testing.T) {
	u := newTestUI(t, fast)
	_, cmd, _ := u.jobWorld()
	s := u.live("/ui/stream/commands/"+itoa(cmd), map[string]string{"Last-Event-ID": "500"}) // after a restart
	defer s.stop()
	mustContain(t, "gap", s.until("missed"), "Reload the page")
}

func TestStreamsRefuseWhatDoesNotExist(t *testing.T) {
	u := newTestUI(t)
	if r := u.send("GET", "/ui/stream/commands/99", nil, map[string]string{"Accept": "text/event-stream"}); r.Status != 404 {
		t.Errorf("status = %d", r.Status)
	}
	if r := u.send("GET", "/ui/stream/commands/x", nil, nil); r.Status != 404 {
		t.Errorf("status = %d", r.Status)
	}
}

func TestToastStream(t *testing.T) {
	u := newTestUI(t, fast)
	_, cmd, jobs := u.jobWorld()
	srv, err := u.env.Services.Servers.List(t.Context(), domain.Page{})
	check(t, err)
	s := u.live("/ui/stream/toasts", nil)
	defer s.stop()

	// Jobs finishing are one toast, failures another, and the list's progress moves.
	u.setJob(jobs[0].ID, func(j *domain.Job) { j.Status = domain.StatusFinished })
	for _, j := range jobs[:2] {
		u.env.Events.Append(events.Event{Type: events.Finished, JobID: j.ID, CommandID: cmd, ProjectID: 1, Server: "gpu1"})
	}
	u.env.Events.Append(events.Event{Type: events.Error, JobID: jobs[2].ID, CommandID: cmd, ProjectID: 1, Server: "gpu1"})
	got := s.until(`id="cmd-download-` + itoa(cmd) + `"`)
	mustContain(t, "toasts", got, `class="toast finished"`, "2 jobs finished.", `class="toast error"`, "1 job failed: #3.", `cmd-progress-`+itoa(cmd), "1/3",
		`id="cmd-download-`+itoa(cmd)+`"`, `href="/commands/`+itoa(cmd)+`/download"`)

	// A server going away says so and redraws its badge.
	u.env.Fleet.SetDown(srv.Items[0].ID, true)
	u.env.Events.Append(events.Event{Type: events.ServerOffline, Server: "gpu1"})
	got = s.until(`id="server-status-`)
	mustContain(t, "server", got, "Server gpu1 is offline.", `class="badge offline"`)

	u.env.Events.Append(events.Event{Type: events.CommandFinished, CommandID: cmd, ProjectID: 1})
	mustContain(t, "command finished", s.until("finished."), "Command 1 finished.")
}

func TestStreamsEndWhenTheInterfaceStops(t *testing.T) {
	u := newTestUI(t, fast)
	s := u.live("/ui/stream/toasts", nil)
	defer s.stop()
	u.ui.Stop()
	done := make(chan struct{})
	go func() {
		for s.body.Scan() {
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the stream did not end after Stop")
	}
}

func TestPagesStartTheToastStream(t *testing.T) {
	u := newTestUI(t)
	mustContain(t, "page", u.get("/servers").Body, "@get('/ui/stream/toasts'", "openWhenHidden")
	au := newTestUI(t, func(d *Deps) { d.AuthToken = "x" })
	mustNotContain(t, "sign-in page", au.get("/login").Body, "/ui/stream/toasts")
}

// The viewer walks through every picture of the command, not the ones of the
// page that is on the screen.
func TestPictureListIsTheWholeCollection(t *testing.T) {
	u := newTestUI(t)
	project := u.commandWorld()
	cmd, err := u.env.Services.Commands.Add(t.Context(), project, serviceCommand("gpu1 -$ anime: characters repeat 10 with seed=5"))
	check(t, err)
	jobs, err := u.env.DB.Repos().Jobs.AllByCommand(t.Context(), cmd.ID)
	check(t, err)
	if len(jobs) <= domain.DefaultPageSize {
		t.Fatalf("%d jobs: the test needs more than a page", len(jobs))
	}
	var want []string
	for i, j := range jobs {
		if i%3 == 1 { // some jobs are not finished
			continue
		}
		name := "/files/results/1/job" + itoa(j.ID) + ".png"
		u.setJob(j.ID, func(j *domain.Job) {
			j.Status, j.ResultPath = domain.StatusFinished, u.env.Dirs.Results+"/1/job"+itoa(j.ID)+".png"
		})
		want = append(want, name)
	}
	listURL := "/ui/commands/" + itoa(cmd.ID) + "/jobs"

	// The page of the list holds only 20 jobs, and says where the whole list is.
	page := u.get("/commands/" + itoa(cmd.ID) + "/jobs")
	mustContain(t, "table", page.Body, `id="jobs-table" data-pictures="/ui/commands/`+itoa(cmd.ID)+`/jobs/pictures"`)
	if n := strings.Count(page.Body, "data-viewer="); n >= len(want) {
		t.Fatalf("the page has %d pictures: the test needs a collection larger than a page", n)
	}

	pictures := func(path string) []map[string]string {
		t.Helper()
		r := u.get(path)
		if r.Status != 200 || !strings.Contains(r.Header.Get("Content-Type"), "application/json") {
			t.Fatalf("%s: status %d, content type %q", path, r.Status, r.Header.Get("Content-Type"))
		}
		var got []map[string]string
		check(t, json.Unmarshal([]byte(r.Body), &got))
		return got
	}
	got := pictures(listURL + "/pictures")
	if len(got) != len(want) {
		t.Fatalf("%d pictures, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i]["url"] != want[i] || !strings.HasPrefix(got[i]["label"], "Job ") {
			t.Fatalf("picture %d = %v, want %s (job order)", i, got[i], want[i])
		}
	}

	// The filter the table shows applies to the list: finished is all of them,
	// the other statuses have no pictures.
	mustContain(t, "filtered table", u.datastar("GET", listURL+"/table?status=finished", struct{}{}).patches(),
		`data-pictures="/ui/commands/`+itoa(cmd.ID)+`/jobs/pictures?status=finished"`)
	if n := len(pictures(listURL + "/pictures?status=finished")); n != len(want) {
		t.Errorf("finished: %d pictures, want %d", n, len(want))
	}
	if n := len(pictures(listURL + "/pictures?status=error")); n != 0 {
		t.Errorf("error: %d pictures", n)
	}
	if r := u.get(listURL + "/pictures?status=nope"); r.Status != http.StatusBadRequest {
		t.Errorf("a bad status: status %d", r.Status)
	}
	if u.get("/ui/commands/999/jobs/pictures").Status != 404 || u.get("/ui/commands/x/jobs/pictures").Status != 404 {
		t.Error("a missing command must be a 404")
	}
}

func TestTimesAreLocalisedByTheBrowser(t *testing.T) {
	tests := []struct {
		name string
		in   time.Time
		want template.HTML
	}{
		{"zero", time.Time{}, ""},
		{"utc", time.Date(2026, 10, 15, 14, 32, 7, 0, time.UTC), `<time datetime="2026-10-15T14:32:07Z">2026-10-15 14:32 UTC</time>`},
		{"other zone", time.Date(2026, 10, 15, 14, 32, 0, 0, time.FixedZone("EEST", 3*3600)), `<time datetime="2026-10-15T11:32:00Z">2026-10-15 11:32 UTC</time>`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := formatTime(tc.in); got != tc.want {
				t.Errorf("formatTime = %q, want %q", got, tc.want)
			}
		})
	}

	u := newTestUI(t)
	mustContain(t, "layout", u.get("/projects").Body, `src="/static/localtime.js"`)
	mustContain(t, "script", u.get("/static/localtime.js").Body, "time[datetime]", "MutationObserver")
}
