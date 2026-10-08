package tools

import (
	"os"
	"path/filepath"
	"testing"

	"charon/internal/models"
)

func TestDescribeReadsConfiguredTokenLimits(t *testing.T) {
	for _, tc := range []struct {
		name, tool string
		files      map[string]string
		context    int
		output     int
	}{
		{"pi selected model", "pi", map[string]string{
			".pi/agent/settings.json": `{"defaultProvider":"charon","defaultModel":"gpt-6-luna"}`,
			".pi/agent/models.json":   `{"providers":{"charon":{"apiKey":"sk-test","models":[{"id":"other","contextWindow":900000,"maxTokens":90000},{"id":"gpt-6-luna","contextWindow":420000,"maxTokens":32000}]}}}`,
		}, 420000, 32000},
		{"pi absent limits are not inferred", "pi", map[string]string{
			".pi/agent/settings.json": `{"defaultProvider":"charon","defaultModel":"gpt-6-luna"}`,
			".pi/agent/models.json":   `{"providers":{"charon":{"apiKey":"sk-test","models":[{"id":"gpt-6-luna"}]}}}`,
		}, 0, 0},
		{"pi inactive provider", "pi", map[string]string{
			".pi/agent/settings.json": `{"defaultProvider":"other","defaultModel":"gpt-6-luna"}`,
			".pi/agent/models.json":   `{"providers":{"charon":{"apiKey":"sk-test","models":[{"id":"gpt-6-luna","contextWindow":420000,"maxTokens":32000}]}}}`,
		}, 0, 0},
		{"pi legacy extension", "pi", map[string]string{
			".pi/agent/settings.json":        `{"defaultProvider":"charon","defaultModel":"legacy-model"}`,
			".pi/agent/extensions/charon.ts": string(legacyPiExtension(t)),
		}, 128000, 32000},
		{"codex global overrides catalog", "codex", map[string]string{
			".codex/config.toml":        "model = 'gpt-6-luna'\nmodel_context_window = 256000\nmodel_catalog_json = 'custom_models.json'\n",
			".codex/custom_models.json": `{"models":[{"slug":"gpt-6-luna","context_window":420000}]}`,
		}, 256000, 0},
		{"codex selected catalog entry", "codex", map[string]string{
			".codex/config.toml":        "model = 'gpt-6-luna'\nmodel_catalog_json = 'custom_models.json'\n",
			".codex/custom_models.json": `{"models":[{"slug":"other","context_window":900000},{"slug":"gpt-6-luna","context_window":420000}]}`,
		}, 420000, 0},
		{"codex native defaults not inferred", "codex", map[string]string{
			".codex/config.toml": "model = 'gpt-6-luna'\n",
		}, 0, 0},
		{"claude env override", "claude", map[string]string{
			".claude/settings.json": `{"env":{"ANTHROPIC_API_KEY":"sk-test","ANTHROPIC_MODEL":"custom","CLAUDE_CODE_MAX_CONTEXT_TOKENS":"420000"}}`,
		}, 420000, 0},
		{"claude invalid env override", "claude", map[string]string{
			".claude/settings.json": `{"env":{"ANTHROPIC_API_KEY":"sk-test","CLAUDE_CODE_MAX_CONTEXT_TOKENS":"invalid"}}`,
		}, 0, 0},
		{"opencode qualified model id", "opencode", map[string]string{
			".config/opencode/opencode.json": `{"model":"charon/openai/gpt-6-luna","provider":{"charon":{"models":{"openai/gpt-6-luna":{"limit":{"context":420000,"output":32000}}}}}}`,
		}, 420000, 32000},
		{"opencode agent fallback", "opencode", map[string]string{
			".config/opencode/opencode.json": `{"agent":{"build":{"model":"charon/openai/gpt-6-luna"}},"provider":{"charon":{"models":{"openai/gpt-6-luna":{"limit":{"context":420000,"output":32000}}}}}}`,
		}, 420000, 32000},
		{"opencode other provider", "opencode", map[string]string{
			".config/opencode/opencode.json": `{"model":"other/gpt-6-luna","provider":{"charon":{"models":{"gpt-6-luna":{"limit":{"context":900000}}}},"other":{"models":{"gpt-6-luna":{"limit":{"context":420000,"output":32000}}}}}}`,
		}, 420000, 32000},
		{"grok selected model", "grok", map[string]string{
			".grok/config.toml": "[models]\ndefault = 'charon-luna'\n[model.other]\ncontext_window = 900000\n[model.charon-luna]\nmodel = 'gpt-6-luna'\ncontext_window = 420000\n",
		}, 420000, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := sandboxHome(t)
			for name, content := range tc.files {
				writeFile(t, filepath.Join(home, name), content)
			}
			info, err := Find(tc.tool).Describe()
			if err != nil {
				t.Fatal(err)
			}
			if info.ContextWindow != tc.context || info.MaxTokens != tc.output {
				t.Fatalf("limits = %d/%d, want %d/%d", info.ContextWindow, info.MaxTokens, tc.context, tc.output)
			}
			for name, content := range tc.files {
				data, err := os.ReadFile(filepath.Join(home, name))
				if err != nil || string(data) != content {
					t.Fatalf("Describe changed %s: %v", name, err)
				}
			}
		})
	}
}

func TestCodexDescribeAPIKeyAuthMode(t *testing.T) {
	home := sandboxHome(t)
	writeFile(t, filepath.Join(home, ".codex", "auth.json"),
		`{"auth_mode":"apikey","OPENAI_API_KEY":"sk-plain-123"}`)

	info, _ := Find("codex").Describe()
	if info.AuthMode != "api" || info.Secret != "sk-plain-123" {
		t.Errorf("apikey auth_mode: got AuthMode=%q Secret=%q, want api/sk-plain-123", info.AuthMode, info.Secret)
	}
}

func TestCodexDescribeUnknownAuthModePassesThrough(t *testing.T) {
	home := sandboxHome(t)
	writeFile(t, filepath.Join(home, ".codex", "auth.json"), `{"auth_mode":"something-else"}`)

	info, _ := Find("codex").Describe()
	if info.AuthMode != "something-else" {
		t.Errorf("AuthMode = %q, want passthrough of unrecognized auth_mode", info.AuthMode)
	}
}

func TestCodexContextWindow(t *testing.T) {
	cases := map[string]int{
		"claude-opus-4-7": models.DefaultContextCeiling,
		"CLAUDE-SONNET":   models.DefaultContextCeiling,
		"deepseek-chat":   models.DefaultContextCeiling,
		"gemini-2.5-pro":  models.DefaultContextCeiling,
		"qwen-2.5-coder":  models.DefaultContextCeiling,
		"custom-slug":     models.DefaultContextCeiling,
		"gpt-5.5":         0,
		"o1-mini":         0,
		"o3-mini":         0,
		"chatgpt-4o":      0,
		"openai/gpt-4o":   0,
		"":                0,
	}
	for model, want := range cases {
		if got := codexContextWindow(model, 0); got != want {
			t.Errorf("codexContextWindow(%q) = %d, want %d", model, got, want)
		}
	}
}

func TestClaudeKeyIDShortKeyReturnsAsIs(t *testing.T) {
	if got := claudeKeyID("short"); got != "short" {
		t.Errorf("claudeKeyID(short) = %q, want unchanged", got)
	}
}

func TestOpenCodeDescribeFallsBackToAuthJSONLogin(t *testing.T) {
	home := sandboxHome(t)
	writeFile(t, filepath.Join(home, ".local", "share", "opencode", "auth.json"),
		`{"anthropic":{"type":"oauth"}}`)

	info, _ := Find("opencode").Describe()
	if info.AuthMode != "oauth (anthropic)" {
		t.Errorf("AuthMode = %q, want oauth (anthropic)", info.AuthMode)
	}
	// An OAuth login carries no email, unlike Claude/Codex, so status falls back to
	// showing the provider name as the account identity.
	if info.Account != "anthropic" {
		t.Errorf("Account = %q, want anthropic (provider-name fallback for oauth logins)", info.Account)
	}
}

func TestOpenCodeDescribeReportsAccountAfterProviderLogin(t *testing.T) {
	sandboxHome(t)
	writeFile(t, filepath.Join(home(), ".local", "share", "opencode", "auth.json"),
		`{"github-copilot":{"type":"oauth","refresh":"r","access":"a","expires":0}}`)

	info, err := Find("opencode").Describe()
	if err != nil {
		t.Fatal(err)
	}
	if info.Account == "" {
		t.Fatal("Account should be set after an OAuth provider login for status display")
	}
	if info.Account != "github-copilot" {
		t.Errorf("Account = %q, want github-copilot", info.Account)
	}
}

// TestOpenCodeAccountDetectedDespiteUnrelatedAPIKeyEntry reproduces the real bug: a
// user connects to ChatGPT via OpenCode's own `/connect`, but auth.json also has an
// unrelated "opencode" API-key entry (OpenCode's own hosted-models login). Account
// detection must still report the OAuth identity for status.
func TestOpenCodeAccountDetectedDespiteUnrelatedAPIKeyEntry(t *testing.T) {
	sandboxHome(t)
	// Matches the real shape of an OpenAI access token (as ChatGPT via /connect stores
	// it): email nested under this OIDC profile claim, not a top-level "email".
	jwt := makeJWT(t, map[string]any{
		"https://api.openai.com/profile": map[string]any{"email": "user@example.com"},
	})
	writeFile(t, filepath.Join(home(), ".local", "share", "opencode", "auth.json"), `{
		"opencode": {"type":"api","key":"sk-opencode-own-key"},
		"openai": {"type":"oauth","access":"`+jwt+`","refresh":"r","expires":0}
	}`)

	info, err := Find("opencode").Describe()
	if err != nil {
		t.Fatal(err)
	}
	if info.Account != "user@example.com" {
		t.Errorf("Account = %q, want user@example.com (the ChatGPT oauth login's JWT email)", info.Account)
	}
}

func TestOpenCodeDescribeFallsBackToNonCharonProviderEndpoint(t *testing.T) {
	home := sandboxHome(t)
	writeFile(t, filepath.Join(home, ".config", "opencode", "opencode.jsonc"),
		`{"provider":{"myllm":{"options":{"baseURL":"https://mine/v1"}}}}`)

	info, _ := Find("opencode").Describe()
	if info.Endpoint != "https://mine/v1" {
		t.Errorf("Endpoint = %q, want fallback to user provider's baseURL", info.Endpoint)
	}
}

func TestOpenCodeConfigPathPrefersExistingJSONC(t *testing.T) {
	home := sandboxHome(t)
	// No config yet: charon must default to the jsonc path.
	if got, want := opencodeConfigPath(), filepath.Join(home, ".config", "opencode", "opencode.jsonc"); got != want {
		t.Errorf("default config path = %q, want %q", got, want)
	}

	// A legacy opencode.json on disk (and no .jsonc) must be edited in place.
	writeFile(t, filepath.Join(home, ".config", "opencode", "opencode.json"), `{}`)
	if got, want := opencodeConfigPath(), filepath.Join(home, ".config", "opencode", "opencode.json"); got != want {
		t.Errorf("legacy config path = %q, want %q", got, want)
	}
}
