package tools

import "testing"

func TestValidateEndpoint(t *testing.T) {
	valid := []string{
		"",   // blank accepts the tool's default
		"  ", // whitespace-only is trimmed to blank
		"https://api.example.com/v1",
		"http://localhost:8080",
		"https://api.example.com:8443/v1/",
	}
	for _, ep := range valid {
		if err := ValidateEndpoint(ep); err != nil {
			t.Errorf("ValidateEndpoint(%q) = %v, want nil", ep, err)
		}
	}

	invalid := []string{
		"not a url",
		"ftp://api.example.com", // wrong scheme
		"api.example.com",       // missing scheme
		"https://",              // missing host
		"://broken",             // unparseable scheme
		"javascript:alert(1)",   // no host
	}
	for _, ep := range invalid {
		if err := ValidateEndpoint(ep); err == nil {
			t.Errorf("ValidateEndpoint(%q) = nil, want error", ep)
		}
	}
}

func TestEndpointNeedsV1Hint(t *testing.T) {
	openai := &Tool{Name: "codex"}
	claude := &Tool{Name: "claude"}
	tests := []struct {
		name string
		tool *Tool
		ep   string
		want bool
	}{
		{"missing v1", openai, "https://gateway.example", true},
		{"has v1", openai, "https://gateway.example/v1", false},
		{"nested v1", openai, "https://gateway.example/openai/v1/", false},
		{"claude", claude, "https://gateway.example", false},
		{"invalid", openai, "not a url", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := EndpointNeedsV1Hint(tt.tool, tt.ep); got != tt.want {
				t.Errorf("EndpointNeedsV1Hint(%q) = %v, want %v", tt.ep, got, tt.want)
			}
		})
	}
}

func TestEndpointHasClaudeV1(t *testing.T) {
	claude := &Tool{Name: "claude"}
	other := &Tool{Name: "codex"}
	tests := []struct {
		name string
		tool *Tool
		ep   string
		want bool
	}{
		{"trailing v1", claude, "https://gateway.example/v1", true},
		{"trailing v1 slash", claude, "https://gateway.example/v1/", true},
		{"other path", claude, "https://gateway.example/v1beta", false},
		{"non Claude", other, "https://gateway.example/v1", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := EndpointHasClaudeV1(tt.tool, tt.ep); got != tt.want {
				t.Errorf("EndpointHasClaudeV1(%q) = %v, want %v", tt.ep, got, tt.want)
			}
		})
	}
}

func TestValidateKey(t *testing.T) {
	if err := ValidateKey("sk-test"); err != nil {
		t.Errorf("ValidateKey(sk-test) = %v, want nil", err)
	}
	for _, key := range []string{"", "   "} {
		if err := ValidateKey(key); err == nil {
			t.Errorf("ValidateKey(%q) = nil, want error", key)
		}
	}
}
