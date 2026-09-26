package catalog

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProjectRendersBinding(t *testing.T) {
	c := openTest(t)
	home := t.TempDir()
	t.Setenv("HOME", home)

	_, cr := seed(t, c, "https://gateway.example", "sk-secret", "kimi-k2", "glm-4.6")
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
