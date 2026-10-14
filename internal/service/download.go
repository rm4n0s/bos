package service

import (
	"archive/zip"
	"context"
	"encoding/json"
	"io"
	"path/filepath"
	"strconv"

	"github.com/rm4n0s/bos/internal/domain"
	"github.com/rm4n0s/errors"
)

// summaryName is the JSON file inside an export archive.
const summaryName = "summary.json"

// summaryEntry describes one finished job in the archive (SPEC §7.2): all the
// job's information, with ResultPath changed to the name of its file inside the
// archive.
type summaryEntry struct {
	domain.Job
}

// Download writes the export archive of a command: the result file of every
// finished job and a JSON summary with all the information of those jobs. The
// command need not be finished; the archive holds what is finished when it is
// made. It remembers the archive's path on the command and returns it. A command
// with no finished job fails with CommandNoResults.
func (s *CommandService) Download(ctx context.Context, id domain.ID) (string, error) {
	r := s.Store.Repos()
	cmd, err := r.Commands().Get(ctx, id)
	if err != nil {
		return "", err
	}
	all, err := r.Jobs().AllByCommand(ctx, id)
	if err != nil {
		return "", err
	}
	jobs := make([]domain.Job, 0, len(all))
	for i := range all {
		if all[i].Status == domain.StatusFinished && all[i].ResultPath != "" {
			jobs = append(jobs, all[i])
		}
	}
	if len(jobs) == 0 {
		return "", errors.New("CommandNoResults", "the command has no finished job to download", "command_id", id, "status", string(cmd.Status))
	}
	path := filepath.Join(s.Dirs.Downloads, "command-"+strconv.FormatInt(id, 10)+".zip")

	// The archive is built while it is written. If building fails, that is the
	// error to report, not the broken pipe the file store then sees.
	pr, pw := io.Pipe()
	built := make(chan error, 1)
	go func() {
		err := s.writeArchive(ctx, pw, jobs)
		_ = pw.CloseWithError(err) // always nil
		built <- err
	}()
	writeErr := s.Files.Write(path, pr)
	_ = pr.Close() // unblocks the builder if Write stopped early
	if err := <-built; err != nil {
		return "", err
	}
	if writeErr != nil {
		return "", writeErr
	}
	if cmd.DownloadFile != path {
		old := cmd.DownloadFile
		if err := r.Commands().SetDownloadFile(ctx, id, path); err != nil {
			return "", err
		}
		if old != "" {
			s.removeFiles([]string{old})
		}
	}
	return path, nil
}

// writeArchive writes the zip of the jobs' results to w.
func (s *CommandService) writeArchive(ctx context.Context, w io.Writer, jobs []domain.Job) error {
	zw := zip.NewWriter(w)
	summary := make([]summaryEntry, 0, len(jobs))
	used := map[string]bool{}
	for i := range jobs {
		if err := ctx.Err(); err != nil {
			return errors.NewErr("FileStoreFailure", err).SetMetadata("operation", "archive")
		}
		j := &jobs[i]
		name := filepath.Base(j.ResultPath)
		if used[name] {
			name = strconv.FormatInt(j.ID, 10) + "_" + name
		}
		used[name] = true
		if err := s.addResult(zw, name, j.ResultPath); err != nil {
			return err
		}
		entry := summaryEntry{Job: *j}
		entry.ResultPath = name
		summary = append(summary, entry)
	}
	sw, err := zw.Create(summaryName)
	if err != nil {
		return errors.NewErr("FileStoreFailure", err).SetMetadata("operation", "archive")
	}
	enc := json.NewEncoder(sw)
	enc.SetIndent("", "  ")
	if err := enc.Encode(summary); err != nil {
		return errors.NewErr("FileStoreFailure", err).SetMetadata("operation", "archive")
	}
	if err := zw.Close(); err != nil {
		return errors.NewErr("FileStoreFailure", err).SetMetadata("operation", "archive")
	}
	return nil
}

// addResult copies one result file into the archive.
func (s *CommandService) addResult(zw *zip.Writer, name, path string) error {
	src, err := s.Files.Open(path)
	if err != nil {
		return err
	}
	zf, err := zw.Create(name)
	if err != nil {
		_ = src.Close() // the archive error is the one to report
		return errors.NewErr("FileStoreFailure", err).SetMetadata("operation", "archive")
	}
	_, copyErr := io.Copy(zf, src)
	closeErr := src.Close()
	if copyErr != nil {
		return errors.NewErr("FileStoreFailure", copyErr).SetMetadata("operation", "archive", "path", path)
	}
	if closeErr != nil {
		return errors.NewErr("FileStoreFailure", closeErr).SetMetadata("operation", "archive", "path", path)
	}
	return nil
}
