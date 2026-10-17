package catalog

import (
	"os"
	"path/filepath"
	"testing"

	"charon/internal/models"
)

// switchAndOff activates a binding on tool (capturing the stash on the way),
// then takes charon back out.
func switchAndOff(t *testing.T, c *Catalog, tool, endpoint string) {
	t.Helper()
	p, err := c.PutProvider(endpoint)
	if err != nil {
		t.Fatal(err)
	}
	cr, err := c.PutCredential(p.ID, "sk-stash")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.PutModel(p.ID, "stash-model"); err != nil {
		t.Fatal(err)
	}
	b, err := c.AddBinding(tool, "stash", cr.ID, "stash-model", []string{"stash-model"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Activate(b.ID); err != nil {
		t.Fatal(err)
	}
	if err := c.Off(tool); err != nil {
		t.Fatal(err)
	}
}

func mustEqualBytes(t *testing.T, path, want string) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Errorf("%s not restored byte-exact:\n--want--\n%s\n--got--\n%s", path, want, got)
	}
}

func TestOffCodexRestoresByteExact(t *testing.T) {
	c := openTest(t)
	config := filepath.Join(homeOf(t), ".codex", "config.toml")
	os.MkdirAll(filepath.Dir(config), 0o700)
	orig := "# my codex\nmodel = \"gpt-5.6\"   # daily\nmodel_reasoning_effort = \"medium\"\n\n[model_providers.mine]\nname = \"mine\"\n"
	os.WriteFile(config, []byte(orig), 0o600)

	switchAndOff(t, c, "codex", "https://gw.example/v1")

	mustEqualBytes(t, config, orig)
	if _, err := os.Stat(filepath.Join(homeOf(t), ".codex", "custom_models.json")); !os.IsNotExist(err) {
		t.Errorf("charon-created catalog file survived off: %v", err)
	}
}

func TestOffClaudeRestoresByteExact(t *testing.T) {
	c := openTest(t)
	config := filepath.Join(homeOf(t), ".claude", "settings.json")
	os.MkdirAll(filepath.Dir(config), 0o700)
	orig := "{\n  \"theme\": \"dark\",\n  \"model\": \"claude-sonnet-4-5\",\n  \"env\": {\n    \"ANTHROPIC_API_KEY\": \"sk-old\",\n    \"KEEP\": \"me\"\n  }\n}\n"
	os.WriteFile(config, []byte(orig), 0o600)

	switchAndOff(t, c, "claude", "https://gw.example")

	mustEqualBytes(t, config, orig)
}

func TestOffGrokRestoresByteExact(t *testing.T) {
	c := openTest(t)
	config := filepath.Join(homeOf(t), ".grok", "config.toml")
	os.MkdirAll(filepath.Dir(config), 0o700)
	orig := "# grok\n\n[ui]\nyolo = true\n\n[models]\ndefault = \"mine\"\nweb_search = \"mine\"\n\n[model.mine]\nname = \"mine\"\n"
	os.WriteFile(config, []byte(orig), 0o600)

	switchAndOff(t, c, "grok", "https://gw.example/v1")

	mustEqualBytes(t, config, orig)
}

func TestOffPiRestoresByteExact(t *testing.T) {
	c := openTest(t)
	dir := filepath.Join(homeOf(t), ".pi", "agent")
	os.MkdirAll(dir, 0o700)
	modelsOrig := "{\n  \"providers\": {\n    \"mine\": { \"baseUrl\": \"https://mine\", \"api\": \"openai-completions\" }\n  }\n}\n"
	settingsOrig := "{\n  \"defaultModel\": \"m-1\",\n  \"defaultProvider\": \"mine\",\n  \"theme\": \"dark\"\n}\n"
	os.WriteFile(filepath.Join(dir, "models.json"), []byte(modelsOrig), 0o600)
	os.WriteFile(filepath.Join(dir, "settings.json"), []byte(settingsOrig), 0o600)

	switchAndOff(t, c, "pi", "https://gw.example/v1")

	mustEqualBytes(t, filepath.Join(dir, "models.json"), modelsOrig)
	mustEqualBytes(t, filepath.Join(dir, "settings.json"), settingsOrig)
}

func TestOffWithoutStashRemovesCharonOnly(t *testing.T) {
	c := openTest(t)
	config := filepath.Join(homeOf(t), ".codex", "config.toml")
	os.MkdirAll(filepath.Dir(config), 0o700)
	// A config as an older charon left it (pre-stash-feature): charon's
	// entries but no stash ever taken.
	current := "# mine\nmodel = \"stash-model\"\nmodel_provider = \"charon\"\n\n[model_providers.charon]\nname = \"charon\"\nbase_url = \"https://gw.example/v1\"\n\n[model_providers.mine]\nname = \"mine\"\n"
	os.WriteFile(config, []byte(current), 0o600)

	if err := c.Off("codex"); err != nil {
		t.Fatal(err)
	}
	mustEqualBytes(t, config, "# mine\n\n[model_providers.mine]\nname = \"mine\"\n")
}

func TestOffClearsActivePointerAndStash(t *testing.T) {
	c := openTest(t)
	config := filepath.Join(homeOf(t), ".grok", "config.toml")
	os.MkdirAll(filepath.Dir(config), 0o700)
	os.WriteFile(config, []byte("[models]\ndefault = \"mine\"\n"), 0o600)

	p, _ := c.PutProvider("https://gw.example/v1")
	cr, _ := c.PutCredential(p.ID, "sk-stash")
	if _, err := c.PutModel(p.ID, "stash-model"); err != nil {
		t.Fatal(err)
	}
	b, err := c.AddBinding("grok", "stash", cr.ID, "stash-model", []string{"stash-model"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Activate(b.ID); err != nil {
		t.Fatal(err)
	}
	if _, found, _ := c.Active("grok"); !found {
		t.Fatal("binding should be active before off")
	}
	if err := c.Off("grok"); err != nil {
		t.Fatal(err)
	}
	if _, found, _ := c.Active("grok"); found {
		t.Error("off left an active binding behind")
	}
	// A later switch takes a fresh stash (the pre-charon state now).
	if _, err := c.Activate(b.ID); err != nil {
		t.Fatal(err)
	}
	mustEqualBytes(t, config, "[models]\ndefault = \"charon-stash-model\"\n\n[model.charon-stash-model]\nmodel = \"stash-model\"\nname = \"stash-model\"\nbase_url = \"https://gw.example/v1\"\napi_key = \"sk-stash\"\ncontext_window = 500000\n")
}

func homeOf(t *testing.T) string {
	t.Helper()
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	return home
}

func TestPutModelPrefersModelsDevOverBuiltin(t *testing.T) {
	c := openTest(t)
	t.Setenv("CHARON_MODELS_DEV", "off")
	p, err := c.PutProvider("https://gw.example/v1")
	if err != nil {
		t.Fatal(err)
	}

	// With models.dev off, a builtin-known slug takes the builtin window.
	m, err := c.PutModel(p.ID, "gpt-5.6")
	if err != nil {
		t.Fatal(err)
	}
	if m.ContextWindowSource != WindowBuiltin || m.ContextWindow == 0 {
		t.Fatalf("builtin fill missing: %+v", m)
	}

	// With a fixture catalog, a slug only it knows gets its window and source.
	fixture := filepath.Join(t.TempDir(), "models-dev.json")
	os.WriteFile(fixture, []byte(`{"ven":{"models":{"ven-x":{"limit":{"context":777000,"output":4096}}}}}`), 0o644)
	t.Setenv("CHARON_MODELS_DEV", fixture)
	models.ResetModelsDev()
	m2, err := c.PutModel(p.ID, "ven-x")
	if err != nil {
		t.Fatal(err)
	}
	if m2.ContextWindow != 777000 || m2.ContextWindowSource != WindowModelsDev {
		t.Fatalf("models.dev fill missing: %+v", m2)
	}

	// A manual window still wins and survives later updates.
	mw, err := c.PutModelWithWindow(p.ID, "ven-x", 123456)
	if err != nil {
		t.Fatal(err)
	}
	if mw.ContextWindow != 123456 || mw.ContextWindowSource != WindowManual {
		t.Fatalf("manual window must win: %+v", mw)
	}
}
