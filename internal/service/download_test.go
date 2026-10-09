package service

import (
	"archive/zip"
	"encoding/json"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rm4n0s/bos/internal/domain"
)

func readZip(t *testing.T, path string) map[string]string {
	t.Helper()
	zr, err := zip.OpenReader(path)
	check(t, err)
	defer func() { check(t, zr.Close()) }()
	out := map[string]string{}
	for _, f := range zr.File {
		rc, err := f.Open()
		check(t, err)
		b, err := io.ReadAll(rc)
		check(t, err)
		check(t, rc.Close())
		out[f.Name] = string(b)
	}
	return out
}

func TestDownloadArchivesTheResults(t *testing.T) {
	e := newEnv(t)
	w := e.world(t)
	cmd := e.add(t, w, "gpu1 -$ anime: characters")
	jobs := e.jobsOf(t, cmd.ID)
	for _, j := range jobs {
		e.write(t, strings.TrimPrefix(j.ResultPath, e.root+"/"), "image of "+j.PositivePrompt)
		e.setJob(t, j.ID, func(j *domain.Job) {
			j.Status = domain.StatusFinished
			j.LoRAs = []domain.LoRA{{Name: "l.safetensors", StrengthModel: 1, StrengthClip: 1}}
		})
	}
	check(t, e.repos.Commands().SetStatus(ctx(t), cmd.ID, domain.StatusFinished))

	path, err := e.Commands.Download(ctx(t), cmd.ID)
	check(t, err)
	if path != filepath.Join(e.dirs.Downloads, "command-1.zip") {
		t.Errorf("path = %s", path)
	}
	files := readZip(t, path)
	if len(files) != 4 {
		t.Fatalf("archive holds %d files: %v", len(files), files)
	}
	for _, j := range jobs {
		if got := files[filepath.Base(j.ResultPath)]; got != "image of "+j.PositivePrompt {
			t.Errorf("%s = %q", filepath.Base(j.ResultPath), got)
		}
	}
	var summary []summaryEntry
	check(t, json.Unmarshal([]byte(files[summaryName]), &summary))
	if len(summary) != 3 || summary[0].PositivePrompt != "alice" || len(summary[0].LoRAs) != 1 || summary[0].File != filepath.Base(jobs[0].ResultPath) {
		t.Errorf("summary = %+v", summary)
	}
	if got, _ := e.Commands.Get(ctx(t), cmd.ID); got.DownloadFile != path {
		t.Errorf("the command remembers %q", got.DownloadFile)
	}

	// Again: the same archive, rebuilt.
	again, err := e.Commands.Download(ctx(t), cmd.ID)
	check(t, err)
	if again != path {
		t.Errorf("second path = %s", again)
	}
}

func TestDownloadRefusals(t *testing.T) {
	e := newEnv(t)
	w := e.world(t)
	_, err := e.Commands.Download(ctx(t), 999)
	wantRoute(t, err, "CommandService.Download", "CommandNotFound")

	cmd := e.add(t, w, "gpu1 -$ anime: characters")
	_, err = e.Commands.Download(ctx(t), cmd.ID)
	wantRoute(t, err, "CommandService.Download", "CommandNotFinished")

	// Finished, but a result file is gone.
	for _, j := range e.jobsOf(t, cmd.ID) {
		e.setJob(t, j.ID, func(j *domain.Job) { j.Status = domain.StatusFinished })
	}
	check(t, e.repos.Commands().SetStatus(ctx(t), cmd.ID, domain.StatusFinished))
	_, err = e.Commands.Download(ctx(t), cmd.ID)
	wantRoute(t, err, "CommandService.writeArchive->CommandService.addResult", "FileNotFound")
	if got, _ := e.Commands.Get(ctx(t), cmd.ID); got.DownloadFile != "" {
		t.Error("a failed download was remembered")
	}
	if exists(filepath.Join(e.dirs.Downloads, "command-1.zip")) {
		t.Error("a failed download left a file")
	}
}
