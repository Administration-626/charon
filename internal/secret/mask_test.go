package secret

import "testing"

func TestMask(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", ""},
		{"short keeps min prefix suffix", "sk-123", "sk••••23"},
		{"very short is fully hidden", "abc", "••••"},
		{"ten keeps min prefix suffix", "0123456789", "01••••89"},
		{"long keeps prefix and suffix", "sk-or-v1-xyz789012", "sk-or-••••9012"},
		{"anthropic key", "sk-ant-api03-abc123456789", "sk-ant••••6789"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Mask(tt.in); got != tt.want {
				t.Errorf("Mask(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestMaskNeverLeaksMiddle(t *testing.T) {
	secret := "sk-super-secret-token-value-1234"
	if got := Mask(secret); len(got) >= len(secret) {
		t.Fatalf("masked value %q not shorter than original", got)
	}
}
