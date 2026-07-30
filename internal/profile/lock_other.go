//go:build !(linux || darwin)

package profile

// On platforms without an advisory-lock primitive (and without the OS keychain
// charon cares about), locking is a no-op: concurrent charon invocations there
// are not serialized. This keeps the Store API uniform across build targets.
func (s *Store) openLock() error      { return nil }
func (s *Store) acquireOSLock() error { return nil }
func (s *Store) releaseOSLock() error { return nil }
