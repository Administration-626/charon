package tools

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strings"
	"testing"
)

// Pi is the one multi-endpoint tool: one apply registers the binding's own
// endpoint and every other endpoint configured for pi, each with its own key,
// model list and request protocol, so /model can switch between gateways
// without charon (see pi.go's piGroups and catalog's endpointGroups).
//
// That makes one apply do the work of several. The differential here is that
// those are the same work: an endpoint registered as a group must come out
// exactly as it would if its own binding had been applied on its own. The two
// paths share model/header value builders — the group path goes through
// piGroups/piEndpointLabels/piProviderKey, the single path goes straight from
// an AuthSpec — so a drift in label folding, protocol resolution, model
// window defaults, thinking presets or header projection shows up as a difference.
//
// The one thing that is allowed to differ is the label: it is picked to stay
// unique across the whole apply, so an endpoint that shares a host with another
// gets a longer one. The comparison therefore normalizes names — the provider's
// own name and the " (label)" suffix on each model's name — and requires those
// to agree with each other.

// piProviderOf returns the charon provider entry for one endpoint, whichever key
// it ended up under, and its key.
func piProviderOf(t *testing.T, home, endpoint string) (map[string]any, string) {
	t.Helper()
	for name, provider := range readPiProviders(t, home) {
		if !strings.HasPrefix(name, managedProvider) {
			continue
		}
		if provider["baseUrl"] == endpoint {
			return provider, name
		}
	}
	t.Fatalf("no charon provider for endpoint %s", endpoint)
	return nil, ""
}

// normalizePiProvider is entry without the two things a label may change: the
// provider's display name and the label suffix pi shows next to each model. The
// suffix is checked separately, against the provider's own name, so dropping it
// here cannot hide a model labelled after the wrong endpoint.
func normalizePiProvider(t *testing.T, entry map[string]any) map[string]any {
	t.Helper()
	out := map[string]any{}
	for k, v := range entry {
		if k == "name" {
			continue
		}
		out[k] = v
	}
	if list, ok := entry["models"].([]any); ok {
		models := make([]any, 0, len(list))
		for _, raw := range list {
			m, ok := raw.(map[string]any)
			if !ok {
				models = append(models, raw)
				continue
			}
			clean := map[string]any{}
			for k, v := range m {
				if k == "name" {
					continue
				}
				clean[k] = v
			}
			models = append(models, clean)
		}
		out["models"] = models
	}
	return out
}

// checkPiModelLabels asserts that every model of entry is named "<id> (<provider
// name>)", so a model is never labelled after another endpoint.
func checkPiModelLabels(t *testing.T, entry map[string]any) {
	t.Helper()
	providerName, _ := entry["name"].(string)
	list, _ := entry["models"].([]any)
	for _, raw := range list {
		m, _ := raw.(map[string]any)
		id, _ := m["id"].(string)
		name, _ := m["name"].(string)
		want := id
		if providerName != "" {
			want = id + " (" + providerName + ")"
		}
		if name != want {
			t.Errorf("model %q named %q, want %q", id, name, want)
		}
	}
}

// piEndpoints is the shape a real catalog has: two gateways and a model id
// served by both, so the per-endpoint model lists are visibly distinct.
func piEndpoints() []EndpointGroup {
	return []EndpointGroup{
		{
			Endpoint: "https://other.example/v1",
			Key:      "sk-other",
			PiAPI:    "anthropic-messages",
			Headers:  map[string]string{"User-Agent": "other-client", "X-Key": "!literal-$KEY"},
			Models: []ModelSpec{
				// A slug whose thinking preset is bound to the Anthropic
				// dialect, so the entry's compat block and level map are only
				// right if the endpoint's own protocol reached piBuildModels.
				{Slug: "claude-opus-4-6", ContextWindow: 200000},
				// The same id the primary endpoint serves, at another window:
				// the entry has to carry this endpoint's value, not the other's.
				{Slug: "shared-model", ContextWindow: 65536},
			},
		},
		{
			Endpoint: "https://third.example:8443/v1",
			Key:      "sk-third",
			Headers:  map[string]string{},
			Models: []ModelSpec{
				// An OpenAI-dialect vendor preset, the mirror of the case above.
				{Slug: "grok-4.7", ContextWindow: 400000, Effort: "high"},
			},
		},
	}
}

func piPrimarySpec() AuthSpec {
	return AuthSpec{
		Endpoint: "https://primary.example/v1",
		Key:      "sk-primary",
		Model:    "shared-model",
		Headers:  map[string]string{"Originator": "primary-client"},
		Models: []ModelSpec{
			{Slug: "shared-model", ContextWindow: 131072},
			{Slug: "kimi-k2", ContextWindow: 262144},
		},
	}
}

// TestPiGroupEntryMatchesAStandaloneApply is the differential: each endpoint of
// an aggregated apply must equal what applying it alone produces.
func TestPiGroupEntryMatchesAStandaloneApply(t *testing.T) {
	groups := piEndpoints()

	// The aggregated apply.
	home := sandboxHome(t)
	spec := piPrimarySpec()
	spec.Groups = groups
	if err := Find("pi").ApplyAuth(spec); err != nil {
		t.Fatal(err)
	}
	aggregated := map[string]map[string]any{}
	for _, ep := range append([]string{spec.Endpoint}, func() []string {
		var out []string
		for _, g := range groups {
			out = append(out, g.Endpoint)
		}
		return out
	}()...) {
		entry, key := piProviderOf(t, home, ep)
		aggregated[ep] = entry
		t.Logf("aggregated %s -> %s", ep, key)
	}
	if primary := piDefaultProvider(t, home); !strings.HasPrefix(primary, managedProvider) {
		t.Fatalf("defaultProvider %q is not charon's", primary)
	}

	// Each endpoint applied on its own, in its own home.
	singles := []AuthSpec{piPrimarySpec()}
	for _, g := range groups {
		singles = append(singles, AuthSpec{
			Endpoint: g.Endpoint,
			PiAPI:    g.PiAPI,
			Key:      g.Key,
			Headers:  g.Headers,
			Model:    g.Models[0].Slug,
			Models:   g.Models,
		})
	}
	for _, single := range singles {
		t.Run(single.Endpoint, func(t *testing.T) {
			solo := sandboxHome(t)
			if err := Find("pi").ApplyAuth(single); err != nil {
				t.Fatal(err)
			}
			want, _ := piProviderOf(t, solo, single.Endpoint)
			got, ok := aggregated[single.Endpoint]
			if !ok {
				t.Fatalf("endpoint %s missing from the aggregated apply", single.Endpoint)
			}
			// The default entry of the standalone apply is the same entry the
			// aggregate keeps for that endpoint.
			if soloDefault := piDefaultProvider(t, solo); soloDefault == "" {
				t.Fatal("standalone apply set no defaultProvider")
			}
			// The entry must name its own endpoint, key included: a group
			// carrying the primary's key would authenticate against the wrong
			// account, and the standalone apply is the reference for both.
			if single.Key != "" && got["apiKey"] != single.Key {
				t.Errorf("group apiKey = %v, want this endpoint's own %q", got["apiKey"], single.Key)
			}
			if a, b := normalizePiProvider(t, got), normalizePiProvider(t, want); !reflect.DeepEqual(a, b) {
				ja, _ := json.MarshalIndent(a, "", "  ")
				jb, _ := json.MarshalIndent(b, "", "  ")
				t.Fatalf("group entry differs from a standalone apply\n--- aggregated ---\n%s\n--- standalone ---\n%s", ja, jb)
			}
			checkPiModelLabels(t, got)
		})
	}
}

// TestPiAggregationKeepsEachEndpointsProtocol: the protocol a binding saved must
// travel with its endpoint. An anthropic-messages gateway registered next to an
// OpenAI-completions one has to keep its own dialect — including the thinking
// presets that dialect selects — or pi sends the wrong request shape.
func TestPiAggregationKeepsEachEndpointsProtocol(t *testing.T) {
	home := sandboxHome(t)
	spec := piPrimarySpec()
	spec.Groups = piEndpoints()
	if err := Find("pi").ApplyAuth(spec); err != nil {
		t.Fatal(err)
	}
	byEndpoint := map[string]map[string]any{}
	for name, provider := range readPiProviders(t, home) {
		if strings.HasPrefix(name, managedProvider) {
			byEndpoint[provider["baseUrl"].(string)] = provider
		}
	}
	if got, want := byEndpoint[spec.Endpoint]["api"], "openai-completions"; got != want {
		t.Errorf("primary api = %v, want %v", got, want)
	}
	// Default protocol when a group carries none.
	if got, want := byEndpoint["https://third.example:8443/v1"]["api"], "openai-completions"; got != want {
		t.Errorf("third api = %v, want %v", got, want)
	}
	// Explicit protocol preserved, and its dialect's compat block with it.
	other := byEndpoint["https://other.example/v1"]
	if got, want := other["api"], "anthropic-messages"; got != want {
		t.Fatalf("other api = %v, want %v", got, want)
	}

	// The same holds against a standalone apply of that endpoint, which is the
	// reference the aggregation must match.
	solo := sandboxHome(t)
	group := piEndpoints()[0]
	if err := Find("pi").ApplyAuth(AuthSpec{
		Endpoint: group.Endpoint, PiAPI: group.PiAPI, Key: group.Key,
		Model: group.Models[0].Slug, Models: group.Models, Headers: group.Headers,
	}); err != nil {
		t.Fatal(err)
	}
	want, _ := piProviderOf(t, solo, group.Endpoint)
	if a, b := normalizePiProvider(t, other), normalizePiProvider(t, want); !reflect.DeepEqual(a, b) {
		ja, _ := json.MarshalIndent(a, "", "  ")
		jb, _ := json.MarshalIndent(b, "", "  ")
		t.Fatalf("anthropic group differs from its standalone apply\n--- aggregated ---\n%s\n--- standalone ---\n%s", ja, jb)
	}
}

// TestPiAggregationIsOrderIndependent: the catalog sorts its groups by URL, but
// the invariant has to hold on its own — the same set of endpoints registered in
// any order must produce the same set of provider entries, so a reordered
// catalog cannot change what pi offers.
func TestPiAggregationIsOrderIndependent(t *testing.T) {
	build := func(order []int) map[string]map[string]any {
		home := sandboxHome(t)
		spec := piPrimarySpec()
		all := piEndpoints()
		for _, i := range order {
			spec.Groups = append(spec.Groups, all[i])
		}
		if err := Find("pi").ApplyAuth(spec); err != nil {
			t.Fatal(err)
		}
		byEndpoint := map[string]map[string]any{}
		for name, provider := range readPiProviders(t, home) {
			if strings.HasPrefix(name, managedProvider) {
				byEndpoint[provider["baseUrl"].(string)] = normalizePiProvider(t, provider)
			}
		}
		return byEndpoint
	}
	a := build([]int{0, 1})
	b := build([]int{1, 0})
	if !reflect.DeepEqual(a, b) {
		ja, _ := json.MarshalIndent(a, "", "  ")
		jb, _ := json.MarshalIndent(b, "", "  ")
		t.Fatalf("group order changed the result\n--- [0 1] ---\n%s\n--- [1 0] ---\n%s", ja, jb)
	}
}

// TestPiAggregationDropsGroupsTheBindingNoLongerHas: the entry set follows the
// catalog, so dropping a binding (or hiding it) removes its endpoint on the next
// apply without disturbing the others.
func TestPiAggregationDropsGroupsTheBindingNoLongerHas(t *testing.T) {
	home := sandboxHome(t)
	spec := piPrimarySpec()
	spec.Groups = piEndpoints()
	if err := Find("pi").ApplyAuth(spec); err != nil {
		t.Fatal(err)
	}
	if got := len(charonProviders(t, home)); got != 3 {
		t.Fatalf("providers after the first apply = %d, want 3", got)
	}

	// Keep only the first group. The second endpoint's entry must go, and the
	// remaining ones must be untouched.
	first := readPiProviders(t, home)
	spec.Groups = piEndpoints()[:1]
	if err := Find("pi").ApplyAuth(spec); err != nil {
		t.Fatal(err)
	}
	second := readPiProviders(t, home)
	if got := len(charonProviders(t, home)); got != 2 {
		t.Fatalf("providers after dropping one = %d, want 2", got)
	}
	for name, entry := range second {
		if !strings.HasPrefix(name, managedProvider) {
			continue
		}
		if prev, ok := first[name]; ok && !reflect.DeepEqual(prev, entry) {
			t.Errorf("provider %s changed when another was dropped", name)
		}
	}
}

// TestPiAggregationReapplyIsByteStable: applying the same aggregated spec twice
// must not rewrite the file. It is the property the write order in
// piWriteProviders exists for, and the reason an idle `charon switch` does not
// touch models.json.
func TestPiAggregationReapplyIsByteStable(t *testing.T) {
	home := sandboxHome(t)
	spec := piPrimarySpec()
	spec.Groups = piEndpoints()
	if err := Find("pi").ApplyAuth(spec); err != nil {
		t.Fatal(err)
	}
	modelsPath := filepath.Join(home, ".pi", "agent", "models.json")
	first, err := os.ReadFile(modelsPath)
	if err != nil {
		t.Fatal(err)
	}
	for i := 2; i <= 3; i++ {
		if err := Find("pi").ApplyAuth(spec); err != nil {
			t.Fatalf("apply %d: %v", i, err)
		}
		again, err := os.ReadFile(modelsPath)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(first, again) {
			t.Fatalf("apply %d changed the file\n--- first ---\n%s\n--- again ---\n%s", i, first, again)
		}
	}
}

// TestPiAggregationKeysStayUnique: two endpoints whose labels fold to the same
// provider key must both be registered, the second under a -2 suffix, and each
// must still carry its own endpoint URL and key. This is the only way a group
// can be lost: pi reads models.json as a map, so a second entry under one key
// silently replaces the first.
func TestPiAggregationKeysStayUnique(t *testing.T) {
	home := sandboxHome(t)
	spec := piPrimarySpec()
	// Both labels fold to "a-b-c": the two hosts differ only in where the dot
	// and the dash are, and foldLabel writes both as a dash.
	spec.Groups = []EndpointGroup{
		{Endpoint: "https://a.b-c.example/v1", Key: "sk-a", Models: []ModelSpec{{Slug: "a"}}},
		{Endpoint: "https://a-b.c.example/v1", Key: "sk-b", Models: []ModelSpec{{Slug: "b"}}},
	}
	if err := Find("pi").ApplyAuth(spec); err != nil {
		t.Fatal(err)
	}
	names := charonProviders(t, home)
	if len(names) != 3 {
		t.Fatalf("charon providers = %v, want three", names)
	}
	if !slices.Contains(names, managedProvider+"-a-b-c") || !slices.Contains(names, managedProvider+"-a-b-c-2") {
		t.Errorf("colliding labels were not suffixed: %v", names)
	}
	seen := map[string]string{}
	for name, provider := range readPiProviders(t, home) {
		if !strings.HasPrefix(name, managedProvider) {
			continue
		}
		baseURL := provider["baseUrl"].(string)
		if prev, dup := seen[baseURL]; dup {
			t.Errorf("%s and %s both hold endpoint %s", prev, name, baseURL)
		}
		seen[baseURL] = name
	}
	for _, ep := range []string{"https://a.b-c.example/v1", "https://a-b.c.example/v1", spec.Endpoint} {
		if _, ok := seen[ep]; !ok {
			t.Errorf("endpoint %s was not registered: %v", ep, seen)
		}
	}
}

func charonProviders(t *testing.T, home string) []string {
	t.Helper()
	var out []string
	for name := range readPiProviders(t, home) {
		if strings.HasPrefix(name, managedProvider) {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}
