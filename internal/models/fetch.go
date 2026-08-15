// Package models queries a provider's API for the models available to an endpoint + key.
package models

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"
)

// Provider selects the wire format used to list models.
type Provider string

// OpenAI uses GET {base}/v1/models with an Authorization: Bearer header.
const OpenAI Provider = "openai"

// Anthropic uses GET {base}/v1/models with x-api-key + anthropic-version headers.
const Anthropic Provider = "anthropic"

// modelsURL builds the models-list URL from an endpoint, with or without a trailing "/v1".
func modelsURL(endpoint string) string {
	base := strings.TrimRight(strings.TrimSpace(endpoint), "/")
	if base == "" {
		base = "https://api.openai.com/v1"
	}
	if strings.HasSuffix(base, "/v1") || strings.Contains(base, "/v1/") {
		return base + "/models"
	}
	return base + "/v1/models"
}

// Fetch returns the sorted model IDs offered by endpoint for the given key.
func Fetch(provider Provider, endpoint, key string) ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, modelsURL(endpoint), nil)
	if err != nil {
		return nil, err
	}
	// Always provide Bearer authorization so that custom/third-party OpenAI-compatible
	// relays and gateways work seamlessly regardless of the selected CLI tool.
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("x-api-key", key)
	if provider == Anthropic {
		req.Header.Set("anthropic-version", "2023-06-01")
	}

	// A custom client lets us drop the auth headers on any redirect to a different
	// host, so a redirect issued by the endpoint can't forward the user's API key to
	// a third party. http.DefaultClient would carry Authorization (and always carries
	// the non-standard x-api-key) along to whatever host it lands on.
	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) > 0 && req.URL.Host != via[0].URL.Host {
				req.Header.Del("Authorization")
				req.Header.Del("x-api-key")
				req.Header.Del("anthropic-version")
			}
			return nil
		},
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("API returned %s (check endpoint and key)", resp.Status)
	}

	var out struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("could not parse model list: %w", err)
	}

	ids := make([]string, 0, len(out.Data))
	for _, m := range out.Data {
		if m.ID != "" {
			ids = append(ids, m.ID)
		}
	}
	if len(ids) == 0 {
		return nil, fmt.Errorf("no models returned by %s", endpoint)
	}
	sort.Strings(ids)
	return ids, nil
}
