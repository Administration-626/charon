package catalog

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"charon/internal/models"
	"charon/internal/tools"
)

// piDefaultProvider is the provider key charon made default in settings.json:
// the endpoint-keyed name of the active binding's own entry.
func piDefaultProvider(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(os.Getenv("HOME"), ".pi", "agent", "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	var s struct {
		DefaultProvider string `json:"defaultProvider"`
	}
	if err := json.Unmarshal(data, &s); err != nil {
		t.Fatal(err)
	}
	if s.DefaultProvider == "" {
		t.Fatal("settings.json has no defaultProvider")
	}
	return s.DefaultProvider
}

func TestProjectCodexMultipleModels(t *testing.T) {
	for _, endpoint := range []string{"https://gateway.example/v1", "https://api.openai.com/v1"} {
		t.Run(endpoint, func(t *testing.T) {
			cat := openTest(t)
			provider, credential := seed(t, cat, endpoint, "sk-test", "gpt-5.5", "glm-5.3-flash")
			custom, err := cat.PutModelWithWindow(provider.ID, "glm-5.3-flash", 128000)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := cat.SetModelEffort(custom.ID, "max"); err != nil {
				t.Fatal(err)
			}
			binding, err := cat.AddBinding("codex", "multi", credential.ID, "gpt-5.5", []string{"gpt-5.5", custom.Slug})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := cat.Activate(binding.ID); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(filepath.Join(os.Getenv("HOME"), ".codex", "custom_models.json"))
			if err != nil {
				t.Fatal(err)
			}
			var catalog struct {
				Models []struct {
					Slug   string `json:"slug"`
					Window int    `json:"context_window"`
					Effort string `json:"default_reasoning_level"`
				} `json:"models"`
			}
			if err := json.Unmarshal(data, &catalog); err != nil {
				t.Fatal(err)
			}
			if len(catalog.Models) != 2 || catalog.Models[0].Slug != "gpt-5.5" || catalog.Models[0].Window != models.DefaultContextWindow("gpt-5.5") || catalog.Models[0].Effort != "medium" || catalog.Models[1].Slug != custom.Slug || catalog.Models[1].Window != 128000 || catalog.Models[1].Effort != "max" {
				t.Fatalf("projected models = %+v", catalog.Models)
			}
			data, err = os.ReadFile(filepath.Join(os.Getenv("HOME"), ".codex", "config.toml"))
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(data), "model_context_window") {
				t.Fatal("multi-model binding must use per-model context windows")
			}
		})
	}
}

func TestReapplyPiRepairsThinkingCapabilities(t *testing.T) {
	c := openTest(t)
	_, cr := seed(t, c, "https://gateway.example/v1", "sk-test", "gpt-6-luna", "gpt-4o")
	b, err := c.AddBinding("pi", "work", cr.ID, "gpt-6-luna", []string{"gpt-6-luna", "gpt-4o"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Activate(b.ID); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(os.Getenv("HOME"), ".pi", "agent")
	// Simulate an already-active binding rendered by the old adapter.
	if err := os.WriteFile(filepath.Join(dir, "models.json"), []byte(
		`{"providers":{"charon":{"models":[{"id":"gpt-6-luna","reasoning":false,"maxTokens":8192}]}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(
		`{"defaultThinkingLevel":"high","compaction":{"enabled":false}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Reapply(b.ID); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "models.json"))
	if err != nil {
		t.Fatal(err)
	}
	var config struct {
		Providers map[string]struct {
			Models []struct {
				ID               string             `json:"id"`
				Reasoning        bool               `json:"reasoning"`
				MaxTokens        int                `json:"maxTokens"`
				ThinkingLevelMap map[string]*string `json:"thinkingLevelMap"`
			} `json:"models"`
		} `json:"providers"`
	}
	if err := json.Unmarshal(data, &config); err != nil {
		t.Fatal(err)
	}
	entries := config.Providers[piDefaultProvider(t)].Models
	if len(entries) != 2 || entries[0].ID != "gpt-6-luna" || !entries[0].Reasoning {
		t.Fatalf("Pi models not repaired: %+v", entries)
	}
	if level := entries[0].ThinkingLevelMap["max"]; level == nil || *level != "max" {
		t.Fatalf("Pi max thinking level unavailable: %+v", entries[0])
	}
	if entries[0].MaxTokens != 128_000 || entries[1].MaxTokens != 16_384 {
		t.Fatalf("Pi output limits not repaired: %+v", entries)
	}
	if entries[1].ID != "gpt-4o" || !entries[1].Reasoning {
		t.Fatalf("default thinking capability missing from another model: %+v", entries[1])
	}
	data, err = os.ReadFile(filepath.Join(dir, "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	var settings struct {
		DefaultThinkingLevel string `json:"defaultThinkingLevel"`
		Compaction           struct {
			Enabled bool `json:"enabled"`
		} `json:"compaction"`
	}
	if err := json.Unmarshal(data, &settings); err != nil {
		t.Fatal(err)
	}
	if settings.DefaultThinkingLevel != "high" || settings.Compaction.Enabled {
		t.Fatalf("Pi preferences changed: %+v", settings)
	}
}

func TestProjectRendersBinding(t *testing.T) {
	c := openTest(t)
	home := t.TempDir()
	t.Setenv("HOME", home)

	_, cr := seed(t, c, "https://gateway.example", "sk-secret", "kimi-k2", "glm-4.6")
	if _, err := c.PutModelWithWindow("p1", "kimi-k2", 128000); err != nil {
		t.Fatal(err)
	}
	if _, err := c.PutModelWithWindow("p1", "glm-4.6", 200000); err != nil {
		t.Fatal(err)
	}
	b, err := c.AddBinding("claude", "work", cr.ID, "glm-4.6", []string{"kimi-k2", "glm-4.6"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Activate(b.ID); err != nil {
		t.Fatalf("project: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(home, ".claude", "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)
	for _, want := range []string{"https://gateway.example", "sk-secret", "glm-4.6", "kimi-k2"} {
		if !strings.Contains(got, want) {
			t.Errorf("rendered settings.json missing %q:\n%s", want, got)
		}
	}
}

func TestProjectCodexLeavesBuiltinOpenAIWindowToCodex(t *testing.T) {
	c := openTest(t)
	home := t.TempDir()
	t.Setenv("HOME", home)

	p, err := c.PutProvider("https://api.openai.com/v1")
	if err != nil {
		t.Fatal(err)
	}
	cr, err := c.PutCredential(p.ID, "sk-secret")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.PutModel(p.ID, "gpt-5.5"); err != nil {
		t.Fatal(err)
	}
	b, err := c.AddBinding("codex", "official", cr.ID, "gpt-5.5", []string{"gpt-5.5"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Activate(b.ID); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(filepath.Join(home, ".codex", "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "model_context_window") {
		t.Fatalf("Codex builtin OpenAI window should remain native:\n%s", data)
	}
}

func TestProjectCodexPinsGatewayGPTWindow(t *testing.T) {
	c := openTest(t)
	home := t.TempDir()
	t.Setenv("HOME", home)

	p, err := c.PutProvider("https://gateway.example/v1")
	if err != nil {
		t.Fatal(err)
	}
	cr, err := c.PutCredential(p.ID, "sk-secret")
	if err != nil {
		t.Fatal(err)
	}
	m, err := c.PutModel(p.ID, "gpt-5.6-sol")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.SetModelEffort(m.ID, "xhigh"); err != nil {
		t.Fatal(err)
	}
	b, err := c.AddBinding("codex", "gateway", cr.ID, "gpt-5.6-sol", []string{"gpt-5.6-sol"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Activate(b.ID); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(filepath.Join(home, ".codex", "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), fmt.Sprintf("model_context_window = %d", models.DefaultContextCeiling)) || !strings.Contains(string(data), "model_catalog_json") || !strings.Contains(string(data), "model_reasoning_effort = \"xhigh\"") {
		t.Fatalf("gateway GPT model lost its catalog window or effort:\n%s", data)
	}
}

func TestActivateSameBindingPreservesLiveModel(t *testing.T) {
	c := openTest(t)
	home := t.TempDir()
	t.Setenv("HOME", home)

	_, cr := seed(t, c, "https://gateway.example", "sk-secret", "kimi-k2", "glm-4.6")
	b, err := c.AddBinding("opencode", "work", cr.ID, "kimi-k2", []string{"kimi-k2", "glm-4.6"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Activate(b.ID); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(home, ".config", "opencode", "opencode.jsonc")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	data = []byte(strings.Replace(string(data), `"model": "charon/kimi-k2"`, `"model": "charon/glm-4.6"`, 1))
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := c.Activate(b.ID); err != nil {
		t.Fatal(err)
	}
	data, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"model": "charon/glm-4.6"`) {
		t.Fatalf("Activate(same binding) reset the live model:\n%s", data)
	}
	if _, err := c.Reapply(b.ID); err != nil {
		t.Fatal(err)
	}
	data, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"model": "charon/kimi-k2"`) {
		t.Fatal("Reapply did not restore the saved default model")
	}
}

func TestProjectSingleModelReplacesExistingList(t *testing.T) {
	c := openTest(t)
	home := t.TempDir()
	t.Setenv("HOME", home)

	_, cr := seed(t, c, "https://gateway.example", "sk-secret", "kimi-k2", "glm-4.6")
	full, err := c.AddBinding("opencode", "work", cr.ID, "kimi-k2", []string{"kimi-k2", "glm-4.6"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Activate(full.ID); err != nil {
		t.Fatal(err)
	}

	// A binding with one model must not inherit another endpoint's model list.
	only, err := c.AddBinding("opencode", "rotated", cr.ID, "glm-4.6", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Activate(only.ID); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(home, ".config", "opencode", "opencode.jsonc"))
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)
	if strings.Contains(got, "kimi-k2") {
		t.Errorf("single-model render retained the previous binding's model:\n%s", got)
	}
	if !strings.Contains(got, `"model": "charon/glm-4.6"`) {
		t.Errorf("default model not switched:\n%s", got)
	}
}

func TestProjectBindingReplacesPreviousToolsModelList(t *testing.T) {
	// Pi is excluded on purpose: activating one pi binding keeps every other
	// endpoint registered (see TestProjectSpecAggregatesEndpointsForPi), so its
	// previous endpoint is not supposed to disappear from models.json.
	for _, tool := range []string{"claude", "opencode"} {
		t.Run(tool, func(t *testing.T) {
			c := openTest(t)
			home := t.TempDir()
			t.Setenv("HOME", home)

			_, oldCredential := seed(t, c, "https://old.example/v1", "sk-old", "old-model", "old-extra")
			old, err := c.AddBinding(tool, "old", oldCredential.ID, "old-model", []string{"old-model", "old-extra"})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := c.Activate(old.ID); err != nil {
				t.Fatal(err)
			}

			newProvider, err := c.PutProvider("https://new.example/v1")
			if err != nil {
				t.Fatal(err)
			}
			newCredential, err := c.PutCredential(newProvider.ID, "sk-new")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := c.PutModel(newProvider.ID, "new-model"); err != nil {
				t.Fatal(err)
			}
			current, err := c.AddBinding(tool, "new", newCredential.ID, "new-model", nil)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := c.Activate(current.ID); err != nil {
				t.Fatal(err)
			}

			var content strings.Builder
			err = filepath.WalkDir(home, func(path string, entry os.DirEntry, err error) error {
				if err != nil {
					return err
				}
				if entry.IsDir() {
					return nil
				}
				data, err := os.ReadFile(path)
				if err == nil {
					content.Write(data)
				}
				return err
			})
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(content.String(), "old-model") || strings.Contains(content.String(), "old-extra") {
				t.Fatalf("previous binding's models remain in %s config:\n%s", tool, content.String())
			}
			if !strings.Contains(content.String(), "new-model") {
				t.Fatalf("current binding's model missing from %s config:\n%s", tool, content.String())
			}
		})
	}
}

func TestActivateUnknownBinding(t *testing.T) {
	c := openTest(t)
	if _, err := c.Activate("nope"); err == nil {
		t.Fatal("activating a missing binding should fail")
	}
}

func TestActivateProjectionFailureKeepsPreviousActive(t *testing.T) {
	c := openTest(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	_, cr := seed(t, c, "https://gateway.example", "sk-secret", "model-a")
	first, err := c.AddBinding("claude", "first", cr.ID, "model-a", nil)
	if err != nil {
		t.Fatal(err)
	}
	second, err := c.AddBinding("claude", "second", cr.ID, "model-a", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Activate(first.ID); err != nil {
		t.Fatal(err)
	}
	settings := filepath.Join(home, ".claude", "settings.json")
	if err := os.MkdirAll(filepath.Dir(settings), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(settings, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Activate(second.ID); err == nil {
		t.Fatal("projection from malformed settings.json should fail")
	}
	active, found, err := c.Active("claude")
	if err != nil || !found || active.ID != first.ID {
		t.Fatalf("active = %+v, found=%v, err=%v; want previous binding %s", active, found, err, first.ID)
	}
}

func TestActivateActiveWriteFailureReportsChangedToolConfig(t *testing.T) {
	c := openTest(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	_, firstCredential := seed(t, c, "https://first.example", "sk-first", "model-a")
	_, secondCredential := seed(t, c, "https://second.example", "sk-second", "model-b")
	first, err := c.AddBinding("claude", "first", firstCredential.ID, "model-a", nil)
	if err != nil {
		t.Fatal(err)
	}
	second, err := c.AddBinding("claude", "second", secondCredential.ID, "model-b", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Activate(first.ID); err != nil {
		t.Fatal(err)
	}

	// The tool's config remains writable while the catalog cannot replace active.json.
	if err := os.Chmod(c.Root, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(c.Root, 0o700) })
	if probe, err := os.CreateTemp(c.Root, "write-probe-*"); err == nil {
		_ = probe.Close()
		_ = os.Remove(probe.Name())
		t.Skip("catalog directory remains writable despite mode 0500")
	}

	_, err = c.Activate(second.ID)
	if err == nil || !strings.Contains(err.Error(), "tool config may have changed") {
		t.Fatalf("activation error must explain partial write, got %v", err)
	}
	active, found, err := c.Active("claude")
	if err != nil || !found || active.ID != first.ID {
		t.Fatalf("active binding = %+v, found=%v, err=%v; want first", active, found, err)
	}
	data, err := os.ReadFile(filepath.Join(home, ".claude", "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "https://second.example") {
		t.Fatalf("tool config did not reflect second binding after active pointer write failed")
	}
	if _, err := c.Reapply(first.ID); err != nil {
		t.Fatalf("reapply of confirmed binding should work without rewriting active.json: %v", err)
	}
	data, err = os.ReadFile(filepath.Join(home, ".claude", "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "https://first.example") {
		t.Fatal("explicit reapply did not restore the confirmed binding")
	}
}

func TestProjectIfActiveUsesCurrentBindingAndSkipsInactive(t *testing.T) {
	c := openTest(t)
	home := t.TempDir()
	t.Setenv("HOME", home)

	_, oldCredential := seed(t, c, "https://old.example/v1", "sk-old", "model-old")
	work, err := c.AddBinding("claude", "work", oldCredential.ID, "model-old", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Activate(work.ID); err != nil {
		t.Fatal(err)
	}

	newProvider, err := c.PutProvider("https://new.example/v1")
	if err != nil {
		t.Fatal(err)
	}
	newCredential, err := c.PutCredential(newProvider.ID, "sk-new")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.PutModel(newProvider.ID, "model-new"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.UpdateBinding(work.ID, "work", newCredential.ID, "model-new", nil); err != nil {
		t.Fatal(err)
	}
	if applied, err := c.ProjectIfActive(work.ID); err != nil || !applied {
		t.Fatalf("ProjectIfActive() = %v, %v; want applied", applied, err)
	}
	data, err := os.ReadFile(filepath.Join(home, ".claude", "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	if got := string(data); !strings.Contains(got, "https://new.example") || !strings.Contains(got, "sk-new") || !strings.Contains(got, "model-new") {
		t.Fatalf("projected stale binding data: %s", got)
	}

	other, err := c.AddBinding("claude", "other", newCredential.ID, "model-new", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Activate(other.ID); err != nil {
		t.Fatal(err)
	}
	if applied, err := c.ProjectIfActive(work.ID); err != nil || applied {
		t.Fatalf("ProjectIfActive(inactive) = %v, %v; want skipped", applied, err)
	}
	active, found, err := c.Active("claude")
	if err != nil || !found || active.ID != other.ID {
		t.Fatalf("active = %+v, found=%v, err=%v", active, found, err)
	}
}

func TestRetiredToolBindingIsPreservedAndNotProjected(t *testing.T) {
	c := openTest(t)
	_, cr := seed(t, c, "https://example.test/v1", "sk-test", "custom")
	b, err := c.AddBinding("omp", "legacy", cr.ID, "custom", nil)
	if err != nil {
		t.Fatal(err)
	}
	// An older Charon could have left this tool active before support was removed.
	if err := c.writeActive(map[string]string{"omp": b.ID}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(os.Getenv("HOME"), ".omp", "agent", "models.yml")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("# existing user config\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	paths := []string{path}
	for _, file := range []string{"bindings.json", "active.json", "providers.json", "credentials.json", "models.json"} {
		paths = append(paths, c.table(file))
	}
	before := make(map[string]string, len(paths))
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		before[path] = string(data)
	}
	reopened, err := Open()
	if err != nil {
		t.Fatal(err)
	}
	if applied, err := reopened.ProjectIfActive(b.ID); err != nil || applied {
		t.Fatalf("retired tool should be skipped: applied=%v, err=%v", applied, err)
	}
	if _, err := reopened.Reapply(b.ID); err == nil || !strings.Contains(err.Error(), "unknown tool") {
		t.Fatalf("explicit reapply should reject a retired tool: %v", err)
	}
	for path, want := range before {
		data, err := os.ReadFile(path)
		if err != nil || string(data) != want {
			t.Fatalf("retiring tool changed %s: %v", path, err)
		}
	}
}

func TestProjectSpecAggregatesToolBindingsForPi(t *testing.T) {
	c := openTest(t)
	// The binding's own endpoint keeps a catalog larger than its curated list:
	// aggregation follows pi's bindings, so the unselected model stays out.
	own, ownCredential := seed(t, c, "https://own.example/v1", "sk-own", "own-default", "own-extra")
	if _, err := c.PutModelWithWindow(own.ID, "own-default", 128000); err != nil {
		t.Fatal(err)
	}
	if _, err := c.PutModelWithWindow(own.ID, "own-extra", 40000); err != nil {
		t.Fatal(err)
	}

	// A key and models another tool uses are that tool's business: pi must not
	// register an endpoint it has no binding for.
	claudeOnly, claudeCredential := seed(t, c, "https://claude-only.example/v1", "sk-claude", "claude-a")
	if _, err := c.PutModelWithWindow(claudeOnly.ID, "claude-a", 200000); err != nil {
		t.Fatal(err)
	}
	if _, err := c.AddBinding("claude", "c", claudeCredential.ID, "claude-a", []string{"claude-a"}); err != nil {
		t.Fatal(err)
	}

	// Sorts before the other endpoint, so group order cannot be binding order.
	earlier, earlierCredential := seed(t, c, "https://a5.example/v1", "sk-five", "five-a")
	if _, err := c.PutModelWithWindow(earlier.ID, "five-a", 200000); err != nil {
		t.Fatal(err)
	}
	if _, err := c.AddBinding("pi", "five", earlierCredential.ID, "five-a", []string{"five-a"}); err != nil {
		t.Fatal(err)
	}

	second, secondCredential := seed(t, c, "https://m2.example/v1", "sk-two", "two-a", "two-b")
	if _, err := c.PutModelWithWindow(second.ID, "two-a", 64000); err != nil {
		t.Fatal(err)
	}
	if _, err := c.PutModelWithWindow(second.ID, "two-b", 32000); err != nil {
		t.Fatal(err)
	}
	if _, err := c.AddBinding("pi", "two", secondCredential.ID, "two-a", []string{"two-a", "two-b"}, "anthropic-messages"); err != nil {
		t.Fatal(err)
	}
	// A second pi binding on the same endpoint keeps the first entry only.
	rotated, err := c.PutCredential(second.ID, "sk-two-rotated")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.AddBinding("pi", "two-copy", rotated.ID, "two-a", []string{"two-a"}); err != nil {
		t.Fatal(err)
	}

	// A second pi binding on the binding's own endpoint (a rotated key) must not
	// become another entry for the endpoint providers.charon already holds.
	ownRotated, err := c.PutCredential(own.ID, "sk-own-rotated")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.AddBinding("pi", "own-copy", ownRotated.ID, "own-default", []string{"own-default"}); err != nil {
		t.Fatal(err)
	}

	b, err := c.AddBinding("pi", "agg", ownCredential.ID, "own-default", []string{"own-default"})
	if err != nil {
		t.Fatal(err)
	}
	spec, err := c.ProjectSpec(b)
	if err != nil {
		t.Fatal(err)
	}
	if spec.Endpoint != "https://own.example/v1" || spec.Key != "sk-own" || spec.Model != "own-default" {
		t.Fatalf("binding's own endpoint changed: %+v", spec)
	}
	wantGroups := []tools.EndpointGroup{
		{Endpoint: "https://a5.example/v1", Key: "sk-five", PiAPI: "openai-completions", Models: []tools.ModelSpec{{Slug: "five-a", ContextWindow: 200000}}},
		{
			Endpoint: "https://m2.example/v1", Key: "sk-two", PiAPI: "anthropic-messages",
			Models: []tools.ModelSpec{{Slug: "two-a", ContextWindow: 64000}, {Slug: "two-b", ContextWindow: 32000}},
		},
	}
	if !reflect.DeepEqual(spec.Groups, wantGroups) {
		t.Fatalf("groups = %+v, want %+v", spec.Groups, wantGroups)
	}
	// The primary entry lists what this binding picked, not the endpoint's whole
	// catalog: the catalog's extra model was never part of pi's configuration.
	wantModels := []tools.ModelSpec{{Slug: "own-default", ContextWindow: 128000}}
	if !reflect.DeepEqual(spec.Models, wantModels) {
		t.Fatalf("models = %+v, want %+v", spec.Models, wantModels)
	}
}

func TestProjectSpecOmitsGroupsForOtherTools(t *testing.T) {
	for _, tool := range []string{"codex", "claude"} {
		t.Run(tool, func(t *testing.T) {
			c := openTest(t)
			own, cr := seed(t, c, "https://one.example/v1", "sk-one", "one-a", "one-b")
			if _, err := c.PutModelWithWindow(own.ID, "one-a", 128000); err != nil {
				t.Fatal(err)
			}
			if _, err := c.PutModelWithWindow(own.ID, "one-b", 96000); err != nil {
				t.Fatal(err)
			}
			// A pi binding on another endpoint: a multi-endpoint tool would
			// register it, this one must not.
			_, piCredential := seed(t, c, "https://two.example/v1", "sk-two", "two-a")
			if _, err := c.AddBinding("pi", "pi-two", piCredential.ID, "two-a", []string{"two-a"}); err != nil {
				t.Fatal(err)
			}
			b, err := c.AddBinding(tool, "single", cr.ID, "one-a", []string{"one-a", "one-b"})
			if err != nil {
				t.Fatal(err)
			}
			spec, err := c.ProjectSpec(b)
			if err != nil {
				t.Fatal(err)
			}
			if spec.Groups != nil {
				t.Fatalf("%s must not aggregate endpoints: %+v", tool, spec.Groups)
			}
			want := []tools.ModelSpec{{Slug: "one-a", ContextWindow: 128000}, {Slug: "one-b", ContextWindow: 96000}}
			if !reflect.DeepEqual(spec.Models, want) {
				t.Fatalf("models = %+v, want the binding's list %+v", spec.Models, want)
			}
		})
	}
}

func TestAggregateCounts(t *testing.T) {
	c := openTest(t)
	oneProvider, one := seed(t, c, "https://one.example/v1", "sk-one", "a", "b")
	_, two := seed(t, c, "https://two.example/v1", "sk-two", "c")
	// A key and models without a binding of their own never count.
	seed(t, c, "https://unbound.example/v1", "sk-unbound", "d")
	if _, err := c.AddBinding("pi", "one", one.ID, "a", []string{"a", "b"}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.AddBinding("pi", "two", two.ID, "c", []string{"c"}); err != nil {
		t.Fatal(err)
	}
	// A second binding on the same endpoint (a rotated key) is still one endpoint.
	rotated, err := c.PutCredential(oneProvider.ID, "sk-one-rotated")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.AddBinding("pi", "one-copy", rotated.ID, "a", []string{"a"}); err != nil {
		t.Fatal(err)
	}

	endpoints, modelCount, err := c.AggregateCounts("pi")
	if err != nil {
		t.Fatal(err)
	}
	if endpoints != 2 || modelCount != 3 {
		t.Fatalf("AggregateCounts(pi) = %d endpoints, %d models; want 2, 3", endpoints, modelCount)
	}
	// Another tool's bindings are its own business.
	if endpoints, modelCount, err := c.AggregateCounts("codex"); err != nil || endpoints != 0 || modelCount != 0 {
		t.Fatalf("AggregateCounts(codex) = %d endpoints, %d models, err=%v; want 0, 0", endpoints, modelCount, err)
	}

	empty := openTest(t)
	if endpoints, modelCount, err := empty.AggregateCounts("pi"); err != nil || endpoints != 0 || modelCount != 0 {
		t.Fatalf("empty catalog = %d endpoints, %d models, err=%v; want 0, 0", endpoints, modelCount, err)
	}
}
