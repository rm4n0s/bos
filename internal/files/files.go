// Package files owns the application's file storage under the configured
// directories (SPEC §14.2).
package files

import (
	"os"

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
