package tools

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	toml "github.com/pelletier/go-toml/v2"

	"charon/internal/artifact"
	"charon/internal/models"
)

func home() string {
	h, _ := os.UserHomeDir()
	return h
}

func isOpenAISlug(model string) bool {
	m := strings.ToLower(strings.TrimSpace(model))
	m = strings.TrimPrefix(m, "openai/")
	return strings.HasPrefix(m, "gpt-") ||
		strings.HasPrefix(m, "o1") ||
		strings.HasPrefix(m, "o3") ||
		strings.HasPrefix(m, "chatgpt-")
}

// codexContextWindow returns the window to pin for an unrecognized model, else 0.
// OpenAI slugs Codex already sizes itself from its own catalog, so those stay unset.
func codexContextWindow(model string, configured int) int {
	model = strings.TrimSpace(model)
	if configured > 0 {
		return configured
	}
	if model == "" || isOpenAISlug(model) {
		return 0
	}
	return models.DefaultContextCeiling
}

// IsOfficialOpenAIEndpoint reports whether endpoint is OpenAI's own API host.
func IsOfficialOpenAIEndpoint(endpoint string) bool {
	return isOfficialEndpoint(endpoint, "api.openai.com")
}

func codexReasoningLevels() []any {
	return []any{
		map[string]any{"effort": "low", "description": "Fast responses with lighter reasoning"},
		map[string]any{"effort": "medium", "description": "Balances speed and reasoning depth for everyday tasks"},
		map[string]any{"effort": "high", "description": "Greater reasoning depth for complex problems"},
		map[string]any{"effort": "xhigh", "description": "Extra high reasoning depth for complex problems"},
		map[string]any{"effort": "max", "description": "Maximum reasoning depth for the hardest problems"},
		map[string]any{"effort": "ultra", "description": "Maximum reasoning with automatic task delegation"},
	}
}

func codexEfforts() []string {
	return []string{"low", "medium", "high", "xhigh", "max", "ultra"}
}

// ValidateCodexEffort accepts only effort levels present in the generated catalog.
func ValidateCodexEffort(effort string) error {
	if slices.Contains(codexEfforts(), strings.ToLower(strings.TrimSpace(effort))) {
		return nil
	}
	return fmt.Errorf("effort must be one of: %s", strings.Join(codexEfforts(), ", "))
}

func codexCatalog(path string, auth AuthSpec) (int, error) {
	catalog, err := loadJSONMap(path)
	if err != nil {
		return 0, err
	}
	existing, _ := catalog["models"].([]any)
	specs := append([]ModelSpec(nil), auth.Models...)
	if len(specs) == 0 {
		for _, raw := range existing {
			if entry, ok := raw.(map[string]any); ok {
				if slug, ok := entry["slug"].(string); ok && slug != "" {
					specs = append(specs, ModelSpec{Slug: slug})
				}
			}
		}
	}
	if auth.Model != "" && !slices.ContainsFunc(specs, func(spec ModelSpec) bool { return spec.Slug == auth.Model }) {
		specs = append(specs, ModelSpec{Slug: auth.Model})
	}
	entries := make([]any, 0, len(specs))
	seen := make(map[string]bool, len(specs))
	var singleWindow int
	for _, spec := range specs {
		if spec.Slug == "" || seen[spec.Slug] {
			continue
		}
		seen[spec.Slug] = true
		var entry map[string]any
		for _, raw := range existing {
			if candidate, ok := raw.(map[string]any); ok && candidate["slug"] == spec.Slug {
				entry = candidate
				break
			}
		}
		if entry == nil {
			entry = map[string]any{
				"additional_speed_tiers":            []any{},
				"apply_patch_tool_type":             "freeform",
				"base_instructions":                 "",
				"comp_hash":                         "charon",
				"default_reasoning_level":           "medium",
				"default_reasoning_summary":         "none",
				"default_verbosity":                 "medium",
				"description":                       "Model registered by Charon",
				"display_name":                      spec.Slug,
				"effective_context_window_percent":  95,
				"experimental_supported_tools":      []any{},
				"include_apps_usage_instructions":   false,
				"include_plugin_usage_instructions": false,
				"include_skills_usage_instructions": false,
				"input_modalities":                  []any{"text"},
				"model_messages":                    map[string]any{},
				"priority":                          1,
				"service_tiers":                     []any{},
				"shell_type":                        "unified_exec",
				"support_verbosity":                 true,
				"supported_in_api":                  true,
				"supported_reasoning_levels":        codexReasoningLevels(),
				"supports_experimental_context":     false,
				"supports_image_detail_original":    true,
				"supports_reasoning_effort_updates": true,
				"supports_search_tool":              true,
				"truncation_policy":                 map[string]any{"limit": 10000, "mode": "tokens"},
				"visibility":                        "list",
				"web_search_tool_type":              "text_and_image",
			}
		}
		entry["slug"] = spec.Slug
		window := spec.ContextWindow
		if window == 0 && len(auth.Models) == 0 {
			if previous, ok := entry["context_window"].(float64); ok {
				window = int(previous)
			}
		}
		if window <= 0 {
			window = models.DefaultContextCeiling
		}
		entry["context_window"] = window
		entry["max_context_window"] = window
		entry["supported_reasoning_levels"] = codexReasoningLevels()
		entry["supports_reasoning_effort_updates"] = true
		entry["visibility"] = "list"
		entry["supported_in_api"] = true
		// use_responses_lite and tool_mode="code_mode_only" make Codex register its V8
		// "exec" orchestrator as a {"type":"custom"} tool instead of the regular shell
		// tool. OpenAI-compatible gateways commonly reject that tool type (Z.AI error
		// 1214 "tools[0].type:type is illegal") and the model loses shell access, so
		// never carry either field over from an existing entry.
		delete(entry, "use_responses_lite")
		delete(entry, "tool_mode")
		effort := spec.Effort
		if spec.Slug == auth.Model && auth.Effort != "" {
			effort = auth.Effort
		}
		if effort != "" {
			entry["default_reasoning_level"] = effort
		} else if len(auth.Models) > 0 || entry["default_reasoning_level"] == nil {
			entry["default_reasoning_level"] = "medium"
		}
		entries = append(entries, entry)
		singleWindow = window
	}
	catalog["models"] = entries
	data, err := json.MarshalIndent(catalog, "", "  ")
	if err != nil {
		return 0, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return 0, err
	}
	if len(entries) != 1 {
		singleWindow = 0
	}
	return singleWindow, artifact.AtomicWrite(path, append(data, '\n'), 0o600)
}

// newCodex describes the OpenAI Codex CLI (~/.codex).
func newCodex() *Tool {
	dir := filepath.Join(home(), ".codex")
	configPath := filepath.Join(dir, "config.toml")
	authPath := filepath.Join(dir, "auth.json")
	catalogPath := filepath.Join(dir, "custom_models.json")

	return &Tool{
		Name:            "codex",
		Title:           "Codex",
		Provider:        "openai",
		ModelMenu:       "/model",
		DefaultEndpoint: "https://api.openai.com/v1",
		ApplyAuth: func(a AuthSpec) error {
			// Register a self-contained OpenAI-compatible provider (key embedded inline)
			// and point Codex at it; auth.json (ChatGPT OAuth) is left untouched.
			cfg, err := loadTOMLMap(configPath)
			if err != nil {
				return err
			}
			modelSlug := strings.TrimSpace(a.Model)
			if modelSlug == "" {
				if current, ok := cfg["model"].(string); ok {
					modelSlug = strings.TrimSpace(current)
				}
			}
			if modelSlug != "" {
				cfg["model"] = modelSlug
			}
			delete(cfg, "model_context_window")
			var configuredWindow int
			for _, spec := range a.Models {
				if spec.Slug == modelSlug {
					configuredWindow = spec.ContextWindow
					break
				}
			}
			useCatalog := !IsOfficialOpenAIEndpoint(a.Endpoint) || codexContextWindow(modelSlug, configuredWindow) != 0 || len(a.Models) > 1 ||
				(len(a.Models) == 0 && cfg["model_catalog_json"] == catalogPath)
			if modelSlug != "" && useCatalog {
				a.Model = modelSlug
				window, err := codexCatalog(catalogPath, a)
				if err != nil {
					return err
				}
				if window > 0 {
					cfg["model_context_window"] = window
				}
				cfg["model_catalog_json"] = catalogPath
			} else {
				delete(cfg, "model_catalog_json")
			}
			if a.Effort != "" {
				cfg["model_reasoning_effort"] = a.Effort
			}
			cfg["model_provider"] = "charon"
			providers := subMap(cfg, "model_providers")
			original := snapshotProviders(providers) // guard: write may only touch "charon"
			providers["charon"] = map[string]any{
				"name":     "charon",
				"base_url": a.Endpoint,
				// "responses" is the only wire API since Codex dropped "chat" (openai/codex #7782).
				"wire_api":                  "responses",
				"experimental_bearer_token": a.Key,
			}
			if err := ensureOnlyCharonChanged(original, providers); err != nil {
				return err
			}
			return writeTOMLMap(configPath, cfg, 0o600)
		},
		Detected: func() bool {
			return detected("codex", configPath, authPath)
		},
		Describe: func() (Info, error) {
			var info Info

			if data, err := os.ReadFile(configPath); err == nil {
				var cfg struct {
					Model                string `toml:"model"`
					ModelReasoningEffort string `toml:"model_reasoning_effort"`
					ModelContextWindow   int    `toml:"model_context_window"`
					ModelCatalogJSON     string `toml:"model_catalog_json"`
					ModelProvider        string `toml:"model_provider"`
					ModelProviders       map[string]struct {
						BaseURL     string `toml:"base_url"`
						BearerToken string `toml:"experimental_bearer_token"`
					} `toml:"model_providers"`
				}
				if toml.Unmarshal(data, &cfg) == nil {
					info.Model = cfg.Model
					info.Effort = cfg.ModelReasoningEffort
					info.ContextWindow = cfg.ModelContextWindow
					if info.ContextWindow == 0 && cfg.ModelCatalogJSON != "" {
						path := cfg.ModelCatalogJSON
						if !filepath.IsAbs(path) {
							path = filepath.Join(filepath.Dir(configPath), path)
						}
						if data, err := os.ReadFile(path); err == nil {
							var catalog struct {
								Models []struct {
									Slug          string `json:"slug"`
									ContextWindow int    `json:"context_window"`
								} `json:"models"`
							}
							if json.Unmarshal(data, &catalog) == nil {
								for _, model := range catalog.Models {
									if model.Slug == info.Model {
										info.ContextWindow = model.ContextWindow
										break
									}
								}
							}
						}
					}
					if p, ok := cfg.ModelProviders[cfg.ModelProvider]; ok {
						if p.BaseURL != "" {
							info.Endpoint = p.BaseURL
						}
						if p.BearerToken != "" {
							info.Secret, info.AuthMode = p.BearerToken, "api"
						}
					}
				}
			}

			if data, err := os.ReadFile(authPath); err == nil {
				var auth struct {
					Tokens struct {
						IDToken   string `json:"id_token"`
						AccountID string `json:"account_id"`
					} `json:"tokens"`
				}
				// A ChatGPT login carries a JWT whose "email" names the account.
				if json.Unmarshal(data, &auth) == nil {
					info.Account = decodeJWTEmail(auth.Tokens.IDToken)
					if info.Account == "" {
						info.Account = auth.Tokens.AccountID
					}
				}
			}

			if info.AuthMode == "" {
				if data, err := os.ReadFile(authPath); err == nil {
					var auth struct {
						AuthMode string `json:"auth_mode"`
						APIKey   string `json:"OPENAI_API_KEY"`
					}
					if json.Unmarshal(data, &auth) == nil {
						info.Secret = auth.APIKey
						// Codex writes "chatgpt" for an OAuth login and "apikey" for a key;
						// present them as the friendlier "oauth"/"api".
						switch auth.AuthMode {
						case "chatgpt":
							info.AuthMode = "oauth"
						case "apikey":
							info.AuthMode = "api"
						default:
							info.AuthMode = auth.AuthMode
						}
						if info.AuthMode == "" && auth.APIKey != "" {
							info.AuthMode = "api"
						}
					}
				}
			}

			return info.withDefaults("api.openai.com (default)"), nil
		},
	}
}
