package catalog

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenMigratesEditableLegacyProfilesOnce(t *testing.T) {
	base := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", base)
	root := filepath.Join(base, "charon")
	workDir := filepath.Join(root, "profiles", "claude", "work")
	oauthDir := filepath.Join(root, "profiles", "claude", "oauth")
	defaultDir := filepath.Join(root, "profiles", "pi", "default")
	for _, dir := range []string{workDir, oauthDir, defaultDir} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(workDir, "manifest.json"), []byte(`{
		"spec":{"endpoint":"https://gateway.example/v1","key":"sk-legacy","model":"model-b","models":["model-a","model-b"]}
	}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(oauthDir, "manifest.json"), []byte(`{
		"account":"user@example.com","present":{"settings.json":true}
	}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(defaultDir, "manifest.json"), []byte(`{
		"spec":{"endpoint":"https://pi.example/v1","key":"sk-default","model":"pi-model"}
	}`), 0o600); err != nil {
		t.Fatal(err)
	}
	legacyConfig := `{"active":{"claude":"work","pi":"default"},"oauthFingerprint":{"claude":"fingerprint"}}`
	if err := os.WriteFile(filepath.Join(root, "config.json"), []byte(legacyConfig), 0o600); err != nil {
		t.Fatal(err)
	}

	c, err := Open()
	if err != nil {
		t.Fatalf("open and migrate: %v", err)
	}
	bindings, err := c.Bindings("claude")
	if err != nil {
		t.Fatal(err)
	}
	if len(bindings) != 1 || bindings[0].Name != "work" {
		t.Fatalf("migrated bindings = %+v, want only editable work profile", bindings)
	}
	gotModels, err := c.ModelSlugs(bindings[0].Models)
	if err != nil || strings.Join(gotModels, ",") != "model-a,model-b" {
		t.Fatalf("models = %v, err=%v", gotModels, err)
	}
	active, found, err := c.Active("claude")
	if err != nil || !found || active.ID != bindings[0].ID {
		t.Fatalf("active = %+v, found=%v, err=%v", active, found, err)
	}
	credential, err := c.Credential(bindings[0].CredentialID)
	if err != nil || credential.Key != "sk-legacy" {
		t.Fatalf("credential = %+v, err=%v", credential, err)
	}
	piBindings, err := c.Bindings("pi")
	if err != nil || len(piBindings) != 1 || piBindings[0].Name != "imported" {
		t.Fatalf("legacy custom default = %+v, err=%v; want imported binding", piBindings, err)
	}
	piActive, found, err := c.Active("pi")
	if err != nil || !found || piActive.ID != piBindings[0].ID {
		t.Fatalf("pi active = %+v, found=%v, err=%v", piActive, found, err)
	}

	// Migration reads the old state but keeps the snapshot and old active record.
	if _, err := os.Stat(oauthDir); err != nil {
		t.Fatalf("legacy OAuth profile was removed: %v", err)
	}
	configAfter, err := os.ReadFile(filepath.Join(root, "config.json"))
	if err != nil || string(configAfter) != legacyConfig {
		t.Fatalf("legacy config changed: %q, err=%v", configAfter, err)
	}
	marker, err := os.Stat(filepath.Join(root, legacyMigrationMarker))
	if err != nil || marker.Mode().Perm() != 0o600 {
		t.Fatalf("migration marker mode = %v, err=%v", marker, err)
	}

	if err := c.RemoveBinding(bindings[0].ID); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(); err != nil {
		t.Fatal(err)
	}
	bindings, err = c.Bindings("claude")
	if err != nil || len(bindings) != 0 {
		t.Fatalf("deleted legacy binding was re-imported: %+v, err=%v", bindings, err)
	}
}
