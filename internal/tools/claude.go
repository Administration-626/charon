package tools

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"charon/internal/secret"
)

const claudeKeychainService = "Claude Code-credentials"

// claudeMaxContextTokens is what a custom endpoint declares as its context window.
// Claude Code falls back to 200K for any model id it does not recognize, which is
// every model served through a gateway. One million matches the largest window the
// models routed this way advertise; a model with a smaller real window still stops
// at its own limit, it just no longer compacts at 200K.
const claudeMaxContextTokens = "1000000"

// newClaude describes Claude Code: API keys in ~/.claude/settings.json, OAuth in the keychain.
func newClaude() *Tool {
	dir := filepath.Join(home(), ".claude")
	settingsPath := filepath.Join(dir, "settings.json")

	return &Tool{
		Name:            "claude",
		Title:           "Claude Code",
		Provider:        "anthropic",
		ModelMenu:       "/model",
		DefaultEndpoint: "https://api.anthropic.com",
		ApplyAuth: func(a AuthSpec) error {
			// Preserve the already-registered list when a caller does not supply one.
			existingModels := claudeExistingModels(settingsPath)
			s, err := loadJSONMap(settingsPath)
			if err != nil {
				return err
			}
			env := subMap(s, "env")
			// Clear every auth key so we never send conflicting headers or a stale base URL.
			delete(env, "ANTHROPIC_API_KEY")
			delete(env, "ANTHROPIC_AUTH_TOKEN")
			delete(env, "ANTHROPIC_BASE_URL")
			delete(env, "ANTHROPIC_MODEL")
			delete(env, "ANTHROPIC_CUSTOM_MODEL_OPTION")
			// Claude Code treats a model id it does not recognize as "unknown" and clamps
			// its context window to 200K, which makes a larger model compact constantly.
			// Declaring the window lifts that fallback. Only set for a custom endpoint;
			// the stock branch below clears it so an official login is not pinned.
			delete(env, "CLAUDE_CODE_MAX_CONTEXT_TOKENS")

			custom := a.Endpoint != "" && !strings.Contains(a.Endpoint, "api.anthropic.com")
			if custom {
				// Gateways want Bearer auth at a custom base URL.
				env["ANTHROPIC_BASE_URL"] = normalizeClaudeBaseURL(a.Endpoint)
				env["ANTHROPIC_AUTH_TOKEN"] = a.Key
				env["CLAUDE_CODE_MAX_CONTEXT_TOKENS"] = claudeMaxContextTokens
				// Gateway models aren't in Claude Code's catalog; the top-level "model"
				// selector validates against it and rejects them, so route via ANTHROPIC_MODEL.
				delete(s, "model")
				if a.Model != "" {
					env["ANTHROPIC_MODEL"] = a.Model
					// Gateway model discovery only surfaces ids prefixed "claude"/"anthropic" in
					// /model, which most gateway model ids aren't. ANTHROPIC_CUSTOM_MODEL_OPTION
					// adds this one model to the picker regardless of its id shape.
					env["ANTHROPIC_CUSTOM_MODEL_OPTION"] = a.Model
				}
				// A gateway serves models from many vendors under one endpoint, so curate the
				// whole fetched list into /model via modelPicker — the same "register every
				// model, not just the chosen one" move opencode.go and pi.go already make.
				// Without it /model holds one row (ANTHROPIC_CUSTOM_MODEL_OPTION above) and
				// switching model means going back through charon.
				ids := a.AllModels
				if len(ids) == 0 {
					ids = existingModels
				}
				if picker := claudeModelPicker(ids, a.Model); picker != nil {
					s["modelPicker"] = picker
				} else {
					delete(s, "modelPicker")
				}
			} else {
				// Anthropic's own API uses x-api-key. Leave ANTHROPIC_BASE_URL unset: pointing it
				// at the default endpoint makes Claude Code treat it as a gateway and break connectors.
				env["ANTHROPIC_API_KEY"] = a.Key
				// Pre-approve the key (and un-disable it) so a prior "No" can't leave it ignored.
				approveClaudeAPIKey(s, a.Key)
				// Stock models are in the catalog, so the top-level selector is preferred. Any
				// gateway's modelPicker must go first, or the official account inherits its list.
				delete(s, "modelPicker")
				if a.Model != "" {
					s["model"] = a.Model
				}
			}
			return writeJSONMap(settingsPath, s, 0o600)
		},
		Detected: func() bool {
			if detected("claude", settingsPath) {
				return true
			}
			_, err := secret.KeychainRead(claudeKeychainService)
			return err == nil
		},
		Describe: func() (Info, error) {
			var info Info

			if data, err := os.ReadFile(settingsPath); err == nil {
				var s struct {
					Model       string `json:"model"`
					EffortLevel string `json:"effortLevel"`
					Env         struct {
						BaseURL   string `json:"ANTHROPIC_BASE_URL"`
						APIKey    string `json:"ANTHROPIC_API_KEY"`
						AuthToken string `json:"ANTHROPIC_AUTH_TOKEN"`
						Model     string `json:"ANTHROPIC_MODEL"`
					} `json:"env"`
				}
				if json.Unmarshal(data, &s) == nil {
					info.Endpoint = s.Env.BaseURL
					info.Model = s.Model
					if info.Model == "" {
						info.Model = s.Env.Model
					}
					info.Effort = s.EffortLevel
					if s.Env.AuthToken != "" {
						info.Secret, info.AuthMode = s.Env.AuthToken, "api (bearer)"
					} else if s.Env.APIKey != "" {
						info.Secret, info.AuthMode = s.Env.APIKey, "api"
					}
				}
			}

			if info.AuthMode == "" {
				if _, err := secret.KeychainRead(claudeKeychainService); err == nil {
					info.AuthMode = "oauth"
				}
			}

			info.Account = claudeAccountEmail()

			return info.withDefaults("api.anthropic.com (default)"), nil
		},
	}
}

// claudeAccountEmail reads the logged-in account's email from ~/.claude.json for
// display only; the file is never written. "" if absent.
func claudeAccountEmail() string {
	data, err := os.ReadFile(filepath.Join(home(), ".claude.json"))
	if err != nil {
		return ""
	}
	var c struct {
		OAuthAccount struct {
			EmailAddress string `json:"emailAddress"`
		} `json:"oauthAccount"`
	}
	if json.Unmarshal(data, &c) != nil {
		return ""
	}
	return c.OAuthAccount.EmailAddress
}

// claudeExistingModels reads the model ids currently registered in settings.json's
// modelPicker, so a write without a replacement list can keep them.
func claudeExistingModels(settingsPath string) []string {
	data, err := os.ReadFile(settingsPath)
	if err != nil {
		return nil
	}
	var s struct {
		ModelPicker struct {
			Options []struct {
				Model string `json:"model"`
			} `json:"options"`
		} `json:"modelPicker"`
	}
	if json.Unmarshal(data, &s) != nil {
		return nil
	}
	ids := make([]string, 0, len(s.ModelPicker.Options))
	for _, o := range s.ModelPicker.Options {
		if o.Model != "" {
			ids = append(ids, o.Model)
		}
	}
	return ids
}

// normalizeClaudeBaseURL trims a trailing "/v1": Claude appends "/v1/messages", so a
// "/v1" base URL 404s as "/v1/v1/messages". (Claude-only; Codex genuinely wants "/v1".)
func normalizeClaudeBaseURL(ep string) string {
	ep = strings.TrimRight(ep, "/")
	ep = strings.TrimSuffix(ep, "/v1")
	return strings.TrimRight(ep, "/")
}

// claudeModelPicker builds the settings.json "modelPicker" value that curates Claude
// Code's /model menu: one row per gateway model, labeled with its own id, plus
// replaceBuiltInOptions so Claude Code's built-in lineup is hidden — a gateway rarely
// serves those ids, so offering them only yields failed requests. Returns nil when
// there is nothing to register, so the caller deletes any stale modelPicker rather
// than writing an empty one (which would blank the menu).
func claudeModelPicker(allModels []string, model string) map[string]any {
	ids := claudeModelIDs(allModels, model)
	if len(ids) == 0 {
		return nil
	}
	options := make([]map[string]any, 0, len(ids))
	for _, id := range ids {
		options = append(options, map[string]any{"model": id, "label": id})
	}
	return map[string]any{"options": options, "replaceBuiltInOptions": true}
}

// claudeModelIDs returns the model ids to register, in first-seen order: every fetched
// id, then the configured model as a fallback for callers that brought no list (the
// CLI's --model flag). Blank and duplicate ids are dropped.
func claudeModelIDs(allModels []string, model string) []string {
	seen := make(map[string]bool, len(allModels)+1)
	var ids []string
	for _, id := range append(append([]string{}, allModels...), model) {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		ids = append(ids, id)
	}
	return ids
}

// claudeKeyIDLen is how many trailing characters of a key Claude Code uses as its ID.
const claudeKeyIDLen = 20

// claudeKeyID is how Claude Code identifies a key: its last claudeKeyIDLen characters.
func claudeKeyID(key string) string {
	if len(key) <= claudeKeyIDLen {
		return key
	}
	return key[len(key)-claudeKeyIDLen:]
}

// approveClaudeAPIKey marks key approved and un-disabled in customApiKeyResponses,
// so Claude Code stops prompting and a prior rejection can't keep it switched off.
func approveClaudeAPIKey(s map[string]any, key string) {
	if key == "" {
		return
	}
	id := claudeKeyID(key)
	resp := subMap(s, "customApiKeyResponses")

	resp["approved"] = addString(toStringSlice(resp["approved"]), id)
	resp["disabled"] = removeString(toStringSlice(resp["disabled"]), id)
}

// toStringSlice coerces a JSON-decoded []any (or []string) into a []string.
func toStringSlice(v any) []string {
	switch t := v.(type) {
	case []string:
		return t
	case []any:
		out := make([]string, 0, len(t))
		for _, e := range t {
			if s, ok := e.(string); ok {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

func addString(list []string, s string) []string {
	for _, e := range list {
		if e == s {
			return list
		}
	}
	return append(list, s)
}

func removeString(list []string, s string) []string {
	out := list[:0]
	for _, e := range list {
		if e != s {
			out = append(out, e)
		}
	}
	return out
}
