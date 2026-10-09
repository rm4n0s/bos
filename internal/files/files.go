// Package files owns the application's file storage under the configured
// directories (SPEC §14.2).
package files

import (
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/rm4n0s/errors"
)

// EnsureDirs creates every directory in dirs, with parents, when it is
// missing (SPEC §2, startup step 1).
func EnsureDirs(dirs []string) error {
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return errors.NewErr("FileStoreFailure", err).SetMetadata("path", dir, "operation", "mkdir")
		}
	}
	return nil
}

// Store reads and removes files, but only under the directories it was given:
// the paths kept in the database are never trusted to stay inside them
// (SPEC §11).
type Store struct{ roots []string }

// NewStore returns a Store that works under the given directories.
func NewStore(dirs []string) (*Store, error) {
	roots := make([]string, 0, len(dirs))
	for _, dir := range dirs {
		abs, err := filepath.Abs(dir)
		if err != nil {
			return nil, errors.NewErr("FileStoreFailure", err).SetMetadata("path", dir, "operation", "resolve")
		}
		roots = append(roots, abs)
	}
	return &Store{roots: roots}, nil
}

// resolve returns the absolute form of path, or FileOutsideStorage when it is
// not strictly inside one of the directories.
func (s *Store) resolve(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", errors.NewErr("FileStoreFailure", err).SetMetadata("path", path, "operation", "resolve")
	}
	for _, root := range s.roots {
		if rel, err := filepath.Rel(root, abs); err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return abs, nil
		}
	}
	return "", errors.New("FileOutsideStorage", "the path is not inside a storage directory", "path", path)
}

// Remove deletes a file, or a folder with its contents. A path that does not
// exist is not an error.
func (s *Store) Remove(path string) error {
	abs, err := s.resolve(path)
	if err != nil {
		return err
	}
	if err := os.RemoveAll(abs); err != nil {
		return errors.NewErr("FileStoreFailure", err).SetMetadata("path", path, "operation", "remove")
	}
	return nil
}

// Open opens a file for reading.
func (s *Store) Open(path string) (io.ReadCloser, error) {
	abs, err := s.resolve(path)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(abs)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, errors.New("FileNotFound", "the file does not exist", "path", path)
	}
	if err != nil {
		return nil, errors.NewErr("FileStoreFailure", err).SetMetadata("path", path, "operation", "open")
	}
	return f, nil
}

// Write stores the content of r at path, creating the folders above it. The
// content goes to a temporary file first and is renamed into place when it is
// complete, so a reader never sees half a file and a failed copy leaves the old
// file as it was.
func (s *Store) Write(path string, r io.Reader) (err error) {
	abs, err := s.resolve(path)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o750); err != nil {
		return errors.NewErr("FileStoreFailure", err).SetMetadata("path", path, "operation", "mkdir")
	}
	tmp, err := os.CreateTemp(filepath.Dir(abs), ".write-*")
	if err != nil {
		return errors.NewErr("FileStoreFailure", err).SetMetadata("path", path, "operation", "create")
	}
	defer func() {
		if err != nil {
			_ = tmp.Close()           // already failed; the first error is the one to report
			_ = os.Remove(tmp.Name()) // best effort: the temporary file is hidden anyway
		}
	}()
	if _, err = io.Copy(tmp, r); err != nil {
		return errors.NewErr("FileStoreFailure", err).SetMetadata("path", path, "operation", "write")
	}
	if err = tmp.Close(); err != nil {
		return errors.NewErr("FileStoreFailure", err).SetMetadata("path", path, "operation", "close")
	}
	if err = os.Rename(tmp.Name(), abs); err != nil {
		return errors.NewErr("FileStoreFailure", err).SetMetadata("path", path, "operation", "rename")
	}
	return nil
}
