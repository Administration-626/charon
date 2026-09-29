package catalog

import (
	"os"
	"path/filepath"
	"testing"

	"charon/internal/models"
)

// legacyModels writes models.json without the contextWindow fields an older charon
// version stored, so Open has to reconcile it with the builtin table.
func legacyModels(t *testing.T, root string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, "models.json"), []byte(`[
  {"id":"m1","providerId":"p1","slug":"claude-sonnet-4-5"},
  {"id":"m2","providerId":"p1","slug":"unrecognized-custom-model"},
  {"id":"m3","providerId":"p1","slug":"gpt-5.6","contextWindow":256000,"contextWindowSource":"manual"}
]`), 0o600); err != nil {
		t.Fatal(err)
	}
}

// ceilingModels writes models.json as an older charon stored it: builtin rows
// pinned at full vendor-spec windows above the current ceiling, plus manual and
// small-builtin rows that must not change.
func ceilingModels(t *testing.T, root string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, "models.json"), []byte(`[
  {"id":"m1","providerId":"p1","slug":"glm-5.3","contextWindow":1048576,"contextWindowSource":"builtin"},
  {"id":"m2","providerId":"p1","slug":"gpt-5.6-sol","contextWindow":1050000,"contextWindowSource":"builtin"},
  {"id":"m3","providerId":"p1","slug":"grok-4.7","contextWindow":500000,"contextWindowSource":"builtin"},
  {"id":"m4","providerId":"p1","slug":"glm-4.7","contextWindow":204800,"contextWindowSource":"builtin"},
  {"id":"m5","providerId":"p1","slug":"glm-5.3","contextWindow":1048576,"contextWindowSource":"manual"}
]`), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestOpenClampsBuiltinWindowsToCeiling(t *testing.T) {
	base := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", base)
	root := filepath.Join(base, "charon")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	ceilingModels(t, root)

	c, err := Open()
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	ids := map[string]struct {
		window int
		source WindowSource
	}{
		"m1": {models.DefaultContextCeiling, WindowBuiltin},
		"m2": {models.DefaultContextCeiling, WindowBuiltin},
		"m3": {500_000, WindowBuiltin},
		"m4": {204_800, WindowBuiltin},
		"m5": {1_048_576, WindowManual},
	}
	for id, w := range ids {
		m, err := c.Model(id)
		if err != nil {
			t.Fatal(err)
		}
		if m.ContextWindow != w.window || m.ContextWindowSource != w.source {
			t.Errorf("%s (%s) window = %d (%q), want %d (%q)", id, m.Slug, m.ContextWindow, m.ContextWindowSource, w.window, w.source)
		}
	}
}

func TestOpenBackfillsLegacyContextWindows(t *testing.T) {
	base := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", base)
	root := filepath.Join(base, "charon")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	legacyModels(t, root)

	c, err := Open()
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	want := map[string]struct {
		window int
		source WindowSource
	}{
		"claude-sonnet-4-5":         {200_000, WindowBuiltin},
		"unrecognized-custom-model": {500_000, WindowBuiltin},
		"gpt-5.6":                   {256_000, WindowManual},
	}
	ms, err := c.Models()
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != len(want) {
		t.Fatalf("models = %+v", ms)
	}
	for _, m := range ms {
		w, ok := want[m.Slug]
		if !ok {
			t.Fatalf("unexpected model %+v", m)
		}
		if m.ContextWindow != w.window || m.ContextWindowSource != w.source {
			t.Errorf("%s window = %d (%q), want %d (%q)", m.Slug, m.ContextWindow, m.ContextWindowSource, w.window, w.source)
		}
	}
}

func TestOpenBackfillsLegacyContextWindowsOnce(t *testing.T) {
	base := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", base)
	root := filepath.Join(base, "charon")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	legacyModels(t, root)

	c, err := Open()
	if err != nil {
		t.Fatalf("first open: %v", err)
	}
	if m, err := c.Model("m1"); err != nil || m.ContextWindow != 200_000 {
		t.Fatalf("first open backfill: %+v, err=%v", m, err)
	}
	// Clearing a window must stay cleared: the one-shot marker keeps a later open
	// from re-filling it from the builtin table.
	if _, err := c.SetModelWindow("m1", 0); err != nil {
		t.Fatal(err)
	}
	c2, err := Open()
	if err != nil {
		t.Fatalf("second open: %v", err)
	}
	if m, err := c2.Model("m1"); err != nil || m.ContextWindow != 0 || m.ContextWindowSource != "" {
		t.Fatalf("second open refilled cleared window: %+v, err=%v", m, err)
	}
}
