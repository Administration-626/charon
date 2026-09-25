package tools

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// grokModelPrefix marks every [model.*] table charon owns in ~/.grok/config.toml.
// The picker name is this prefix plus the API slug; the table's model field stays
// the slug the endpoint actually accepts.
const grokModelPrefix = "charon-"

func grokOwnedModel(name string) bool {
	return strings.HasPrefix(name, grokModelPrefix)
}

func grokMenuName(slug string) string {
	return grokModelPrefix + slug
}

// grokExistingModels returns the API slugs already registered under charon- tables.
func grokExistingModels(models map[string]any) []string {
	ids := make([]string, 0, len(models))
	for name, raw := range models {
		if !grokOwnedModel(name) {
			continue
		}
		slug := strings.TrimPrefix(name, grokModelPrefix)
		if entry, ok := raw.(map[string]any); ok {
			if declared, _ := entry["model"].(string); strings.TrimSpace(declared) != "" {
				slug = strings.TrimSpace(declared)
			}
		}
		if slug != "" {
			ids = append(ids, slug)
		}
	}
	sort.Strings(ids)
	return ids
}

// grokSnapshotUserModels records every model table charon does not own.
func grokSnapshotUserModels(models map[string]any) map[string]string {
	snap := map[string]string{}
	for name, v := range models {
		if grokOwnedModel(name) {
			continue
		}
		b, _ := json.Marshal(v)
		snap[name] = string(b)
	}
	return snap
}

// grokTable returns m[key] when it is a TOML table, creating it when absent.
// A key that exists but is not a table is left untouched and reported, so a
// stray scalar cannot be overwritten by the model registry.
func grokTable(m map[string]any, key string) (map[string]any, error) {
	v, ok := m[key]
	if !ok {
		created := map[string]any{}
		m[key] = created
		return created, nil
	}
	table, ok := v.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("refusing to write config: %q is not a table", key)
	}
	return table, nil
}

func grokModelEntry(slug, endpoint, key string) map[string]any {
	entry := map[string]any{
		"model":    slug,
		"name":     slug,
		"base_url": endpoint,
	}
	if key != "" {
		entry["api_key"] = key
	}
	return entry
}

// grokAccount names a browser or OIDC login from auth.json without returning the
// token. A JWT key yields its email; otherwise the first issuer key is the label.
func grokAccount(auth map[string]any) string {
	names := make([]string, 0, len(auth))
	for name, raw := range auth {
		names = append(names, name)
		entry, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		token, _ := entry["key"].(string)
		if email := decodeJWTEmail(token); email != "" {
			return email
		}
	}
	sort.Strings(names)
	if len(names) == 0 {
		return ""
	}
	return names[0]
}

// newGrok describes the Grok CLI (~/.grok/config.toml). Named models live in
// [model.<name>] tables and [models].default selects the one a new session starts
// on. auth.json (browser, OIDC, external-provider login) is never written.
func newGrok() *Tool {
	dir := filepath.Join(home(), ".grok")
	configPath := filepath.Join(dir, "config.toml")
	authPath := filepath.Join(dir, "auth.json")

	return &Tool{
		Name:            "grok",
		Title:           "Grok",
		Provider:        "openai",
		ModelMenu:       "/model",
		DefaultEndpoint: "https://api.x.ai/v1",
		ApplyAuth: func(a AuthSpec) error {
			cfg, err := loadTOMLMap(configPath)
			if err != nil {
				return err
			}
			modelTable, err := grokTable(cfg, "model")
			if err != nil {
				return err
			}
			original := grokSnapshotUserModels(modelTable)

			ids := a.AllModels
			if len(ids) == 0 {
				ids = grokExistingModels(modelTable)
			}
			modelSlug := strings.TrimSpace(a.Model)
			if len(ids) == 0 && modelSlug != "" {
				ids = []string{modelSlug}
			}
			if modelSlug == "" && len(ids) > 0 {
				modelSlug = strings.TrimSpace(ids[0])
			}

			// Replace the owned tables outright so a shorter list cannot keep the
			// previous endpoint's models. User-authored [model.*] tables stay.
			if modelSlug != "" || len(ids) > 0 {
				for name := range modelTable {
					if grokOwnedModel(name) {
						delete(modelTable, name)
					}
				}
				seen := map[string]bool{}
				for _, id := range ids {
					id = strings.TrimSpace(id)
					if id == "" || seen[id] {
						continue
					}
					seen[id] = true
					modelTable[grokMenuName(id)] = grokModelEntry(id, a.Endpoint, a.Key)
				}
			}
			if err := ensureOnlyCharonChanged(original, modelTable); err != nil {
				return err
			}

			if modelSlug != "" {
				defaults, err := grokTable(cfg, "models")
				if err != nil {
					return err
				}
				defaults["default"] = grokMenuName(modelSlug)
			}
			return writeTOMLMap(configPath, cfg, 0o600)
		},
		Detected: func() bool {
			return detected("grok", configPath, authPath)
		},
		Describe: func() (Info, error) {
			var info Info
			cfg, err := loadTOMLMap(configPath)
			if err != nil {
				return info, err
			}
			modelTable, _ := cfg["model"].(map[string]any)
			defaults, _ := cfg["models"].(map[string]any)
			defName, _ := defaults["default"].(string)
			if entry, ok := modelTable[defName].(map[string]any); ok {
				if slug, _ := entry["model"].(string); strings.TrimSpace(slug) != "" {
					info.Model = strings.TrimSpace(slug)
				} else if grokOwnedModel(defName) {
					info.Model = strings.TrimPrefix(defName, grokModelPrefix)
				} else {
					info.Model = defName
				}
				if ep, _ := entry["base_url"].(string); ep != "" {
					info.Endpoint = ep
				}
				if key, _ := entry["api_key"].(string); key != "" {
					info.Secret, info.AuthMode = key, "api"
				}
			} else if defName != "" {
				info.Model = defName
			}

			if info.AuthMode == "" {
				if data, err := os.ReadFile(authPath); err == nil && len(data) > 0 {
					var auth map[string]any
					if json.Unmarshal(data, &auth) == nil && len(auth) > 0 {
						info.AuthMode = "oauth"
						info.Account = grokAccount(auth)
					}
				}
			}
			return info.withDefaults("api.x.ai (default)"), nil
		},
	}
}
