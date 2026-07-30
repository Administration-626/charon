//go:build linux || darwin

package profile

import (
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

// openLock opens the store's lock file and keeps its fd open for the process
// lifetime, so the advisory lock auto-releases if the process exits or crashes.
func (s *Store) openLock() error {
	f, err := os.OpenFile(filepath.Join(s.Root, ".lock"), os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return err
	}
	s.lockFile = f
	return nil
}

// acquireOSLock takes an exclusive, non-blocking advisory lock on the lock file.
// A second charon process already holding it makes Flock return EWOULDBLOCK/EACCES;
// we surface that as ErrStoreLocked so the caller can fail with a clear message
// instead of waiting or corrupting the other instance's work.
func (s *Store) acquireOSLock() error {
	if s.lockFile == nil {
		return nil
	}
	if err := unix.Flock(int(s.lockFile.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		if err == unix.EWOULDBLOCK || err == unix.EACCES {
			return ErrStoreLocked
		}
		return err
	}
	return nil
}

// releaseOSLock drops the advisory lock. Flock releases are not recursive, so the
// caller must pair this with the Store's depth counter (see lock/unlock).
func (s *Store) releaseOSLock() error {
	if s.lockFile == nil {
		return nil
	}
	return unix.Flock(int(s.lockFile.Fd()), unix.LOCK_UN)
}
