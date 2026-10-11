package models

import (
	"slices"
	"strings"
)

// ThinkingPreset is a vendor's verified thinking-level configuration for one
// model family. Presets are curated from WorkBuddy model presets (official
// provider channels first, see docs/workbuddy-context-sync.md) and official
// provider specifications. They describe the vendor's own request shape;
// aggregators that rewrite the reasoning dialect (OpenRouter and friends) are
// intentionally not preset, because a charon gateway cannot be told apart
// from the vendor endpoint. Gemini and MiniMax are omitted: their official
// endpoints speak another dialect, so an OpenAI-completions preset would be a
// guess. Claude rows bind to anthropic-messages bindings only. Empty Levels
// mean a toggle-only vendor API (e.g. Xiaomi MiMo): pi gets the thinking
// switch and never an effort value.
type ThinkingPreset struct {
	// Levels maps Pi thinking levels to vendor-native values. A nil value
	// marks a level the model does not support; an absent key stays unmapped
	// (Pi then offers the level and passes its own name through). Empty means
	// a toggle-only vendor API (e.g. Xiaomi MiMo): the map stays unset.
	Levels map[string]*string

	// APIs restricts the preset to bindings speaking these Pi request
	// protocols (e.g. "anthropic-messages" for Claude, whose official
	// thinking maps only apply to that dialect). Nil applies everywhere.
	APIs []string

	// OpenAI-completions request-format flags. Zero values keep Pi's derived
	// defaults, which assume a generic OpenAI-style endpoint.
	Format string // compat.thinkingFormat, e.g. "deepseek", "zai", "qwen"
	// SupportsReasoningEffort nil lets Pi derive the flag; vendors that reject
	// reasoning_effort need an explicit false so Pi never sends it.
	SupportsReasoningEffort *bool
	MaxTokensField          string // "max_tokens" when the vendor API rejects max_completion_tokens
	// RequiresReasoningContentOnAssistantMessages mirrors the DeepSeek-API
	// requirement to tag assistant reasoning content on follow-up turns.
	RequiresReasoningContentOnAssistantMessages bool
	// NoStore records vendor APIs that reject `store` and the developer role.
	NoStore bool
	// ForceAdaptiveThinking marks Anthropic adaptive-thinking models: pi must
	// send thinking:{type:"adaptive"} + output_config.effort instead of the
	// legacy budget_tokens shape. Pi never infers this at a gateway.
	ForceAdaptiveThinking bool
}

// thinkingRule is one curated preset row. levels uses the same level=value
// syntax as charon's TUI thinking-map field: a value of "off" records an
// unavailable (null) level, and omitted levels stay unmapped. apis lists the
// Pi request protocols the row applies to (nil = all dialects).
type thinkingRule struct {
	pattern          string
	levels           string
	format           string
	effort           *bool // nil lets Pi derive supportsReasoningEffort
	maxTokens        string
	reasoningContent bool
	noStore          bool
	adaptive         bool
	apis             []string
}

func boolPtr(b bool) *bool { return &b }

// thinkingPresets is hand-curated from `scripts/extract_workbuddy_models.py
// --thinking` drafts. Rows are matched by the built-in context-window prefix
// rules, longest pattern first, so a pattern also covers its dated and variant
// suffixes (glm-5.3 covers glm-5.3-flash; deepseek-v4-pro beats deepseek-v4).
var thinkingPresets = []thinkingRule{
	// OpenAI — plain reasoning_effort, no format override needed. 5.1+ models
	// accept off:"none"; older ones and -pro variants cannot disable thinking.
	{pattern: "gpt-5-pro", levels: "off=off,minimal=off,low=off,medium=off,high=high,xhigh=off,max=off"},
	{pattern: "gpt-5.5-pro", levels: "off=off,minimal=off,low=off,medium=medium,high=high,xhigh=xhigh,max=off"},
	{pattern: "gpt-5.4-pro", levels: "off=off,minimal=off,low=off,medium=medium,high=high,xhigh=xhigh,max=off"},
	{pattern: "gpt-5.6", levels: "off=none,minimal=off,low=low,medium=medium,high=high,xhigh=xhigh,max=max"},
	{pattern: "gpt-6-luna", levels: "off=none,minimal=off,low=low,medium=medium,high=high,xhigh=xhigh,max=max"},
	{pattern: "gpt-6-sol", levels: "off=none,minimal=off,low=low,medium=medium,high=high,xhigh=xhigh,max=max"},
	{pattern: "gpt-6-astra", levels: "off=off,minimal=off,low=low,medium=medium,high=high,xhigh=xhigh,max=max"},
	{pattern: "gpt-6.1", levels: "off=off,minimal=off,low=low,medium=medium,high=high,xhigh=xhigh,max=max"},
	{pattern: "gpt-6", levels: "off=off,minimal=off,low=low,medium=medium,high=high,xhigh=xhigh,max=max"},
	{pattern: "gpt-5.3-codex", levels: "off=none,minimal=off,low=low,medium=medium,high=high,xhigh=xhigh"},
	{pattern: "gpt-5.3", levels: "off=off,minimal=off,low=off,medium=off,high=off,xhigh=xhigh,max=off"},
	{pattern: "gpt-5.5", levels: "off=none,minimal=off,low=low,medium=medium,high=high,xhigh=xhigh,max=off"},
	{pattern: "gpt-5.4", levels: "off=none,minimal=off,low=low,medium=medium,high=high,xhigh=xhigh,max=off"},
	{pattern: "gpt-5.2", levels: "off=none,minimal=off,low=low,medium=medium,high=high,xhigh=xhigh,max=off"},
	{pattern: "gpt-5.1", levels: "off=none,minimal=off,low=low,medium=medium,high=high,xhigh=off,max=off"},
	{pattern: "gpt-5", levels: "off=off,minimal=minimal,low=low,medium=medium,high=high,xhigh=off,max=off"},
	{pattern: "o1", levels: "off=off,minimal=off,low=low,medium=medium,high=high,xhigh=off,max=off"},
	{pattern: "o3", levels: "off=off,minimal=off,low=low,medium=medium,high=high,xhigh=off,max=off"},
	{pattern: "o4", levels: "off=off,minimal=off,low=low,medium=medium,high=high,xhigh=off,max=off"},

	// DeepSeek — thinking:{type} switch plus reasoning_effort. The off level
	// stays unmapped so Pi can send thinking:{type:"disabled"}.
	{pattern: "deepseek-v4-pro", levels: "minimal=off,low=off,medium=off,high=high,max=max",
		format: "deepseek", maxTokens: "max_tokens", reasoningContent: true, noStore: true},
	{pattern: "deepseek-flash", levels: "minimal=off,low=low,medium=off,high=high,max=max",
		format: "deepseek", maxTokens: "max_tokens", reasoningContent: true, noStore: true},
	{pattern: "deepseek-v4", levels: "minimal=off,low=low,medium=off,high=high,max=max",
		format: "deepseek", maxTokens: "max_tokens", reasoningContent: true, noStore: true},

	// Zhipu GLM — z.ai thinking switch plus reasoning_effort; 5.3 cannot
	// disable thinking, 4.7 has no effort levels at all.
	{pattern: "glm-5.3", levels: "off=off,minimal=off,low=low,medium=off,high=high,xhigh=off,max=max",
		format: "zai", effort: boolPtr(true), maxTokens: "max_tokens", noStore: true},
	{pattern: "glm-5.2", levels: "off=none,minimal=off,low=off,medium=off,high=high,xhigh=off,max=max",
		format: "zai", effort: boolPtr(true), maxTokens: "max_tokens", noStore: true},
	{pattern: "glm-4.7", levels: "off=none",
		format: "zai", effort: boolPtr(false), maxTokens: "max_tokens", noStore: true},

	// Moonshot Kimi — k3 speaks the OpenAI dialect; k2.7-code cannot disable
	// thinking, k2 toggles only and rejects reasoning_effort.
	{pattern: "kimi-k3", levels: "off=off,minimal=off,low=low,medium=off,high=high,xhigh=off,max=max",
		maxTokens: "max_tokens", reasoningContent: true, noStore: true},
	{pattern: "kimi-k2.7-code", levels: "off=off",
		format: "deepseek", effort: boolPtr(false), maxTokens: "max_tokens", noStore: true},
	{pattern: "kimi-k2", levels: "minimal=off,low=off,medium=off",
		format: "deepseek", effort: boolPtr(false), maxTokens: "max_tokens", noStore: true},

	// Qwen — DashScope token-plan channel: enable_thinking plus
	// reasoning_effort, no minimal/high/max tiers.
	{pattern: "qwen3.8", levels: "off=off,minimal=off,low=low,medium=medium,high=off,xhigh=xhigh,max=off",
		format: "qwen", effort: boolPtr(true), noStore: true},

	// Xiaomi MiMo — toggle-only API (no effort levels): thinking switch plus
	// reasoning-content tagging, never a reasoning_effort value. Empty levels
	// keep pi's thinkingLevelMap unset, matching the official xiaomi channel.
	{pattern: "mimo", levels: "",
		format: "deepseek", effort: boolPtr(false), reasoningContent: true},

	// xAI Grok — official channel publishes level maps (openai-responses
	// dialect) but rejects reasoning_effort, which pi only knows behind its
	// x.ai endpoint fingerprint; the explicit false preserves that at a
	// gateway. 4.3 can disable thinking, 4.5+ cannot. grok-4.20 predates the
	// level API and stays unpreset.
	{pattern: "grok-4.3", levels: "off=none,minimal=off,low=low,medium=medium,high=high,xhigh=off,max=off",
		effort: boolPtr(false)},
	{pattern: "grok-4.5", levels: "off=off,minimal=off,low=low,medium=medium,high=high,xhigh=off,max=off",
		effort: boolPtr(false)},
	{pattern: "grok-4.6", levels: "off=off,minimal=off,low=low,medium=medium,high=high,xhigh=xhigh,max=off",
		effort: boolPtr(false)},
	{pattern: "grok-4.7", levels: "off=off,minimal=off,low=low,medium=medium,high=high,xhigh=xhigh,max=off",
		effort: boolPtr(false)},

	// Anthropic Claude — official channel maps and adaptive-thinking flags,
	// valid only on bindings that speak the Anthropic dialect: on
	// openai-completions gateways the same slugs carry different
	// aggregator-rewritten levels, so those stay on the generic fallback.
	// Every level-mapped generation is adaptive; pi cannot infer that at a
	// gateway, so the flag travels with the preset. Generations before 4-6
	// (haiku/opus/sonnet-4-5) ship no level map and stay unpreset.
	{pattern: "claude-opus-4-6", levels: "off=off,minimal=off,low=off,medium=off,high=off,xhigh=off,max=max",
		adaptive: true, apis: []string{"anthropic-messages"}},
	{pattern: "claude-sonnet-4-6", levels: "off=off,minimal=off,low=off,medium=off,high=off,xhigh=off,max=max",
		adaptive: true, apis: []string{"anthropic-messages"}},
	{pattern: "claude-opus-4-7", levels: "off=off,minimal=off,low=off,medium=off,high=off,xhigh=xhigh,max=max",
		adaptive: true, apis: []string{"anthropic-messages"}},
	{pattern: "claude-opus-4-8", levels: "off=off,minimal=off,low=off,medium=off,high=off,xhigh=xhigh,max=max",
		adaptive: true, apis: []string{"anthropic-messages"}},
	{pattern: "claude-sonnet-5", levels: "off=off,minimal=off,low=off,medium=off,high=off,xhigh=xhigh,max=max",
		adaptive: true, apis: []string{"anthropic-messages"}},
	{pattern: "claude-opus-5-5", levels: "off=off,minimal=off,low=low,medium=medium,high=high,xhigh=xhigh,max=max",
		adaptive: true, apis: []string{"anthropic-messages"}},
	{pattern: "claude-opus-5", levels: "off=off,minimal=off,low=off,medium=off,high=off,xhigh=xhigh,max=max",
		adaptive: true, apis: []string{"anthropic-messages"}},
	{pattern: "claude-sonnet-5-5", levels: "off=off,minimal=off,low=low,medium=medium,high=high,xhigh=xhigh,max=max",
		adaptive: true, apis: []string{"anthropic-messages"}},
	{pattern: "claude-haiku-5-5", levels: "off=off,minimal=off,low=low,medium=medium,high=high,xhigh=xhigh,max=max",
		adaptive: true, apis: []string{"anthropic-messages"}},
	{pattern: "claude-fable-5", levels: "off=off,minimal=off,low=off,medium=off,high=off,xhigh=xhigh,max=max",
		adaptive: true, apis: []string{"anthropic-messages"}},
}

// LookupThinkingPreset returns the vendor thinking preset for slug. Matching
// follows the built-in context-window rules (NormalizeSlug plus prefix
// matching); the most specific pattern wins. A preset with empty Levels is a
// toggle-only API: piThinkingLevelMap must leave the map unset. Rows bound to
// specific request dialects (apis) are skipped when api is empty or speaks
// another dialect — pass the binding's Pi request protocol.
func LookupThinkingPreset(slug, api string) (ThinkingPreset, bool) {
	norm := NormalizeSlug(slug)
	if norm == "" {
		return ThinkingPreset{}, false
	}
	best := -1
	for i, rule := range thinkingPresets {
		if !matchBuiltinPattern(norm, rule.pattern) {
			continue
		}
		if len(rule.apis) > 0 && !slices.Contains(rule.apis, api) {
			continue
		}
		if best < 0 || len(rule.pattern) > len(thinkingPresets[best].pattern) {
			best = i
		}
	}
	if best < 0 {
		return ThinkingPreset{}, false
	}
	rule := thinkingPresets[best]
	return ThinkingPreset{
		Levels:                  parseThinkingLevels(rule.levels),
		APIs:                    rule.apis,
		Format:                  rule.format,
		SupportsReasoningEffort: rule.effort,
		MaxTokensField:          rule.maxTokens,
		RequiresReasoningContentOnAssistantMessages: rule.reasoningContent,
		NoStore:               rule.noStore,
		ForceAdaptiveThinking: rule.adaptive,
	}, true
}

// parseThinkingLevels decodes a curated level=value list. A value of "off"
// records an unavailable (null) level.
func parseThinkingLevels(spec string) map[string]*string {
	if spec == "" {
		return nil
	}
	levels := make(map[string]*string)
	for _, part := range strings.Split(spec, ",") {
		key, value, ok := strings.Cut(strings.TrimSpace(part), "=")
		if !ok {
			continue // the curated table is validated by tests
		}
		if value == "off" {
			levels[key] = nil
		} else {
			levels[key] = &value
		}
	}
	return levels
}
