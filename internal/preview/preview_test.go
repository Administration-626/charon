package preview

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"charon/internal/tools"
)

func TestDiffRunsOnCopiesAndLeavesRealFilesAlone(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	config := filepath.Join(home, ".grok", "config.toml")
	if err := os.MkdirAll(filepath.Dir(config), 0o700); err != nil {
		t.Fatal(err)
	}
	orig := "# grok config\n\n[models]\ndefault = \"mine\"\n\n[model.mine]\nname = \"mine\"\n"
	if err := os.WriteFile(config, []byte(orig), 0o600); err != nil {
		t.Fatal(err)
	}

	tt := tools.Find("grok")
	diffs, err := Diff(tt, tools.AuthSpec{Endpoint: "https://gw/v1", Key: "sk-x", Model: "glm-4.6"})
	if err != nil {
		t.Fatal(err)
	}
	if len(diffs) != 1 {
		t.Fatalf("diffs = %d, want 1", len(diffs))
	}
	d := diffs[0]
	if d.Path != config {
		t.Errorf("path = %q", d.Path)
	}
	rendered := d.Render()
	if !strings.Contains(rendered, "+[model.\"charon-glm-4.6\"]") {
		t.Errorf("new model table missing from diff:\n%s", rendered)
	}
	if !strings.Contains(rendered, "-default = \"mine\"") || !strings.Contains(rendered, "+default = \"charon-glm-4.6\"") {
		t.Errorf("default not diffed:\n%s", rendered)
	}
	if !strings.Contains(rendered, "# grok config") {
		t.Errorf("context lines missing:\n%s", rendered)
	}

	// The real file is untouched.
	after, err := os.ReadFile(config)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != orig {
		t.Errorf("real config was modified:\n%s", after)
	}
}

func TestDiffReportsNewFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	tt := tools.Find("claude") // no ~/.claude/settings.json exists
	diffs, err := Diff(tt, tools.AuthSpec{Endpoint: "https://gw/v1", Key: "sk-x", Model: "m"})
	if err != nil {
		t.Fatal(err)
	}
	if len(diffs) != 1 || diffs[0].Before != nil {
		t.Fatalf("diffs = %+v, want one new-file diff", diffs)
	}
	r := diffs[0].Render()
	if !strings.HasPrefix(r, "(new file)") || !strings.Contains(r, "\"ANTHROPIC_BASE_URL\": \"https://gw\"") {
		t.Errorf("new-file diff wrong:\n%s", r)
	}
	if _, err := os.Stat(filepath.Join(home, ".claude")); !os.IsNotExist(err) {
		t.Errorf("dry run created files under the real home: %v", err)
	}
}

func TestDiffUnchangedWhenAlreadyApplied(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	config := filepath.Join(home, ".grok", "config.toml")
	os.MkdirAll(filepath.Dir(config), 0o700)
	os.WriteFile(config, []byte("[models]\ndefault = \"charon-glm-4.6\"\n\n[model.\"charon-glm-4.6\"]\nmodel = \"glm-4.6\"\nname = \"glm-4.6\"\nbase_url = \"https://gw/v1\"\napi_key = \"sk-x\"\n"), 0o600)

	diffs, err := Diff(tools.Find("grok"), tools.AuthSpec{Endpoint: "https://gw/v1", Key: "sk-x", Model: "glm-4.6"})
	if err != nil {
		t.Fatal(err)
	}
	if len(diffs) != 0 {
		for _, d := range diffs {
			t.Logf("unexpected change:\n%s", d.Render())
		}
		t.Fatalf("diffs = %d, want 0 (already applied)", len(diffs))
	}
}

func TestRenderDeletedFile(t *testing.T) {
	d := FileDiff{Path: "x", Before: []byte("a\nb\n")}
	r := d.Render()
	if !strings.Contains(r, "(file removed)") || !strings.Contains(r, "-a") {
		t.Errorf("deleted-file render wrong:\n%s", r)
	}
}

func TestDiffRenderHunks(t *testing.T) {
	d := FileDiff{
		Before: []byte("1\n2\n3\n4\n5\n6\n7\n8\n9\n10\n"),
		After:  []byte("1\n2\n3\n4\n5\n6\n7\n8\n9\nten\n"),
	}
	r := d.Render()
	if !strings.HasPrefix(r, "@@ -7,4 +7,4 @@") {
		t.Errorf("hunk header wrong:\n%s", r)
	}
	if !strings.Contains(r, "-10") || !strings.Contains(r, "+ten") {
		t.Errorf("change missing:\n%s", r)
	}
	if strings.Contains(r, "-1\n") {
		t.Errorf("far-away lines leaked into the hunk:\n%s", r)
	}
}
