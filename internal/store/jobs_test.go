package store

import (
	"reflect"
	"sort"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/rm4n0s/bos/internal/domain"
	"github.com/rm4n0s/errors"
)

func fullJob(command domain.ID) *domain.Job {
	server, preferred := int64(3), int64(4)
	seed := uint64(18446744073709551615)
	return &domain.Job{
		ProjectID: 7, CommandID: command,
		Items:       []domain.UsedItem{{GroupID: 1, GroupCode: "characters", ItemID: 2, ItemCode: "alice"}},
		CommandText: "gpu1 -$ g: characters", Status: domain.StatusQueued, QueueSeq: 5,
		ServerID: &server, ServerHost: "http://gpu1:8188", PreferredServerID: &preferred, Pinned: true,
		Fixers:         []domain.FixerStep{{FixerID: 1, FixerCode: "upscale", ServerID: 2, ServerCode: "gpu2"}, {FixerID: 3, FixerCode: "face"}},
		FinishedFixers: 1, RepeatIndex: 2, RegionIndex: 1, Seed: &seed, Attempts: 2,
		GeneratorCodeName: "g", OutputType: domain.OutputImage,
		OutputAttributes: domain.OutputAttributes{"width": {Kind: domain.AttrInt, Int: 512}, "cfg": {Kind: domain.AttrDecimal, Float: 6.5}},
		PromptID:         "abc-123", PositivePrompt: "alice", NegativePrompt: "ugly",
		ControlNets:   []domain.ControlNet{{Type: "canny", ImagePath: "cn.png", IsReference: true, Strength: 0.9}},
		RegionPrompts: []domain.RegionPrompt{{Keyword: "red", MaskFile: "m.png", Prompt: "bob", LoRAs: []domain.LoRA{{Name: "l"}}}, {Keyword: "left", Coordinates: &domain.CoordinatedRegion{Keyword: "left", Width: 1, Height: 2, X: 3, Y: 4}, Prompt: "carol"}},
		IPAdapters:    []domain.IPAdapter{{ImageFile: "ip.png", Weight: 0.5}},
		LoRAs:         []domain.LoRA{{Name: "a.safetensors", StrengthModel: 0.8, StrengthClip: 0.6}},
		ResultPath:    "results/1.png", Error: "boom", ErrorTrace: "{}", Warnings: []string{"node 5: bad input"},
		IsGenerated: true, IsFixed: false,
	}
}

func TestJobRoundTrip(t *testing.T) {
	r := newDB(t).Repos()
	j := fullJob(1)
	check(t, r.Jobs.CreateMany(ctx(t), []*domain.Job{j}))
	if j.ID == 0 || j.CreatedAt.IsZero() {
		t.Fatalf("id and timestamps not set: %+v", j)
	}
	got, err := r.Jobs.Get(ctx(t), j.ID)
	check(t, err)
	if !reflect.DeepEqual(got, j) {
		t.Errorf("Get = %+v\nwant  %+v", got, j)
	}

	// Optional fields stay nil and lists stay empty, never null.
	bare := newJob(1, "bare")
	check(t, r.Jobs.CreateMany(ctx(t), []*domain.Job{bare}))
	got, err = r.Jobs.Get(ctx(t), bare.ID)
	check(t, err)
	if got.ServerID != nil || got.PreferredServerID != nil || got.Seed != nil || got.RegionIndex != -1 {
		t.Errorf("bare job = %+v", got)
	}

	j.Status, j.Error, j.Seed, j.ServerID = domain.StatusError, "changed", nil, nil
	j.Warnings = nil
	check(t, r.Jobs.Update(ctx(t), j))
	got, _ = r.Jobs.Get(ctx(t), j.ID)
	if got.Status != domain.StatusError || got.Error != "changed" || got.Seed != nil || got.ServerID != nil || !got.UpdatedAt.After(got.CreatedAt) {
		t.Errorf("after update: %+v", got)
	}
	wantRoute(t, r.Jobs.Update(ctx(t), &domain.Job{ID: 999}), "JobRepo.Update", "JobNotFound")
	_, err = r.Jobs.Get(ctx(t), 999)
	wantRoute(t, err, "JobRepo.Get", "JobNotFound")
}

// Job order is id order, also across the insert batches.
func TestCreateManyKeepsOrderAndSetsIDs(t *testing.T) {
	r := newDB(t).Repos()
	jobs := mustJobs(t, r, 1, 60) // 25 + 25 + 10
	for i, j := range jobs {
		if i > 0 && j.ID != jobs[i-1].ID+1 {
			t.Fatalf("job %d has id %d after %d", i, j.ID, jobs[i-1].ID)
		}
	}
	all, err := r.Jobs.AllByCommand(ctx(t), 1)
	check(t, err)
	if len(all) != 60 {
		t.Fatalf("%d jobs stored", len(all))
	}
	for i, j := range all {
		if j.ID != jobs[i].ID || j.PositivePrompt != jobs[i].PositivePrompt {
			t.Fatalf("job %d: stored id %d prompt %q; created id %d prompt %q", i, j.ID, j.PositivePrompt, jobs[i].ID, jobs[i].PositivePrompt)
		}
	}
	check(t, r.Jobs.CreateMany(ctx(t), nil))

	page, err := r.Jobs.ListByCommand(ctx(t), 1, "", domain.Page{Number: 3, Size: 25})
	check(t, err)
	if page.Total != 60 || len(page.Items) != 10 || page.TotalPages != 3 {
		t.Errorf("page = %d items of %d", len(page.Items), page.Total)
	}
	jobs[0].Status = domain.StatusError
	check(t, r.Jobs.Update(ctx(t), jobs[0]))
	page, err = r.Jobs.ListByCommand(ctx(t), 1, domain.StatusError, domain.Page{})
	check(t, err)
	if page.Total != 1 || page.Items[0].ID != jobs[0].ID {
		t.Errorf("filtered page = %+v", page)
	}
	counts, err := r.Jobs.StatusCounts(ctx(t), 1)
	check(t, err)
	if counts[domain.StatusError] != 1 || counts[domain.StatusIdle] != 59 {
		t.Errorf("status counts = %v", counts)
	}
	total, finished, err := r.Jobs.Counts(ctx(t), 1)
	check(t, err)
	if total != 60 || finished != 0 {
		t.Errorf("counts = %d, %d", total, finished)
	}
	if total, finished, _ = r.Jobs.Counts(ctx(t), 42); total != 0 || finished != 0 {
		t.Errorf("counts of an empty command = %d, %d", total, finished)
	}
}

func TestCreateManyIsAtomic(t *testing.T) {
	r := newDB(t).Repos()
	good := newJob(1, "good")
	bad := newJob(1, "bad")
	// A NaN cannot be written as JSON, so the second job fails after the first
	// has been prepared; neither may be stored.
	bad.OutputAttributes = domain.OutputAttributes{"x": {Kind: domain.AttrDecimal, Float: nan()}}
	err := r.Jobs.CreateMany(ctx(t), []*domain.Job{good, bad})
	wantRoute(t, err, "JobRepo.CreateMany", "StoreFailure")
	all, _ := r.Jobs.AllByCommand(ctx(t), 1)
	if len(all) != 0 {
		t.Errorf("%d jobs stored from a failed batch", len(all))
	}
}

func nan() float64 { var z float64; return z / z }

// Queueing numbers the run so that the command started first is served first
// (SPEC §8.1, §8.7), and a job keeps its place when it goes back to the queue.
func TestQueueOrderAndCursor(t *testing.T) {
	r := newDB(t).Repos()
	first := mustJobs(t, r, 1, 3)
	second := mustJobs(t, r, 2, 2)

	n, err := r.Jobs.QueueCommand(ctx(t), 2) // run the later command first
	check(t, err)
	if n != 2 {
		t.Errorf("queued %d", n)
	}
	n, err = r.Jobs.QueueCommand(ctx(t), 1)
	check(t, err)
	if n != 3 {
		t.Errorf("queued %d", n)
	}
	if n, _ = r.Jobs.QueueCommand(ctx(t), 1); n != 0 {
		t.Errorf("queued %d jobs that were already queued", n)
	}

	queue, err := r.Jobs.ListQueued(ctx(t), QueueCursor{}, 100)
	check(t, err)
	var got []int64
	for _, j := range queue {
		got = append(got, j.ID)
	}
	want := []int64{second[0].ID, second[1].ID, first[0].ID, first[1].ID, first[2].ID}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("queue = %v, want %v", got, want)
	}

	// Walking the queue with a cursor and a small page sees every job once.
	var walked []int64
	cur := QueueCursor{}
	for {
		page, err := r.Jobs.ListQueued(ctx(t), cur, 2)
		check(t, err)
		if len(page) == 0 {
			break
		}
		for _, j := range page {
			walked = append(walked, j.ID)
		}
		last := page[len(page)-1]
		cur = QueueCursor{Seq: last.QueueSeq, ID: last.ID}
	}
	if !reflect.DeepEqual(walked, want) {
		t.Errorf("walked = %v, want %v", walked, want)
	}

	// A single job queued later goes to the back.
	extra := mustJobs(t, r, 3, 1)[0]
	ok, err := r.Jobs.QueueJob(ctx(t), extra.ID)
	check(t, err)
	if !ok {
		t.Error("QueueJob refused an idle job")
	}
	if ok, _ = r.Jobs.QueueJob(ctx(t), extra.ID); ok {
		t.Error("QueueJob accepted a queued job")
	}
	queue, _ = r.Jobs.ListQueued(ctx(t), QueueCursor{}, 100)
	if queue[len(queue)-1].ID != extra.ID {
		t.Errorf("the job queued last is not at the back: %v", queue)
	}
	byStatus, err := r.Jobs.ListByStatus(ctx(t), domain.StatusQueued)
	check(t, err)
	if len(byStatus) != 6 {
		t.Errorf("%d queued jobs", len(byStatus))
	}
}

// Acceptance tests 8 and 9: stopping and halting return queued jobs to idle and
// reset their progress; running jobs are not touched.
func TestStopAndHalt(t *testing.T) {
	r := newDB(t).Repos()
	jobs := mustJobs(t, r, 1, 4)
	_, err := r.Jobs.QueueCommand(ctx(t), 1)
	check(t, err)
	// jobs[0] runs; jobs[1] is queued for its second fixer step; the rest wait.
	ok, err := r.Jobs.Claim(ctx(t), jobs[0].ID)
	check(t, err)
	if !ok {
		t.Fatal("claim failed")
	}
	jobs[1].IsGenerated, jobs[1].FinishedFixers, jobs[1].PromptID, jobs[1].Status = true, 1, "p1", domain.StatusQueued
	check(t, r.Jobs.Update(ctx(t), jobs[1]))

	n, err := r.Jobs.StopQueued(ctx(t), 1, jobs[3].ID) // a failed job halts the others, not itself
	check(t, err)
	if n != 2 {
		t.Errorf("stopped %d jobs, want 2", n)
	}
	for id, wantStatus := range map[int64]domain.Status{
		jobs[0].ID: domain.StatusProcessing, jobs[1].ID: domain.StatusIdle, jobs[2].ID: domain.StatusIdle, jobs[3].ID: domain.StatusQueued,
	} {
		got, _ := r.Jobs.Get(ctx(t), id)
		if got.Status != wantStatus {
			t.Errorf("job %d is %s, want %s", id, got.Status, wantStatus)
		}
	}
	got, _ := r.Jobs.Get(ctx(t), jobs[1].ID)
	if got.IsGenerated || got.FinishedFixers != 0 || got.PromptID != "" {
		t.Errorf("progress not reset: %+v", got)
	}

	ok, err = r.Jobs.StopJob(ctx(t), jobs[3].ID)
	check(t, err)
	if !ok {
		t.Error("StopJob refused a queued job")
	}
	if ok, _ = r.Jobs.StopJob(ctx(t), jobs[0].ID); ok {
		t.Error("StopJob stopped a processing job")
	}
	if ok, _ = r.Jobs.StopJob(ctx(t), jobs[3].ID); ok {
		t.Error("StopJob stopped an idle job")
	}
	if n, _ = r.Jobs.StopQueued(ctx(t), 1, 0); n != 0 {
		t.Errorf("stopped %d with nothing queued", n)
	}
	ok, err = r.Jobs.Transition(ctx(t), jobs[0].ID, domain.StatusProcessing, domain.StatusFinished)
	check(t, err)
	if !ok {
		t.Error("transition failed")
	}
	if ok, _ = r.Jobs.Transition(ctx(t), jobs[0].ID, domain.StatusProcessing, domain.StatusError); ok {
		t.Error("transition from the wrong status succeeded")
	}
	if _, finished, _ := r.Jobs.Counts(ctx(t), 1); finished != 1 {
		t.Errorf("finished = %d", finished)
	}
}

func TestDeleteJobsAndRepl(t *testing.T) {
	db := newDB(t)
	r := db.Repos()
	mustJobs(t, r, 1, 2)
	mustJobs(t, r, 2, 1)
	files, err := r.Jobs.DeleteByCommand(ctx(t), 1)
	check(t, err)
	if !reflect.DeepEqual(files, []string{"results/jobaa.png", "results/jobba.png"}) {
		t.Errorf("files = %v", files)
	}
	if all, _ := r.Jobs.AllByCommand(ctx(t), 1); len(all) != 0 {
		t.Error("jobs remain")
	}
	if all, _ := r.Jobs.AllByCommand(ctx(t), 2); len(all) != 1 {
		t.Error("another command's job was deleted")
	}

	// There is never more than one REPL job (SPEC §8.6).
	_, err = r.Jobs.GetRepl(ctx(t))
	wantRoute(t, err, "JobRepo.GetRepl", "JobNotFound")
	first := newJob(0, "first")
	prev, err := r.Jobs.ReplaceRepl(ctx(t), first)
	check(t, err)
	if len(prev) != 0 || first.ProjectID != domain.ReplID || first.CommandID != domain.ReplID {
		t.Errorf("first repl: files %v, job %+v", prev, first)
	}
	second := newJob(0, "second")
	prev, err = r.Jobs.ReplaceRepl(ctx(t), second)
	check(t, err)
	if !reflect.DeepEqual(prev, []string{"results/first.png"}) {
		t.Errorf("previous files = %v", prev)
	}
	got, err := r.Jobs.GetRepl(ctx(t))
	check(t, err)
	if got.ID != second.ID || got.PositivePrompt != "second" {
		t.Errorf("repl = %+v", got)
	}
	var n int64
	check(t, db.gorm.Model(&jobRecord{}).Where("project_id = ?", domain.ReplID).Count(&n).Error)
	if n != 1 {
		t.Errorf("%d REPL jobs", n)
	}
}

// Acceptance test 33: a job is claimed by exactly one worker.
func TestClaimHasOneWinner(t *testing.T) {
	for name, open := range map[string]func(*testing.T) *DB{"memory": newDB, "file": newFileDB} {
		t.Run(name, func(t *testing.T) {
			db := open(t)
			r := db.Repos()
			j := mustJobs(t, r, 1, 1)[0]
			_, err := r.Jobs.QueueCommand(ctx(t), 1)
			check(t, err)

			const workers = 16
			var wins atomic.Int32
			var wg sync.WaitGroup
			start := make(chan struct{})
			for range workers {
				wg.Add(1)
				go func() {
					defer wg.Done()
					<-start
					ok, err := db.Repos().Jobs.Claim(ctx(t), j.ID)
					if err != nil {
						t.Error(err)
						return
					}
					if ok {
						wins.Add(1)
					}
				}()
			}
			close(start)
			wg.Wait()
			if wins.Load() != 1 {
				t.Errorf("%d workers won the claim, want 1", wins.Load())
			}
		})
	}
}

// Many workers pulling from one queue never run a job twice and never miss one.
func TestConcurrentWorkersDrainTheQueue(t *testing.T) {
	for name, open := range map[string]func(*testing.T) *DB{"memory": newDB, "file": newFileDB} {
		t.Run(name, func(t *testing.T) {
			db := open(t)
			r := db.Repos()
			const total = 120
			jobs := mustJobs(t, r, 1, total)
			_, err := r.Jobs.QueueCommand(ctx(t), 1)
			check(t, err)

			var mu sync.Mutex
			claimedBy := map[int64][]int{}
			var wg sync.WaitGroup
			for w := range 8 {
				wg.Add(1)
				go func() {
					defer wg.Done()
					repos := db.Repos()
					for {
						queue, err := repos.Jobs.ListQueued(ctx(t), QueueCursor{}, 5)
						if err != nil {
							t.Error(err)
							return
						}
						if len(queue) == 0 {
							return
						}
						for _, j := range queue {
							ok, err := repos.Jobs.Claim(ctx(t), j.ID)
							if err != nil {
								t.Error(err)
								return
							}
							if ok {
								mu.Lock()
								claimedBy[j.ID] = append(claimedBy[j.ID], w)
								mu.Unlock()
								break // pick the next job from the front again
							}
						}
					}
				}()
			}
			wg.Wait()

			ids := make([]int64, 0, len(claimedBy))
			for id, ws := range claimedBy {
				ids = append(ids, id)
				if len(ws) != 1 {
					t.Errorf("job %d was claimed %d times", id, len(ws))
				}
			}
			sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
			if len(ids) != total || ids[0] != jobs[0].ID || ids[total-1] != jobs[total-1].ID {
				t.Errorf("%d of %d jobs were claimed", len(ids), total)
			}
			if counts, _ := r.Jobs.StatusCounts(ctx(t), 1); counts[domain.StatusProcessing] != total {
				t.Errorf("status counts = %v", counts)
			}
		})
	}
}

func TestInTx(t *testing.T) {
	db := newFileDB(t)
	r := db.Repos()

	// Commit: several repositories change together.
	err := db.InTx(ctx(t), func(tx *Repos) error {
		p := &domain.Project{Name: "tx"}
		if err := tx.Projects.Create(ctx(t), p); err != nil {
			return err
		}
		c := &domain.Command{ProjectID: p.ID, CommandCode: "x"}
		if err := tx.Commands.Create(ctx(t), c, 0); err != nil {
			return err
		}
		return tx.Jobs.CreateMany(ctx(t), []*domain.Job{newJob(c.ID, "a"), newJob(c.ID, "b")})
	})
	check(t, err)
	if page, _ := r.Projects.List(ctx(t), domain.Page{}); page.Total != 1 {
		t.Errorf("committed %d projects", page.Total)
	}

	// An error rolls everything back and comes back untouched.
	sentinel := errors.New("Sentinel", "stop here")
	err = db.InTx(ctx(t), func(tx *Repos) error {
		if err := tx.Projects.Create(ctx(t), &domain.Project{Name: "gone"}); err != nil {
			return err
		}
		return sentinel
	})
	if got, ok := errors.FromError(err); !ok || got != sentinel {
		t.Errorf("InTx returned %v, want the callback's error unchanged", err)
	}
	if page, _ := r.Projects.List(ctx(t), domain.Page{}); page.Total != 1 {
		t.Errorf("%d projects after a rollback", page.Total)
	}

	// A panic rolls back too, and is re-raised.
	func() {
		defer func() {
			if recover() == nil {
				t.Error("the panic was swallowed")
			}
		}()
		_ = db.InTx(ctx(t), func(tx *Repos) error {
			check(t, tx.Projects.Create(ctx(t), &domain.Project{Name: "panic"}))
			panic("boom")
		})
	}()
	if page, _ := r.Projects.List(ctx(t), domain.Page{}); page.Total != 1 {
		t.Errorf("%d projects after a panic", page.Total)
	}

	// Within a transaction, repository methods that open their own use it.
	err = db.InTx(ctx(t), func(tx *Repos) error {
		g := &domain.Group{Name: "g", CodeName: "g", Tags: []string{"t"}}
		if err := tx.Groups.Create(ctx(t), g); err != nil {
			return err
		}
		if _, err := tx.Groups.Delete(ctx(t), g.ID); err != nil {
			return err
		}
		_, err := tx.Groups.Get(ctx(t), g.ID)
		return err
	})
	wantRoute(t, err, "TestInTx", "GroupNotFound")
}

// Every repository reports a failed database as StoreFailure.
func TestStoreFailureWhenDatabaseIsClosed(t *testing.T) {
	db, err := Open(ctx(t), InMemory, WithClock(fakeClock()))
	check(t, err)
	check(t, db.Migrate(ctx(t)))
	r := db.Repos()
	check(t, db.Close())

	calls := map[string]func() error{
		"ServerRepo.Create":   func() error { return r.Servers.Create(ctx(t), newServer("x")) },
		"ServerRepo.All":      func() error { _, err := r.Servers.All(ctx(t)); return err },
		"ServerRepo.List":     func() error { _, err := r.Servers.List(ctx(t), domain.Page{}); return err },
		"GeneratorRepo.Get":   func() error { _, err := r.Generators.Get(ctx(t), 1); return err },
		"GroupRepo.Create":    func() error { return r.Groups.Create(ctx(t), &domain.Group{CodeName: "g"}) },
		"ItemRepo.AllByGroup": func() error { _, err := r.Items.AllByGroup(ctx(t), 1); return err },
		"TagRepo.List":        func() error { _, err := r.Tags.List(ctx(t), domain.TagScopeItem, ""); return err },
		"ProjectRepo.Delete":  func() error { _, err := r.Projects.Delete(ctx(t), 1); return err },
		"CommandRepo.Move":    func() error { _, err := r.Commands.Move(ctx(t), 1, 1); return err },
		"CommandRepo.ListByStatus": func() error {
			_, err := r.Commands.ListByStatus(ctx(t), domain.StatusQueued)
			return err
		},
		"JobRepo.Claim":      func() error { _, err := r.Jobs.Claim(ctx(t), 1); return err },
		"JobRepo.ListQueued": func() error { _, err := r.Jobs.ListQueued(ctx(t), QueueCursor{}, 1); return err },
		"JobRepo.ListQueuedFor": func() error {
			_, err := r.Jobs.ListQueuedFor(ctx(t), QueueCursor{}, 1, QueueFilter{Server: 1, Healthy: []int64{1}})
			return err
		},
		"JobRepo.QueueCommand": func() error { _, err := r.Jobs.QueueCommand(ctx(t), 1); return err },
		"JobRepo.ReplaceRepl":  func() error { _, err := r.Jobs.ReplaceRepl(ctx(t), newJob(0, "x")); return err },
	}
	for entry, call := range calls {
		t.Run(entry, func(t *testing.T) { wantRoute(t, call(), entry, "StoreFailure") })
	}
	wantRoute(t, db.InTx(ctx(t), func(*Repos) error { return nil }), "DB.InTx", "StoreFailure")
	wantRoute(t, db.Migrate(ctx(t)), "DB.Migrate", "MigrationFailed")
	_, err = db.SchemaVersion(ctx(t))
	wantRoute(t, err, "DB.SchemaVersion", "StoreFailure")
}

func TestListQueuedForFiltersByServer(t *testing.T) {
	r := newDB(t).Repos()
	const gpu1, gpu2, gpu3 = 1, 2, 3
	mk := func(name string, preferred *int64, pinned, generated bool) *domain.Job {
		j := newJob(1, name)
		j.PreferredServerID, j.Pinned, j.IsGenerated = preferred, pinned, generated
		return j
	}
	one, two, three := int64(gpu1), int64(gpu2), int64(gpu3)
	jobs := []*domain.Job{
		mk("dynamic", nil, false, false),
		mk("gpu1", &one, false, false),
		mk("gpu2", &two, false, false),
		mk("gpu2-pinned", &two, true, false),
		mk("gpu3", &three, false, false),
		mk("gpu3-generated", &three, true, true),
	}
	check(t, r.Jobs.CreateMany(ctx(t), jobs))
	_, err := r.Jobs.QueueCommand(ctx(t), 1)
	check(t, err)

	names := func(f QueueFilter) []string {
		got, err := r.Jobs.ListQueuedFor(ctx(t), QueueCursor{}, 100, f)
		check(t, err)
		var out []string
		for _, j := range got {
			out = append(out, j.PositivePrompt)
		}
		return out
	}
	tests := []struct {
		name string
		f    QueueFilter
		want []string
	}{
		{"gpu1, everyone healthy", QueueFilter{Server: gpu1, Healthy: []int64{gpu1, gpu2, gpu3}}, []string{"dynamic", "gpu1", "gpu3-generated"}},
		{"gpu1, no list given", QueueFilter{Server: gpu1}, []string{"dynamic", "gpu1", "gpu3-generated"}},
		{"gpu2, everyone healthy", QueueFilter{Server: gpu2, Healthy: []int64{gpu1, gpu2, gpu3}}, []string{"dynamic", "gpu2", "gpu2-pinned", "gpu3-generated"}},
		{"gpu1, an empty list: no server is healthy", QueueFilter{Server: gpu1, Healthy: []int64{}}, []string{"dynamic", "gpu1", "gpu2", "gpu3", "gpu3-generated"}},
		{"gpu1 when gpu2 is not healthy", QueueFilter{Server: gpu1, Healthy: []int64{gpu1, gpu3}}, []string{"dynamic", "gpu1", "gpu2", "gpu3-generated"}},
		{"gpu1 when gpu2 and gpu3 are not healthy", QueueFilter{Server: gpu1, Healthy: []int64{gpu1}}, []string{"dynamic", "gpu1", "gpu2", "gpu3", "gpu3-generated"}},
		{"a server that nobody prefers, when the others are not healthy", QueueFilter{Server: 99, Healthy: []int64{99}}, []string{"dynamic", "gpu1", "gpu2", "gpu3", "gpu3-generated"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := names(tt.f); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("jobs = %v, want %v", got, tt.want)
			}
		})
	}

	// Jobs of commands the server is not a target of pass only once generated.
	other := newJob(2, "other-command")
	check(t, r.Jobs.CreateMany(ctx(t), []*domain.Job{other}))
	_, err = r.Jobs.QueueCommand(ctx(t), 2)
	check(t, err)
	if got := names(QueueFilter{Server: gpu1, Healthy: []int64{gpu1, gpu2, gpu3}}); !contains(got, "other-command") {
		t.Errorf("without a command list every command counts: %v", got)
	}
	if got := names(QueueFilter{Server: gpu1, Commands: []int64{1}, Healthy: []int64{gpu1, gpu2, gpu3}}); !reflect.DeepEqual(got, []string{"dynamic", "gpu1", "gpu3-generated"}) {
		t.Errorf("targets of command 1 only: %v", got)
	}
	if got := names(QueueFilter{Server: gpu1, Commands: []int64{}, Healthy: []int64{gpu1}}); !reflect.DeepEqual(got, []string{"gpu3-generated"}) {
		t.Errorf("target of no command: %v", got)
	}

	// The cursor and the limit work as in ListQueued, and only queued jobs count.
	first, err := r.Jobs.ListQueuedFor(ctx(t), QueueCursor{}, 1, QueueFilter{Server: gpu1})
	check(t, err)
	next, err := r.Jobs.ListQueuedFor(ctx(t), QueueCursor{Seq: first[0].QueueSeq, ID: first[0].ID}, 1, QueueFilter{Server: gpu1})
	check(t, err)
	if len(first) != 1 || len(next) != 1 || next[0].ID <= first[0].ID {
		t.Errorf("paging: %v then %v", first, next)
	}
	claimed, err := r.Jobs.Claim(ctx(t), first[0].ID)
	check(t, err)
	if !claimed {
		t.Fatal("claim failed")
	}
	if got := names(QueueFilter{Server: gpu1}); reflect.DeepEqual(got, []string{"dynamic", "gpu1", "gpu3-generated"}) {
		t.Errorf("a claimed job is still listed: %v", got)
	}
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
