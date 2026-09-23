package catalog

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// openTest points the catalog at a throwaway config dir. Tests must never touch a
// real $HOME or $XDG_CONFIG_HOME: credentials land on disk unencrypted.
func openTest(t *testing.T) *Catalog {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	c, err := Open()
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	return c
}

// seed builds one provider with one credential and the named model slugs.
func seed(t *testing.T, c *Catalog, baseURL, key string, slugs ...string) (Provider, Credential) {
	t.Helper()
	p, err := c.PutProvider(baseURL)
	if err != nil {
		t.Fatalf("provider: %v", err)
	}
	cr, err := c.PutCredential(p.ID, key)
	if err != nil {
		t.Fatalf("credential: %v", err)
	}
	for _, slug := range slugs {
		if _, err := c.PutModel(p.ID, slug); err != nil {
			t.Fatalf("model %s: %v", slug, err)
		}
	}
	return p, cr
}

func TestProviderDedupesByURL(t *testing.T) {
	c := openTest(t)
	a, err := c.PutProvider("https://gateway.example/v1/")
	if err != nil {
		t.Fatal(err)
	}
	b, err := c.PutProvider("https://gateway.example/v1")
	if err != nil {
		t.Fatal(err)
	}
	if a.ID != b.ID {
		t.Fatalf("trailing slash should not create a second provider: %s vs %s", a.ID, b.ID)
	}
	ps, err := c.Providers()
	if err != nil {
		t.Fatal(err)
	}
	if len(ps) != 1 || ps[0].BaseURL != "https://gateway.example/v1" {
		t.Fatalf("providers = %+v", ps)
	}
	if _, err := c.PutProvider("  "); err == nil {
		t.Fatal("blank base URL should be rejected")
	}
}

func TestCredentialAndModelReuse(t *testing.T) {
	c := openTest(t)
	p, _ := seed(t, c, "https://gateway.example/v1", "sk-1", "kimi-k2")

	again, err := c.PutCredential(p.ID, "sk-1")
	if err != nil {
		t.Fatal(err)
	}
	first, err := c.Credential(again.ID)
	if err != nil {
		t.Fatal(err)
	}
	other, err := c.PutCredential(p.ID, "sk-2")
	if err != nil {
		t.Fatal(err)
	}
	if other.ID == first.ID {
		t.Fatal("a different key must be a different credential")
	}

	m1, err := c.PutModel(p.ID, "kimi-k2")
	if err != nil {
		t.Fatal(err)
	}
	m2, err := c.PutModel(p.ID, "kimi-k2")
	if err != nil {
		t.Fatal(err)
	}
	if m1.ID != m2.ID {
		t.Fatalf("same slug stored twice: %s %s", m1.ID, m2.ID)
	}
	if _, err := c.PutCredential("nope", "sk"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown provider: got %v", err)
	}
	if _, err := c.PutModel(p.ID, " "); err == nil {
		t.Fatal("blank slug should be rejected")
	}
}

func TestBindingSameProviderConstraint(t *testing.T) {
	c := openTest(t)
	_, cr := seed(t, c, "https://a.example/v1", "sk-a", "model-a")
	pb, _ := seed(t, c, "https://b.example/v1", "sk-b", "model-b")

	if _, err := c.AddBinding("claude", "work", cr.ID, "model-b", nil); err == nil {
		t.Fatal("a slug from another provider must not bind to this credential")
	}
	if _, err := c.AddBinding("claude", "work", cr.ID, "no-such-model", nil); err == nil {
		t.Fatal("an unregistered slug must be rejected")
	}

	b, err := c.AddBinding("claude", "work", cr.ID, "model-a", []string{"model-a"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.AddBinding("claude", "work", cr.ID, "model-a", nil); err == nil {
		t.Fatal("duplicate name within a tool must be rejected")
	}
	// The same name is fine on a different tool.
	if _, err := c.AddBinding("opencode", "work", cr.ID, "model-a", nil); err != nil {
		t.Fatalf("same name on another tool: %v", err)
	}
	// And a model registered only for the other provider stays unusable here.
	if _, err := c.PutModel(pb.ID, "model-a"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.UpdateBinding(b.ID, "work", cr.ID, "model-b", nil); err == nil {
		t.Fatal("update must enforce the same constraint")
	}
}

func TestCodexBindingAllowsOnlyOneModel(t *testing.T) {
	c := openTest(t)
	_, cr := seed(t, c, "https://a.example/v1", "sk-a", "one", "two")

	if _, err := c.AddBinding("codex", "proxy", cr.ID, "one", []string{"one", "two"}); err == nil {
		t.Fatal("codex has no model list; two slugs must be rejected")
	}
	b, err := c.AddBinding("codex", "proxy", cr.ID, "one", nil)
	if err != nil {
		t.Fatalf("a single model is fine: %v", err)
	}
	slugs, err := c.ModelSlugs(b.Models)
	if err != nil {
		t.Fatal(err)
	}
	if len(slugs) != 1 || slugs[0] != "one" {
		t.Fatalf("models = %v", slugs)
	}
	if b.ModelID != b.Models[0] {
		t.Fatalf("default model %s not in %v", b.ModelID, b.Models)
	}
}

func TestActivePointer(t *testing.T) {
	c := openTest(t)
	t.Setenv("HOME", t.TempDir())
	_, cr := seed(t, c, "https://a.example/v1", "sk-a", "m")
	b, err := c.AddBinding("claude", "work", cr.ID, "m", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, found, err := c.Active("claude"); err != nil || found {
		t.Fatalf("nothing active yet: found=%v err=%v", found, err)
	}
	if _, err := c.Activate(b.ID); err != nil {
		t.Fatal(err)
	}
	got, found, err := c.Active("claude")
	if err != nil || !found || got.ID != b.ID {
		t.Fatalf("active = %+v found=%v err=%v", got, found, err)
	}
	if _, err := c.Activate("nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("activating a missing binding: %v", err)
	}

	if err := c.RemoveBinding(b.ID); err != nil {
		t.Fatal(err)
	}
	if _, found, err := c.Active("claude"); err != nil || found {
		t.Fatalf("removing the active binding should clear it: found=%v err=%v", found, err)
	}
	// The credential survives the binding that referenced it.
	if _, err := c.Credential(cr.ID); err != nil {
		t.Fatalf("credential removed with its binding: %v", err)
	}
}

func TestUpdateBindingKeepsItsTool(t *testing.T) {
	c := openTest(t)
	t.Setenv("HOME", t.TempDir())
	_, cr := seed(t, c, "https://a.example/v1", "sk-a", "m")
	b, err := c.AddBinding("claude", "work", cr.ID, "m", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Activate(b.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := c.UpdateBinding(b.ID, "renamed", cr.ID, "m", nil); err != nil {
		t.Fatal(err)
	}
	got, found, err := c.Active("claude")
	if err != nil || !found || got.Name != "renamed" {
		t.Fatalf("update should preserve the tool and active binding: %+v found=%v err=%v", got, found, err)
	}
}

func TestNameValidation(t *testing.T) {
	c := openTest(t)
	_, cr := seed(t, c, "https://a.example/v1", "sk-a", "m")
	for _, name := range []string{"", "has space", "line\nbreak"} {
		if _, err := c.AddBinding("claude", name, cr.ID, "m", nil); err == nil {
			t.Fatalf("name %q should be rejected", name)
		}
	}
	if _, err := c.AddBinding("claude", "工作", cr.ID, "m", nil); err != nil {
		t.Fatalf("unicode name: %v", err)
	}
	b, err := c.AddBinding("claude", "valid", cr.ID, "m", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.UpdateBinding(b.ID, "has space", cr.ID, "m", nil); err == nil {
		t.Fatal("rename to a name with whitespace should be rejected")
	}
}

func TestTablesArePrivate(t *testing.T) {
	c := openTest(t)
	seed(t, c, "https://a.example/v1", "sk-secret", "m")
	for _, name := range []string{"providers.json", "credentials.json", "models.json"} {
		info, err := os.Stat(filepath.Join(c.Root, name))
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("%s mode = %o, want 0600", name, info.Mode().Perm())
		}
	}
	info, err := os.Stat(c.Root)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o700 {
		t.Fatalf("dir mode = %o, want 0700", info.Mode().Perm())
	}
}

func TestCorruptTableSurfaces(t *testing.T) {
	c := openTest(t)
	if err := os.WriteFile(filepath.Join(c.Root, "providers.json"), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Providers(); err == nil {
		t.Fatal("malformed providers.json should be an error, not an empty list")
	}
}
