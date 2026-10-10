package service

import (
	"strconv"
	"strings"
	"testing"

	"github.com/rm4n0s/bos/internal/domain"
)

func (e *env) setJob(t *testing.T, id domain.ID, change func(j *domain.Job)) {
	t.Helper()
	j, err := e.repos.Jobs().Get(ctx(t), id)
	check(t, err)
	change(j)
	check(t, e.repos.Jobs().Update(ctx(t), j))
}

func TestRunQueuesTheIdleJobs(t *testing.T) {
	e := newEnv(t)
	w := e.world(t)
	cmd := e.add(t, w, "gpu1 -$ anime: characters")
	jobs := e.jobsOf(t, cmd.ID)
	e.setJob(t, jobs[0].ID, func(j *domain.Job) { j.Status = domain.StatusFinished })

	ran, err := e.Commands.Run(ctx(t), cmd.ID)
	check(t, err)
	if ran.Status != domain.StatusQueued || e.wakes.Load() != 1 {
		t.Errorf("command = %s, wakes = %d", ran.Status, e.wakes.Load())
	}
	got := e.jobsOf(t, cmd.ID)
	if got[0].Status != domain.StatusFinished || got[1].Status != domain.StatusQueued || got[2].Status != domain.StatusQueued {
		t.Errorf("job statuses = %s %s %s: only the idle jobs queue", got[0].Status, got[1].Status, got[2].Status)
	}
	if got[1].QueueSeq == 0 || got[1].QueueSeq != got[2].QueueSeq {
		t.Errorf("queue numbers = %d, %d: a command's jobs share one", got[1].QueueSeq, got[2].QueueSeq)
	}

	// Run again while it is waiting: refused.
	_, err = e.Commands.Run(ctx(t), cmd.ID)
	wantRoute(t, err, "CommandService.Run", "CommandNotRunnable")
	if e.wakes.Load() != 1 {
		t.Error("a refused run woke the scheduler")
	}
}

func TestRunRefusals(t *testing.T) {
	e := newEnv(t)
	w := e.world(t)
	_, err := e.Commands.Run(ctx(t), 999)
	wantRoute(t, err, "CommandService.Run", "CommandNotFound")

	cmd := e.add(t, w, "gpu1 -$ anime: characters(a)")
	e.setJob(t, e.jobsOf(t, cmd.ID)[0].ID, func(j *domain.Job) { j.Status = domain.StatusFinished })
	_, err = e.Commands.Run(ctx(t), cmd.ID)
	wantRoute(t, err, "CommandService.Run", "CommandNotRunnable")

	empty := e.commandRaw(t, w.project.ID, "empty", "x")
	_, err = e.Commands.Run(ctx(t), empty.ID)
	wantRoute(t, err, "CommandService.Run", "CommandNotRunnable")

	running := e.add(t, w, "gpu1 -$ anime: characters(b)")
	check(t, e.repos.Commands().SetStatus(ctx(t), running.ID, domain.StatusProcessing))
	_, err = e.Commands.Run(ctx(t), running.ID)
	wantRoute(t, err, "CommandService.Run", "CommandNotRunnable")
}

func TestStopReturnsQueuedJobsToIdle(t *testing.T) {
	e := newEnv(t)
	w := e.world(t)
	cmd := e.add(t, w, "gpu1 -$ anime: characters")
	_, err := e.Commands.Run(ctx(t), cmd.ID)
	check(t, err)
	jobs := e.jobsOf(t, cmd.ID)
	e.setJob(t, jobs[0].ID, func(j *domain.Job) { j.Status = domain.StatusProcessing })
	e.setJob(t, jobs[1].ID, func(j *domain.Job) { j.IsGenerated, j.PromptID = true, "p" })

	n, err := e.Commands.Stop(ctx(t), cmd.ID)
	check(t, err)
	got := e.jobsOf(t, cmd.ID)
	if n != 2 || got[0].Status != domain.StatusProcessing || got[1].Status != domain.StatusIdle || got[2].Status != domain.StatusIdle {
		t.Errorf("stopped %d: %s %s %s", n, got[0].Status, got[1].Status, got[2].Status)
	}
	if got[1].IsGenerated || got[1].PromptID != "" {
		t.Errorf("a stopped job keeps its progress: %+v", got[1])
	}
	// A job is still running, so the command is too.
	if c, _ := e.Commands.Get(ctx(t), cmd.ID); c.Status != domain.StatusProcessing {
		t.Errorf("command = %s", c.Status)
	}
	e.setJob(t, jobs[0].ID, func(j *domain.Job) { j.Status = domain.StatusFinished })
	n, err = e.Commands.Stop(ctx(t), cmd.ID)
	check(t, err)
	if c, _ := e.Commands.Get(ctx(t), cmd.ID); n != 0 || c.Status != domain.StatusIdle {
		t.Errorf("stopped %d, command %s", n, c.Status)
	}
	_, err = e.Commands.Stop(ctx(t), 999)
	wantRoute(t, err, "CommandService.Stop", "CommandNotFound")
}

func TestJobsOfACommand(t *testing.T) {
	e := newEnv(t)
	w := e.world(t)
	cmd := e.add(t, w, "gpu1 -$ anime: characters")
	jobs := e.jobsOf(t, cmd.ID)
	e.setJob(t, jobs[1].ID, func(j *domain.Job) { j.Status, j.Error = domain.StatusError, "boom" })

	page, err := e.Jobs.ListByCommand(ctx(t), cmd.ID, "", domain.Page{Size: 2})
	check(t, err)
	if page.Total != 3 || len(page.Items) != 2 || page.Items[0].ID != jobs[0].ID {
		t.Errorf("page = %+v", page)
	}
	page, err = e.Jobs.ListByCommand(ctx(t), cmd.ID, domain.StatusError, domain.Page{})
	check(t, err)
	if page.Total != 1 || page.Items[0].Error != "boom" {
		t.Errorf("errors = %+v", page)
	}
	got, err := e.Jobs.Get(ctx(t), jobs[1].ID)
	check(t, err)
	if got.Error != "boom" {
		t.Errorf("job = %+v", got)
	}

	_, err = e.Jobs.ListByCommand(ctx(t), 999, "", domain.Page{})
	wantRoute(t, err, "JobService.ListByCommand", "CommandNotFound")
	_, err = e.Jobs.ListByCommand(ctx(t), cmd.ID, "sleeping", domain.Page{})
	wantRoute(t, err, "JobService.ListByCommand", "InvalidRequest")
	_, err = e.Jobs.Get(ctx(t), 999)
	wantRoute(t, err, "JobService.Get", "JobNotFound")
}

func TestRunAndStopOneJob(t *testing.T) {
	e := newEnv(t)
	w := e.world(t)
	cmd := e.add(t, w, "gpu1 -$ anime: characters")
	jobs := e.jobsOf(t, cmd.ID)

	run, err := e.Jobs.Run(ctx(t), jobs[1].ID)
	check(t, err)
	if run.Status != domain.StatusQueued || e.wakes.Load() != 1 {
		t.Errorf("job = %s, wakes %d", run.Status, e.wakes.Load())
	}
	if c, _ := e.Commands.Get(ctx(t), cmd.ID); c.Status != domain.StatusQueued {
		t.Errorf("command = %s", c.Status)
	}
	_, err = e.Jobs.Run(ctx(t), jobs[1].ID)
	wantRoute(t, err, "JobService.Run", "JobNotRunnable")
	_, err = e.Jobs.Run(ctx(t), 999)
	wantRoute(t, err, "JobService.Run", "JobNotFound")

	stopped, err := e.Jobs.Stop(ctx(t), jobs[1].ID)
	check(t, err)
	if stopped.Status != domain.StatusIdle {
		t.Errorf("job = %s", stopped.Status)
	}
	if c, _ := e.Commands.Get(ctx(t), cmd.ID); c.Status != domain.StatusIdle {
		t.Errorf("command = %s", c.Status)
	}
	_, err = e.Jobs.Stop(ctx(t), jobs[1].ID)
	wantRoute(t, err, "JobService.Stop", "JobNotRunnable")
	_, err = e.Jobs.Stop(ctx(t), 999)
	wantRoute(t, err, "JobService.Stop", "JobNotFound")
}

func TestEditingPromptsStartsTheJobOver(t *testing.T) {
	e := newEnv(t)
	w := e.world(t)
	cmd := e.add(t, w, "gpu1 -$ anime: characters(a) > upscale")
	job := e.jobsOf(t, cmd.ID)[0]
	result := e.write(t, strings.TrimPrefix(job.ResultPath, e.root+"/"), "image")
	e.setJob(t, job.ID, func(j *domain.Job) {
		j.Status, j.IsGenerated, j.IsFixed, j.FinishedFixers, j.PromptID = domain.StatusFinished, true, true, 1, "p1"
	})
	check(t, e.repos.Commands().SetStatus(ctx(t), cmd.ID, domain.StatusFinished))

	// The same prompts change nothing.
	same, err := e.Jobs.EditPrompts(ctx(t), job.ID, job.PositivePrompt, job.NegativePrompt)
	check(t, err)
	if same.Status != domain.StatusFinished || !exists(result) {
		t.Errorf("an edit without a change reset the job: %s, result exists %v", same.Status, exists(result))
	}

	edited, err := e.Jobs.EditPrompts(ctx(t), job.ID, "new positive", "new negative")
	check(t, err)
	if edited.PositivePrompt != "new positive" || edited.NegativePrompt != "new negative" || edited.Status != domain.StatusIdle ||
		edited.IsGenerated || edited.IsFixed || edited.FinishedFixers != 0 || edited.PromptID != "" {
		t.Errorf("job = %+v", edited)
	}
	if exists(result) {
		t.Error("the old result was kept")
	}
	if c, _ := e.Commands.Get(ctx(t), cmd.ID); c.Status != domain.StatusIdle {
		t.Errorf("command = %s", c.Status)
	}

	e.setJob(t, job.ID, func(j *domain.Job) { j.Status = domain.StatusProcessing })
	_, err = e.Jobs.EditPrompts(ctx(t), job.ID, "again", "")
	wantRoute(t, err, "JobService.EditPrompts", "JobBusy")
	_, err = e.Jobs.EditPrompts(ctx(t), 999, "x", "y")
	wantRoute(t, err, "JobService.EditPrompts", "JobNotFound")
}

func TestWithoutADispatcherNothingBreaks(t *testing.T) {
	e := newEnv(t, func(d *Deps) { d.Dispatcher = nil })
	w := e.world(t)
	cmd := e.add(t, w, "gpu1 -$ anime: characters")
	_, err := e.Commands.Run(ctx(t), cmd.ID)
	check(t, err)
}

func TestSummaryAndCounts(t *testing.T) {
	e := newEnv(t)
	w := e.world(t)
	cmd := e.add(t, w, "gpu1 -$ anime: characters")
	jobs := e.jobsOf(t, cmd.ID)
	e.setJob(t, jobs[0].ID, func(j *domain.Job) { j.Status = domain.StatusFinished })
	e.setJob(t, jobs[1].ID, func(j *domain.Job) { j.Status = domain.StatusError })

	sum, err := e.Commands.Summary(ctx(t), cmd.ID)
	check(t, err)
	if sum.TotalJobs != 3 || sum.FinishedJobs != 1 || sum.ID != cmd.ID {
		t.Errorf("summary = %+v", sum)
	}
	counts, err := e.Jobs.Counts(ctx(t), cmd.ID)
	check(t, err)
	if counts[domain.StatusFinished] != 1 || counts[domain.StatusError] != 1 || counts[domain.StatusIdle] != 1 {
		t.Errorf("counts = %v", counts)
	}

	_, err = e.Commands.Summary(ctx(t), 999)
	wantRoute(t, err, "CommandService.Summary", "CommandNotFound")
	_, err = e.Jobs.Counts(ctx(t), 999)
	wantRoute(t, err, "JobService.Counts", "CommandNotFound")
}

// The results of a command are all of them, past the 200 of a page, in job
// order, and only the finished jobs.
func TestResultsOfACommandAreTheWholeCollection(t *testing.T) {
	e := newEnv(t)
	w := e.world(t)
	cmd := e.add(t, w, "gpu1 -$ anime: characters repeat 70 with seed=5") // 3 items x 70 = 210 jobs, two pages
	jobs := e.jobsOf(t, cmd.ID)
	if len(jobs) <= domain.MaxPageSize {
		t.Fatalf("%d jobs: the test needs more than a page", len(jobs))
	}
	var want []domain.ID
	for i, j := range jobs {
		if i%2 == 0 { // the others stay idle, with only the planned path of a result
			e.setJob(t, j.ID, func(j *domain.Job) {
				j.Status, j.ResultPath = domain.StatusFinished, "r/"+strconv.FormatInt(j.ID, 10)+".png"
			})
			want = append(want, j.ID)
		}
	}
	// The last job is on the second page.
	last := jobs[len(jobs)-1]
	e.setJob(t, last.ID, func(j *domain.Job) { j.Status, j.ResultPath = domain.StatusFinished, "r/last.png" })
	want = append(want, last.ID)

	got, err := e.Jobs.Results(ctx(t), cmd.ID)
	check(t, err)
	if len(got) != len(want) {
		t.Fatalf("%d results, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i].ID != want[i] {
			t.Fatalf("result %d is job %d, want %d (job order)", i, got[i].ID, want[i])
		}
	}

	none, err := e.Jobs.Results(ctx(t), e.add(t, w, "gpu1 -$ anime: characters").ID)
	check(t, err)
	if len(none) != 0 {
		t.Errorf("a command with nothing finished has %d results", len(none))
	}
	_, err = e.Jobs.Results(ctx(t), 999)
	wantRoute(t, err, "JobService.Results->JobService.ListByCommand", "CommandNotFound")
}
