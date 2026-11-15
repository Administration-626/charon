package tools

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestPiHeadersWritePreserveReplaceAndClear(t *testing.T) {
	home := sandboxHome(t)
	spec := AuthSpec{
		Endpoint: "https://alpha.example/v1", Key: "sk-alpha", Model: "custom",
		Headers: map[string]string{
			"Originator": "codex_cli_rs",
			"User-Agent": "custom-client/1.0",
			"X.Literal":  "!literal-$TOKEN-${OTHER}",
		},
		Groups: []EndpointGroup{{
			Endpoint: "https://beta.example/v1", Key: "sk-beta",
			Headers: map[string]string{"X-Client": "beta"},
			Models:  []ModelSpec{{Slug: "custom"}},
		}},
	}
	want := map[string]any{
		"Originator": "codex_cli_rs", "User-Agent": "custom-client/1.0",
		"X.Literal": "$!literal-$$TOKEN-$${OTHER}",
	}
	for _, step := range []string{"write", "preserve", "replace", "clear"} {
		switch step {
		case "preserve":
			spec.Headers, spec.Groups[0].Headers = nil, nil
		case "replace":
			spec.Headers = map[string]string{"User-Agent": "replacement"}
			want = map[string]any{"User-Agent": "replacement"}
		case "clear":
			spec.Headers, spec.Groups[0].Headers = map[string]string{}, map[string]string{}
			want = map[string]any{}
		}
		if err := Find("pi").ApplyAuth(spec); err != nil {
			t.Fatalf("%s: %v", step, err)
		}
		primary, _ := piProviderOf(t, home, spec.Endpoint)
		if !reflect.DeepEqual(primary["headers"], want) {
			t.Fatalf("%s: primary headers = %#v, want %#v", step, primary["headers"], want)
		}
		group, _ := piProviderOf(t, home, spec.Groups[0].Endpoint)
		groupWant := map[string]any{"X-Client": "beta"}
		if step == "clear" {
			groupWant = map[string]any{}
		}
		if !reflect.DeepEqual(group["headers"], groupWant) {
			t.Fatalf("%s: group headers = %#v, want %#v", step, group["headers"], groupWant)
		}
	}
	for _, file := range []string{"models.json", "settings.json"} {
		info, err := os.Stat(filepath.Join(home, ".pi", "agent", file))
		if err != nil || info.Mode().Perm() != 0o600 {
			t.Fatalf("%s is not private: %v", file, err)
		}
	}
}

func TestPiHeadersFollowEndpointsAcrossRenamesAndSwitches(t *testing.T) {
	home := sandboxHome(t)
	modelsPath := filepath.Join(home, ".pi", "agent", "models.json")
	writeFile(t, modelsPath, `{"providers":{
		"charon": {"baseUrl":"https://alpha.example/v1","headers":{"X-Key":"$ALPHA_KEY","Originator":"alpha"}},
		"charon-old-beta": {"baseUrl":"https://beta.example/v1","headers":{"X-Key":"!beta-key-command"}},
		"mine": {"baseUrl":"https://alpha.other/v1","headers":{"X-Key":"user-only"}}
	}}`)
	alpha := AuthSpec{Endpoint: "https://alpha.example/v1", Key: "sk-alpha", Model: "custom"}
	beta := EndpointGroup{Endpoint: "https://beta.example/v1", Key: "sk-beta", Models: []ModelSpec{{Slug: "custom"}}}
	alpha.Groups = []EndpointGroup{beta}
	alphaGroup := EndpointGroup{Endpoint: alpha.Endpoint, Key: alpha.Key, Models: []ModelSpec{{Slug: "custom"}}}
	collision := EndpointGroup{Endpoint: "https://alpha.other/v1", Key: "sk-other", Models: []ModelSpec{{Slug: "custom"}}}
	for _, spec := range []AuthSpec{
		alpha,
		{Endpoint: beta.Endpoint, Key: beta.Key, Model: "custom", Groups: []EndpointGroup{alphaGroup}},
		{Endpoint: alpha.Endpoint, Key: "sk-rotated", Model: "custom", Groups: []EndpointGroup{beta, collision}},
		alpha,
	} {
		if err := Find("pi").ApplyAuth(spec); err != nil {
			t.Fatal(err)
		}
		for endpoint, want := range map[string]any{
			alpha.Endpoint: map[string]any{"X-Key": "$ALPHA_KEY", "Originator": "alpha"},
			beta.Endpoint:  map[string]any{"X-Key": "!beta-key-command"},
		} {
			provider, _ := piProviderOf(t, home, endpoint)
			if !reflect.DeepEqual(provider["headers"], want) {
				t.Fatalf("headers did not follow %s: %#v", endpoint, provider["headers"])
			}
		}
		if len(spec.Groups) == 2 {
			provider, _ := piProviderOf(t, home, collision.Endpoint)
			if _, exists := provider["headers"]; exists {
				t.Fatal("new endpoint inherited headers from a user provider or another endpoint")
			}
		}
		if got := readPiProviders(t, home)["mine"]["headers"]; !reflect.DeepEqual(got, map[string]any{"X-Key": "user-only"}) {
			t.Fatal("user provider was changed")
		}
	}
	providers := readPiProviders(t, home)
	if providers["charon"] != nil || providers["charon-old-beta"] != nil {
		t.Fatal("stale providers survived header migration")
	}
}

func TestPiHeadersDoNotLeakThroughReusedProviderKey(t *testing.T) {
	home := sandboxHome(t)
	if err := Find("pi").ApplyAuth(AuthSpec{
		Endpoint: "https://same.one/v1", Key: "sk-one", Model: "custom",
		Headers: map[string]string{"Authorization": "old-endpoint-secret"},
	}); err != nil {
		t.Fatal(err)
	}
	oldKey := piDefaultProvider(t, home)
	if err := Find("pi").ApplyAuth(AuthSpec{
		Endpoint: "https://same.two/v1", Key: "sk-two", Model: "custom",
	}); err != nil {
		t.Fatal(err)
	}
	if piDefaultProvider(t, home) != oldKey {
		t.Fatal("test must exercise reuse of the same provider key")
	}
	provider := readPiProvider(t, home)
	if headers, ok := provider["headers"].(map[string]any); ok && len(headers) > 0 {
		t.Fatal("old endpoint's headers leaked to the new endpoint")
	}
}

func TestPiHeaderPreflightLeavesFilesUntouched(t *testing.T) {
	for _, tc := range []struct {
		name     string
		existing any
		supplied map[string]string
		group    bool
	}{
		{"null", nil, nil, false},
		{"array", []string{"bad"}, nil, false},
		{"non-string", map[string]any{"X-Key": 123}, nil, false},
		{"group non-string", map[string]any{"X-Key": 123}, nil, true},
		{"invalid inherited value", map[string]any{"X-Key": "private-value\r\n"}, nil, false},
		{"invalid explicit name", map[string]any{}, map[string]string{"Bad Name": "private-value"}, false},
		{"invalid explicit group", map[string]any{}, map[string]string{"X-Key": "private-value\n"}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := sandboxHome(t)
			dir := filepath.Join(home, ".pi", "agent")
			spec := AuthSpec{
				Endpoint: "https://alpha.example/v1", Key: "sk-alpha", Model: "custom",
				Groups: []EndpointGroup{{Endpoint: "https://beta.example/v1", Key: "sk-beta", Models: []ModelSpec{{Slug: "custom"}}}},
			}
			endpoint := spec.Endpoint
			spec.Headers = tc.supplied
			if tc.group {
				endpoint = spec.Groups[0].Endpoint
				spec.Headers, spec.Groups[0].Headers = nil, tc.supplied
			}
			data, err := json.Marshal(map[string]any{"providers": map[string]any{
				"charon-old": map[string]any{"baseUrl": endpoint, "headers": tc.existing},
			}})
			if err != nil {
				t.Fatal(err)
			}
			writeFile(t, filepath.Join(dir, "models.json"), string(data))
			writeFile(t, filepath.Join(dir, "settings.json"), `{"theme":"keep"}`)
			writeFile(t, filepath.Join(dir, "extensions", "charon.ts"), string(legacyPiExtension(t)))
			files := []string{"models.json", "settings.json", "extensions/charon.ts"}
			before := map[string][]byte{}
			for _, file := range files {
				before[file], err = os.ReadFile(filepath.Join(dir, file))
				if err != nil {
					t.Fatal(err)
				}
			}
			if err := Find("pi").ApplyAuth(spec); err == nil || strings.Contains(err.Error(), "private-value") {
				t.Fatalf("invalid headers were accepted or exposed: %v", err)
			}
			for _, file := range files {
				after, err := os.ReadFile(filepath.Join(dir, file))
				if err != nil || !bytes.Equal(before[file], after) {
					t.Fatalf("%s changed during failed header preflight: %v", file, err)
				}
			}
		})
	}
}
