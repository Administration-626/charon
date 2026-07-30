//go:build linux || darwin

package profile

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"charon/internal/artifact"
	"charon/internal/tools"
)

// TestStoreLockBlocksConcurrentInstance verifies the advisory lock actually
// excludes a second charon instance: two Stores rooted at the same directory open
// distinct fds on the shared .lock file, and the second must fail with
// ErrStoreLocked while the first holds it. (This is the cross-process guarantee,
// exercised with two fds in one process.)
func TestStoreLockBlocksConcurrentInstance(t *testing.T) {
	s := newStore(t) // roots Store at XDG_CONFIG_HOME; lock file opened but not held
	if err := s.lock(); err != nil {
		t.Fatalf("first lock: %v", err)
	}
	defer s.unlock()

	s2, err := Open()
	if err != nil {
		t.Fatal(err)
	}
	if err := s2.lock(); !errors.Is(err, ErrStoreLocked) {
		t.Fatalf("second lock err = %v, want ErrStoreLocked", err)
	}
}

// TestStoreLockReentrant verifies nested lock/unlock calls don't release the OS
// lock early: the outer holder keeps it until its own unlock.
func TestStoreLockReentrant(t *testing.T) {
	s := newStore(t)
	if err := s.lock(); err != nil {
		t.Fatal(err)
	}
	if err := s.lock(); err != nil { // nested, same process/fd
		t.Fatalf("nested lock: %v", err)
	}

	// A second Store must still be blocked while the first is nested-held.
	s2, err := Open()
	if err != nil {
		t.Fatal(err)
	}
	if err := s2.lock(); !errors.Is(err, ErrStoreLocked) {
		t.Fatalf("second lock during nested hold err = %v, want ErrStoreLocked", err)
	}

	s.unlock() // still nested
	if err := s2.lock(); !errors.Is(err, ErrStoreLocked) {
		t.Fatalf("second lock after one unlock err = %v, want ErrStoreLocked (outer still holds)", err)
	}

	s.unlock() // fully released
	if err := s2.lock(); err != nil {
		t.Fatalf("second lock after full release err = %v, want nil", err)
	}
	s2.unlock()
}

// TestAddProfileReleasesLockForOtherInstance drives the real nested lock path:
// AddProfile takes the store lock and, internally, calls the already-locked
// SaveWithSpec. When AddProfile returns, its deferred unlock must drop the OS lock
// completely (not leave it held at depth>0), so a separate charon instance can
// immediately lock the same store. This makes the "nested path releases the lock"
// property explicit rather than relying on the rest of the suite passing.
func TestAddProfileReleasesLockForOtherInstance(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "config")
	auth := filepath.Join(dir, "auth")
	tool := &tools.Tool{
		Name:     "fake",
		Title:    "Fake",
		Detected: func() bool { _, err := os.Stat(cfg); return err == nil },
		ApplyAuth: func(a tools.AuthSpec) error {
			return os.WriteFile(cfg, []byte(a.Key), 0o600)
		},
		Artifacts: []artifact.Artifact{
			artifact.NewFile("config", cfg, 0o644),
			artifact.NewFile("auth", auth, 0o600),
		},
	}
	write(t, cfg, "initial")

	s := newStore(t)
	if err := s.AddProfile(tool, "p1", Spec{Endpoint: "https://api.openai.com/v1", Key: "k1", Model: "m1"}); err != nil {
		t.Fatalf("AddProfile: %v", err)
	}

	// AddProfile returned; a second Store instance must now be able to lock.
	s2, err := Open()
	if err != nil {
		t.Fatal(err)
	}
	if err := s2.lock(); err != nil {
		t.Fatalf("lock after AddProfile err = %v, want nil (AddProfile must fully release the store lock)", err)
	}
	s2.unlock()
}
