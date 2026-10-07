package catalog

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"charon/internal/models"
)

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
	entries := config.Providers["charon"].Models
	if len(entries) != 2 || entries[0].ID != "gpt-6-luna" || !entries[0].Reasoning {
		t.Fatalf("Pi models not repaired: %+v", entries)
	}
	if level := entries[0].ThinkingLevelMap["max"]; level == nil || *level != "max" {
		t.Fatalf("Pi max thinking level unavailable: %+v", entries[0])
	}
	if entries[0].MaxTokens != 128_000 || entries[1].MaxTokens != models.FallbackMaxTokens {
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
	if !strings.Contains(string(data), fmt.Sprintf("model_context_window = %d", models.DefaultContextCeiling)) || !strings.Contains(string(data), "model_catalog_json") || !strings.Contains(string(data), "model_reasoning_effort = 'xhigh'") {
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
	for _, tool := range []string{"claude", "opencode", "pi"} {
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
