//go:build !linux && !darwin

package profile

import (
	"testing"
)

// TestStoreLockNoOpOnUnsupportedPlatform verifies that on platforms without an
// advisory-lock primitive (e.g. Windows), the Store's lock API is a safe no-op:
// it never errors, never blocks a second instance, and nested lock/unlock calls
// are reentrant-safe. This guards lock_other.go so a Windows (or other non-unix)
// build can't silently start erroring or panicking on the lock calls that every
// mutating method now makes.
func TestStoreLockNoOpOnUnsupportedPlatform(t *testing.T) {
	s := newStore(t)
	if err := s.lock(); err != nil {
		t.Fatalf("first lock err = %v, want nil (no-op platform)", err)
	}
	if err := s.lock(); err != nil { // nested, must not error or block
		t.Fatalf("nested lock err = %v, want nil (no-op platform)", err)
	}

	// A second Store must also lock without error or blocking.
	s2, err := Open()
	if err != nil {
		t.Fatal(err)
	}
	if err := s2.lock(); err != nil {
		t.Fatalf("second lock err = %v, want nil (no-op never blocks)", err)
	}

	// Over-unlocking must be safe (no panic) on the no-op path.
	s.unlock()
	s.unlock()
	s2.unlock()
}
