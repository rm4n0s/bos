package scheduler

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rm4n0s/bos/internal/domain"
	"github.com/rm4n0s/errors"
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
// generation and for every fixer step, but only when the setting is on.
func TestResultsAreRemovedFromComfyUIAfterTheDownload(t *testing.T) {
	tests := []struct {
		name string
		on   bool
		// left is how many files stay in the output folder: 3 jobs make a picture
		// each and a fixer step makes another.
		left int
	}{
		{name: "on", on: true, left: 0},
		{name: "off", on: false, left: 6},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := newRig(t, func(d *Deps) { d.DeleteOutputs = tc.on })
			root := t.TempDir()
			_, gpu1 := r.serverAt("gpu1", 1, root)
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
			if left := outputFiles(t, root); len(left) != tc.left {
				t.Errorf("%d files left in the ComfyUI output folder %v, want %d", len(left), left, tc.left)
			}
			gpu1.set(func(f *fakeComfy) {
				if tc.on && len(f.deleted) != 6 || !tc.on && len(f.deleted) != 0 {
					t.Errorf("the server was asked to delete %v", f.deleted)
				}
			})
		})
	}
}

// A file that cannot be removed does not fail a job that already has its copy.
func TestAFailedRemovalIsOnlyAWarning(t *testing.T) {
	var logs syncBuffer
	logger := slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelInfo}))
	r := newRig(t, func(d *Deps) { d.DeleteOutputs = true; d.Logger = logger })
	_, gpu1 := r.serverAt("gpu1", 1, t.TempDir())
	gpu1.set(func(f *fakeComfy) { f.deleteErr = errors.New("ModelCrudDisabled", "deletes are switched off") })
	w := r.world(false)
	cmd := r.command(w, "gpu1 -$ anime: chars limit 1")
	r.start()
	r.run(cmd)
	r.waitStatus(cmd, domain.StatusFinished)
	if got := logs.String(); !strings.Contains(got, "could not remove a result from the ComfyUI output folder") || !strings.Contains(got, "ModelCrudDisabled") {
		t.Errorf("no warning in the log:\n%s", got)
	}
}
