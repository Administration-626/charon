package tools

import (
	"fmt"
	"net/url"
	"strings"
)

// ValidateEndpoint reports whether ep is usable as an API base URL. A blank
// value is fine (the tool's own default applies); otherwise it must parse as
// an absolute http(s) URL with a host, so a typo is caught before it's written
// into a tool's live config.
func ValidateEndpoint(ep string) error {
	ep = strings.TrimSpace(ep)
	if ep == "" {
		return nil
	}
	u, err := url.Parse(ep)
	if err != nil || u.Host == "" {
		return fmt.Errorf("invalid URL %q — expected e.g. https://api.example.com/v1", ep)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("URL %q must start with http:// or https://", ep)
	}
	return nil
}

// EndpointNeedsV1Hint reports whether a custom endpoint may need an explicit
// "/v1" path for OpenAI-compatible tools. Claude Code is excluded because it
// appends "/v1/messages" itself and normalizes that suffix separately.
func EndpointNeedsV1Hint(t *Tool, ep string) bool {
	if t == nil || t.Name == "claude" || strings.TrimSpace(ep) == "" {
		return false
	}
	if err := ValidateEndpoint(ep); err != nil {
		return false
	}
	u, err := url.Parse(strings.TrimSpace(ep))
	if err != nil {
		return false
	}
	path := strings.TrimRight(u.Path, "/")
	return !strings.HasSuffix(path, "/v1") && !strings.Contains(path, "/v1/")
}

// EndpointHasClaudeV1 reports whether Claude Code will strip a trailing "/v1"
// before appending its own "/v1/messages" path.
func EndpointHasClaudeV1(t *Tool, ep string) bool {
	if t == nil || t.Name != "claude" || strings.TrimSpace(ep) == "" {
		return false
	}
	if err := ValidateEndpoint(ep); err != nil {
		return false
	}
	u, err := url.Parse(strings.TrimSpace(ep))
	if err != nil {
		return false
	}
	return strings.HasSuffix(strings.TrimRight(u.Path, "/"), "/v1")
}

// ValidateKey reports whether key is a non-empty API key/token once trimmed.
func ValidateKey(key string) error {
	if strings.TrimSpace(key) == "" {
		return fmt.Errorf("API key is required")
	}
	return nil
}
