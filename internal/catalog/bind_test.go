package catalog

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"charon/internal/tools"
)

func TestPiBindingProtocolRoundTrip(t *testing.T) {
	c := openTest(t)
	tool := tools.Find("pi")
	endpoint := "https://example.test/v1"
	for _, api := range []string{"openai-completions", "openai-responses", "anthropic-messages", "google-generative-ai"} {
		b, err := StoreBinding(c, tool, nil, api, endpoint, "sk-test", "custom", nil, false, nil, api)
		if err != nil {
			t.Fatal(err)
		}
		// An ordinary edit/rename must preserve the protocol without a new option.
		b, err = StoreBinding(c, tool, &b, api+"-renamed", endpoint, "sk-rotated", "custom", nil, false, nil)
		if err != nil || b.PiAPI != api {
			t.Fatalf("edit protocol = %q, err=%v, want %q", b.PiAPI, err, api)
		}
	}
	reopened, err := Open()
	if err != nil {
		t.Fatal(err)
	}
	bindings, err := reopened.Bindings("pi")
	if err != nil || len(bindings) != 4 {
		t.Fatalf("bindings = %v, err=%v", bindings, err)
	}
	assertAPI := func(want string) {
		t.Helper()
		data, err := os.ReadFile(filepath.Join(os.Getenv("HOME"), ".pi", "agent", "models.json"))
		if err != nil {
			t.Fatal(err)
		}
		var cfg struct {
			Providers map[string]struct {
				API string `json:"api"`
			} `json:"providers"`
		}
		if err := json.Unmarshal(data, &cfg); err != nil || cfg.Providers["charon"].API != want {
			t.Fatalf("projected API = %q, err=%v, want %q", cfg.Providers["charon"].API, err, want)
		}
	}
	for _, b := range bindings {
		if _, err := reopened.Activate(b.ID); err != nil {
			t.Fatal(err)
		}
		assertAPI(b.PiAPI)
		updated, err := reopened.UpdateBinding(b.ID, b.Name, b.CredentialID, "custom", nil, "google-generative-ai")
		if err != nil {
			t.Fatal(err)
		}
		if ok, err := reopened.ProjectIfActive(updated.ID); !ok || err != nil {
			t.Fatalf("reproject active: %v, %v", ok, err)
		}
		assertAPI("google-generative-ai")
	}
	// Simulate a bindings.json row written before piApi existed. Reapplying it
	// must replace the previous binding's Gemini API with default Chat Completions.
	bindings, err = reopened.Bindings("pi")
	if err != nil {
		t.Fatal(err)
	}
	bindings[0].PiAPI = ""
	if err := writeTable(reopened.table("bindings.json"), bindings); err != nil {
		t.Fatal(err)
	}
	if _, err := reopened.Reapply(bindings[0].ID); err != nil {
		t.Fatal(err)
	}
	assertAPI("openai-completions")
}

func TestBindingRejectsInvalidPiProtocolBeforeWriting(t *testing.T) {
	for _, tc := range []struct {
		tool string
		api  []string
	}{
		{"pi", []string{"openai-responses-compact"}},
		{"pi", []string{"invalid"}},
		{"pi", []string{"openai-responses", "anthropic-messages"}},
		{"claude", []string{"anthropic-messages"}},
	} {
		t.Run(tc.tool+":"+tc.api[0], func(t *testing.T) {
			c := openTest(t)
			if _, err := StoreBinding(c, tools.Find(tc.tool), nil, "work", "", "sk-test", "custom", nil, false, nil, tc.api...); err == nil {
				t.Fatal("invalid protocol accepted")
			}
			cs, err := c.credentials()
			if err != nil || len(cs) != 0 {
				t.Fatalf("invalid protocol wrote credentials: count=%d, err=%v", len(cs), err)
			}
			_, cr := seed(t, c, "https://example.test/v1", "sk-test", "custom")
			if _, err := c.AddBinding(tc.tool, "work", cr.ID, "custom", nil, tc.api...); err == nil {
				t.Fatal("AddBinding accepted invalid protocol")
			}
			b, err := c.AddBinding(tc.tool, "work", cr.ID, "custom", nil)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := c.UpdateBinding(b.ID, "renamed", cr.ID, "custom", nil, tc.api...); err == nil {
				t.Fatal("UpdateBinding accepted invalid protocol")
			}
			saved, found, err := c.BindingByName(tc.tool, "work")
			if err != nil || !found || saved.PiAPI != b.PiAPI {
				t.Fatalf("invalid update changed binding: %v, %v", saved, err)
			}
		})
	}
}

func TestStoreBindingValidatesBeforeWritingCredential(t *testing.T) {
	c := openTest(t)
	tool := &tools.Tool{Name: "claude", DefaultEndpoint: "https://gateway.example/v1"}

	if _, err := StoreBinding(c, tool, nil, "line\nbreak", "", "sk-secret", "model-a", nil, false, nil); err == nil {
		t.Fatal("invalid name was accepted")
	}
	if credentials, err := c.credentials(); err != nil {
		t.Fatal(err)
	} else if len(credentials) != 0 {
		t.Fatalf("failed add stored credentials: %+v", credentials)
	}

	if _, err := StoreBinding(c, tool, nil, "work", "", "sk-secret", "model-a", nil, false, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := StoreBinding(c, tool, nil, "work", "https://other.example/v1", "sk-orphan", "model-b", nil, false, nil); err == nil {
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
	if _, err := StoreBinding(c, tool, &b, "bad name", "", "sk-rotated", "model-a", nil, false, nil); err == nil {
		t.Fatal("renaming to another invalid name was accepted")
	}

	updated, err := StoreBinding(c, tool, &b, b.Name, "", "sk-rotated", "model-b", nil, false, nil)
	if err != nil {
		t.Fatalf("editing without renaming legacy name: %v", err)
	}
	if updated.Name != b.Name {
		t.Fatalf("name changed during edit: got %q, want %q", updated.Name, b.Name)
	}
	if slug, err := c.ModelSlug(updated.ModelID); err != nil || slug != "model-b" {
		t.Fatalf("updated model = %q, err=%v; want model-b", slug, err)
	}

	renamed, err := StoreBinding(c, tool, &updated, "work", "", "sk-rotated", "model-b", nil, false, nil)
	if err != nil {
		t.Fatalf("renaming legacy name: %v", err)
	}
	if renamed.Name != "work" {
		t.Fatalf("renamed name = %q, want work", renamed.Name)
	}
}
