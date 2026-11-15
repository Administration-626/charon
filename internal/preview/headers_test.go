package preview

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"charon/internal/tools"
)

func TestPiHeaderDryRunDoesNotWriteOrExposeValues(t *testing.T) {
	for _, tc := range []struct {
		name, before string
	}{
		{"new file", ""},
		{"JSON with BOM", "\ufeff" + `{
			"providers": {"charon-alpha": {
				"baseUrl": "https://alpha.example/v1",
				"apiKey": "sk-old-private-key",
				"headers": {"X-Key": "\u0070rivate-old-value"},
				"models": [{"id":"custom","headers":{"X-Model-Key":"private-model-value"}}]
			}}
		}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("USERPROFILE", home)
			t.Setenv("XDG_CONFIG_HOME", t.TempDir())
			path := filepath.Join(home, ".pi", "agent", "models.json")
			if tc.before != "" {
				if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(tc.before), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			diffs, err := Diff(tools.Find("pi"), tools.AuthSpec{
				Endpoint: "https://alpha.example/v1", Key: "sk-new-private-key", Model: "custom",
				Headers: map[string]string{"Authorization": "Bearer private-new-value", "X.Literal": "!private-$TOKEN"},
			})
			if err != nil {
				t.Fatal(err)
			}
			var rendered strings.Builder
			for _, diff := range diffs {
				rendered.WriteString(diff.Render())
			}
			for _, value := range []string{
				"sk-old-private-key", "sk-new-private-key", "private-old-value", `\u0070rivate-old-value`,
				"private-new-value", "private-model-value", "$!private-$$TOKEN",
			} {
				if strings.Contains(rendered.String(), value) {
					t.Fatalf("dry-run exposed a credential/header value: %q", value)
				}
			}
			if !strings.Contains(rendered.String(), `"headers"`) || !strings.Contains(rendered.String(), `"Authorization"`) {
				t.Fatal("masked preview omitted header names")
			}
			after, err := os.ReadFile(path)
			if tc.before == "" {
				if !os.IsNotExist(err) {
					t.Fatal("dry-run created models.json")
				}
			} else if err != nil || !bytes.Equal(after, []byte(tc.before)) {
				t.Fatal("dry-run modified models.json")
			}
			if _, err := os.Stat(filepath.Join(home, ".pi", "agent", "settings.json")); !os.IsNotExist(err) {
				t.Fatal("dry-run created settings.json")
			}
		})
	}
}

func TestHeaderMasksKeepShortSecretChangesVisible(t *testing.T) {
	before, after := []byte("{\"headers\":{\"X-Key\":\"old\"}}\n"), []byte("{\"headers\":{\"X-Key\":\"new\"}}\n")
	for _, diff := range []FileDiff{
		{Before: before, After: after},
		{Before: before},
		{After: after},
		{Before: []byte("\ufeff// comment\n" + string(before))},
	} {
		rendered := diff.Render()
		if strings.Contains(rendered, `"old"`) || strings.Contains(rendered, `"new"`) {
			t.Fatal("short header value was not masked")
		}
		if diff.Before != nil && !strings.Contains(rendered, `-{"headers"`) {
			t.Fatal("masked removal was lost")
		}
		if diff.After != nil && !strings.Contains(rendered, `+{"headers"`) {
			t.Fatal("masked addition was lost")
		}
	}
	if string(before) != "{\"headers\":{\"X-Key\":\"old\"}}\n" || string(after) != "{\"headers\":{\"X-Key\":\"new\"}}\n" {
		t.Fatal("rendering modified the raw diff bytes")
	}
}
