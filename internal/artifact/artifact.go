// Package artifact provides atomic file writes for charon's catalog and tool configs.
package artifact

import (
	"os"
	"path/filepath"
)

// AtomicWrite replaces path through a temporary file so a crash cannot leave a
// partially written credential or tool config.
func AtomicWrite(path string, data []byte, perm os.FileMode) error {
	if perm == 0 {
		perm = 0o600
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".charon-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpName, perm); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}
