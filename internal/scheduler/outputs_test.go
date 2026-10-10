package scheduler

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rm4n0s/bos/internal/domain"
)

// outputFiles lists the files left in the output folder of a fake installation.
func outputFiles(t *testing.T, root string) []string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(root, "output"))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

// A result is removed from ComfyUI once this program has its copy, for the
// generation and for every fixer step, but only when the setting is on and the
// server has a folder.
func TestResultsAreRemovedFromComfyUIAfterTheDownload(t *testing.T) {
	tests := []struct {
		name string
		on   bool
		path bool
		// left is how many files stay in the output folder: 3 jobs make a picture
		// each and a fixer step makes another.
		left int
	}{
		{name: "on, with a folder", on: true, path: true, left: 0},
		{name: "off", on: false, path: true, left: 6},
		{name: "on, but the server has no folder", on: true, path: false, left: -1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := newRig(t, func(d *Deps) { d.DeleteOutputs = tc.on })
			root := t.TempDir()
			folder := root
			if !tc.path {
				folder = ""
			}
			_, gpu1 := r.serverAt("gpu1", 1, folder)
			if !tc.path {
				// The fake still works on disk, so the test sees what is left.
				gpu1.set(func(f *fakeComfy) { f.comfyRoot = root })
			}
			w := r.world(false)
			cmd := r.command(w, "gpu1 -$ anime: chars > upscale")
			r.start()
			r.run(cmd)
			r.waitStatus(cmd, domain.StatusFinished)

			// Every result was fetched while it still existed (the fake fails
			// otherwise) and every job kept its copy.
			for _, j := range r.jobs(cmd) {
				if j.Status != domain.StatusFinished || !strings.HasPrefix(r.read(j.ResultPath), "image from gpu1") {
					t.Errorf("job %d: status %s, error %q, result %q", j.ID, j.Status, j.Error, j.ResultPath)
				}
			}
			left := outputFiles(t, root)
			want := tc.left
			if want < 0 {
				want = 6
			}
			if len(left) != want {
				t.Errorf("%d files left in the ComfyUI output folder %v, want %d", len(left), left, want)
			}
		})
	}
}

// A file that cannot be removed does not fail a job that already has its copy.
func TestAFailedRemovalIsOnlyAWarning(t *testing.T) {
	var logs syncBuffer
	logger := slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelInfo}))
	r := newRig(t, func(d *Deps) { d.DeleteOutputs = true; d.Logger = logger })
	_, gpu1 := r.serverAt("gpu1", 1, t.TempDir())
	gpu1.set(func(f *fakeComfy) { f.outputIsDir = true })
	w := r.world(false)
	cmd := r.command(w, "gpu1 -$ anime: chars limit 1")
	r.start()
	r.run(cmd)
	r.waitStatus(cmd, domain.StatusFinished)
	if got := logs.String(); !strings.Contains(got, "could not remove a result from the ComfyUI output folder") || !strings.Contains(got, "FileStoreFailure") {
		t.Errorf("no warning in the log:\n%s", got)
	}
}
