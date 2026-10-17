package tools

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"charon/internal/edit"
)

// opencodeConfigPath returns the existing config (opencode.jsonc, else legacy
// opencode.json) so charon edits it in place, defaulting to opencode.jsonc.
func opencodeConfigPath() string {
	dir := filepath.Join(home(), ".config", "opencode")
	for _, name := range []string{"opencode.jsonc", "opencode.json"} {
		p := filepath.Join(dir, name)
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return filepath.Join(dir, "opencode.jsonc")
}

// newOpenCode describes OpenCode: providers in ~/.config/opencode/opencode.jsonc,
// credentials in ~/.local/share/opencode/auth.json.
func newOpenCode() *Tool {
	configPath := opencodeConfigPath()
	dataDir := filepath.Join(home(), ".local", "share", "opencode")
	authPath := filepath.Join(dataDir, "auth.json")

	return &Tool{
		Name:            "opencode",
		Title:           "OpenCode",
		Provider:        "openai",
		ModelMenu:       "/models",
		DefaultEndpoint: "https://api.openai.com/v1",
		Files:           []string{configPath},
		Owned: []OwnedFile{{
			Path: configPath,
			JSON: []string{"$schema", "model", "provider." + managedProvider},
		}},
		ApplyAuth: func(a AuthSpec) error {
			// Read-only: the previously-registered model list, tolerating the
			// JSONC comments the config carries.
			var existingModels []string
			if raw, ok := edit.GetJSONRaw(configPath, "provider.charon.models"); ok {
				var models map[string]any
				if json.Unmarshal([]byte(raw), &models) == nil {
					for id := range models {
						existingModels = append(existingModels, id)
					}
				}
			}
			var sets []edit.KV
			if _, ok := edit.GetJSONRaw(configPath, "$schema"); !ok {
				sets = append(sets, edit.KV{Path: "$schema", Value: "https://opencode.ai/config.json"})
			}

			// Register a "charon" provider: OpenCode needs options.apiKey and a
			// non-empty models map for the models to show in /models.
			options := map[string]any{"baseURL": a.Endpoint}
			if a.Key != "" {
				options["apiKey"] = a.Key
			}
			entry := map[string]any{
				"npm":     "@ai-sdk/openai-compatible",
				"name":    "charon",
				"options": options,
			}
			modelSlug := strings.TrimPrefix(strings.TrimSpace(a.Model), "charon/")
			if modelSlug != "" {
				// Register every model the caller already fetched (e.g. the TUI wizard's
				// picker list), not just modelSlug, so OpenCode's own /models picker can
				// switch between them without re-adding the binding. Falls back to the
				// previously-registered list, then just modelSlug, when the caller has no
				// fetched list (e.g. the CLI --model flag or an edit of another field).
				specs := a.Models
				if specs == nil {
					specs = opencodeSpecsFromIDs(existingModels)
				}
				if specs == nil {
					specs = []ModelSpec{{Slug: modelSlug}}
				}
				modelMap := make(map[string]any, len(specs))
				for _, spec := range specs {
					cleanID := strings.TrimPrefix(strings.TrimSpace(spec.Slug), "charon/")
					entry := map[string]any{"name": cleanID}
					if w := spec.ContextWindow; w > 0 {
						entry["limit"] = map[string]any{"context": w}
					}
					modelMap[cleanID] = entry
				}
				entry["models"] = modelMap
				sets = append(sets, edit.KV{Path: "model", Value: "charon/" + modelSlug})
			}
			// The edit names only the paths charon owns ($schema, model, and its own
			// provider entry); user-authored providers, comments and ordering stay
			// as they are.
			sets = append(sets, edit.KV{Path: "provider.charon", Value: entry})
			return edit.SetJSON(configPath, sets...)
		},
		Detected: func() bool {
			return detected("opencode", configPath, authPath)
		},
		Describe: func() (Info, error) {
			var info Info

			// A charon-managed provider keeps endpoint + key in the config.
			if data, err := os.ReadFile(configPath); err == nil {
				var cfg struct {
					Model           string `json:"model"`
					SmallModel      string `json:"small_model"`
					ReasoningEffort string `json:"reasoningEffort"`
					Agent           map[string]struct {
						Model           string `json:"model"`
						ReasoningEffort string `json:"reasoningEffort"`
					} `json:"agent"`
					Agents map[string]struct {
						Model           string `json:"model"`
						ReasoningEffort string `json:"reasoningEffort"`
					} `json:"agents"`
					Provider map[string]struct {
						Models map[string]struct {
							Limit struct {
								Context int `json:"context"`
								Output  int `json:"output"`
							} `json:"limit"`
						} `json:"models"`
						Options struct {
							BaseURL string `json:"baseURL"`
							APIKey  string `json:"apiKey"`
						} `json:"options"`
					} `json:"provider"`
				}
				if json.Unmarshal(data, &cfg) == nil {
					modelRef := cfg.Model
					if modelRef == "" {
						modelRef = cfg.SmallModel
					}
					info.Effort = cfg.ReasoningEffort

					// Fallback to agent-specific configs
					if modelRef == "" {
						for _, agent := range cfg.Agents {
							if agent.Model != "" {
								modelRef = agent.Model
								break
							}
						}
					}
					if modelRef == "" && cfg.Agent != nil {
						for _, agent := range cfg.Agent {
							if agent.Model != "" {
								modelRef = agent.Model
								break
							}
						}
					}

					if info.Effort == "" {
						for _, agent := range cfg.Agents {
							if agent.ReasoningEffort != "" {
								info.Effort = agent.ReasoningEffort
								break
							}
						}
					}
					if info.Effort == "" && cfg.Agent != nil {
						for _, agent := range cfg.Agent {
							if agent.ReasoningEffort != "" {
								info.Effort = agent.ReasoningEffort
								break
							}
						}
					}
					info.Model = strings.TrimPrefix(modelRef, "charon/")
					if provider, modelID, ok := strings.Cut(modelRef, "/"); ok {
						if model, ok := cfg.Provider[provider].Models[modelID]; ok {
							info.ContextWindow, info.MaxTokens = model.Limit.Context, model.Limit.Output
						}
					}
					if p, ok := cfg.Provider["charon"]; ok {
						info.Endpoint = p.Options.BaseURL
						if p.Options.APIKey != "" {
							info.Secret, info.AuthMode = p.Options.APIKey, "api"
						}
					}
					if info.Endpoint == "" {
						for _, p := range cfg.Provider {
							if p.Options.BaseURL != "" {
								info.Endpoint = p.Options.BaseURL
								break
							}
						}
					}
				}
			}

			// Otherwise fall back to a key stored in auth.json (login-based).
			if data, err := os.ReadFile(authPath); err == nil {
				var auth map[string]struct {
					Type   string `json:"type"`
					Key    string `json:"key"`
					Access string `json:"access"` // oauth: access token, sometimes a JWT (e.g. ChatGPT via /connect)
				}
				if json.Unmarshal(data, &auth) == nil {
					names := make([]string, 0, len(auth))
					for name := range auth {
						names = append(names, name)
					}
					sort.Strings(names) // map iteration order is random; keep this deterministic

					if info.AuthMode == "" {
						for _, name := range names {
							if p := auth[name]; p.Type == "api" && p.Key != "" {
								info.AuthMode, info.Secret = "api ("+name+")", p.Key
								break
							}
						}
						if info.AuthMode == "" {
							for _, name := range names {
								info.AuthMode = auth[name].Type + " (" + name + ")"
								break
							}
						}
					}

					// An oauth login's account identity must be detected independent of
					// which entry AuthMode picked above — e.g. an unrelated "opencode" api
					// key entry shouldn't hide a real ChatGPT/GitHub login for backup
					// purposes. Prefer a JWT-decodable email (ChatGPT via /connect) over a
					// bare provider name (e.g. github-copilot, whose token isn't a JWT).
					for _, name := range names {
						p := auth[name]
						if p.Type != "oauth" {
							continue
						}
						if info.Account == "" {
							info.Account = name
						}
						if email := decodeJWTEmail(p.Access); email != "" {
							info.Account = email
							break
						}
					}
				}
			}

			return info.withDefaults("(provider default)"), nil
		},
	}
}

func opencodeSpecsFromIDs(ids []string) []ModelSpec {
	// An empty list reads as "no list" (nil): a caller that gave only a
	// model must still fall through to the single-model default.
	if len(ids) == 0 {
		return nil
	}
	specs := make([]ModelSpec, 0, len(ids))
	for _, id := range ids {
		if id != "" {
			specs = append(specs, ModelSpec{Slug: id})
		}
	}
	return specs
}
