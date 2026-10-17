package tools

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"charon/internal/models"
)

func readPiProvider(t *testing.T, home string) map[string]any {
	t.Helper()
	providers := readPiProviders(t, home)
	if provider, ok := providers[managedProvider]; ok {
		return provider // written by an older charon
	}
	var found map[string]any
	for name, provider := range providers {
		if !strings.HasPrefix(name, managedProvider+"-") {
			continue
		}
		if found != nil {
			t.Fatalf("several charon providers, pick one by name: %v", piProviderNames(providers))
		}
		found = provider
	}
	if found == nil {
		t.Fatal("missing charon provider")
	}
	return found
}

// piDefaultProvider is the provider key charon made default in settings.json:
// the endpoint-keyed name of the binding's own entry.
func piDefaultProvider(t *testing.T, home string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(home, ".pi", "agent", "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	var s struct {
		DefaultProvider string `json:"defaultProvider"`
	}
	if err := json.Unmarshal(data, &s); err != nil {
		t.Fatal(err)
	}
	if s.DefaultProvider == "" {
		t.Fatal("settings.json has no defaultProvider")
	}
	return s.DefaultProvider
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
	// Unknown models get the generic extended levels; known vendors get their
	// curated preset (see internal/models/thinking.go).
	defaultLevels := map[string]*string{"off": &none, "xhigh": &xhigh, "max": &maxLevel}
	presetGPT6 := map[string]*string{"off": &none, "minimal": nil, "low": strPtr("low"), "medium": strPtr("medium"), "high": strPtr("high"), "xhigh": &xhigh, "max": &maxLevel}
	lockedGPT6 := map[string]*string{"off": nil, "minimal": nil, "low": strPtr("low"), "medium": strPtr("medium"), "high": strPtr("high"), "xhigh": &xhigh, "max": &maxLevel}
	kimiK3 := map[string]*string{"off": nil, "minimal": nil, "low": strPtr("low"), "medium": nil, "high": strPtr("high"), "xhigh": nil, "max": &maxLevel}
	glm53 := map[string]*string{"off": nil, "minimal": nil, "low": strPtr("low"), "medium": nil, "high": strPtr("high"), "xhigh": nil, "max": &maxLevel}
	o3levels := map[string]*string{"off": nil, "minimal": nil, "low": strPtr("low"), "medium": strPtr("medium"), "high": strPtr("high"), "xhigh": nil, "max": nil}
	manualLevels := map[string]*string{"max": &maximum, "xhigh": nil}
	for _, tc := range []struct {
		name      string
		spec      ModelSpec
		reasoning bool
		levels    map[string]*string
	}{
		{"luna", ModelSpec{Slug: "gpt-6-luna"}, true, presetGPT6},
		{"sol", ModelSpec{Slug: "gpt-6-sol"}, true, presetGPT6},
		{"astra", ModelSpec{Slug: "gpt-6-astra"}, true, lockedGPT6},
		{"sol 6.1", ModelSpec{Slug: "gpt-6.1-sol"}, true, lockedGPT6},
		{"namespaced", ModelSpec{Slug: "openai/gpt-6-luna:free"}, true, presetGPT6},
		{"manual map wins", ModelSpec{Slug: "gpt-6-luna", Effort: "max", ThinkingLevelMap: manualLevels}, true, manualLevels},
		{"explicit empty map", ModelSpec{Slug: "gpt-6-luna", ThinkingLevelMap: map[string]*string{}}, true, map[string]*string{}},
		{"custom max", ModelSpec{Slug: "custom", Effort: "max"}, true, defaultLevels},
		{"custom xhigh", ModelSpec{Slug: "custom", Effort: "xhigh"}, true, defaultLevels},
		{"custom mapping", ModelSpec{Slug: "custom", ThinkingLevelMap: manualLevels}, true, manualLevels},
		{"older reasoning model", ModelSpec{Slug: "o3-mini"}, true, o3levels},
		{"known non-reasoning model", ModelSpec{Slug: "gpt-4o"}, true, defaultLevels},
		{"unknown model", ModelSpec{Slug: "custom"}, true, defaultLevels},
		{"kimi k3", ModelSpec{Slug: "moonshotai/kimi-k3"}, true, kimiK3},
		{"glm 5.3", ModelSpec{Slug: "z-ai/glm-5.3"}, true, glm53},
		{"glm 4.7", ModelSpec{Slug: "glm-4.7"}, true, map[string]*string{"off": &none}},
		{"deepseek v4 pro", ModelSpec{Slug: "deepseek-v4-pro"}, true, map[string]*string{"minimal": nil, "low": nil, "medium": nil, "high": strPtr("high"), "max": &maxLevel}},
		{"gpt 5.5", ModelSpec{Slug: "openai/gpt-5.5"}, true, map[string]*string{"off": &none, "minimal": nil, "low": strPtr("low"), "medium": strPtr("medium"), "high": strPtr("high"), "xhigh": &xhigh, "max": nil}},
		// Toggle-only APIs (MiMo) and effort-rejecting vendors (Grok) stay mapless.
		{"mimo toggle-only", ModelSpec{Slug: "xiaomi/mimo-v2.5"}, true, nil},
		{"grok 4.5", ModelSpec{Slug: "grok-4.5"}, true, map[string]*string{"off": nil, "minimal": nil, "low": strPtr("low"), "medium": strPtr("medium"), "high": strPtr("high"), "xhigh": nil, "max": nil}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before, err := json.Marshal(tc.spec)
			if err != nil {
				t.Fatal(err)
			}
			got := piBuildModels([]ModelSpec{tc.spec}, "", "")
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

// strPtr is a test helper for non-nil string map values.
func strPtr(s string) *string { return &s }

func TestPiCompatPresetProjectsVendorFormat(t *testing.T) {
	home := sandboxHome(t)
	if err := Find("pi").ApplyAuth(AuthSpec{
		Endpoint: "https://gateway.example/v1", Key: "sk-test",
		Models: []ModelSpec{{Slug: "deepseek-v4-pro"}, {Slug: "glm-5.3"}, {Slug: "custom"}, {Slug: "gpt-6-luna"}},
	}); err != nil {
		t.Fatal(err)
	}
	var config struct {
		Providers map[string]struct {
			Models []struct {
				ID     string         `json:"id"`
				Compat map[string]any `json:"compat"`
			} `json:"models"`
		} `json:"providers"`
	}
	data, err := os.ReadFile(filepath.Join(home, ".pi", "agent", "models.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &config); err != nil {
		t.Fatal(err)
	}
	compat := map[string]map[string]any{}
	for _, m := range config.Providers[piDefaultProvider(t, home)].Models {
		compat[m.ID] = m.Compat
	}
	deepseek := compat["deepseek-v4-pro"]
	if deepseek["thinkingFormat"] != "deepseek" || deepseek["maxTokensField"] != "max_tokens" ||
		deepseek["requiresReasoningContentOnAssistantMessages"] != true ||
		deepseek["supportsStore"] != false || deepseek["supportsDeveloperRole"] != false {
		t.Fatalf("deepseek compat = %#v", deepseek)
	}
	glm := compat["glm-5.3"]
	if glm["thinkingFormat"] != "zai" || glm["supportsReasoningEffort"] != true || glm["maxTokensField"] != "max_tokens" {
		t.Fatalf("glm compat = %#v", glm)
	}
	// Unknown models and plain-effort vendors carry no compat object.
	if compat["custom"] != nil {
		t.Fatalf("custom compat = %#v, want none", compat["custom"])
	}
	if compat["gpt-6-luna"] != nil {
		t.Fatalf("gpt compat = %#v, want none", compat["gpt-6-luna"])
	}
}

func TestPiClaudeDialectPreset(t *testing.T) {
	home := sandboxHome(t)
	models := []ModelSpec{{Slug: "claude-opus-5"}, {Slug: "claude-sonnet-5"}, {Slug: "claude-opus-4-6"}}
	// Anthropic-dialect bindings get the official adaptive presets.
	if err := Find("pi").ApplyAuth(AuthSpec{
		Endpoint: "https://gateway.example/v1", Key: "sk-test", PiAPI: "anthropic-messages", Models: models,
	}); err != nil {
		t.Fatal(err)
	}
	readCompat := func() (map[string]map[string]any, map[string]map[string]any) {
		data, err := os.ReadFile(filepath.Join(home, ".pi", "agent", "models.json"))
		if err != nil {
			t.Fatal(err)
		}
		var config struct {
			Providers map[string]struct {
				Models []struct {
					ID               string         `json:"id"`
					ThinkingLevelMap map[string]any `json:"thinkingLevelMap"`
					Compat           map[string]any `json:"compat"`
				} `json:"models"`
			} `json:"providers"`
		}
		if err := json.Unmarshal(data, &config); err != nil {
			t.Fatal(err)
		}
		compat, maps := map[string]map[string]any{}, map[string]map[string]any{}
		for _, m := range config.Providers[piDefaultProvider(t, home)].Models {
			compat[m.ID] = m.Compat
			maps[m.ID] = m.ThinkingLevelMap
		}
		return compat, maps
	}
	compat, maps := readCompat()
	if maps["claude-opus-5"]["off"] != nil || maps["claude-opus-5"]["xhigh"] != "xhigh" {
		t.Fatalf("opus-5 map = %#v, want locked xhigh/max", maps["claude-opus-5"])
	}
	for _, slug := range []string{"claude-opus-5", "claude-sonnet-5", "claude-opus-4-6"} {
		if compat[slug]["forceAdaptiveThinking"] != true {
			t.Fatalf("%s compat = %#v, want forceAdaptiveThinking", slug, compat[slug])
		}
	}
	// The same slugs on an openai-completions binding keep the generic fallback
	// and no compat: aggregator-rewritten levels must not leak in.
	if err := Find("pi").ApplyAuth(AuthSpec{
		Endpoint: "https://gateway.example/v1", Key: "sk-test", PiAPI: "openai-completions", Models: models,
	}); err != nil {
		t.Fatal(err)
	}
	compat, maps = readCompat()
	for _, slug := range []string{"claude-opus-5", "claude-sonnet-5", "claude-opus-4-6"} {
		if compat[slug] != nil {
			t.Fatalf("%s compat = %#v, want none on openai-completions", slug, compat[slug])
		}
		if maps[slug]["off"] == nil {
			t.Fatalf("%s map = %#v, want the generic fallback on openai-completions", slug, maps[slug])
		}
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

func TestPiNamesProviderAfterEndpoint(t *testing.T) {
	home := sandboxHome(t)
	if err := Find("pi").ApplyAuth(AuthSpec{
		Endpoint: "https://gw.example:8443/v1", Key: "sk-test", Model: "custom",
	}); err != nil {
		t.Fatal(err)
	}
	providers := readPiProviders(t, home)
	// Pi prints a model as provider/id; a bare "charon" would hide the endpoint
	// even when it is the only one.
	if got := piProviderNames(providers); !reflect.DeepEqual(got, []string{"charon-gw-example"}) {
		t.Fatalf("providers = %v, want the endpoint-keyed entry", got)
	}
	entry := providers["charon-gw-example"]
	if entry["name"] != "gw.example" || entry["baseUrl"] != "https://gw.example:8443/v1" {
		t.Fatalf("entry = %#v, want the host as its name", entry)
	}
	if got := piDefaultProvider(t, home); got != "charon-gw-example" {
		t.Fatalf("defaultProvider = %q, want the endpoint-keyed entry", got)
	}
	// Nothing is aggregated here, so model names stay bare slugs: the provider
	// key already carries the endpoint.
	if got := piModelField(entry["models"], "name"); !reflect.DeepEqual(got, []string{"custom"}) {
		t.Fatalf("model names = %v", got)
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
	// The legacy entry is carried over to the endpoint-keyed one and removed, so
	// /model never lists the same endpoint twice.
	if _, exists := readPiProviders(t, home)[managedProvider]; exists {
		t.Fatal("legacy providers.charon entry survived the rename")
	}
	if entry := readPiProviders(t, home)["charon-new-example"]; entry == nil {
		t.Fatalf("entry not keyed by endpoint: %v", piProviderNames(readPiProviders(t, home)))
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

// readPiProviders returns every provider entry of models.json, keyed by its
// provider name, for assertions over a whole aggregated file.
func readPiProviders(t *testing.T, home string) map[string]map[string]any {
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
	return cfg.Providers
}

// piModelField collects one string field from every entry of a decoded
// models array.
func piModelField(entries any, field string) []string {
	list, _ := entries.([]any)
	values := make([]string, 0, len(list))
	for _, entry := range list {
		m, _ := entry.(map[string]any)
		value, _ := m[field].(string)
		values = append(values, value)
	}
	return values
}

// piProviderNames is the sorted key set of a providers map.
func piProviderNames(providers map[string]map[string]any) []string {
	names := make([]string, 0, len(providers))
	for name := range providers {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func TestPiAggregatesEveryEndpoint(t *testing.T) {
	home := sandboxHome(t)
	dir := filepath.Join(home, ".pi", "agent")
	writeFile(t, filepath.Join(dir, "models.json"), `{
		"customSetting": "kept",
		"providers": {
			"mine": {"baseUrl": "https://mine.example", "apiKey": "sk-mine", "models": [{"id": "m"}]}
		}
	}`)
	writeFile(t, filepath.Join(dir, "settings.json"),
		`{"theme":"custom","defaultThinkingLevel":"high","defaultProvider":"other","defaultModel":"old"}`)

	spec := AuthSpec{
		Endpoint: "https://primary.example/v1",
		Key:      "sk-primary",
		Model:    "m-1",
		Models:   []ModelSpec{{Slug: "m-1"}, {Slug: "m-2"}},
		Groups: []EndpointGroup{
			{Endpoint: "https://g1.example/v1", Key: "sk-g1", Models: []ModelSpec{{Slug: "a-1"}}},
			{Endpoint: "https://g2.example:8443/v1", Key: "sk-g2", Models: []ModelSpec{{Slug: "b-1"}, {Slug: "b-2"}}},
		},
	}
	if err := Find("pi").ApplyAuth(spec); err != nil {
		t.Fatal(err)
	}

	providers := readPiProviders(t, home)
	wantNames := []string{"charon-g1-example", "charon-g2-example", "charon-primary", "mine"}
	if got := piProviderNames(providers); !reflect.DeepEqual(got, wantNames) {
		t.Fatalf("providers = %v, want %v", got, wantNames)
	}
	// Pi prints a model as provider/id, so the binding's own entry is keyed
	// after its endpoint instead of a bare "charon".
	if got := piDefaultProvider(t, home); got != "charon-primary" {
		t.Fatalf("defaultProvider = %q, want the endpoint-keyed provider", got)
	}

	main := providers["charon-primary"]
	if main["name"] != "primary" || main["baseUrl"] != spec.Endpoint ||
		main["apiKey"] != "sk-primary" || main["api"] != "openai-completions" {
		t.Fatalf("primary provider = %#v", main)
	}
	if got := piModelField(main["models"], "id"); !reflect.DeepEqual(got, []string{"m-1", "m-2"}) {
		t.Fatalf("primary model ids = %v", got)
	}
	if got := piModelField(main["models"], "name"); !reflect.DeepEqual(got, []string{"m-1 (primary)", "m-2 (primary)"}) {
		t.Fatalf("primary model names = %v", got)
	}

	g1 := providers["charon-g1-example"]
	if g1["name"] != "g1.example" || g1["baseUrl"] != "https://g1.example/v1" ||
		g1["apiKey"] != "sk-g1" || g1["api"] != "openai-completions" {
		t.Fatalf("g1 provider = %#v", g1)
	}
	if got := piModelField(g1["models"], "id"); !reflect.DeepEqual(got, []string{"a-1"}) {
		t.Fatalf("g1 model ids = %v", got)
	}
	if got := piModelField(g1["models"], "name"); !reflect.DeepEqual(got, []string{"a-1 (g1.example)"}) {
		t.Fatalf("g1 model names = %v", got)
	}

	// A port is not part of the label: Hostname() drops it.
	g2 := providers["charon-g2-example"]
	if g2["name"] != "g2.example" || g2["baseUrl"] != "https://g2.example:8443/v1" || g2["apiKey"] != "sk-g2" {
		t.Fatalf("g2 provider = %#v", g2)
	}
	if got := piModelField(g2["models"], "name"); !reflect.DeepEqual(got, []string{"b-1 (g2.example)", "b-2 (g2.example)"}) {
		t.Fatalf("g2 model names = %v", got)
	}

	mine := providers["mine"]
	if !reflect.DeepEqual(mine, map[string]any{
		"baseUrl": "https://mine.example", "apiKey": "sk-mine",
		"models": []any{map[string]any{"id": "m"}},
	}) {
		t.Fatalf("user provider changed: %#v", mine)
	}
	modelsFile, err := loadJSONMap(filepath.Join(dir, "models.json"))
	if err != nil {
		t.Fatal(err)
	}
	if modelsFile["customSetting"] != "kept" {
		t.Fatalf("unrelated models.json keys changed: %#v", modelsFile)
	}

	settings, err := loadJSONMap(filepath.Join(dir, "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	if settings["theme"] != "custom" || settings["defaultThinkingLevel"] != "high" {
		t.Fatalf("user preferences changed: %#v", settings)
	}
	if settings["defaultProvider"] != "charon-primary" || settings["defaultModel"] != "m-1" {
		t.Fatalf("defaults = %v/%v, want charon-primary/m-1", settings["defaultProvider"], settings["defaultModel"])
	}

	// Group entries follow the primary entry in group order, so /model and a
	// diff of the file both read the same way on every apply.
	raw, err := os.ReadFile(filepath.Join(dir, "models.json"))
	if err != nil {
		t.Fatal(err)
	}
	positions := make([]int, 0, 3)
	for _, key := range []string{`"charon-primary"`, `"charon-g1-example"`, `"charon-g2-example"`} {
		at := strings.Index(string(raw), key)
		if at < 0 {
			t.Fatalf("models.json lost %s:\n%s", key, raw)
		}
		positions = append(positions, at)
	}
	if positions[0] >= positions[1] || positions[1] >= positions[2] {
		t.Fatalf("provider order = %v in\n%s", positions, raw)
	}

	info, err := os.Stat(filepath.Join(dir, "models.json"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("models.json mode = %o, want 600", info.Mode().Perm())
	}
}

func TestPiAggregationDropsStaleProviders(t *testing.T) {
	home := sandboxHome(t)
	tool := Find("pi")
	base := AuthSpec{
		Endpoint: "https://primary.example/v1",
		Key:      "sk-primary",
		Model:    "m-1",
		Models:   []ModelSpec{{Slug: "m-1"}},
	}
	both := base
	both.Groups = []EndpointGroup{
		{Endpoint: "https://a.example/v1", Key: "sk-a", Models: []ModelSpec{{Slug: "a-1"}}},
		{Endpoint: "https://b.example/v1", Key: "sk-b", Models: []ModelSpec{{Slug: "b-1"}}},
	}
	if err := tool.ApplyAuth(both); err != nil {
		t.Fatal(err)
	}
	if names := piProviderNames(readPiProviders(t, home)); !reflect.DeepEqual(names,
		[]string{"charon-a-example", "charon-b-example", "charon-primary"}) {
		t.Fatalf("aggregated providers = %v", names)
	}

	onlyB := base
	onlyB.Groups = []EndpointGroup{
		{Endpoint: "https://b.example/v1", Key: "sk-b2", Models: []ModelSpec{{Slug: "b-2"}}},
	}
	if err := tool.ApplyAuth(onlyB); err != nil {
		t.Fatal(err)
	}
	providers := readPiProviders(t, home)
	if got := piProviderNames(providers); !reflect.DeepEqual(got, []string{"charon-b-example", "charon-primary"}) {
		t.Fatalf("providers after dropping A = %v", got)
	}
	b := providers["charon-b-example"]
	if b["apiKey"] != "sk-b2" || !reflect.DeepEqual(piModelField(b["models"], "id"), []string{"b-2"}) {
		t.Fatalf("updated B provider = %#v", b)
	}
	main := providers["charon-primary"]
	if main["baseUrl"] != base.Endpoint || main["apiKey"] != "sk-primary" ||
		!reflect.DeepEqual(piModelField(main["models"], "id"), []string{"m-1"}) {
		t.Fatalf("charon provider changed: %#v", main)
	}
}

func TestPiEndpointLabelsStayShort(t *testing.T) {
	// Pi prints the label beside every model, so the label keeps only the
	// part of the host that names the service.
	short := piEndpointLabels("https://newapi.maimaidx.icu/v1", []EndpointGroup{
		{Endpoint: "https://grok-heavy.878.indevs.in/v1"},
		{Endpoint: "https://luckyg.131518.xyz/v1"},
	})
	for endpoint, want := range map[string]string{
		"https://newapi.maimaidx.icu/v1":      "newapi",
		"https://grok-heavy.878.indevs.in/v1": "grok-heavy",
		"https://luckyg.131518.xyz/v1":        "luckyg",
	} {
		if got := short[endpoint]; got != want {
			t.Fatalf("label of %s = %q, want %q", endpoint, got, want)
		}
	}

	// A first label too short to say anything keeps the second one.
	if got := piEndpointLabels("https://ai.hyper.nyc.mn/v1", nil)["https://ai.hyper.nyc.mn/v1"]; got != "ai.hyper" {
		t.Fatalf("label of ai.hyper.nyc.mn = %q, want ai.hyper", got)
	}

	// Two services under the same short label cannot share it, so both keep
	// their whole host.
	twins := piEndpointLabels("https://newapi.one.example/v1", []EndpointGroup{
		{Endpoint: "https://newapi.two.example/v1"},
	})
	for endpoint, want := range map[string]string{
		"https://newapi.one.example/v1": "newapi.one.example",
		"https://newapi.two.example/v1": "newapi.two.example",
	} {
		if got := twins[endpoint]; got != want {
			t.Fatalf("label of %s = %q, want %q", endpoint, got, want)
		}
	}

	// An address has no service label, so it stays whole.
	if got := piEndpointLabels("http://127.0.0.1:8080/v1", nil)["http://127.0.0.1:8080/v1"]; got != "127.0.0.1" {
		t.Fatalf("label of an address = %q, want 127.0.0.1", got)
	}
}

func TestPiAggregationSameHostUsesURL(t *testing.T) {
	home := sandboxHome(t)
	spec := AuthSpec{
		Endpoint: "https://primary.example/v1",
		Key:      "sk-primary",
		Model:    "m-1",
		Models:   []ModelSpec{{Slug: "m-1"}},
		Groups: []EndpointGroup{
			{Endpoint: "https://new.sharedchat.cc/v1", Key: "sk-1", Models: []ModelSpec{{Slug: "s-1"}}},
			{Endpoint: "https://new.sharedchat.cc/codex", Key: "sk-2", Models: []ModelSpec{{Slug: "s-2"}}},
		},
	}
	if err := Find("pi").ApplyAuth(spec); err != nil {
		t.Fatal(err)
	}
	providers := readPiProviders(t, home)
	if got := piProviderNames(providers); !reflect.DeepEqual(got,
		[]string{"charon-https---new-sharedchat-cc-codex", "charon-https---new-sharedchat-cc-v1", "charon-primary"}) {
		t.Fatalf("providers = %v", got)
	}
	for endpoint, key := range map[string]string{
		"https://new.sharedchat.cc/v1":    "charon-https---new-sharedchat-cc-v1",
		"https://new.sharedchat.cc/codex": "charon-https---new-sharedchat-cc-codex",
	} {
		entry := providers[key]
		if entry["name"] != endpoint || entry["baseUrl"] != endpoint {
			t.Fatalf("%s entry = %#v, want the full URL as name and baseUrl", key, entry)
		}
	}
	// The label reaches the model display names too, so two entries of the
	// same model id stay distinguishable in /model.
	if got := piModelField(providers["charon-https---new-sharedchat-cc-v1"]["models"], "name"); !reflect.DeepEqual(
		got, []string{"s-1 (https://new.sharedchat.cc/v1)"}) {
		t.Fatalf("v1 model names = %v", got)
	}
}

func TestPiAggregationReapplyIsStable(t *testing.T) {
	home := sandboxHome(t)
	dir := filepath.Join(home, ".pi", "agent")
	writeFile(t, filepath.Join(dir, "models.json"), `{
		"providers": {
			"mine": {"baseUrl": "https://mine.example", "apiKey": "sk-mine"}
		}
	}`)
	spec := AuthSpec{
		Endpoint: "https://primary.example/v1",
		Key:      "sk-primary",
		Model:    "m-1",
		Models:   []ModelSpec{{Slug: "m-1"}, {Slug: "m-2"}},
		Groups: []EndpointGroup{
			{Endpoint: "https://a.example/v1", Key: "sk-a", Models: []ModelSpec{{Slug: "a-1"}}},
			{Endpoint: "https://b.example/v1", Key: "sk-b", Models: []ModelSpec{{Slug: "b-1"}}},
		},
	}
	tool := Find("pi")
	if err := tool.ApplyAuth(spec); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "models.json")
	first, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := tool.ApplyAuth(spec); err != nil {
		t.Fatal(err)
	}
	second, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatalf("reapplying one spec changed models.json:\nfirst:\n%s\nsecond:\n%s", first, second)
	}
}

func TestPiAggregationUsesEachGroupProtocol(t *testing.T) {
	home := sandboxHome(t)
	spec := AuthSpec{
		Endpoint: "https://primary.example/v1",
		PiAPI:    "openai-responses",
		Key:      "sk-primary",
		Model:    "m-1",
		Models:   []ModelSpec{{Slug: "m-1"}},
		Groups: []EndpointGroup{
			{Endpoint: "https://claude.example/v1", Key: "sk-claude", PiAPI: "anthropic-messages", Models: []ModelSpec{{Slug: "claude-opus-5"}}},
			{Endpoint: "https://plain.example/v1", Key: "sk-plain", Models: []ModelSpec{{Slug: "plain"}}},
		},
	}
	if err := Find("pi").ApplyAuth(spec); err != nil {
		t.Fatal(err)
	}
	providers := readPiProviders(t, home)
	if got := providers["charon-primary"]["api"]; got != "openai-responses" {
		t.Fatalf("primary api = %v, want openai-responses", got)
	}
	if got := providers["charon-claude"]["api"]; got != "anthropic-messages" {
		t.Fatalf("claude group api = %v, want the protocol its own binding saved", got)
	}
	// An endpoint whose binding never chose a protocol keeps pi's default; it
	// must not inherit the primary binding's dialect, which its gateway would
	// reject.
	if got := providers["charon-plain"]["api"]; got != "openai-completions" {
		t.Fatalf("plain group api = %v, want openai-completions", got)
	}
}

func TestPiAggregationRejectsUnknownGroupProtocol(t *testing.T) {
	home := sandboxHome(t)
	dir := filepath.Join(home, ".pi", "agent")
	before := `{"providers":{"mine":{"baseUrl":"https://mine.example"}}}`
	writeFile(t, filepath.Join(dir, "models.json"), before)
	err := Find("pi").ApplyAuth(AuthSpec{
		Endpoint: "https://primary.example/v1", Key: "sk-primary", Model: "m-1",
		Groups: []EndpointGroup{
			{Endpoint: "https://g.example/v1", Key: "sk-g", PiAPI: "openai-responses-compact", Models: []ModelSpec{{Slug: "g"}}},
		},
	})
	if err == nil {
		t.Fatal("accepted an unsupported group protocol")
	}
	data, readErr := os.ReadFile(filepath.Join(dir, "models.json"))
	if readErr != nil || string(data) != before {
		t.Fatalf("models.json written before the group was validated: %s", data)
	}
}

func TestPiOffRemovesAggregatedProviders(t *testing.T) {
	home := sandboxHome(t)
	dir := filepath.Join(home, ".pi", "agent")
	writeFile(t, filepath.Join(dir, "models.json"), `{
		"customSetting": "kept",
		"providers": {
			"mine": {"baseUrl": "https://mine.example", "apiKey": "sk-mine"}
		}
	}`)
	tool := Find("pi")
	if err := tool.ApplyAuth(AuthSpec{
		Endpoint: "https://primary.example/v1",
		Key:      "sk-primary",
		Model:    "m-1",
		Models:   []ModelSpec{{Slug: "m-1"}},
		Groups: []EndpointGroup{
			{Endpoint: "https://a.example/v1", Key: "sk-a", Models: []ModelSpec{{Slug: "a-1"}}},
			{Endpoint: "https://b.example/v1", Key: "sk-b", Models: []ModelSpec{{Slug: "b-1"}}},
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := tool.Off(ToolStash{}); err != nil {
		t.Fatal(err)
	}
	providers := readPiProviders(t, home)
	if got := piProviderNames(providers); !reflect.DeepEqual(got, []string{"mine"}) {
		t.Fatalf("providers after off = %v, want only the user's", got)
	}
	modelsFile, err := loadJSONMap(filepath.Join(dir, "models.json"))
	if err != nil {
		t.Fatal(err)
	}
	if modelsFile["customSetting"] != "kept" {
		t.Fatalf("off touched unrelated keys: %#v", modelsFile)
	}
}
