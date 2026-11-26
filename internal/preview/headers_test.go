package preview

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"charon/internal/tools"
)

func TestPiHeaderDryRunDoesNotWriteOrExposeValues(t *testing.T) {
	for _, tc := range []struct {
		name, before string
	}{
		{"new file", ""},
		{"JSON with BOM", "\ufeff" + `{
			"providers": {"charon-alpha": {
				"baseUrl": "https://alpha.example/v1",
				"apiKey": "sk-old-private-key",
				"headers": {"X-Key": "\u0070rivate-old-value"},
				"models": [{"id":"custom","headers":{"X-Model-Key":"private-model-value"}}]
			}}
		}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("USERPROFILE", home)
			t.Setenv("XDG_CONFIG_HOME", t.TempDir())
			path := filepath.Join(home, ".pi", "agent", "models.json")
			if tc.before != "" {
				if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(tc.before), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			diffs, err := Diff(tools.Find("pi"), tools.AuthSpec{
				Endpoint: "https://alpha.example/v1", Key: "sk-new-private-key", Model: "custom",
				Headers: map[string]string{"Authorization": "Bearer private-new-value", "X.Literal": "!private-$TOKEN"},
			})
			if err != nil {
				t.Fatal(err)
			}
			var rendered strings.Builder
			for _, diff := range diffs {
				rendered.WriteString(diff.Render())
			}
			for _, value := range []string{
				"sk-old-private-key", "sk-new-private-key", "private-old-value", `\u0070rivate-old-value`,
				"private-new-value", "private-model-value", "$!private-$$TOKEN",
			} {
				if strings.Contains(rendered.String(), value) {
					t.Fatalf("dry-run exposed a credential/header value: %q", value)
				}
			}
			if !strings.Contains(rendered.String(), `"headers"`) || !strings.Contains(rendered.String(), `"Authorization"`) {
				t.Fatal("masked preview omitted header names")
			}
			after, err := os.ReadFile(path)
			if tc.before == "" {
				if !os.IsNotExist(err) {
					t.Fatal("dry-run created models.json")
				}
			} else if err != nil || !bytes.Equal(after, []byte(tc.before)) {
				t.Fatal("dry-run modified models.json")
			}
			if _, err := os.Stat(filepath.Join(home, ".pi", "agent", "settings.json")); !os.IsNotExist(err) {
				t.Fatal("dry-run created settings.json")
			}
		})
	}
}

func TestHeaderMasksKeepShortSecretChangesVisible(t *testing.T) {
	before, after := []byte("{\"headers\":{\"X-Key\":\"old\"}}\n"), []byte("{\"headers\":{\"X-Key\":\"new\"}}\n")
	for _, diff := range []FileDiff{
		{Before: before, After: after},
		{Before: before},
		{After: after},
		{Before: []byte("\ufeff// comment\n" + string(before))},
	} {
		rendered := diff.Render()
		if strings.Contains(rendered, `"old"`) || strings.Contains(rendered, `"new"`) {
			t.Fatal("short header value was not masked")
		}
		if diff.Before != nil && !strings.Contains(rendered, `-{"headers"`) {
			t.Fatal("masked removal was lost")
		}
		if diff.After != nil && !strings.Contains(rendered, `+{"headers"`) {
			t.Fatal("masked addition was lost")
		}
	}
	if string(before) != "{\"headers\":{\"X-Key\":\"old\"}}\n" || string(after) != "{\"headers\":{\"X-Key\":\"new\"}}\n" {
		t.Fatal("rendering modified the raw diff bytes")
	}
}

func TestCodexClaudeGrokDryRunDoNotExposeSecrets(t *testing.T) {
	cases := []struct {
		name      string
		tool      string
		relPath   string
		before    string
		spec      tools.AuthSpec
		secrets   []string
		keepNames []string
	}{
		{
			name:    "codex toml bearer token",
			tool:    "codex",
			relPath: filepath.Join(".codex", "config.toml"),
			before:  "[model_providers.charon]\nname = \"charon\"\nbase_url = \"https://old.example/v1\"\nwire_api = \"responses\"\nexperimental_bearer_token = \"sk-old-codex-secret\"\n",
			spec:    tools.AuthSpec{Endpoint: "https://new.example/v1", Key: "sk-new-codex-secret", Model: "gpt-5"},
			secrets: []string{"sk-old-codex-secret", "sk-new-codex-secret"},
			keepNames: []string{
				"experimental_bearer_token",
				"https://new.example/v1",
			},
		},
		{
			name:    "grok toml api_key",
			tool:    "grok",
			relPath: filepath.Join(".grok", "config.toml"),
			before:  "[models]\ndefault = \"charon-grok-3\"\n\n[model.\"charon-grok-3\"]\nmodel = \"grok-3\"\nname = \"grok-3\"\nbase_url = \"https://old.example/v1\"\napi_key = \"sk-old-grok-secret\"\n",
			spec:    tools.AuthSpec{Endpoint: "https://new.example/v1", Key: "sk-new-grok-secret", Model: "grok-3"},
			secrets: []string{"sk-old-grok-secret", "sk-new-grok-secret"},
			keepNames: []string{
				"api_key",
				"https://new.example/v1",
			},
		},
		{
			name:    "claude gateway auth token",
			tool:    "claude",
			relPath: filepath.Join(".claude", "settings.json"),
			before:  "{\n  \"env\": {\n    \"ANTHROPIC_BASE_URL\": \"https://old.example\",\n    \"ANTHROPIC_AUTH_TOKEN\": \"sk-old-claude-token\"\n  }\n}\n",
			spec:    tools.AuthSpec{Endpoint: "https://new.example/v1", Key: "sk-new-claude-token", Model: "claude-sonnet-4-6"},
			secrets: []string{"sk-old-claude-token", "sk-new-claude-token"},
			keepNames: []string{
				"ANTHROPIC_AUTH_TOKEN",
				"ANTHROPIC_BASE_URL",
			},
		},
		{
			name:    "claude official api key and short approval suffix",
			tool:    "claude",
			relPath: filepath.Join(".claude", "settings.json"),
			before:  "{\n  \"env\": {\n    \"ANTHROPIC_API_KEY\": \"sk-old-short\"\n  },\n  \"customApiKeyResponses\": {\n    \"approved\": [\"sk-old-short\"],\n    \"disabled\": []\n  }\n}\n",
			spec:    tools.AuthSpec{Endpoint: "https://api.anthropic.com", Key: "sk-new-short", Model: "claude-sonnet-4-6"},
			secrets: []string{"sk-old-short", "sk-new-short"},
			keepNames: []string{
				"ANTHROPIC_API_KEY",
				"customApiKeyResponses",
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("USERPROFILE", home)
			t.Setenv("XDG_CONFIG_HOME", t.TempDir())
			path := filepath.Join(home, tc.relPath)
			if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(tc.before), 0o600); err != nil {
				t.Fatal(err)
			}
			diffs, err := Diff(tools.Find(tc.tool), tc.spec)
			if err != nil {
				t.Fatal(err)
			}
			var rendered strings.Builder
			for _, d := range diffs {
				rendered.WriteString(d.Render())
			}
			out := rendered.String()
			for _, sec := range tc.secrets {
				if strings.Contains(out, sec) {
					t.Fatalf("dry-run exposed secret %q in:\n%s", sec, out)
				}
			}
			for _, keep := range tc.keepNames {
				if !strings.Contains(out, keep) {
					t.Fatalf("dry-run omitted expected field %q in:\n%s", keep, out)
				}
			}
			after, err := os.ReadFile(path)
			if err != nil || string(after) != tc.before {
				t.Fatalf("dry-run modified %s", path)
			}
		})
	}
}

func TestTOMLSecretMasksKeepShortSecretChangesVisible(t *testing.T) {
	before := []byte("[model.charon-m]\napi_key = \"old\"\n")
	after := []byte("[model.charon-m]\napi_key = \"new\"\n")
	d := FileDiff{Path: "config.toml", Before: before, After: after}
	rendered := d.Render()
	if strings.Contains(rendered, `"old"`) || strings.Contains(rendered, `"new"`) {
		t.Fatalf("short TOML secret was not masked:\n%s", rendered)
	}
	if !strings.Contains(rendered, "-api_key = \"••••\"") || !strings.Contains(rendered, "+api_key = \"••••\"") {
		t.Fatalf("masked TOML +/- lines lost:\n%s", rendered)
	}
}
