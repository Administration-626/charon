package tools

import (
	"os"
	"path/filepath"
	"testing"

	"charon/internal/models"
)

func TestCodexModelCatalogLifecycle(t *testing.T) {
	home := sandboxHome(t)
	configPath := filepath.Join(home, ".codex", "config.toml")
	catalogPath := filepath.Join(home, ".codex", "custom_models.json")
	writeFile(t, configPath, "model_context_window = 1048576\nservice_tier = 'fast'\n")
	writeFile(t, catalogPath, `{"metadata":"keep","models":[
		{"slug":"glm-5.3-flash","context_window":258048,"visibility":"hide","supported_in_api":false,"use_responses_lite":true,"tool_mode":"code_mode_only"},
		{"slug":"stale-model","context_window":272000}
	]}`)
	registered := []ModelSpec{
		{Slug: "glm-5.3-flash", ContextWindow: 256000, Effort: "high"},
		{Slug: "gpt-5.6-luna", ContextWindow: 512000, Effort: "xhigh"},
	}
	for _, test := range []struct {
		name     string
		auth     AuthSpec
		want     []ModelSpec
		selected string
	}{
		{
			name: "register binding",
			auth: AuthSpec{Model: registered[0].Slug, Models: registered},
			want: registered, selected: registered[0].Slug,
		},
		{
			name: "rotate key without model list",
			auth: AuthSpec{}, want: registered, selected: registered[0].Slug,
		},
		{
			name:     "change default without model list",
			auth:     AuthSpec{Model: registered[1].Slug, Effort: "max"},
			want:     []ModelSpec{registered[0], {Slug: registered[1].Slug, ContextWindow: 512000, Effort: "max"}},
			selected: registered[1].Slug,
		},
		{
			name:     "replace with one-model binding",
			auth:     AuthSpec{Model: registered[0].Slug, Models: []ModelSpec{{Slug: registered[0].Slug, ContextWindow: 128000}}},
			want:     []ModelSpec{{Slug: registered[0].Slug, ContextWindow: 128000, Effort: "medium"}},
			selected: registered[0].Slug,
		},
		{
			name:     "replace with unknown model",
			auth:     AuthSpec{Model: "unknown", Models: []ModelSpec{{Slug: "unknown"}}},
			want:     []ModelSpec{{Slug: "unknown", ContextWindow: models.DefaultContextCeiling, Effort: "medium"}},
			selected: "unknown",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			auth := test.auth
			auth.Endpoint = "https://gateway.example/v1"
			auth.Key = "sk-test"
			if err := Find("codex").ApplyAuth(auth); err != nil {
				t.Fatal(err)
			}
			catalog, err := loadJSONMap(catalogPath)
			if err != nil {
				t.Fatal(err)
			}
			if catalog["metadata"] != "keep" {
				t.Fatal("unrelated catalog metadata was lost")
			}
			entries, ok := catalog["models"].([]any)
			if !ok || len(entries) != len(test.want) {
				t.Fatalf("catalog model count = %d, want %d", len(entries), len(test.want))
			}
			for index, expected := range test.want {
				entry := entries[index].(map[string]any)
				if entry["slug"] != expected.Slug || entry["context_window"] != float64(expected.ContextWindow) || entry["max_context_window"] != float64(expected.ContextWindow) || entry["default_reasoning_level"] != expected.Effort {
					t.Fatalf("model entry = %#v, want %+v", entry, expected)
				}
				if entry["visibility"] != "list" || entry["supported_in_api"] != true || entry["supports_reasoning_effort_updates"] != true {
					t.Fatalf("model must be selectable with effort support: %#v", entry)
				}
				if entry["use_responses_lite"] != nil || entry["tool_mode"] != nil {
					t.Fatal("gateway-incompatible tool fields survived")
				}
				levels, ok := entry["supported_reasoning_levels"].([]any)
				if !ok || len(levels) != len(codexEfforts()) {
					t.Fatalf("reasoning levels = %#v", levels)
				}
			}
			cfg, err := loadTOMLMap(configPath)
			if err != nil {
				t.Fatal(err)
			}
			if cfg["model"] != test.selected || cfg["model_catalog_json"] != catalogPath || cfg["service_tier"] != "fast" {
				t.Fatal("default model, catalog path, or unrelated config changed unexpectedly")
			}
			if len(test.want) > 1 && cfg["model_context_window"] != nil {
				t.Fatal("global context window would override per-model windows")
			}
			if len(test.want) == 1 && cfg["model_context_window"] != int64(test.want[0].ContextWindow) {
				t.Fatal("single-model context pin does not match the catalog")
			}
			for _, path := range []string{catalogPath, configPath} {
				info, err := os.Stat(path)
				if err != nil {
					t.Fatal(err)
				}
				if info.Mode().Perm() != 0o600 {
					t.Fatalf("%s permissions = %o", path, info.Mode().Perm())
				}
			}
		})
	}
}
