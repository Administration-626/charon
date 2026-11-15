package catalog

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"charon/internal/edit"
)

func visibilityFixture(t *testing.T) (*Catalog, Binding, Binding, Binding, Binding) {
	t.Helper()
	c := openTest(t)
	_, ak := seed(t, c, "https://a.example/v1", "sk-a", "small", "big")
	bp, bk := seed(t, c, "https://b.example/v1/", "sk-shared", "backup")
	bk2, err := c.PutCredential(bp.ID, "sk-b2")
	if err != nil {
		t.Fatal(err)
	}
	_, ck := seed(t, c, "https://c.example/v1", "sk-shared", "other")
	add := func(name, key, slug string, slugs ...string) Binding {
		b, err := c.AddBinding("pi", name, key, slug, slugs)
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	a := add("work", ak.ID, "small", "small", "big")
	b := add("backup1", bk.ID, "backup", "backup")
	b2 := add("backup2", bk2.ID, "backup", "backup")
	other := add("other", ck.ID, "other", "other")
	if _, err := c.Activate(a.ID); err != nil {
		t.Fatal(err)
	}
	if err := edit.SetJSON(visibilitySettings(), edit.KV{Path: "defaultModel", Value: "big"}); err != nil {
		t.Fatal(err)
	}
	return c, a, b, b2, other
}
func visibilityModels() string {
	return filepath.Join(os.Getenv("HOME"), ".pi", "agent", "models.json")
}
func visibilitySettings() string {
	return filepath.Join(os.Getenv("HOME"), ".pi", "agent", "settings.json")
}
func visibilityBytes(t *testing.T, path string) []byte {
	t.Helper()
	b, e := os.ReadFile(path)
	if e != nil {
		t.Fatal(e)
	}
	return b
}
func visibilityHidden(t *testing.T, c *Catalog, name string, want bool) {
	t.Helper()
	b, ok, e := c.BindingByName("pi", name)
	if e != nil || !ok || b.Hidden != want {
		t.Fatalf("%s hidden=%v want=%v found=%v err=%v", name, b.Hidden, want, ok, e)
	}
}

func TestPiVisibilityEndpointImmediateAndDefaultsUntouched(t *testing.T) {
	c, a, b, _, _ := visibilityFixture(t)
	path := visibilityModels()
	if err := edit.SetJSON(path, edit.KV{Path: "providers.charon-a-example.userNote", Value: "preserve exactly"}, edit.KV{Path: "providers.user", Value: map[string]any{"baseUrl": "https://b.example/v1", "apiKey": "user-owned"}}); err != nil {
		t.Fatal(err)
	}
	settings := visibilityBytes(t, visibilitySettings())
	active := visibilityBytes(t, c.table("active.json"))
	own, _ := edit.GetJSONRaw(path, "providers.charon-a-example")
	other, _ := edit.GetJSONRaw(path, "providers.charon-c-example")
	user, _ := edit.GetJSONRaw(path, "providers.user")
	for _, hidden := range []bool{true, false} {
		if err := c.SetBindingHidden(b.ID, hidden); err != nil {
			t.Fatal(err)
		}
		for _, name := range []string{"backup1", "backup2"} {
			visibilityHidden(t, c, name, hidden)
		}
		visibilityHidden(t, c, "other", false)
		visibilityHidden(t, c, a.Name, false)
		disk := visibilityBytes(t, path)
		if present := strings.Contains(string(disk), "https://b.example/v1"); !present {
			t.Fatal("user provider lost")
		}
		_, registered := edit.GetJSONRaw(path, "providers.charon-b-example")
		if registered == hidden {
			t.Fatalf("hidden=%v registered=%v (must update immediately)", hidden, registered)
		}
		for key, want := range map[string]string{"providers.charon-a-example": own, "providers.charon-c-example": other, "providers.user": user} {
			if got, _ := edit.GetJSONRaw(path, key); got != want {
				t.Fatalf("changed unrelated %s", key)
			}
		}
		if !bytes.Equal(settings, visibilityBytes(t, visibilitySettings())) {
			t.Fatal("hide/show reset Pi live model or rewrote settings")
		}
		if !bytes.Equal(active, visibilityBytes(t, c.table("active.json"))) {
			t.Fatal("hide/show switched active")
		}
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("models mode err=%v", err)
	}
}

func TestPiVisibilityActiveEndpointAliasRefused(t *testing.T) {
	c, a, _, _, _ := visibilityFixture(t)
	alias, e := c.AddBinding("pi", "work-alias", a.CredentialID, "big", []string{"big"})
	if e != nil {
		t.Fatal(e)
	}
	before := visibilityBytes(t, c.table("bindings.json"))
	disk := visibilityBytes(t, visibilityModels())
	if e := c.SetBindingHidden(alias.ID, true); e == nil || !strings.Contains(e.Error(), "active") {
		t.Fatalf("active endpoint alias hide=%v", e)
	}
	if !bytes.Equal(before, visibilityBytes(t, c.table("bindings.json"))) || !bytes.Equal(disk, visibilityBytes(t, visibilityModels())) {
		t.Fatal("refusal changed catalog/config")
	}
}

func TestPiVisibilityDiskDefaultRefused(t *testing.T) {
	c, _, b, _, _ := visibilityFixture(t)
	if e := edit.SetJSON(visibilitySettings(), edit.KV{Path: "defaultProvider", Value: "charon-b-example"}, edit.KV{Path: "defaultModel", Value: "backup"}); e != nil {
		t.Fatal(e)
	}
	before := visibilityBytes(t, c.table("bindings.json"))
	disk := visibilityBytes(t, visibilityModels())
	settings := visibilityBytes(t, visibilitySettings())
	e := c.SetBindingHidden(b.ID, true)
	if e == nil || !strings.Contains(e.Error(), "default") || !strings.Contains(e.Error(), "r") {
		t.Fatalf("disk default hide=%v, want refusal and explicit reapply guidance", e)
	}
	if !bytes.Equal(before, visibilityBytes(t, c.table("bindings.json"))) || !bytes.Equal(disk, visibilityBytes(t, visibilityModels())) || !bytes.Equal(settings, visibilityBytes(t, visibilitySettings())) {
		t.Fatal("disk refusal changed state")
	}
}

func TestPiVisibilityHiddenGroupLifecycle(t *testing.T) {
	c, a, b, _, _ := visibilityFixture(t)
	if e := c.SetBindingHidden(b.ID, true); e != nil {
		t.Fatal(e)
	}
	clone, e := c.AddBinding("pi", "clone", b.CredentialID, "backup", []string{"backup"})
	if e != nil {
		t.Fatal(e)
	}
	if !clone.Hidden {
		t.Fatal("adding into hidden endpoint resurrected it")
	}
	if _, e := c.UpdateBinding(clone.ID, "clone", a.CredentialID, "big", []string{"big"}); e != nil {
		t.Fatal(e)
	}
	visibilityHidden(t, c, "clone", false)
	reopened, e := Open()
	if e != nil {
		t.Fatal(e)
	}
	for _, name := range []string{"backup1", "backup2"} {
		visibilityHidden(t, reopened, name, true)
	}
	if _, e := reopened.Activate(b.ID); e != nil {
		t.Fatal(e)
	}
	for _, name := range []string{"backup1", "backup2"} {
		visibilityHidden(t, reopened, name, false)
	}
}

func TestPiVisibilityInvalidConfigNoMutation(t *testing.T) {
	for _, bad := range []string{`{"providers":[]}`, `{"providers":{"charon-b":42}}`, `{broken`} {
		t.Run(bad, func(t *testing.T) {
			c, _, b, _, _ := visibilityFixture(t)
			path := visibilityModels()
			if e := os.WriteFile(path, []byte(bad), 0600); e != nil {
				t.Fatal(e)
			}
			before := visibilityBytes(t, c.table("bindings.json"))
			e := c.SetBindingHidden(b.ID, true)
			if e == nil {
				t.Fatal("invalid config reported success")
			}
			if !bytes.Equal(before, visibilityBytes(t, c.table("bindings.json"))) || string(visibilityBytes(t, path)) != bad {
				t.Fatal("invalid config mutated state")
			}
		})
	}
}

func TestPiVisibilityRequestPathsRemainIndependent(t *testing.T) {
	c, _, b, _, _ := visibilityFixture(t)
	_, cr := seed(t, c, "https://b.example", "sk-shared", "root")
	if _, e := c.AddBinding("pi", "root", cr.ID, "root", []string{"root"}); e != nil {
		t.Fatal(e)
	}
	if e := c.SetBindingHidden(b.ID, true); e != nil {
		t.Fatal(e)
	}
	visibilityHidden(t, c, "root", false)
}
