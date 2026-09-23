package catalog

import (
	"testing"

	"charon/internal/tools"
)

func TestStoreBindingValidatesBeforeWritingCredential(t *testing.T) {
	c := openTest(t)
	tool := &tools.Tool{Name: "claude", DefaultEndpoint: "https://gateway.example/v1"}

	if _, err := StoreBinding(c, tool, nil, "line\nbreak", "", "sk-secret", "model-a", nil, false); err == nil {
		t.Fatal("invalid name was accepted")
	}
	if credentials, err := c.credentials(); err != nil {
		t.Fatal(err)
	} else if len(credentials) != 0 {
		t.Fatalf("failed add stored credentials: %+v", credentials)
	}

	if _, err := StoreBinding(c, tool, nil, "work", "", "sk-secret", "model-a", nil, false); err != nil {
		t.Fatal(err)
	}
	if _, err := StoreBinding(c, tool, nil, "work", "https://other.example/v1", "sk-orphan", "model-b", nil, false); err == nil {
		t.Fatal("duplicate name was accepted")
	}
	credentials, err := c.credentials()
	if err != nil {
		t.Fatal(err)
	}
	if len(credentials) != 1 || credentials[0].Key != "sk-secret" {
		t.Fatalf("failed duplicate add changed credentials: %+v", credentials)
	}
}

func TestStoreBindingCanEditLegacyName(t *testing.T) {
	c := openTest(t)
	tool := &tools.Tool{Name: "claude", DefaultEndpoint: "https://gateway.example/v1"}
	_, cr := seed(t, c, "https://gateway.example/v1", "sk-secret", "model-a", "model-b")
	b, err := c.AddBinding(tool.Name, "work", cr.ID, "model-a", nil)
	if err != nil {
		t.Fatal(err)
	}

	// Simulate a binding written by an older version that allowed spaces.
	bs, err := c.bindings()
	if err != nil {
		t.Fatal(err)
	}
	bs[0].Name = "old name"
	if err := writeTable(c.table("bindings.json"), bs); err != nil {
		t.Fatal(err)
	}
	b.Name = bs[0].Name
	if _, err := StoreBinding(c, tool, &b, "bad name", "", "sk-rotated", "model-a", nil, false); err == nil {
		t.Fatal("renaming to another invalid name was accepted")
	}

	updated, err := StoreBinding(c, tool, &b, b.Name, "", "sk-rotated", "model-b", nil, false)
	if err != nil {
		t.Fatalf("editing without renaming legacy name: %v", err)
	}
	if updated.Name != b.Name {
		t.Fatalf("name changed during edit: got %q, want %q", updated.Name, b.Name)
	}
	if slug, err := c.ModelSlug(updated.ModelID); err != nil || slug != "model-b" {
		t.Fatalf("updated model = %q, err=%v; want model-b", slug, err)
	}

	renamed, err := StoreBinding(c, tool, &updated, "work", "", "sk-rotated", "model-b", nil, false)
	if err != nil {
		t.Fatalf("renaming legacy name: %v", err)
	}
	if renamed.Name != "work" {
		t.Fatalf("renamed name = %q, want work", renamed.Name)
	}
}
