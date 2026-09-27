// Package models provides model discovery, metadata resolution, and built-in context window mappings.
package models

import (
	"strings"
)

// builtinRule maps a slug prefix/pattern to its standard context window size in tokens.
type builtinRule struct {
	pattern string
	window  int
}

// builtinModelWindows holds default context windows for modern mainstream models,
// derived from WorkBuddy model configurations and official provider specifications.
// When an API endpoint does not report a context window, these presets populate
// the window size for common models, falling back to FallbackContextWindow (500K) if unmatched.
var builtinModelWindows = []builtinRule{
	// OpenAI
	{pattern: "gpt-6", window: 1_050_000},
	{pattern: "gpt-5.6", window: 1_050_000},
	{pattern: "gpt-5.5", window: 1_050_000},
	{pattern: "gpt-5.4-mini", window: 400_000},
	{pattern: "gpt-5.4-nano", window: 400_000},
	{pattern: "gpt-5.4", window: 1_050_000},
	{pattern: "gpt-5.3-codex", window: 400_000},
	{pattern: "gpt-5.2", window: 1_050_000},
	{pattern: "gpt-5.1", window: 1_050_000},
	{pattern: "gpt-5-mini", window: 400_000},
	{pattern: "gpt-5-nano", window: 400_000},
	{pattern: "gpt-5", window: 1_050_000},
	{pattern: "gpt-latest", window: 1_050_000},
	{pattern: "gpt-mini-latest", window: 400_000},
	{pattern: "gpt-4o", window: 128_000},
	{pattern: "gpt-4.5", window: 128_000},
	{pattern: "o1", window: 200_000},
	{pattern: "o3", window: 200_000},

	// Anthropic Claude
	{pattern: "claude-opus-4-7", window: 1_000_000},
	{pattern: "claude-opus-4-6", window: 1_000_000},
	{pattern: "claude-opus-4-8", window: 1_000_000},
	{pattern: "claude-opus-5", window: 1_000_000},
	{pattern: "claude-sonnet-4-6", window: 1_000_000},
	{pattern: "claude-sonnet-5", window: 1_000_000},
	{pattern: "claude-fable", window: 1_000_000},
	{pattern: "claude-opus-latest", window: 1_000_000},
	{pattern: "claude-sonnet-latest", window: 1_000_000},
	{pattern: "claude-fable-latest", window: 1_000_000},
	{pattern: "claude-haiku-4-5", window: 200_000},
	{pattern: "claude-sonnet-4-5", window: 200_000},
	{pattern: "claude-opus-4-1", window: 200_000},
	{pattern: "claude-opus-4-5", window: 200_000},
	{pattern: "claude-haiku-latest", window: 200_000},
	{pattern: "claude-3-7", window: 200_000},
	{pattern: "claude-3-5", window: 200_000},
	{pattern: "claude-3", window: 200_000},

	// xAI Grok
	{pattern: "grok-4.20", window: 2_000_000},
	{pattern: "grok-4.7", window: 500_000},
	{pattern: "grok-4.6", window: 500_000},
	{pattern: "grok-4.5", window: 500_000},
	{pattern: "grok-4.3", window: 1_000_000},
	{pattern: "grok-4.1", window: 1_000_000},
	{pattern: "grok-latest", window: 500_000},

	// Google Gemini
	{pattern: "gemini-2.5", window: 1_048_576},
	{pattern: "gemini-flash-latest", window: 1_048_576},
	{pattern: "gemini-pro-latest", window: 1_048_576},
	{pattern: "gemini-2.0", window: 1_000_000},
	{pattern: "gemini-1.5", window: 1_000_000},

	// DeepSeek
	{pattern: "deepseek-v4", window: 1_048_576},
	{pattern: "deepseek-v3", window: 131_072},
	{pattern: "deepseek-r1", window: 64_000},
	{pattern: "deepseek-chat", window: 131_072},
	{pattern: "deepseek-reasoner", window: 64_000},

	// Zhipu GLM (including z-ai, zai, zai-org, zhipu prefixes)
	{pattern: "glm-5.3", window: 1_048_576},
	{pattern: "glm-5.2", window: 1_048_576},
	{pattern: "glm-5.1", window: 202_752},
	{pattern: "glm-5", window: 202_752},
	{pattern: "glm-4.7", window: 204_800},
	{pattern: "glm-4.6", window: 204_800},
	{pattern: "glm-4.5", window: 131_072},
	{pattern: "glm-flash-latest", window: 1_048_576},
	{pattern: "glm-latest", window: 262_144},

	// Moonshot Kimi
	{pattern: "kimi-k3", window: 1_048_576},
	{pattern: "kimi-k2.7", window: 262_144},
	{pattern: "kimi-k2.6", window: 262_144},
	{pattern: "kimi-k2.5", window: 262_144},
	{pattern: "kimi-k2", window: 131_072},
	{pattern: "kimi-latest", window: 1_048_576},
	{pattern: "kimi", window: 1_048_576},

	// Qwen
	{pattern: "qwen3.8", window: 1_000_000},
	{pattern: "qwen3.7", window: 1_000_000},
	{pattern: "qwen3.6", window: 1_000_000},
	{pattern: "qwen3.5", window: 1_000_000},
	{pattern: "qwen3-coder-flash", window: 1_000_000},
	{pattern: "qwen3-coder-plus", window: 1_000_000},
	{pattern: "qwen3-max", window: 262_144},
	{pattern: "qwen3-coder", window: 262_144},
	{pattern: "qwen2.5-coder", window: 128_000},
	{pattern: "qwen2.5", window: 128_000},
}

// NormalizeSlug strips provider namespace prefixes, region prefixes, and tags.
func NormalizeSlug(slug string) string {
	s := strings.ToLower(strings.TrimSpace(slug))
	if idx := strings.LastIndex(s, "/"); idx >= 0 {
		s = s[idx+1:]
	}
	// Strip provider prefixes separated by dots (e.g. us.anthropic. or zai.)
	prefixes := []string{
		"global.openai.", "openai.",
		"us.anthropic.", "jp.anthropic.", "au.anthropic.", "anthropic.",
		"xai.", "x-ai.",
		"google.",
		"zai.", "z-ai.", "zhipu.",
		"moonshot.", "moonshotai.",
		"qwen.",
	}
	for _, p := range prefixes {
		if strings.HasPrefix(s, p) {
			s = s[len(p):]
			break
		}
	}
	// Strip tag suffix like :batch, :free, :0
	if idx := strings.Index(s, ":"); idx >= 0 {
		s = s[:idx]
	}
	return s
}

// FallbackContextWindow is the default context window in tokens (500K)
// applied when a model slug is not recognized in the builtin table.
const FallbackContextWindow = 500_000

// DefaultContextWindow returns the preset context window size for recognized
// mainstream models, or FallbackContextWindow (500K) for unrecognized non-empty slugs.
func DefaultContextWindow(slug string) int {
	norm := NormalizeSlug(slug)
	if norm == "" {
		return 0
	}
	for _, rule := range builtinModelWindows {
		if strings.HasPrefix(norm, rule.pattern) || strings.Contains(norm, "-"+rule.pattern) {
			return rule.window
		}
	}
	return FallbackContextWindow
}

// IsKnownBuiltin reports whether slug matches a specific pattern in the builtin table
// (excluding the 500K fallback).
func IsKnownBuiltin(slug string) bool {
	norm := NormalizeSlug(slug)
	if norm == "" {
		return false
	}
	for _, rule := range builtinModelWindows {
		if strings.HasPrefix(norm, rule.pattern) || strings.Contains(norm, "-"+rule.pattern) {
			return true
		}
	}
	return false
}
