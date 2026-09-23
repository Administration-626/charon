//go:build linux || darwin

package catalog

import (
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

// openLock opens the catalog's lock file and keeps its fd open for the process
// lifetime, so the advisory lock auto-releases if the process exits or crashes.
func (c *Catalog) openLock() error {
	f, err := os.OpenFile(filepath.Join(c.Root, ".lock"), os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return err
	}
	c.lockFile = f
	return nil
}

// acquireOSLock waits for the exclusive advisory lock. Mutations complete in
// sequence, so the later operation can replace the earlier result without
// racing the catalog tables or a tool's live config.
func (c *Catalog) acquireOSLock() error {
	if c.lockFile == nil {
		return nil
	}
	if err := unix.Flock(int(c.lockFile.Fd()), unix.LOCK_EX); err != nil {
		return err
	}
	return nil
}

// releaseOSLock drops the advisory lock; Catalog.lock/unlock pair it once per
// process-local mutex acquisition.
func (c *Catalog) releaseOSLock() error {
	if c.lockFile == nil {
		return nil
	}
	return unix.Flock(int(c.lockFile.Fd()), unix.LOCK_UN)
}
