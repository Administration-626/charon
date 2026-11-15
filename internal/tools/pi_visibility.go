package tools

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"charon/internal/edit"
)

func piEndpointIdentity(endpoint string) string {
	return strings.TrimRight(strings.TrimSpace(endpoint), "/")
}

// PreparePiEndpointVisibility validates an endpoint-only edit before the caller
// changes the catalog. The returned write never touches settings or other sites.
func PreparePiEndpointVisibility(group EndpointGroup, hidden bool) (func() error, error) {
	dir := filepath.Join(home(), ".pi", "agent")
	path := filepath.Join(dir, "models.json")
	config, err := loadJSONMap(path)
	if err != nil {
		return nil, fmt.Errorf("read Pi models.json: %w", err)
	}
	if config == nil {
		return nil, fmt.Errorf("pi models.json must be an object")
	}
	settings, err := loadJSONMap(filepath.Join(dir, "settings.json"))
	if err != nil {
		return nil, fmt.Errorf("read Pi settings.json: %w", err)
	}
	if settings == nil {
		return nil, fmt.Errorf("pi settings.json must be an object")
	}
	if raw, exists := config["providers"]; exists {
		if _, ok := raw.(map[string]any); !ok {
			return nil, fmt.Errorf("pi providers must be an object")
		}
	}
	current, ok := settings["defaultProvider"].(string)
	if _, exists := settings["defaultProvider"]; exists && !ok {
		return nil, fmt.Errorf("pi defaultProvider must be a string")
	}
	providers := subMap(config, "providers")
	endpoint := piEndpointIdentity(group.Endpoint)
	var matches []string
	var others []EndpointGroup
	taken := make(map[string]bool, len(providers))
	for key, raw := range providers {
		taken[key] = true
		if key != managedProvider && !strings.HasPrefix(key, managedProvider+"-") {
			continue
		}
		provider, ok := raw.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("managed Pi provider must be an object")
		}
		baseURL, ok := provider["baseUrl"].(string)
		if !ok {
			return nil, fmt.Errorf("managed Pi baseUrl must be a string")
		}
		if piEndpointIdentity(baseURL) == endpoint {
			if hidden && (current == key || (current == "" && key == managedProvider)) {
				return nil, fmt.Errorf("pi's on-disk default uses this endpoint: select another endpoint in the TUI; if it is already active, press r to reapply it before hiding")
			}
			matches = append(matches, key)
		} else {
			others = append(others, EndpointGroup{Endpoint: baseURL})
		}
	}
	// A legacy generated extension can still override native providers. Never
	// silently remove or restore a site while that extension is using it.
	legacy, err := piLegacyConfig(filepath.Join(dir, "extensions", "charon.ts"))
	if err != nil {
		return nil, err
	}
	if legacy != nil && piEndpointIdentity(legacy.BaseURL) == endpoint {
		return nil, fmt.Errorf("pi's legacy extension uses this endpoint: select another endpoint and press r in the TUI to migrate it first")
	}
	sort.Strings(matches)
	del := make([]string, 0, len(matches))
	for _, key := range matches {
		del = append(del, "providers."+piJSONKey(key))
	}
	var set []edit.KV
	if !hidden {
		label := piEndpointLabels(group.Endpoint, others)[group.Endpoint]
		key := ""
		if len(matches) > 0 {
			key = matches[0]
		} else {
			// Do not hijack an unrelated (possibly currently dangling) default key.
			if current != "" {
				taken[current] = true
			}
			key = piProviderKey(label, taken)
		}
		entry, err := piGroupEntry(group, label, providers)
		if err != nil {
			return nil, err
		}
		set = []edit.KV{{Path: "providers." + piJSONKey(key), Value: entry}}
	}
	return func() error {
		if len(del) == 0 && len(set) == 0 {
			return nil
		}
		return edit.ApplyJSON(path, set, del)
	}, nil
}

// Paths name literal JSON keys, including hand-edited managed names with dots.
func piJSONKey(key string) string {
	key = strings.ReplaceAll(key, `\`, `\\`)
	return strings.ReplaceAll(key, `.`, `\.`)
}
