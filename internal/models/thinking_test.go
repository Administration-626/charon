package models

import (
	"slices"
	"strings"
	"testing"
)

func TestLookupThinkingPreset(t *testing.T) {
	levels := func(preset ThinkingPreset) map[string]*string { return preset.Levels }
	for _, tc := range []struct {
		name       string
		slug       string
		want       bool
		checkLevel func(ThinkingPreset) bool
		format     string
	}{
		// The most specific pattern wins: v4-pro differs from the v4 family.
		{"deepseek pro beats family", "deepseek-v4-pro", true,
			func(p ThinkingPreset) bool { return levels(p)["low"] == nil }, "deepseek"},
		{"deepseek family", "deepseek-ai/DeepSeek-V4-Flash", true,
			func(p ThinkingPreset) bool { return levels(p)["low"] != nil && *levels(p)["low"] == "low" }, "deepseek"},
		{"deepseek flash alias", "deepseek-flash", true,
			func(p ThinkingPreset) bool { return levels(p)["high"] != nil }, "deepseek"},
		{"glm official channel", "zai.glm-5.3", true,
			func(p ThinkingPreset) bool { return levels(p)["off"] == nil }, "zai"},
		{"glm variant suffix", "glm-5.3-flash", true,
			func(p ThinkingPreset) bool { return levels(p)["off"] == nil }, "zai"},
		{"glm 5.2 can disable", "glm-5.2", true,
			func(p ThinkingPreset) bool { return levels(p)["off"] != nil && *levels(p)["off"] == "none" }, "zai"},
		{"kimi openai dialect", "moonshotai/kimi-k3", true,
			func(p ThinkingPreset) bool { return levels(p)["off"] == nil && p.Format == "" }, ""},
		{"kimi k2 never sends effort", "kimi-k2.7-code", true,
			func(p ThinkingPreset) bool { return p.SupportsReasoningEffort != nil && !*p.SupportsReasoningEffort }, "deepseek"},
		{"qwen token plan", "qwen/qwen3.8-max", true,
			func(p ThinkingPreset) bool { return levels(p)["high"] == nil && levels(p)["xhigh"] != nil }, "qwen"},
		{"mimo toggle-only", "xiaomi/mimo-v2.5-pro", true,
			func(p ThinkingPreset) bool {
				return len(p.Levels) == 0 && p.Format == "deepseek" &&
					p.SupportsReasoningEffort != nil && !*p.SupportsReasoningEffort
			}, "deepseek"},
		{"grok 4.3 can disable", "xai/grok-4.3", true,
			func(p ThinkingPreset) bool { return levels(p)["off"] != nil && levels(p)["xhigh"] == nil }, ""},
		{"grok 4.6 locked", "grok-4.6", true,
			func(p ThinkingPreset) bool { return levels(p)["off"] == nil && levels(p)["xhigh"] != nil }, ""},
		{"grok 4.7 same as 4.6", "grok-4.7", true,
			func(p ThinkingPreset) bool { return levels(p)["xhigh"] != nil }, ""},
		{"gpt 5.6 full levels", "gpt-5.6-luna", true,
			func(p ThinkingPreset) bool { return levels(p)["max"] != nil && levels(p)["off"] != nil }, ""},
		{"older openai model", "o3-mini", true,
			func(p ThinkingPreset) bool { return levels(p)["xhigh"] == nil && levels(p)["off"] == nil }, ""},
		// Vendors without an authoritative dialect row stay unpreset on the
		// default (openai-completions) binding.
		{"grok 4.20 predates levels", "xai/grok-4.20", false, nil, ""},
		{"minimax not preset", "minimax-m3", false, nil, ""},
		{"muse not preset", "muse-spark-1.3", false, nil, ""},
		{"claude needs its dialect", "claude-opus-5", false, nil, ""},
		{"gemini not preset", "gemini-2.5-pro", false, nil, ""},
		{"unknown not preset", "totally-custom", false, nil, ""},
		{"empty not preset", "", false, nil, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			preset, ok := LookupThinkingPreset(tc.slug, "")
			if ok != tc.want {
				t.Fatalf("LookupThinkingPreset(%q) found = %v, want %v", tc.slug, ok, tc.want)
			}
			if !ok {
				return
			}
			if preset.Format != tc.format {
				t.Errorf("format = %q, want %q", preset.Format, tc.format)
			}
			if tc.checkLevel != nil && !tc.checkLevel(preset) {
				t.Errorf("preset levels unexpected: %#v", preset.Levels)
			}
		})
	}
}

func TestLookupThinkingPresetDialect(t *testing.T) {
	// Claude's official maps apply only to anthropic-messages bindings; on
	// openai-completions gateways the same slugs keep the generic fallback.
	preset, ok := LookupThinkingPreset("claude-opus-5", "anthropic-messages")
	if !ok || preset.Levels["off"] != nil || preset.Levels["xhigh"] == nil || !preset.ForceAdaptiveThinking {
		t.Fatalf("claude opus-5 anthropic preset = %#v, %v", preset, ok)
	}
	for _, slug := range []string{"claude-opus-4-6", "claude-sonnet-4-6"} {
		preset, ok := LookupThinkingPreset(slug, "anthropic-messages")
		if !ok || preset.Levels["max"] == nil || len(preset.Levels) != 7 {
			t.Fatalf("%s anthropic preset = %#v, %v", slug, preset, ok)
		}
	}
	preset, ok = LookupThinkingPreset("claude-sonnet-5-5", "anthropic-messages")
	if !ok || preset.Levels["off"] != nil || !preset.ForceAdaptiveThinking || len(preset.Levels) != 7 {
		t.Fatalf("claude sonnet-5-5 anthropic preset = %#v, %v", preset, ok)
	}
	// Prefix matching: fable-5 covers fable-5-1.
	if _, ok := LookupThinkingPreset("claude-fable-5-1", "anthropic-messages"); !ok {
		t.Fatal("claude-fable-5-1 should match the fable-5 preset")
	}
	// Dialect-bound rows are skipped for other protocols.
	if _, ok := LookupThinkingPreset("claude-opus-5", "openai-completions"); ok {
		t.Fatal("claude preset must not apply to openai-completions bindings")
	}
	if _, ok := LookupThinkingPreset("claude-opus-5", ""); ok {
		t.Fatal("claude preset must not apply without a dialect")
	}
}

func TestThinkingPresetRulesAreWellFormed(t *testing.T) {
	valid := map[string]bool{"off": true, "minimal": true, "low": true, "medium": true, "high": true, "xhigh": true, "max": true}
	for _, rule := range thinkingPresets {
		// Empty levels are valid for toggle-only APIs (MiMo): the map stays
		// unset and the compat flags carry the request format.
		for _, part := range strings.Split(rule.levels, ",") {
			if strings.TrimSpace(part) == "" {
				continue // toggle-only rows have an empty levels list
			}
			key, _, ok := strings.Cut(strings.TrimSpace(part), "=")
			if !ok || !valid[key] {
				t.Errorf("rule %q has invalid level pair %q", rule.pattern, part)
			}
		}
		switch rule.format {
		case "", "deepseek", "zai", "qwen":
		default:
			t.Errorf("rule %q has unsupported thinkingFormat %q", rule.pattern, rule.format)
		}
		if rule.maxTokens != "" && rule.maxTokens != "max_tokens" && rule.maxTokens != "max_completion_tokens" {
			t.Errorf("rule %q has invalid maxTokensField %q", rule.pattern, rule.maxTokens)
		}
	}
	// A preset whose only deltas are the level map must not emit an empty
	// compat object; gpt and grok rows carry no format override.
	for _, slug := range []string{"gpt-6-luna", "o3", "gpt-5", "grok-4.5"} {
		if preset, ok := LookupThinkingPreset(slug, ""); ok && preset.Format != "" {
			t.Errorf("%s unexpectedly carries a thinking format", slug)
		}
	}
	// Toggle-only presets must keep a non-empty compat (format flag).
	if preset, ok := LookupThinkingPreset("mimo-v2.5", ""); !ok || preset.Format == "" {
		t.Errorf("mimo preset = %#v, want deepseek format", preset)
	}
	// Dialect-bound rows must declare their request protocol.
	for _, rule := range thinkingPresets {
		if strings.HasPrefix(rule.pattern, "claude") && !slices.Equal(rule.apis, []string{"anthropic-messages"}) {
			t.Errorf("rule %q must bind anthropic-messages, apis = %v", rule.pattern, rule.apis)
		}
	}
}
