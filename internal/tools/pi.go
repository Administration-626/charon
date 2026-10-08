package tools

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"charon/internal/models"
)

// Older Charon versions registered Pi providers through a generated extension.
// Keep its exact format to recognize files we can safely migrate to models.json.
const (
	piExtensionOpen  = `pi.registerProvider("charon", `
	piExtensionClose = `);
  // charon:config:end
}
`
)

var piConfigRE = regexp.MustCompile(`(?s)pi\.registerProvider\("charon",\s*(.*?)\);\s*\n\s*// charon:config:end`)

// ResolvePiAPI validates Pi's request protocol, defaulting to OpenAI Chat Completions.
// Pi 1.0.4's pi-ai KnownApi has no separate Responses Compact protocol.
func ResolvePiAPI(api string) (string, error) {
	if api == "" {
		return "openai-completions", nil
	}
	switch api {
	case "openai-completions", "openai-responses", "anthropic-messages", "google-generative-ai":
		return api, nil
	default:
		return "", fmt.Errorf("unsupported Pi endpoint type %q", api)
	}
}

// piModel is one entry of a pi provider's "models" array.
type piModel struct {
	ID               string             `json:"id"`
	Name             string             `json:"name"`
	Reasoning        bool               `json:"reasoning"`
	Input            []string           `json:"input"`
	Cost             piCost             `json:"cost"`
	ContextWindow    int                `json:"contextWindow"`
	MaxTokens        int                `json:"maxTokens"`
	ThinkingLevelMap map[string]*string `json:"thinkingLevelMap,omitempty"`
}

type piCost struct {
	Input      float64 `json:"input"`
	Output     float64 `json:"output"`
	CacheRead  float64 `json:"cacheRead"`
	CacheWrite float64 `json:"cacheWrite"`
}

// piProviderConfig describes Charon's native and legacy provider configuration.
type piProviderConfig struct {
	Name    string    `json:"name"`
	BaseURL string    `json:"baseUrl"`
	APIKey  string    `json:"apiKey"`
	API     string    `json:"api"`
	Models  []piModel `json:"models"`
}

// piEscapeValue escapes "$" and "!", which pi's apiKey/headers fields treat as
// env-var interpolation ("$VAR", "${VAR}") and command execution ("!cmd") markers,
// so a literal key/header value containing either is never misinterpreted.
func piEscapeValue(s string) string {
	s = strings.ReplaceAll(s, "$", "$$")
	s = strings.ReplaceAll(s, "!", "$!")
	return s
}

// piThinkingLevelMap gives every Charon model Pi's standard thinking controls.
// Charon has no authoritative per-model capability table, so extended levels are
// passed through by name; explicit mappings (including null/disabled levels) win.
func piThinkingLevelMap(spec ModelSpec) map[string]*string {
	if spec.ThinkingLevelMap != nil {
		return spec.ThinkingLevelMap
	}
	off, xhigh, maxLevel := "none", "xhigh", "max"
	return map[string]*string{"off": &off, "xhigh": &xhigh, "max": &maxLevel}
}

// piBuildModels turns a list of model ids into pi model entries.
func piBuildModels(specs []ModelSpec) []piModel {
	entries := make([]piModel, 0, len(specs))
	for _, spec := range specs {
		if spec.Slug == "" {
			continue
		}
		window := spec.ContextWindow
		if window == 0 {
			window = models.DefaultContextWindow(spec.Slug)
		}
		entries = append(entries, piModel{
			ID:   spec.Slug,
			Name: spec.Slug,
			// Charon does not have authoritative per-model thinking metadata. Pi
			// offers its standard thinking levels and lets the user turn them off.
			Reasoning:        true,
			Input:            []string{"text", "image"},
			ContextWindow:    window,
			MaxTokens:        models.DefaultMaxTokens(spec.Slug),
			ThinkingLevelMap: piThinkingLevelMap(spec),
		})
	}
	return entries
}

// piExtensionContent reproduces the legacy extension for ownership checks.
func piExtensionContent(cfg piProviderConfig) ([]byte, error) {
	body, err := json.MarshalIndent(cfg, "  ", "  ")
	if err != nil {
		return nil, err
	}
	var b strings.Builder
	b.WriteString(`import type { ExtensionAPI } from "@earendil-works/pi-coding-agent";

export default function (pi: ExtensionAPI) {
  // charon:config
  `)
	b.WriteString(piExtensionOpen)
	b.Write(body)
	b.WriteString(piExtensionClose)
	return []byte(b.String()), nil
}

// piParseExtension extracts the provider config JSON from a previously written
// charon.ts, "" fields / nil models if the file is absent or unrecognized.
func piParseExtension(data []byte) (piProviderConfig, bool) {
	m := piConfigRE.FindSubmatch(data)
	if m == nil {
		return piProviderConfig{}, false
	}
	var cfg piProviderConfig
	if json.Unmarshal(m[1], &cfg) != nil {
		return piProviderConfig{}, false
	}
	return cfg, true
}

// piLegacyConfig refuses to migrate extensions that differ from our generated
// format. In particular, a recognizable JSON block alone does not prove ownership.
func piLegacyConfig(path string) (*piProviderConfig, error) {
	stat, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read legacy pi extension: %w", err)
	}
	if !stat.Mode().IsRegular() {
		return nil, fmt.Errorf("refusing to migrate %s: not a regular file; disable it manually", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read legacy pi extension: %w", err)
	}
	cfg, ok := piParseExtension(data)
	expected, err := piExtensionContent(cfg)
	if !ok || err != nil || !bytes.Equal(data, expected) {
		return nil, fmt.Errorf("refusing to migrate modified or unrecognized %s; disable it manually", path)
	}
	return &cfg, nil
}

// newPi registers a provider in ~/.pi/agent/models.json. Model/effort defaults
// live in settings.json; Pi's own provider logins in auth.json remain untouched.
func newPi() *Tool {
	dir := filepath.Join(home(), ".pi", "agent")
	settingsPath := filepath.Join(dir, "settings.json")
	authPath := filepath.Join(dir, "auth.json")
	extensionPath := filepath.Join(dir, "extensions", "charon.ts")
	modelsPath := filepath.Join(dir, "models.json")

	return &Tool{
		Name:            "pi",
		Title:           "Pi",
		Provider:        "openai",
		ModelMenu:       "/model",
		DefaultEndpoint: "https://api.openai.com/v1",
		ApplyAuth: func(a AuthSpec) error {
			// Read and validate every input before the first write.
			api, err := ResolvePiAPI(a.PiAPI)
			if err != nil {
				return err
			}
			config, err := loadJSONMap(modelsPath)
			if err != nil {
				return fmt.Errorf("read models.json: %w", err)
			}
			s, err := loadJSONMap(settingsPath)
			if err != nil {
				return fmt.Errorf("read settings.json: %w", err)
			}
			if config == nil || s == nil {
				return fmt.Errorf("refusing to write pi config: models.json and settings.json must be objects")
			}
			if value, exists := config["providers"]; exists {
				if _, ok := value.(map[string]any); !ok {
					return fmt.Errorf("refusing to write models.json: providers must be an object")
				}
			}
			providers := subMap(config, "providers")
			if value, exists := providers[managedProvider]; exists {
				if _, ok := value.(map[string]any); !ok {
					return fmt.Errorf("refusing to write models.json: providers.charon must be an object")
				}
			}
			legacy, err := piLegacyConfig(extensionPath)
			if err != nil {
				return err
			}

			modelSlug := strings.TrimSpace(a.Model)
			var entries any
			if a.Models == nil {
				// Keep the complete entries, including fields unknown to Charon.
				if previous, ok := providers[managedProvider].(map[string]any); ok {
					if value, exists := previous["models"]; exists {
						if _, ok := value.([]any); !ok {
							return fmt.Errorf("refusing to write models.json: providers.charon.models must be an array")
						}
						entries = previous["models"]
					}
				}
				if entries == nil && legacy != nil && legacy.Models != nil {
					entries = legacy.Models
				}
			}
			if entries == nil {
				specs := a.Models
				if specs == nil && modelSlug != "" {
					specs = []ModelSpec{{Slug: modelSlug, Effort: a.Effort}}
				}
				entries = piBuildModels(specs)
			}
			original := snapshotProviders(providers)
			providers[managedProvider] = map[string]any{
				"name":    managedProvider,
				"baseUrl": a.Endpoint,
				"apiKey":  piEscapeValue(a.Key),
				"api":     api,
				"models":  entries,
			}
			if err := ensureOnlyCharonChanged(original, providers); err != nil {
				return err
			}
			if modelSlug != "" {
				s["defaultModel"] = modelSlug
			}
			s["defaultProvider"] = "charon"

			// ponytail: writes are atomic per file, not a multi-file transaction.
			// Preflight catches input errors; report partial application on I/O failure.
			if err := writeJSONMap(modelsPath, config, 0o600); err != nil {
				return fmt.Errorf("write models.json: %w", err)
			}
			if err := writeJSONMap(settingsPath, s, 0o600); err != nil {
				return fmt.Errorf("write settings.json (models.json already updated): %w", err)
			}
			if legacy != nil {
				if err := os.Remove(extensionPath); err != nil {
					return fmt.Errorf("remove legacy pi extension (config updated, but old extension may still override it): %w", err)
				}
			}
			return nil
		},
		Detected: func() bool {
			return detected("pi", settingsPath, authPath, extensionPath, modelsPath)
		},
		Describe: func() (Info, error) {
			var info Info

			if data, err := os.ReadFile(settingsPath); err == nil {
				var s struct {
					DefaultProvider      string `json:"defaultProvider"`
					DefaultModel         string `json:"defaultModel"`
					DefaultThinkingLevel string `json:"defaultThinkingLevel"`
				}
				if json.Unmarshal(data, &s) == nil {
					info.Model = s.DefaultModel
					info.Effort = s.DefaultThinkingLevel

					if s.DefaultProvider == "charon" || s.DefaultProvider == "" {
						if data, err := os.ReadFile(modelsPath); err == nil {
							var cfg struct {
								Providers map[string]piProviderConfig `json:"providers"`
							}
							if json.Unmarshal(data, &cfg) == nil {
								if p, ok := cfg.Providers["charon"]; ok {
									info.Endpoint = p.BaseURL
									for _, model := range p.Models {
										if model.ID == info.Model {
											info.ContextWindow, info.MaxTokens = model.ContextWindow, model.MaxTokens
											break
										}
									}
									if p.APIKey != "" {
										info.Secret, info.AuthMode = p.APIKey, "api"
									}
								}
							}
						}
						if info.AuthMode == "" {
							if data, err := os.ReadFile(extensionPath); err == nil {
								if cfg, ok := piParseExtension(data); ok {
									info.Endpoint = cfg.BaseURL
									for _, model := range cfg.Models {
										if model.ID == info.Model {
											info.ContextWindow, info.MaxTokens = model.ContextWindow, model.MaxTokens
											break
										}
									}
									if cfg.APIKey != "" {
										info.Secret, info.AuthMode = cfg.APIKey, "api"
									}
								}
							}
						}
					}
				}
			}

			// Otherwise fall back to an OAuth-based provider login (pi's /login).
			if info.AuthMode == "" {
				if data, err := os.ReadFile(authPath); err == nil {
					var auth map[string]json.RawMessage
					if json.Unmarshal(data, &auth) == nil && len(auth) > 0 {
						names := make([]string, 0, len(auth))
						for name := range auth {
							names = append(names, name)
						}
						sort.Strings(names)
						info.AuthMode = "oauth"
						info.Account = names[0]
					}
				}
			}

			return info.withDefaults("(provider default)"), nil
		},
	}
}
