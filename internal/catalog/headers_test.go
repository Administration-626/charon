package catalog

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"charon/internal/tools"
)

func TestBindingHeadersRoundTripAndOrdinaryEdits(t *testing.T) {
	for _, tc := range []struct {
		name    string
		headers map[string]string
	}{
		{"unset", nil},
		{"clear", map[string]string{}},
		{"literal", map[string]string{"User-Agent": "custom-client", "X-Key": "!literal-$KEY"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := openTest(t)
			_, cr := seed(t, c, "https://alpha.example/v1", "sk-alpha", "custom")
			b, err := c.AddBinding("pi", "work", cr.ID, "custom", nil)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := c.SetBindingHeaders(b.ID, tc.headers); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(c.table("bindings.json"))
			if err != nil || strings.Contains(string(data), `"headers"`) != (tc.headers != nil) {
				t.Fatalf("header override presence changed on disk: %v", err)
			}
			reopened, err := Open()
			if err != nil {
				t.Fatal(err)
			}
			// The form may still hold the binding from before headers were set.
			// Its ordinary edit must preserve the latest saved header override.
			updated, err := StoreBinding(reopened, tools.Find("pi"), &b, "renamed", "https://alpha.example/v1", "sk-rotated", "custom", nil, false, nil)
			if err != nil || !reflect.DeepEqual(updated.Headers, tc.headers) {
				t.Fatalf("ordinary edit lost headers: %#v, %v", updated.Headers, err)
			}
			spec, err := reopened.ProjectSpec(updated)
			if err != nil || !reflect.DeepEqual(spec.Headers, tc.headers) {
				t.Fatalf("headers did not reach AuthSpec unchanged: %#v, %v", spec.Headers, err)
			}
		})
	}
}

func piHeadersByEndpoint(t *testing.T) map[string]map[string]string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(os.Getenv("HOME"), ".pi", "agent", "models.json"))
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		Providers map[string]struct {
			BaseURL string            `json:"baseUrl"`
			Headers map[string]string `json:"headers"`
		} `json:"providers"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatal(err)
	}
	out := map[string]map[string]string{}
	for _, provider := range cfg.Providers {
		out[provider.BaseURL] = provider.Headers
	}
	return out
}

func TestBindingHeadersSurviveSwitchingAndHiding(t *testing.T) {
	c := openTest(t)
	var bindings []Binding
	want := map[string]map[string]string{}
	for _, label := range []string{"alpha", "beta"} {
		endpoint := "https://" + label + ".example/v1"
		_, cr := seed(t, c, endpoint, "sk-"+label, "custom")
		b, err := c.AddBinding("pi", label, cr.ID, "custom", nil)
		if err != nil {
			t.Fatal(err)
		}
		b, err = c.SetBindingHeaders(b.ID, map[string]string{"Originator": label})
		if err != nil {
			t.Fatal(err)
		}
		bindings = append(bindings, b)
		want[endpoint] = b.Headers
	}
	spec, err := c.ProjectSpec(bindings[0])
	if err != nil || len(spec.Groups) != 1 || !reflect.DeepEqual(spec.Groups[0].Headers, bindings[1].Headers) {
		t.Fatalf("other endpoint's headers did not reach Groups: %#v, %v", spec.Groups, err)
	}
	for _, b := range []Binding{bindings[0], bindings[1], bindings[0]} {
		if _, err := c.Activate(b.ID); err != nil {
			t.Fatal(err)
		}
		if got := piHeadersByEndpoint(t); !reflect.DeepEqual(got, want) {
			t.Fatalf("switch did not keep each endpoint's headers: %#v", got)
		}
	}
	if err := c.SetBindingHidden(bindings[1].ID, true); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Reapply(bindings[0].ID); err != nil {
		t.Fatal(err)
	}
	if _, exists := piHeadersByEndpoint(t)["https://beta.example/v1"]; exists {
		t.Fatal("hidden endpoint survived in Pi's config")
	}
	if _, err := c.Activate(bindings[1].ID); err != nil {
		t.Fatal(err)
	}
	if got := piHeadersByEndpoint(t); !reflect.DeepEqual(got, want) {
		t.Fatalf("headers lost after removing and recreating a hidden provider: %#v", got)
	}
}

func TestSetBindingHeadersRejectsInvalidUpdates(t *testing.T) {
	c := openTest(t)
	_, cr := seed(t, c, "https://alpha.example/v1", "sk-alpha", "custom")
	pi, err := c.AddBinding("pi", "work", cr.ID, "custom", nil)
	if err != nil {
		t.Fatal(err)
	}
	claude, err := c.AddBinding("claude", "work", cr.ID, "custom", nil)
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(c.table("bindings.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		id      string
		headers map[string]string
	}{
		{pi.ID, map[string]string{"X-Key": "private-value\n"}},
		{pi.ID, map[string]string{"Bad Name": "private-value"}},
		{claude.ID, map[string]string{"X-Key": "private-value"}},
		{"missing", map[string]string{}},
	} {
		_, err := c.SetBindingHeaders(tc.id, tc.headers)
		if err == nil || strings.Contains(err.Error(), "private-value") {
			t.Fatalf("invalid update accepted or exposed a value: %v", err)
		}
		if tc.id == "missing" && !errors.Is(err, ErrNotFound) {
			t.Fatalf("unknown binding error = %v", err)
		}
		after, err := os.ReadFile(c.table("bindings.json"))
		if err != nil || !bytes.Equal(before, after) {
			t.Fatalf("failed update changed bindings: %v", err)
		}
	}
}
