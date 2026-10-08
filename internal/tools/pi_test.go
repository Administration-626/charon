package tools

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"charon/internal/models"
)

func readPiProvider(t *testing.T, home string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(home, ".pi", "agent", "models.json"))
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		Providers map[string]map[string]any `json:"providers"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatal(err)
	}
	provider, ok := cfg.Providers[managedProvider]
	if !ok {
		t.Fatal("missing charon provider")
	}
	return provider
}

func legacyPiExtension(t *testing.T) []byte {
	t.Helper()
	data, err := piExtensionContent(piProviderConfig{
		Name: "charon", BaseURL: "https://old.example/v1", APIKey: "sk-old", API: "openai-completions",
		Models: []piModel{{
			ID: "legacy-model", Name: "Legacy model", Reasoning: true, Input: []string{"text"},
			ContextWindow: 128000, MaxTokens: 32000,
			Cost: piCost{Input: 1, Output: 2},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestPiBuildModelsThinkingLevels(t *testing.T) {
	none, xhigh, maxLevel, maximum := "none", "xhigh", "max", "maximum"
	defaultLevels := map[string]*string{"off": &none, "xhigh": &xhigh, "max": &maxLevel}
	manualLevels := map[string]*string{"max": &maximum, "xhigh": nil}
	for _, tc := range []struct {
		name      string
		spec      ModelSpec
		reasoning bool
		levels    map[string]*string
	}{
		{"luna", ModelSpec{Slug: "gpt-6-luna"}, true, defaultLevels},
		{"sol", ModelSpec{Slug: "gpt-6-sol"}, true, defaultLevels},
		{"astra", ModelSpec{Slug: "gpt-6-astra"}, true, defaultLevels},
		{"sol 6.1", ModelSpec{Slug: "gpt-6.1-sol"}, true, defaultLevels},
		{"namespaced", ModelSpec{Slug: "openai/gpt-6-luna:free"}, true, defaultLevels},
		{"manual map wins", ModelSpec{Slug: "gpt-6-luna", Effort: "max", ThinkingLevelMap: manualLevels}, true, manualLevels},
		{"explicit empty map", ModelSpec{Slug: "gpt-6-luna", ThinkingLevelMap: map[string]*string{}}, true, map[string]*string{}},
		{"custom max", ModelSpec{Slug: "custom", Effort: "max"}, true, defaultLevels},
		{"custom xhigh", ModelSpec{Slug: "custom", Effort: "xhigh"}, true, defaultLevels},
		{"custom mapping", ModelSpec{Slug: "custom", ThinkingLevelMap: manualLevels}, true, manualLevels},
		{"older reasoning model", ModelSpec{Slug: "o3-mini"}, true, defaultLevels},
		{"known non-reasoning model", ModelSpec{Slug: "gpt-4o"}, true, defaultLevels},
		{"unknown model", ModelSpec{Slug: "custom"}, true, defaultLevels},
		{"kimi k3", ModelSpec{Slug: "moonshotai/kimi-k3"}, true, defaultLevels},
		{"glm 5.3", ModelSpec{Slug: "z-ai/glm-5.3"}, true, defaultLevels},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before, err := json.Marshal(tc.spec)
			if err != nil {
				t.Fatal(err)
			}
			got := piBuildModels([]ModelSpec{tc.spec})
			if len(got) != 1 || got[0].Reasoning != tc.reasoning || !reflect.DeepEqual(got[0].ThinkingLevelMap, tc.levels) {
				t.Fatalf("models = %+v, want reasoning=%v, levels=%v", got, tc.reasoning, tc.levels)
			}
			after, err := json.Marshal(tc.spec)
			if err != nil || !bytes.Equal(before, after) {
				t.Fatal("building Pi models mutated the input spec")
			}
		})
	}
}

func TestPiSingleModelKeepsExplicitEffort(t *testing.T) {
	home := sandboxHome(t)
	if err := Find("pi").ApplyAuth(AuthSpec{
		Endpoint: "https://example.test/v1", Key: "sk-test", Model: "custom", Effort: "max",
	}); err != nil {
		t.Fatal(err)
	}
	entry := readPiProvider(t, home)["models"].([]any)[0].(map[string]any)
	levels, _ := entry["thinkingLevelMap"].(map[string]any)
	if entry["reasoning"] != true || levels["max"] != "max" {
		t.Fatalf("single model lost explicit thinking capability: %#v", entry)
	}
	if readPiProvider(t, home)["api"] != "openai-completions" {
		t.Fatalf("Pi provider api = %#v, want openai-completions", readPiProvider(t, home)["api"])
	}
}

func TestPiEndpointTypes(t *testing.T) {
	for _, api := range []string{"", "openai-completions", "openai-responses", "anthropic-messages", "google-generative-ai"} {
		t.Run("api="+api, func(t *testing.T) {
			home := sandboxHome(t)
			tool := Find("pi")
			spec := AuthSpec{Endpoint: "https://example.test/v1", Key: "sk-test", Model: "custom", PiAPI: api}
			if err := tool.ApplyAuth(spec); err != nil {
				t.Fatal(err)
			}
			want := api
			if want == "" {
				want = "openai-completions"
			}
			if got := readPiProvider(t, home)["api"]; got != want {
				t.Fatalf("api = %v, want %s", got, want)
			}
			paths := []string{
				filepath.Join(home, ".pi", "agent", "models.json"),
				filepath.Join(home, ".pi", "agent", "settings.json"),
			}
			for _, invalid := range []string{"openai-responses-compact", "unknown"} {
				before := make([][]byte, len(paths))
				for i, path := range paths {
					before[i], _ = os.ReadFile(path)
				}
				spec.PiAPI = invalid
				if err := tool.ApplyAuth(spec); err == nil {
					t.Fatalf("accepted unsupported API %q", invalid)
				}
				for i, path := range paths {
					after, err := os.ReadFile(path)
					if err != nil || !bytes.Equal(before[i], after) {
						t.Fatalf("invalid API changed %s: %v", path, err)
					}
					info, err := os.Stat(path)
					if err != nil || info.Mode().Perm() != 0o600 {
						t.Fatalf("unsafe permissions for %s: %v", path, err)
					}
				}
			}
		})
	}
}

func TestPiAuthUpdatePreservesFullModelEntries(t *testing.T) {
	home := sandboxHome(t)
	dir := filepath.Join(home, ".pi", "agent")
	writeFile(t, filepath.Join(dir, "models.json"), `{
		"customSetting": true,
		"providers": {
			"mine": {"baseUrl": "https://mine.example", "custom": [1, 2]},
			"charon": {"models": [{
				"id": "custom", "name": "Custom model", "contextWindow": 128000,
				"reasoning": true, "thinkingLevelMap": {"high": "maximum", "off": null},
				"maxTokens": 32000, "input": ["text"], "cost": {"input": 1.2},
				"samplingParams": {"temperature": 0.7}, "compat": {"supportsStore": false}
			}]}
		}
	}`)
	writeFile(t, filepath.Join(dir, "settings.json"),
		`{"theme":"custom","defaultThinkingLevel":"high","compaction":{"enabled":false}}`)
	auth := `{"mine":{"type":"oauth","refresh":"fake-refresh"}}`
	writeFile(t, filepath.Join(dir, "auth.json"), auth)
	before := readPiProvider(t, home)["models"]

	c := Find("pi")
	if err := c.ApplyAuth(AuthSpec{Endpoint: "https://new.example/v1", Key: "sk-rotated", Model: "custom"}); err != nil {
		t.Fatal(err)
	}
	got := readPiProvider(t, home)
	if !reflect.DeepEqual(got["models"], before) {
		t.Fatalf("model metadata changed: got %#v, want %#v", got["models"], before)
	}
	if got["baseUrl"] != "https://new.example/v1" || got["apiKey"] != "sk-rotated" {
		t.Fatal("endpoint/key not updated")
	}
	cfg, err := loadJSONMap(filepath.Join(dir, "models.json"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg["customSetting"] != true || !reflect.DeepEqual(subMap(cfg, "providers")["mine"],
		map[string]any{"baseUrl": "https://mine.example", "custom": []any{float64(1), float64(2)}}) {
		t.Fatal("unrelated configuration changed")
	}
	settings, err := loadJSONMap(filepath.Join(dir, "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	if settings["theme"] != "custom" || settings["defaultThinkingLevel"] != "high" ||
		!reflect.DeepEqual(settings["compaction"], map[string]any{"enabled": false}) {
		t.Fatalf("user preferences changed: %#v", settings)
	}
	data, err := os.ReadFile(filepath.Join(dir, "auth.json"))
	if err != nil || string(data) != auth {
		t.Fatal("Pi login credentials changed")
	}
	for _, file := range []string{"models.json", "settings.json"} {
		info, err := os.Stat(filepath.Join(dir, file))
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Errorf("%s mode = %o, want 600", file, info.Mode().Perm())
		}
	}
}

func TestPiMigratesOnlyGeneratedExtension(t *testing.T) {
	for _, source := range []string{"legacy", "native", "explicit"} {
		t.Run(source, func(t *testing.T) {
			home := sandboxHome(t)
			dir := filepath.Join(home, ".pi", "agent")
			extensionPath := filepath.Join(dir, "extensions", "charon.ts")
			legacy := legacyPiExtension(t)
			writeFile(t, extensionPath, string(legacy))
			writeFile(t, filepath.Join(dir, "settings.json"), `{"defaultProvider":"charon"}`)
			spec := AuthSpec{Endpoint: "https://new.example/v1", Key: "sk-new", Model: "legacy-model"}
			want := "legacy-model"
			if source == "native" {
				writeFile(t, filepath.Join(dir, "models.json"),
					`{"providers":{"charon":{"models":[{"id":"native-model","contextWindow":64000,"custom":true}]}}}`)
				want, spec.Model = "native-model", "native-model"
			}
			if source == "explicit" {
				want, spec.Model = "new-model", "new-model"
				spec.Models = []ModelSpec{{Slug: "new-model", ContextWindow: 256000}}
			}
			c := Find("pi")
			if source == "legacy" {
				info, err := c.Describe()
				if err != nil || info.Endpoint != "https://old.example/v1" {
					t.Fatalf("legacy Describe = %+v, %v", info, err)
				}
			}
			if err := c.ApplyAuth(spec); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Lstat(extensionPath); !os.IsNotExist(err) {
				t.Fatalf("legacy extension remains: %v", err)
			}
			got := readPiProvider(t, home)
			entries := got["models"].([]any)
			if len(entries) != 1 || entries[0].(map[string]any)["id"] != want {
				t.Fatalf("models after migration = %#v", entries)
			}
			if source == "legacy" {
				cfg, _ := piParseExtension(legacy)
				actual, _ := json.Marshal(entries)
				var actualModels []piModel
				if err := json.Unmarshal(actual, &actualModels); err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(cfg.Models, actualModels) {
					t.Fatal("legacy model metadata changed")
				}
			}
			info, err := c.Describe()
			if err != nil || info.Endpoint != spec.Endpoint || info.Model != want {
				t.Fatalf("Describe after migration = %+v, %v", info, err)
			}
			// Migration is idempotent; no extension is recreated.
			if err := c.ApplyAuth(spec); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(extensionPath); !os.IsNotExist(err) {
				t.Fatal("extension recreated")
			}
		})
	}
}

func TestPiPreflightLeavesFilesUntouched(t *testing.T) {
	for _, tc := range []struct {
		name, file, content string
	}{
		{"broken settings", "settings.json", "{"},
		{"null settings", "settings.json", "null"},
		{"array settings", "settings.json", "[]"},
		{"broken models", "models.json", "{"},
		{"null models", "models.json", "null"},
		{"array providers", "models.json", `{"providers":[]}`},
		{"null providers", "models.json", `{"providers":null}`},
		{"scalar provider", "models.json", `{"providers":{"charon":"mine"}}`},
		{"scalar models", "models.json", `{"providers":{"charon":{"models":"mine"}}}`},
		{"unknown extension", "extensions/charon.ts", "export default function () {}"},
		{"modified extension", "extensions/charon.ts", string(legacyPiExtension(t)) + "\nconsole.log('user code');\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := sandboxHome(t)
			dir := filepath.Join(home, ".pi", "agent")
			writeFile(t, filepath.Join(dir, "settings.json"), `{"theme":"keep"}`)
			writeFile(t, filepath.Join(dir, "extensions", "charon.ts"), string(legacyPiExtension(t)))
			writeFile(t, filepath.Join(dir, tc.file), tc.content)
			files := []string{"settings.json", "models.json", "extensions/charon.ts"}
			before := make(map[string][]byte)
			for _, file := range files {
				before[file], _ = os.ReadFile(filepath.Join(dir, file))
			}
			err := Find("pi").ApplyAuth(AuthSpec{
				Endpoint: "https://new.example/v1", Key: "sk-test", Model: "custom",
			})
			if err == nil {
				t.Fatal("expected preflight refusal")
			}
			for _, file := range files {
				data, readErr := os.ReadFile(filepath.Join(dir, file))
				if before[file] == nil && !os.IsNotExist(readErr) {
					t.Errorf("%s created during failed preflight", file)
				}
				if !bytes.Equal(data, before[file]) {
					t.Errorf("%s changed during failed preflight", file)
				}
			}
		})
	}
}

func TestPiRefusesSymlinkedExtension(t *testing.T) {
	home := sandboxHome(t)
	target := filepath.Join(t.TempDir(), "user.ts")
	data := legacyPiExtension(t)
	writeFile(t, target, string(data))
	dir := filepath.Join(home, ".pi", "agent")
	if err := os.MkdirAll(filepath.Join(dir, "extensions"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(dir, "extensions", "charon.ts")); err != nil {
		t.Fatal(err)
	}
	if err := Find("pi").ApplyAuth(AuthSpec{Model: "new"}); err == nil {
		t.Fatal("expected refusal to migrate symlink")
	}
	after, err := os.ReadFile(target)
	if err != nil || !bytes.Equal(data, after) {
		t.Fatal("symlink target changed")
	}
}

func TestPiNewModelsHavePositiveWindowsAndReplacePicker(t *testing.T) {
	home := sandboxHome(t)
	c := Find("pi")
	spec := AuthSpec{Endpoint: "https://gateway.example/v1", Key: "sk-test", Model: "custom"}
	if err := c.ApplyAuth(spec); err != nil {
		t.Fatal(err)
	}
	entries := readPiProvider(t, home)["models"].([]any)
	if len(entries) != 1 || entries[0].(map[string]any)["contextWindow"] != float64(models.DefaultContextWindow("custom")) {
		t.Fatalf("new model has invalid window: %#v", entries)
	}
	spec.Model, spec.Models = "other", []ModelSpec{{Slug: "other", ContextWindow: 64000}}
	if err := c.ApplyAuth(spec); err != nil {
		t.Fatal(err)
	}
	entries = readPiProvider(t, home)["models"].([]any)
	if len(entries) != 1 || entries[0].(map[string]any)["id"] != "other" {
		t.Fatalf("replacement retained previous models: %#v", entries)
	}
	// A non-nil empty list is explicit, unlike nil which preserves the picker.
	spec.Models = []ModelSpec{}
	if err := c.ApplyAuth(spec); err != nil {
		t.Fatal(err)
	}
	if entries := readPiProvider(t, home)["models"].([]any); len(entries) != 0 {
		t.Fatalf("empty list retained previous models: %#v", entries)
	}
	spec.Models = nil
	if err := c.ApplyAuth(spec); err != nil {
		t.Fatal(err)
	}
	if entries := readPiProvider(t, home)["models"].([]any); len(entries) != 0 {
		t.Fatalf("nil should preserve the existing empty list: %#v", entries)
	}
	for _, path := range []string{".pi", ".pi/agent"} {
		info, err := os.Stat(filepath.Join(home, path))
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o700 {
			t.Errorf("%s mode = %o, want 700", path, info.Mode().Perm())
		}
	}
}

func TestPiDetectsNativeModelsFile(t *testing.T) {
	home := sandboxHome(t)
	writeFile(t, filepath.Join(home, ".pi", "agent", "models.json"), `{}`)
	if !Find("pi").Detected() {
		t.Fatal("models.json alone should detect Pi")
	}
}

func TestPiEscapesLiteralKey(t *testing.T) {
	home := sandboxHome(t)
	if err := Find("pi").ApplyAuth(AuthSpec{
		Endpoint: "https://example.test/v1", Key: "!not-a-command-$KEY-${OTHER}", Model: "custom",
	}); err != nil {
		t.Fatal(err)
	}
	key := readPiProvider(t, home)["apiKey"].(string)
	if key != "$!not-a-command-$$KEY-$${OTHER}" || strings.HasPrefix(key, "!") {
		t.Fatalf("literal key incorrectly escaped: %q", key)
	}
}
