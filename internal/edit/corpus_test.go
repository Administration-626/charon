package edit

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	toml "github.com/pelletier/go-toml/v2"
	"github.com/tidwall/jsonc"
)

var updateGolden = flag.Bool("update", false, "rewrite edit golden files")

// TestEditCorpus applies the edits charon really makes to fixtures shaped like
// each tool's config, then checks the golden bytes, that the result still
// parses, that user content and comments survive, and that a second apply is a
// no-op.
func TestEditCorpus(t *testing.T) {
	cases := []struct {
		name     string
		edit     func(t *testing.T, path string)
		validate func(t *testing.T, out []byte)
		contains []string
	}{
		{
			name:     "codex.toml",
			validate: validateTOMLFile,
			contains: []string{"# Codex config", "model_reasoning_effort = \"medium\"   # keep this", "[profiles.fast]", "name = \"OpenAI\""},
			edit: func(t *testing.T, path string) {
				t.Helper()
				mustEdit(t, PatchTOMLTop(path,
					[]KV{
						{Path: "model", Value: "gpt-5.1"},
						{Path: "model_provider", Value: "charon"},
						{Path: "model_reasoning_effort", Value: "medium"},
					}, nil))
				mustEdit(t, SetTOMLTables(path, []string{"model_providers.charon"}, []Table{
					{Name: "model_providers.charon", KVs: []KV{
						{Path: "name", Value: "charon"},
						{Path: "base_url", Value: "https://gw.example.com/v1"},
						{Path: "experimental_bearer_token", Value: "sk-new"},
					}},
					{Name: "model_providers.charon-work", KVs: []KV{
						{Path: "name", Value: "work"},
						{Path: "base_url", Value: "https://work.example.com/v1"},
					}},
				}))
			},
		},
		{
			name:     "claude.json",
			validate: validateJSONFile,
			contains: []string{`"KEEP_ME": "1"`, `"Bash(ls:*)"`, `"ANTHROPIC_BASE_URL": "https://gw.example.com"`},
			edit: func(t *testing.T, path string) {
				t.Helper()
				mustEdit(t, ApplyJSON(path,
					[]KV{
						{Path: "env.ANTHROPIC_BASE_URL", Value: "https://gw.example.com"},
						{Path: "env.ANTHROPIC_API_KEY", Value: "sk-new"},
						{Path: "model", Value: "claude-opus-4-1"},
					},
					[]string{"env.ANTHROPIC_AUTH_TOKEN"},
				))
			},
		},
		{
			name:     "opencode.jsonc",
			validate: validateJSONFile,
			contains: []string{"// shared config", `"theme": "dark"`, `"mine"`, `"new-model": {}`},
			edit: func(t *testing.T, path string) {
				t.Helper()
				mustEdit(t, SetJSON(path,
					KV{Path: "provider.charon.options.baseURL", Value: "https://gw.example.com/v1"},
					KV{Path: "provider.charon.options.apiKey", Value: "sk-new"},
					KV{Path: "provider.charon.models", Value: map[string]any{"new-model": map[string]any{}}},
				))
			},
		},
		{
			name:     "grok.toml",
			validate: validateTOMLFile,
			contains: []string{"# grok config", "[model.mine]", "[models]", `[model."charon-a.b"]`, "default = \"charon-work\""},
			edit: func(t *testing.T, path string) {
				t.Helper()
				mustEdit(t, SetTOMLKey(path, "models", "default", "charon-work"))
				mustEdit(t, SetTOMLTables(path, []string{"model.charon-"}, []Table{
					{Name: "model.charon-work", KVs: []KV{
						{Path: "name", Value: "work"},
						{Path: "base_url", Value: "https://work.example.com/v1"},
						{Path: "api_key", Value: "sk-new"},
					}},
					{Name: SpellTOMLTableName("model", "charon-a.b"), KVs: []KV{
						{Path: "name", Value: "dotted"},
					}},
				}))
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			src, err := os.ReadFile(filepath.Join("testdata", c.name))
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(t.TempDir(), c.name)
			if err := os.WriteFile(path, src, 0o600); err != nil {
				t.Fatal(err)
			}
			c.edit(t, path)

			out, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			c.validate(t, out)
			for _, want := range c.contains {
				if !bytes.Contains(out, []byte(want)) {
					t.Errorf("missing %q after edit:\n%s", want, out)
				}
			}

			golden := filepath.Join("testdata", c.name+".golden")
			if *updateGolden {
				if err := os.WriteFile(golden, out, 0o644); err != nil {
					t.Fatal(err)
				}
			} else {
				want, err := os.ReadFile(golden)
				if err != nil {
					t.Fatalf("%v (run go test ./internal/edit -update)", err)
				}
				if !bytes.Equal(out, want) {
					t.Errorf("output differs from %s:\n--- got ---\n%s\n--- want ---\n%s", golden, out, want)
				}
			}

			// Applying the same edit again must not change a byte.
			again := filepath.Join(t.TempDir(), c.name)
			if err := os.WriteFile(again, out, 0o600); err != nil {
				t.Fatal(err)
			}
			c.edit(t, again)
			out2, err := os.ReadFile(again)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(out, out2) {
				t.Errorf("edit is not idempotent:\n--- first ---\n%s\n--- second ---\n%s", out, out2)
			}
		})
	}
}

func mustEdit(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func validateJSONFile(t *testing.T, out []byte) {
	t.Helper()
	plain := jsonc.ToJSONInPlace(bytes.Clone(TrimBOM(out)))
	var m map[string]any
	if err := json.Unmarshal(plain, &m); err != nil {
		t.Fatalf("edited file is not valid JSON: %v\n%s", err, out)
	}
}

func validateTOMLFile(t *testing.T, out []byte) {
	t.Helper()
	var m map[string]any
	if err := toml.Unmarshal(TrimBOM(out), &m); err != nil {
		t.Fatalf("edited file is not valid TOML: %v\n%s", err, out)
	}
}

// TestDeleteAllJSONKeys keeps the delete path honest: every key can go in any
// order, and the file stays valid with the keys that were not named intact.
func TestDeleteAllJSONKeys(t *testing.T) {
	src := []byte("{\n  \"a\": 1,\n  \"b\": {\n    \"x\": 2,\n    \"y\": 3\n  },\n  \"c\": 4\n}\n")
	orders := [][]string{
		{"a", "b.x", "b.y", "c"},
		{"c", "b.y", "b.x", "a"},
		{"b.x", "a", "c", "b.y"},
	}
	for _, keys := range orders {
		path := filepath.Join(t.TempDir(), "s.json")
		if err := os.WriteFile(path, src, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := DelJSON(path, keys...); err != nil {
			t.Fatal(err)
		}
		out, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var m map[string]any
		plain := jsonc.ToJSONInPlace(bytes.Clone(TrimBOM(out)))
		if err := json.Unmarshal(plain, &m); err != nil {
			t.Fatalf("delete order %v produced invalid JSON: %v\n%s", keys, err, out)
		}
		// b is only emptied, never named for deletion, so it stays as {}.
		if len(m) != 1 || m["b"] == nil {
			t.Errorf("delete order %v left %v:\n%s", keys, m, out)
		}
		if strings.Contains(string(out), "\"a\"") {
			t.Errorf("deleted key a still present:\n%s", out)
		}
	}
}
