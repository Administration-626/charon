package models

import "testing"

func TestDefaultContextWindow(t *testing.T) {
	cases := []struct {
		slug string
		want int
	}{
		// OpenAI
		{"gpt-5.6-sol", 1_050_000},
		{"openai/gpt-5.6-luna", 1_050_000},
		{"global.openai.gpt-5.6-terra", 1_050_000},
		{"gpt-5.3-codex", 400_000},
		{"gpt-5-mini", 400_000},
		{"gpt-5", 1_050_000},
		{"gpt-4o", 128_000},
		{"o1-preview", 200_000},

		// Claude
		{"claude-opus-4-7", 1_000_000},
		{"anthropic/claude-sonnet-4-6", 1_000_000},
		{"us.anthropic.claude-opus-4-7", 1_000_000},
		{"claude-haiku-4-5", 200_000},
		{"claude-3-7-sonnet", 200_000},

		// xAI
		{"grok-4.20", 2_000_000},
		{"x-ai/grok-4.7", 500_000},
		{"xai.grok-4.3:batch", 1_000_000},

		// Gemini
		{"gemini-2.5-pro", 1_048_576},
		{"google/gemini-2.5-flash", 1_048_576},

		// DeepSeek
		{"deepseek-v4-pro", 1_048_576},
		{"deepseek-ai/DeepSeek-V4-Flash", 1_048_576},
		{"deepseek-chat", 131_072},
		{"deepseek-reasoner", 64_000},

		// Zhipu variants
		{"glm-5.3", 1_048_576},
		{"z-ai/glm-5.3", 1_048_576},
		{"zai/glm-5.3-flash", 1_048_576},
		{"zai-org/GLM-5.3", 1_048_576},
		{"zai.glm-5", 202_752},
		{"workers-ai/@cf/zai-org/glm-4.7-flash", 204_800},
		{"glm-4.7", 204_800},

		// Kimi
		{"moonshotai/kimi-k3", 1_048_576},
		{"kimi-k2.7-code", 262_144},
		{"moonshotai/kimi-k2.6", 262_144},

		// Qwen
		{"qwen3.8-max", 1_000_000},
		{"qwen/qwen3.8-27b", 1_000_000},
		{"qwen3-coder-flash", 1_000_000},
		{"qwen2.5-coder-32b", 128_000},

		// Unknown model falls back to 500K
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
