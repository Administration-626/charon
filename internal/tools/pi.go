package tools

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"charon/internal/edit"
	"charon/internal/models"
)

// Older Charon versions registered Pi providers through a generated extension.
// Keep its exact format to recognize files we can safely migrate to models.json.
const (
	piExtensionOpen  = `pi.registerProvider("charon", `
	piExtensionClose = `);
  // charon:config:end
}
`
)

var piConfigRE = regexp.MustCompile(`(?s)pi\.registerProvider\("charon",\s*(.*?)\);\s*\n\s*// charon:config:end`)

// ResolvePiAPI validates Pi's request protocol, defaulting to OpenAI Chat Completions.
// Pi 1.0.4's pi-ai KnownApi has no separate Responses Compact protocol.
func ResolvePiAPI(api string) (string, error) {
	if api == "" {
		return "openai-completions", nil
	}
	switch api {
	case "openai-completions", "openai-responses", "anthropic-messages", "google-generative-ai":
		return api, nil
	default:
		return "", fmt.Errorf("unsupported Pi endpoint type %q", api)
	}
}

// piModel is one entry of a pi provider's "models" array.
type piModel struct {
	ID               string             `json:"id"`
	Name             string             `json:"name"`
	Reasoning        bool               `json:"reasoning"`
	Input            []string           `json:"input"`
	Cost             piCost             `json:"cost"`
	ContextWindow    int                `json:"contextWindow"`
	MaxTokens        int                `json:"maxTokens"`
	ThinkingLevelMap map[string]*string `json:"thinkingLevelMap,omitempty"`
	Compat           *piCompat          `json:"compat,omitempty"`
}

type piCost struct {
	Input      float64 `json:"input"`
	Output     float64 `json:"output"`
	CacheRead  float64 `json:"cacheRead"`
	CacheWrite float64 `json:"cacheWrite"`
}

// piProviderConfig describes Charon's native and legacy provider configuration.
type piProviderConfig struct {
	Name    string    `json:"name"`
	BaseURL string    `json:"baseUrl"`
	APIKey  string    `json:"apiKey"`
	API     string    `json:"api"`
	Models  []piModel `json:"models"`
}

// piEscapeValue escapes "$" and "!", which pi's apiKey/headers fields treat as
// env-var interpolation ("$VAR", "${VAR}") and command execution ("!cmd") markers,
// so a literal key/header value containing either is never misinterpreted.
func piEscapeValue(s string) string {
	s = strings.ReplaceAll(s, "$", "$$")
	s = strings.ReplaceAll(s, "!", "$!")
	return s
}

// piCompat is the subset of pi's model compat object emitted when a vendor
// thinking preset documents request-format differences. The thinkingFormat /
// supports* keys belong to OpenAI-completions; ForceAdaptiveThinking belongs
// to Anthropic-messages.
type piCompat struct {
	ThinkingFormat string `json:"thinkingFormat,omitempty"`
	// Pointer so an explicit false is written while an unset flag is omitted.
	SupportsReasoningEffort                     *bool  `json:"supportsReasoningEffort,omitempty"`
	SupportsStore                               *bool  `json:"supportsStore,omitempty"`
	SupportsDeveloperRole                       *bool  `json:"supportsDeveloperRole,omitempty"`
	MaxTokensField                              string `json:"maxTokensField,omitempty"`
	RequiresReasoningContentOnAssistantMessages bool   `json:"requiresReasoningContentOnAssistantMessages,omitempty"`
	ForceAdaptiveThinking                       *bool  `json:"forceAdaptiveThinking,omitempty"`
}

// piCompatFor translates a vendor thinking preset into pi compat flags.
// Nil (no preset) keeps pi's derived defaults for a generic endpoint.
func piCompatFor(preset models.ThinkingPreset, found bool) *piCompat {
	if !found {
		return nil
	}
	c := &piCompat{
		ThinkingFormat:          preset.Format,
		SupportsReasoningEffort: preset.SupportsReasoningEffort,
		MaxTokensField:          preset.MaxTokensField,
		RequiresReasoningContentOnAssistantMessages: preset.RequiresReasoningContentOnAssistantMessages,
	}
	if preset.ForceAdaptiveThinking {
		c.ForceAdaptiveThinking = boolPtr(true)
	}
	if preset.NoStore {
		c.SupportsStore, c.SupportsDeveloperRole = boolPtr(false), boolPtr(false)
	}
	if c.ThinkingFormat == "" && c.SupportsReasoningEffort == nil && c.MaxTokensField == "" &&
		!c.RequiresReasoningContentOnAssistantMessages && c.SupportsStore == nil && c.ForceAdaptiveThinking == nil {
		return nil
	}
	return c
}

func boolPtr(b bool) *bool { return &b }

// piThinkingLevelMap gives every Charon model Pi's thinking controls.
// Manual mappings (including null/disabled levels) win; otherwise a curated
// vendor preset supplies the model's verified levels (empty for toggle-only
// APIs like MiMo, where the map stays unset), and unknown models get Pi's
// generic extended levels. api is the binding's Pi request protocol; dialect-
// bound presets (Claude) only apply when it matches.
func piThinkingLevelMap(spec ModelSpec, api string) map[string]*string {
	if spec.ThinkingLevelMap != nil {
		return spec.ThinkingLevelMap
	}
	if preset, ok := models.LookupThinkingPreset(spec.Slug, api); ok {
		if len(preset.Levels) > 0 {
			return preset.Levels
		}
		return nil // toggle-only vendor API: pi manages the switch via compat
	}
	off, xhigh, maxLevel := "none", "xhigh", "max"
	return map[string]*string{"off": &off, "xhigh": &xhigh, "max": &maxLevel}
}

// piBuildModels turns a list of model ids into pi model entries. api is the
// provider's request protocol (ResolvePiAPI form); it selects dialect-bound
// thinking presets and defaults to openai-completions. label, when set, is
// appended to each model's display name so /model tells apart the same id
// served by different endpoints; the id itself never changes.
func piBuildModels(specs []ModelSpec, api, label string) []piModel {
	if api == "" {
		api = "openai-completions"
	}
	entries := make([]piModel, 0, len(specs))
	for _, spec := range specs {
		if spec.Slug == "" {
			continue
		}
		window := spec.ContextWindow
		if window == 0 {
			window = models.DefaultContextWindow(spec.Slug)
		}
		preset, hasPreset := models.LookupThinkingPreset(spec.Slug, api)
		name := spec.Slug
		if label != "" {
			name += " (" + label + ")"
		}
		entries = append(entries, piModel{
			ID:   spec.Slug,
			Name: name,
			// Charon does not have authoritative per-model thinking metadata. Pi
			// offers the model's vendor thinking levels (or the generic ones for
			// unknown models) and lets the user turn them off.
			Reasoning:        true,
			Input:            []string{"text", "image"},
			ContextWindow:    window,
			MaxTokens:        models.DefaultMaxTokens(spec.Slug),
			ThinkingLevelMap: piThinkingLevelMap(spec, api),
			Compat:           piCompatFor(preset, hasPreset),
		})
	}
	return entries
}

// endpointHost is an endpoint's hostname for display: url's Hostname() when
// the endpoint parses and yields one, the raw string otherwise, so a
// malformed endpoint still gets a label instead of an empty one.
func endpointHost(endpoint string) string {
	u, err := url.Parse(endpoint)
	if err != nil {
		return endpoint
	}
	if host := u.Hostname(); host != "" {
		return host
	}
	return endpoint
}

// piEndpointLabels labels every endpoint of one apply as briefly as stays
// unique, because pi prints the label next to every model the endpoint
// offers. An endpoint normally gets its host's first label
// (newapi.maimaidx.icu → newapi); two endpoints that would share that label
// keep their whole host, and two that share a host (same gateway, different
// paths or ports) keep their whole URL, since only the path tells them apart.
// primary takes part in the counting: an endpoint also listed as a group
// still counts as a collision.
func piEndpointLabels(primary string, groups []EndpointGroup) map[string]string {
	endpoints := make([]string, 0, len(groups)+1)
	seen := make(map[string]bool, len(groups)+1)
	add := func(endpoint string) {
		if seen[endpoint] {
			return
		}
		seen[endpoint] = true
		endpoints = append(endpoints, endpoint)
	}
	add(primary)
	for _, g := range groups {
		add(g.Endpoint)
	}
	hostCount := make(map[string]int, len(endpoints))
	shortCount := make(map[string]int, len(endpoints))
	for _, ep := range endpoints {
		hostCount[endpointHost(ep)]++
		shortCount[piShortHost(endpointHost(ep))]++
	}
	labels := make(map[string]string, len(endpoints))
	for _, ep := range endpoints {
		host := endpointHost(ep)
		short := piShortHost(host)
		switch {
		case hostCount[host] > 1:
			labels[ep] = strings.TrimSuffix(ep, "/")
		case shortCount[short] > 1:
			labels[ep] = host
		default:
			labels[ep] = short
		}
	}
	return labels
}

// piShortHost drops the domain from a host, keeping the part that names the
// service: the first label (newapi.maimaidx.icu → newapi), plus the second
// when the first is shorter than four characters (ai.hyper.nyc.mn →
// ai.hyper) so a bare "ai" or "api" still says something. Addresses have no
// service label to keep, so they stay whole.
func piShortHost(host string) string {
	host = strings.ToLower(host)
	parts := strings.Split(host, ".")
	if len(parts) < 2 || !strings.ContainsAny(parts[0], "abcdefghijklmnopqrstuvwxyz") {
		return host
	}
	if len(parts[0]) < 4 {
		return parts[0] + "." + parts[1]
	}
	return parts[0]
}

// foldLabel turns a display label into a provider key fragment: lower case,
// every character outside [a-z0-9] written as "-", outer dashes trimmed. The
// result holds no dot, which a models.json key path would split on.
func foldLabel(label string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(label) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			continue
		}
		b.WriteByte('-')
	}
	return strings.Trim(b.String(), "-")
}

// piProviderKey names one aggregated provider entry: charon's prefix plus the
// folded label, with -2, -3, ... appended when that key is already taken. An
// empty fold keeps the bare prefix, which the taken map then suffixes.
func piProviderKey(label string, taken map[string]bool) string {
	base := managedProvider
	if folded := foldLabel(label); folded != "" {
		base += "-" + folded
	}
	key := base
	for n := 2; taken[key]; n++ {
		key = fmt.Sprintf("%s-%d", base, n)
	}
	taken[key] = true
	return key
}

// piProvider is one aggregated provider entry waiting to be written.
type piProvider struct {
	Key   string
	Entry map[string]any
}

// piAggregation is what one ApplyAuth call writes besides the file defaults:
// the binding's own provider, keyed and labelled after its endpoint, and one
// entry per other endpoint.
type piAggregation struct {
	PrimaryKey   string
	PrimaryLabel string
	Groups       []piProvider
}

// piGroups names the binding's own provider after its endpoint and builds the
// entries for the other endpoints. Pi shows a model as provider/id in /model
// and --list-models, so a provider key of plain "charon" hides which endpoint
// the selected one is; keying it after the endpoint keeps every row readable.
// An endpoint that yields no label (empty or unparsable) keeps the bare key.
// A group on the binding's own endpoint is skipped: the primary entry already
// holds it. Each entry carries that endpoint's own key and request protocol, so
// a request reaches its gateway the way that endpoint's binding would send it;
// an absent protocol means openai-completions, pi's default.
func piGroups(a AuthSpec) (piAggregation, error) {
	labels := piEndpointLabels(a.Endpoint, a.Groups)
	taken := map[string]bool{}
	agg := piAggregation{PrimaryLabel: labels[a.Endpoint]}
	agg.PrimaryKey = piProviderKey(agg.PrimaryLabel, taken)
	for _, g := range a.Groups {
		if g.Endpoint == "" || g.Endpoint == a.Endpoint {
			continue
		}
		api, err := ResolvePiAPI(g.PiAPI)
		if err != nil {
			return piAggregation{}, fmt.Errorf("endpoint %s: %w", g.Endpoint, err)
		}
		label := labels[g.Endpoint]
		key := piProviderKey(label, taken)
		agg.Groups = append(agg.Groups, piProvider{Key: key, Entry: map[string]any{
			"name":    label,
			"baseUrl": g.Endpoint,
			"apiKey":  piEscapeValue(g.Key),
			"api":     api,
			"models":  piBuildModels(g.Models, api, label),
		}})
	}
	return agg, nil
}

// piWriteProviders rewrites charon's provider entries in models.json.
// Entries an earlier binding left behind and this spec does not keep are
// removed first (sorted, so the delete is deterministic), then the binding's
// own entry is set, then each group is inserted right after the previous one.
// That fixed order is why applying one spec twice produces the same bytes.
// Every key outside charon's namespace is left untouched.
func piWriteProviders(path string, existing map[string]any, primaryKey string, kvs []edit.KV, groups []piProvider) error {
	keep := map[string]bool{primaryKey: true}
	for _, g := range groups {
		keep[g.Key] = true
	}
	var stale []string
	for key := range existing {
		if key != managedProvider && !strings.HasPrefix(key, managedProvider+"-") {
			continue
		}
		if !keep[key] {
			stale = append(stale, "providers."+key)
		}
	}
	sort.Strings(stale)
	if len(stale) > 0 {
		if err := edit.DelJSON(path, stale...); err != nil {
			return err
		}
	}
	if err := edit.SetJSON(path, kvs...); err != nil {
		return err
	}
	anchor := "providers." + primaryKey
	for _, g := range groups {
		if err := edit.SetJSONAfter(path, anchor, "providers."+g.Key, g.Entry); err != nil {
			return err
		}
		anchor = "providers." + g.Key
	}
	return nil
}

// piExtensionContent reproduces the legacy extension for ownership checks.
func piExtensionContent(cfg piProviderConfig) ([]byte, error) {
	body, err := json.MarshalIndent(cfg, "  ", "  ")
	if err != nil {
		return nil, err
	}
	var b strings.Builder
	b.WriteString(`import type { ExtensionAPI } from "@earendil-works/pi-coding-agent";

export default function (pi: ExtensionAPI) {
  // charon:config
  `)
	b.WriteString(piExtensionOpen)
	b.Write(body)
	b.WriteString(piExtensionClose)
	return []byte(b.String()), nil
}

// piParseExtension extracts the provider config JSON from a previously written
// charon.ts, "" fields / nil models if the file is absent or unrecognized.
func piParseExtension(data []byte) (piProviderConfig, bool) {
	m := piConfigRE.FindSubmatch(data)
	if m == nil {
		return piProviderConfig{}, false
	}
	var cfg piProviderConfig
	if json.Unmarshal(m[1], &cfg) != nil {
		return piProviderConfig{}, false
	}
	return cfg, true
}

// piLegacyConfig refuses to migrate extensions that differ from our generated
// format. In particular, a recognizable JSON block alone does not prove ownership.
func piLegacyConfig(path string) (*piProviderConfig, error) {
	stat, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read legacy pi extension: %w", err)
	}
	if !stat.Mode().IsRegular() {
		return nil, fmt.Errorf("refusing to migrate %s: not a regular file; disable it manually", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read legacy pi extension: %w", err)
	}
	cfg, ok := piParseExtension(data)
	expected, err := piExtensionContent(cfg)
	if !ok || err != nil || !bytes.Equal(data, expected) {
		return nil, fmt.Errorf("refusing to migrate modified or unrecognized %s; disable it manually", path)
	}
	return &cfg, nil
}

// newPi registers a provider in ~/.pi/agent/models.json. Model/effort defaults
// live in settings.json; Pi's own provider logins in auth.json remain untouched.
func newPi() *Tool {
	dir := filepath.Join(home(), ".pi", "agent")
	settingsPath := filepath.Join(dir, "settings.json")
	authPath := filepath.Join(dir, "auth.json")
	extensionPath := filepath.Join(dir, "extensions", "charon.ts")
	modelsPath := filepath.Join(dir, "models.json")

	return &Tool{
		Name:            "pi",
		Title:           "Pi",
		Provider:        "openai",
		ModelMenu:       "/model",
		DefaultEndpoint: "https://api.openai.com/v1",
		Files:           []string{modelsPath, settingsPath, extensionPath},
		// Pi's models.json holds several providers side by side, each with its
		// own baseUrl/key/models, and /model lists them all.
		MultiEndpoint: true,
		Owned: []OwnedFile{
			{
				Path: modelsPath,
				JSON: []string{"providers." + managedProvider},
				// Aggregated entries (providers.charon-<host>) are charon's own
				// generated providers, not the user's: off deletes them.
				JSONPrefixes: []string{"providers." + managedProvider},
			},
			{Path: settingsPath, JSON: []string{"defaultProvider", "defaultModel"}},
		},
		// A legacy charon.ts extension migrated away stays gone: it was
		// charon's own artifact, not the user's.
		ApplyAuth: func(a AuthSpec) error {
			// Read and validate every input before the first write.
			api, err := ResolvePiAPI(a.PiAPI)
			if err != nil {
				return err
			}
			config, err := loadJSONMap(modelsPath)
			if err != nil {
				return fmt.Errorf("read models.json: %w", err)
			}
			s, err := loadJSONMap(settingsPath)
			if err != nil {
				return fmt.Errorf("read settings.json: %w", err)
			}
			if config == nil || s == nil {
				return fmt.Errorf("refusing to write pi config: models.json and settings.json must be objects")
			}
			if value, exists := config["providers"]; exists {
				if _, ok := value.(map[string]any); !ok {
					return fmt.Errorf("refusing to write models.json: providers must be an object")
				}
			}
			providers := subMap(config, "providers")

			// Aggregation follows the tool's own bindings, so every entry here
			// is an endpoint pi was already set up for, with the protocol that
			// binding saved. The binding's own entry is keyed after its
			// endpoint too, so provider/id never reads a bare "charon".
			agg, err := piGroups(a)
			if err != nil {
				return err
			}
			primaryKey := agg.PrimaryKey
			if value, exists := providers[managedProvider]; exists {
				if _, ok := value.(map[string]any); !ok {
					return fmt.Errorf("refusing to write models.json: providers.charon must be an object")
				}
			}
			if value, exists := providers[primaryKey]; exists {
				if _, ok := value.(map[string]any); !ok {
					return fmt.Errorf("refusing to write models.json: providers.%s must be an object", primaryKey)
				}
			}
			legacy, err := piLegacyConfig(extensionPath)
			if err != nil {
				return err
			}

			// An older charon wrote this entry under the bare "charon" key; its
			// complete model entries carry over once to the endpoint-keyed one.
			previous, ok := providers[primaryKey].(map[string]any)
			if !ok {
				previous, _ = providers[managedProvider].(map[string]any)
			}

			modelSlug := strings.TrimSpace(a.Model)
			var entries any
			if a.Models == nil {
				// Keep the complete entries, including fields unknown to Charon.
				if previous != nil {
					if value, exists := previous["models"]; exists {
						if _, ok := value.([]any); !ok {
							return fmt.Errorf("refusing to write models.json: providers.%s.models must be an array", primaryKey)
						}
						entries = previous["models"]
					}
				}
				if entries == nil && legacy != nil && legacy.Models != nil {
					entries = legacy.Models
				}
			}
			if entries == nil {
				specs := a.Models
				if specs == nil && modelSlug != "" {
					specs = []ModelSpec{{Slug: modelSlug, Effort: a.Effort}}
				}
				label := ""
				if len(a.Groups) > 0 {
					label = agg.PrimaryLabel
				}
				entries = piBuildModels(specs, api, label)
			}
			name := agg.PrimaryLabel
			if name == "" {
				name = managedProvider
			}
			// The edit names only the paths charon owns (its provider entries and
			// the defaults); user providers, comments and ordering stay as they are.
			msets := []edit.KV{
				{Path: "providers." + primaryKey + ".name", Value: name},
				{Path: "providers." + primaryKey + ".baseUrl", Value: a.Endpoint},
				{Path: "providers." + primaryKey + ".apiKey", Value: piEscapeValue(a.Key)},
				{Path: "providers." + primaryKey + ".api", Value: api},
				{Path: "providers." + primaryKey + ".models", Value: entries},
			}
			ssets := []edit.KV{{Path: "defaultProvider", Value: primaryKey}}
			if modelSlug != "" {
				ssets = append(ssets, edit.KV{Path: "defaultModel", Value: modelSlug})
			}

			// ponytail: writes are atomic per file, not a multi-file transaction.
			// Preflight catches input errors; report partial application on I/O failure.
			if err := piWriteProviders(modelsPath, providers, primaryKey, msets, agg.Groups); err != nil {
				return fmt.Errorf("write models.json: %w", err)
			}
			if err := edit.SetJSON(settingsPath, ssets...); err != nil {
				return fmt.Errorf("write settings.json (models.json already updated): %w", err)
			}
			if legacy != nil {
				if err := os.Remove(extensionPath); err != nil {
					return fmt.Errorf("remove legacy pi extension (config updated, but old extension may still override it): %w", err)
				}
			}
			return nil
		},
		Detected: func() bool {
			return detected("pi", settingsPath, authPath, extensionPath, modelsPath)
		},
		Describe: func() (Info, error) {
			var info Info

			if data, err := os.ReadFile(settingsPath); err == nil {
				var s struct {
					DefaultProvider      string `json:"defaultProvider"`
					DefaultModel         string `json:"defaultModel"`
					DefaultThinkingLevel string `json:"defaultThinkingLevel"`
				}
				if json.Unmarshal(data, &s) == nil {
					info.Model = s.DefaultModel
					info.Effort = s.DefaultThinkingLevel

					// Every provider charon writes is keyed charon-<endpoint
					// label>; "charon" alone is what older versions wrote, and
					// an empty value means that same legacy entry.
					provider := s.DefaultProvider
					if provider == "" {
						provider = managedProvider
					}
					if provider == managedProvider || strings.HasPrefix(provider, managedProvider+"-") {
						if data, err := os.ReadFile(modelsPath); err == nil {
							var cfg struct {
								Providers map[string]piProviderConfig `json:"providers"`
							}
							if json.Unmarshal(data, &cfg) == nil {
								p, ok := cfg.Providers[provider]
								if !ok && provider != managedProvider {
									// A settings.json written by a newer charon next
									// to a models.json restored from a backup still
									// describes the legacy entry.
									p, ok = cfg.Providers[managedProvider]
								}
								if ok {
									info.Endpoint = p.BaseURL
									for _, model := range p.Models {
										if model.ID == info.Model {
											info.ContextWindow, info.MaxTokens = model.ContextWindow, model.MaxTokens
											break
										}
									}
									if p.APIKey != "" {
										info.Secret, info.AuthMode = p.APIKey, "api"
									}
								}
							}
						}
						if info.AuthMode == "" {
							if data, err := os.ReadFile(extensionPath); err == nil {
								if cfg, ok := piParseExtension(data); ok {
									info.Endpoint = cfg.BaseURL
									for _, model := range cfg.Models {
										if model.ID == info.Model {
											info.ContextWindow, info.MaxTokens = model.ContextWindow, model.MaxTokens
											break
										}
									}
									if cfg.APIKey != "" {
										info.Secret, info.AuthMode = cfg.APIKey, "api"
									}
								}
							}
						}
					}
				}
			}

			// Otherwise fall back to an OAuth-based provider login (pi's /login).
			if info.AuthMode == "" {
				if data, err := os.ReadFile(authPath); err == nil {
					var auth map[string]json.RawMessage
					if json.Unmarshal(data, &auth) == nil && len(auth) > 0 {
						names := make([]string, 0, len(auth))
						for name := range auth {
							names = append(names, name)
						}
						sort.Strings(names)
						info.AuthMode = "oauth"
						info.Account = names[0]
					}
				}
			}

			return info.withDefaults("(provider default)"), nil
		},
	}
}
