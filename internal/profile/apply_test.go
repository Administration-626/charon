package profile

import (
	"os"
	"path/filepath"
	"testing"

	"charon/internal/artifact"
	"charon/internal/tools"
)

// fakeKeychain is an in-memory artifact standing in for an OS keychain entry holding
// the user's real OAuth login. It implements Preservable so Apply must never delete
// it when a profile's snapshot omits it; removed records whether Remove was called.
type fakeKeychain struct {
	id      string
	value   *string
	removed bool
}

func (f *fakeKeychain) ID() string     { return f.id }
func (f *fakeKeychain) Preserve() bool { return true }
func (f *fakeKeychain) Read() ([]byte, bool, error) {
	if f.value == nil {
		return nil, false, nil
	}
	return []byte(*f.value), true, nil
}
func (f *fakeKeychain) Write(data []byte) error {
	s := string(data)
	f.value = &s
	return nil
}
func (f *fakeKeychain) Remove() error {
	f.removed = true
	f.value = nil
	return nil
}

// TestApplyPreservesKeychainWhenSnapshotOmitsIt reproduces the risk from the
// architect review: a profile captured while the user had no OAuth login records
// the keychain artifact as absent. Later the user logs in, and applying that
// profile would have deleted the live OAuth token. With the Preservable guard it
// survives, while a charon-owned file that the snapshot also omits is still removed.
func TestApplyPreservesKeychainWhenSnapshotOmitsIt(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "config")
	extra := filepath.Join(dir, "extra")
	kc := &fakeKeychain{id: "credentials"}

	tool := &tools.Tool{
		Name:     "kc",
		Title:    "KC",
		Detected: func() bool { _, err := os.Stat(cfg); return err == nil },
		Artifacts: []artifact.Artifact{
			artifact.NewFile("config", cfg, 0o644),
			artifact.NewFile("extra", extra, 0o644),
			kc,
		},
	}

	// 1. Capture "default" while both extra and the keychain are absent.
	write(t, cfg, "c1")
	s := newStore(t)
	if err := s.EnsureDefault(tool); err != nil {
		t.Fatal(err)
	}

	// 2. The user now logs in (keychain) and creates a live extra file.
	v := "real-oauth-token"
	kc.value = &v
	write(t, extra, "e-live")

	// 3. Make a separate active profile so Apply's refresh-on-leave updates that
	//    one, not "default" — leaving default's snapshot reporting keychain absent.
	write(t, cfg, "c2")
	if err := s.Save(tool, "p2", "", ""); err != nil {
		t.Fatal(err)
	}
	if err := s.SetActiveName("kc", "p2"); err != nil {
		t.Fatal(err)
	}

	// 4. Apply "default": its snapshot omits the keychain and extra. The keychain
	//    must survive (Preserve); the owned extra file must be removed (control).
	if _, err := s.Apply(tool, "default"); err != nil {
		t.Fatal(err)
	}

	if kc.removed {
		t.Error("Apply removed the live keychain even though the profile omitted it")
	}
	if kc.value == nil || *kc.value != "real-oauth-token" {
		t.Errorf("Apply clobbered the live keychain value: %v", kc.value)
	}
	if _, err := os.Stat(extra); !os.IsNotExist(err) {
		t.Error("Apply should have removed the owned 'extra' file absent from the snapshot")
	}
}

func TestRefreshNoActiveProfileIsNoop(t *testing.T) {
	dir := t.TempDir()
	tool, cfg, _ := fakeTool(dir)
	write(t, cfg, "c")
	s := newStore(t)
	// No EnsureDefault/Apply has run yet, so nothing is active.
	if err := s.Refresh(tool); err != nil {
		t.Fatalf("Refresh with no active profile should be a no-op, got %v", err)
	}
}

func TestRefreshCapturesLiveChangeIntoActiveProfile(t *testing.T) {
	dir := t.TempDir()
	tool, cfg := mergedToolWithDisplay(dir)
	write(t, cfg, `{"model":"claude-haiku","effortLevel":"low"}`)

	s := newStore(t)
	if err := s.EnsureDefault(tool); err != nil {
		t.Fatal(err)
	}

	// Live /model change with no explicit save and no profile switch.
	write(t, cfg, `{"model":"claude-opus","effortLevel":"high"}`)
	if err := s.Refresh(tool); err != nil {
		t.Fatal(err)
	}

	model, effort := s.ProfileModelEffort(tool, DefaultName)
	if model != "claude-opus" || effort != "high" {
		t.Errorf("after Refresh, default's captured model/effort = %q/%q, want claude-opus/high", model, effort)
	}
}

func TestApplyRejectsInvalidName(t *testing.T) {
	dir := t.TempDir()
	tool, cfg, _ := fakeTool(dir)
	write(t, cfg, "c")
	s := newStore(t)
	if _, err := s.Apply(tool, "../escape"); err == nil {
		t.Error("expected error applying an invalid profile name")
	}
}

func TestApplyMissingProfile(t *testing.T) {
	dir := t.TempDir()
	tool, cfg, _ := fakeTool(dir)
	write(t, cfg, "c")
	s := newStore(t)
	if _, err := s.Apply(tool, "nonexistent"); err == nil {
		t.Error("expected error applying a profile that was never saved")
	}
}

func TestDriftNoActiveProfile(t *testing.T) {
	dir := t.TempDir()
	tool, cfg, _ := fakeTool(dir)
	write(t, cfg, "c")
	s := newStore(t)
	drift, err := s.Drift(tool)
	if err != nil || drift {
		t.Errorf("Drift with no active profile = %v, %v; want false, nil", drift, err)
	}
}
