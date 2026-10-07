package models

import "testing"

func TestDefaultMaxTokens(t *testing.T) {
	for _, tc := range []struct {
		slug string
		want int
	}{
		{"gpt-6-luna", 128_000},
		{"openai/gpt-6-luna", 128_000},
		{"global.openai.gpt-6-luna:batch", 128_000},
		{" GPT-6-LUNA ", 128_000},
		{"gateway-gpt-6-luna", 128_000},
		{"gpt-5.4-mini", 128_000},
		{"gpt-4o", 16_384},
		{"glm-5.3", 131_072},
		{"deepseek-v4-flash", 384_000},
		{"kimi-k2.6", 131_000},
		{"o3-mini", 100_000},
		{"gemini-2.5-pro", 65_536},
		// Unverified models derive window/2 from the builtin window, clamped.
		{"gpt-6-astra", 128_000},
		{"custom", FallbackMaxTokensCeiling},
		{"", 0},
	} {
		if got := DefaultMaxTokens(tc.slug); got != tc.want {
			t.Errorf("DefaultMaxTokens(%q) = %d, want %d", tc.slug, got, tc.want)
		}
	}
}

func TestDefaultMaxTokensFallbackCeiling(t *testing.T) {
	// An unverified slug whose fallback window (500K) would derive 250K stays
	// clamped at FallbackMaxTokensCeiling when the window exceeds 2x the cap.
	if got := DefaultMaxTokens("unknown-huge-model"); got != FallbackMaxTokensCeiling {
		t.Fatalf("DefaultMaxTokens(unverified slug) = %d, want %d", got, FallbackMaxTokensCeiling)
	}
}

func TestIsReasoningBuiltin(t *testing.T) {
	for _, slug := range []string{"gpt-6-luna", "openai/gpt-6-astra", "gpt-6.1-sol", "gpt-5.6-luna", "openai/gpt-5.5", "o3-mini", "deepseek-reasoner"} {
		if !IsReasoningBuiltin(slug) {
			t.Errorf("IsReasoningBuiltin(%q) = false, want true", slug)
		}
	}
	for _, slug := range []string{"gpt-4o", "claude-sonnet-4-6", "some-custom-model"} {
		if IsReasoningBuiltin(slug) {
			t.Errorf("IsReasoningBuiltin(%q) = true, want false", slug)
		}
	}
}

func TestDefaultContextWindow(t *testing.T) {
	cases := []struct {
		slug string
		want int
	}{
		// OpenAI
		{"gpt-6-luna", DefaultContextCeiling},
		{"openai/gpt-6-astra", DefaultContextCeiling},
		{"gpt-5.6-sol", DefaultContextCeiling},
		{"openai/gpt-5.6-luna", DefaultContextCeiling},
		{"global.openai.gpt-5.6-terra", DefaultContextCeiling},
		{"gpt-5.3-codex", 400_000},
		{"gpt-5-mini", 400_000},
		{"gpt-5", DefaultContextCeiling},
		{"gpt-4o", 128_000},
		{"o1-preview", 200_000},

		// Claude
		{"claude-opus-4-7", DefaultContextCeiling},
		{"anthropic/claude-sonnet-4-6", DefaultContextCeiling},
		{"us.anthropic.claude-opus-4-7", DefaultContextCeiling},
		{"claude-haiku-4-5", 200_000},
		{"claude-3-7-sonnet", 200_000},

		// xAI
		{"grok-4.20", DefaultContextCeiling},
		{"x-ai/grok-4.7", 500_000},
		{"xai.grok-4.3:batch", DefaultContextCeiling},

		// Gemini
		{"gemini-2.5-pro", DefaultContextCeiling},
		{"google/gemini-2.5-flash", DefaultContextCeiling},

		// DeepSeek
		{"deepseek-v4-pro", DefaultContextCeiling},
		{"deepseek-ai/DeepSeek-V4-Flash", DefaultContextCeiling},
		{"deepseek-chat", 131_072},
		{"deepseek-reasoner", 64_000},

		// Zhipu variants
		{"glm-5.3", DefaultContextCeiling},
		{"z-ai/glm-5.3", DefaultContextCeiling},
		{"zai/glm-5.3-flash", DefaultContextCeiling},
		{"zai-org/GLM-5.3", DefaultContextCeiling},
		{"zai.glm-5", 202_752},
		{"workers-ai/@cf/zai-org/glm-4.7-flash", 204_800},
		{"glm-4.7", 204_800},

		// Kimi
		{"moonshotai/kimi-k3", DefaultContextCeiling},
		{"kimi-k2.7-code", 262_144},
		{"moonshotai/kimi-k2.6", 262_144},

		// Qwen
		{"qwen3.8-max", DefaultContextCeiling},
		{"qwen/qwen3.8-27b", DefaultContextCeiling},
		{"qwen3-coder-flash", DefaultContextCeiling},
		{"qwen2.5-coder-32b", 128_000},

		// Values above DefaultContextCeiling clamp to it
		{"some-custom-model", 500_000},
		{"", 0},
	}

	for _, tc := range cases {
		got := DefaultContextWindow(tc.slug)
		if got != tc.want {
			t.Errorf("DefaultContextWindow(%q) = %d, want %d", tc.slug, got, tc.want)
		}
	}
}

func TestDefaultContextCeiling(t *testing.T) {
	// 1M-class vendor specs clamp to DefaultContextCeiling; smaller specs pass through.
	cases := []struct {
		slug string
		want int
	}{
		{"glm-5.3", DefaultContextCeiling},
		{"claude-opus-4-7", DefaultContextCeiling},
		{"grok-4.20", DefaultContextCeiling},
		{"gemini-2.5-pro", DefaultContextCeiling},
		{"gpt-6-astra", DefaultContextCeiling},
		{"grok-4.7", 500_000},
		{"glm-4.7", 204_800},
		{"gpt-4o", 128_000},
	}
	for _, tc := range cases {
		if got := DefaultContextWindow(tc.slug); got != tc.want {
			t.Errorf("DefaultContextWindow(%q) = %d, want %d", tc.slug, got, tc.want)
		}
	}
}
