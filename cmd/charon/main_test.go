package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"charon/internal/catalog"
)

// sandbox points HOME and the catalog's XDG_CONFIG_HOME at temp dirs so run()
// never touches real user config (see AGENTS.md).
func sandbox(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USER", "tester")
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("PATH", t.TempDir()) // prevents Claude detection from querying the real Keychain
	claudeDir := filepath.Join(home, ".claude")
	if err := os.MkdirAll(claudeDir, 0o700); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(claudeDir, "settings.json"), `{"env":{"ANTHROPIC_API_KEY":"sk-test"}}`)
	return home
}

// seedCodex fakes an installed Codex CLI (auth.json makes it "detected").
func seedCodex(t *testing.T, home string) {
	t.Helper()
	dir := filepath.Join(home, ".codex")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(dir, "auth.json"), `{"auth_mode":"apikey","OPENAI_API_KEY":"sk-live"}`)
	writeTestFile(t, filepath.Join(dir, "config.toml"), "model = \"gpt-5\"\n")
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func openCatalog(t *testing.T) *catalog.Catalog {
	t.Helper()
	c, err := catalog.Open()
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestRunRejectsUnknownCommandAndTool(t *testing.T) {
	sandbox(t)
	if err := run([]string{"bogus"}); err == nil || !strings.Contains(err.Error(), "unknown command") {
		t.Errorf("run(bogus) = %v, want unknown-command error", err)
	}
	for _, args := range [][]string{
		{"rm", "faketool", "x"},
		{"switch", "faketool", "x"},
		{"edit", "faketool", "x"},
		{"ls", "faketool"},
		{"cp", "faketool", "a", "b"},
	} {
		if err := run(args); err == nil || !strings.Contains(err.Error(), "unknown tool") {
			t.Errorf("run(%v) = %v, want unknown-tool error", args, err)
		}
	}
	// Snapshot commands are gone; they must read as unknown, not as a usage error.
	if err := run([]string{"undo", "codex"}); err == nil || !strings.Contains(err.Error(), "unknown command") {
		t.Errorf("run(undo) = %v, want unknown-command error", err)
	}
}

func TestRunBindingLifecycle(t *testing.T) {
	home := sandbox(t)
	seedCodex(t, home)

	if err := run([]string{"add", "codex", "--name", "work", "--key", "sk-test", "--endpoint", "https://example.com/v1", "--model", "gpt-5"}); err != nil {
		t.Fatalf("add: %v", err)
	}
	if err := run([]string{"ls", "codex"}); err != nil {
		t.Fatalf("ls: %v", err)
	}
	if err := run([]string{"cp", "codex", "work", "work-2"}); err != nil {
		t.Fatalf("cp: %v", err)
	}
	if err := run([]string{"switch", "codex", "work-2"}); err != nil {
		t.Fatalf("switch: %v", err)
	}
	// The active binding cannot be deleted; switch away first.
	if err := run([]string{"rm", "codex", "work-2"}); err == nil {
		t.Fatal("rm of the active binding succeeded, want a refusal")
	}
	if err := run([]string{"switch", "codex", "work"}); err != nil {
		t.Fatalf("switch back: %v", err)
	}
	if err := run([]string{"rm", "codex", "work-2"}); err != nil {
		t.Fatalf("rm: %v", err)
	}

	c := openCatalog(t)
	if _, found, err := c.BindingByName("codex", "work"); err != nil || !found {
		t.Errorf("work binding missing: found=%v err=%v", found, err)
	}
	if _, found, err := c.BindingByName("codex", "work-2"); err != nil || found {
		t.Errorf("work-2 still exists after rm: found=%v err=%v", found, err)
	}
	if _, found, err := c.BindingByName("codex", "default"); err != nil || found {
		t.Errorf("a default binding was captured: found=%v err=%v", found, err)
	}
}

func TestRunCopyBindingAcrossTools(t *testing.T) {
	home := sandbox(t)
	seedCodex(t, home)
	if err := run([]string{"add", "opencode", "--name", "work", "--key", "sk-test", "--endpoint", "https://example.com/v1", "--model", "kimi-k2", "--models", "kimi-k2,glm-4.6"}); err != nil {
		t.Fatalf("add: %v", err)
	}
	if err := run([]string{"cp", "opencode", "work", "codex", "work"}); err != nil {
		t.Fatalf("cross-tool cp: %v", err)
	}

	c := openCatalog(t)
	copied, found, err := c.BindingByName("codex", "work")
	if err != nil || !found {
		t.Fatalf("copied binding missing: found=%v err=%v", found, err)
	}
	credential, err := c.Credential(copied.CredentialID)
	if err != nil {
		t.Fatal(err)
	}
	if credential.Key != "sk-test" {
		t.Fatalf("copied key = %q, want sk-test", credential.Key)
	}
	if slugs, err := c.ModelSlugs(copied.Models); err != nil || len(slugs) != 1 || slugs[0] != "kimi-k2" {
		t.Fatalf("copied models = %v, err=%v; want single kimi-k2 for Codex", slugs, err)
	}
}

func TestRunValidatesBindingNames(t *testing.T) {
	home := sandbox(t)
	seedCodex(t, home)

	for _, args := range [][]string{
		{"rm", "codex", "nope"},      // no such binding
		{"save", "codex", "../evil"}, // unknown command
		{"add", "codex", "--name", "line\nbreak", "--key", "sk-test", "--model", "gpt-5"},
	} {
		if err := run(args); err == nil {
			t.Errorf("run(%v) succeeded, want error", args)
		}
	}
	if err := run([]string{"add", "codex", "--name", "../evil", "--key", "sk-test", "--model", "gpt-5"}); err != nil {
		t.Fatalf("path-shaped text is still only a label: %v", err)
	}
	// Invalid names must not have deleted the catalog dir.
	if _, err := os.Stat(filepath.Join(home, ".config", "charon")); err != nil {
		t.Fatalf("catalog dir damaged: %v", err)
	}
}

func TestRunRejectsInvalidURLAndKey(t *testing.T) {
	home := sandbox(t)
	seedCodex(t, home)

	for _, args := range [][]string{
		{"add", "codex", "--name", "bad-url", "--key", "sk-test", "--model", "gpt-5", "--endpoint", "not a url"},
		{"add", "codex", "--name", "bad-scheme", "--key", "sk-test", "--model", "gpt-5", "--endpoint", "ftp://example.com"},
		{"add", "codex", "--name", "no-key", "--key", "  ", "--model", "gpt-5"},
		{"models", "codex", "--key", "sk-test", "--endpoint", "not a url"},
	} {
		if err := run(args); err == nil {
			t.Errorf("run(%v) succeeded, want validation error", args)
		}
	}

	if err := run([]string{"add", "codex", "--name", "good", "--key", "sk-test", "--endpoint", "https://example.com/v1", "--model", "gpt-5"}); err != nil {
		t.Fatalf("add with valid endpoint/key: %v", err)
	}
	if err := run([]string{"edit", "codex", "good", "--endpoint", "not a url"}); err == nil {
		t.Error("edit with invalid endpoint succeeded, want error")
	}
	if err := run([]string{"edit", "codex", "good", "--key", " "}); err == nil {
		t.Error("edit with blank key succeeded, want error")
	}
}

// TestRunEditDoesNotSwitchInactiveProfile locks in that editing a saved-but-
// inactive binding via the CLI only updates the catalog, leaving the active
// binding (and hence the live config) untouched.
func TestRunEditDoesNotSwitchInactiveProfile(t *testing.T) {
	home := sandbox(t)
	seedCodex(t, home)

	if err := run([]string{"add", "codex", "--name", "work", "--key", "sk-work", "--endpoint", "https://work.example.com/v1", "--model", "gpt-5"}); err != nil {
		t.Fatalf("add work: %v", err)
	}
	if err := run([]string{"add", "codex", "--name", "other", "--key", "sk-other", "--endpoint", "https://other.example.com/v1", "--model", "gpt-5"}); err != nil {
		t.Fatalf("add other: %v", err)
	}
	if err := run([]string{"switch", "codex", "work"}); err != nil {
		t.Fatalf("switch work: %v", err)
	}

	if err := run([]string{"edit", "codex", "other", "--model", "gpt-5.5"}); err != nil {
		t.Fatalf("edit other: %v", err)
	}

	c := openCatalog(t)
	active, found, err := c.Active("codex")
	if err != nil || !found || active.Name != "work" {
		t.Errorf("active = %+v found=%v err=%v, want work (editing an inactive binding must not switch)", active, found, err)
	}
	other, found, err := c.BindingByName("codex", "other")
	if err != nil || !found {
		t.Fatalf("other binding: found=%v err=%v", found, err)
	}
	if slug, err := c.ModelSlug(other.ModelID); err != nil || slug != "gpt-5.5" {
		t.Errorf("other model = %q err=%v, want gpt-5.5 stored without rendering", slug, err)
	}
}

func TestRunEditModelsPromotesFirstItemWhenOldDefaultIsRemoved(t *testing.T) {
	home := sandbox(t)
	seedClaude(t, home)
	if err := run([]string{
		"add", "claude", "--name", "gateway", "--key", "sk-test",
		"--endpoint", "https://gateway.example/v1", "--models", "kimi-k2,glm-4.6",
	}); err != nil {
		t.Fatalf("add: %v", err)
	}
	if err := run([]string{"edit", "claude", "gateway", "--models", "glm-4.6"}); err != nil {
		t.Fatalf("edit: %v", err)
	}

	c := openCatalog(t)
	b, found, err := c.BindingByName("claude", "gateway")
	if err != nil || !found {
		t.Fatalf("gateway binding: found=%v err=%v", found, err)
	}
	if slug, err := c.ModelSlug(b.ModelID); err != nil || slug != "glm-4.6" {
		t.Errorf("default model = %q, err=%v; want first requested model glm-4.6", slug, err)
	}
	if models, err := c.ModelSlugs(b.Models); err != nil || len(models) != 1 || models[0] != "glm-4.6" {
		t.Errorf("models = %v, err=%v; want [glm-4.6]", models, err)
	}
}

func TestRunStatusAndVersion(t *testing.T) {
	home := sandbox(t)
	seedCodex(t, home)
	if err := run([]string{"status", "--json"}); err != nil {
		t.Errorf("status --json: %v", err)
	}
	if err := run([]string{"status"}); err != nil {
		t.Errorf("status: %v", err)
	}
	if err := run([]string{"version"}); err != nil {
		t.Errorf("version: %v", err)
	}
}

func TestRunCompletionBash(t *testing.T) {
	sandbox(t)
	if err := run([]string{"completion", "bash"}); err != nil {
		t.Errorf("completion bash: %v", err)
	}
}

func TestRunCompletionZsh(t *testing.T) {
	sandbox(t)
	if err := run([]string{"completion", "zsh"}); err != nil {
		t.Errorf("completion zsh: %v", err)
	}
}

func TestRunCompletionFish(t *testing.T) {
	sandbox(t)
	if err := run([]string{"completion", "fish"}); err != nil {
		t.Errorf("completion fish: %v", err)
	}
}

func TestRunCompletionUnsupportedShell(t *testing.T) {
	sandbox(t)
	if err := run([]string{"completion", "powershell"}); err == nil || !strings.Contains(err.Error(), "unsupported shell") {
		t.Errorf("completion powershell: %v, want unsupported-shell error", err)
	}
}

func TestRunProfilesListsBindingNames(t *testing.T) {
	home := sandbox(t)
	seedCodex(t, home)

	if err := run([]string{"add", "codex", "--name", "work", "--key", "sk-test", "--endpoint", "https://example.com/v1", "--model", "gpt-5"}); err != nil {
		t.Fatalf("add: %v", err)
	}
	// __profiles is a hidden command for shell completion.
	if err := run([]string{"__profiles", "codex"}); err != nil {
		t.Errorf("__profiles codex: %v", err)
	}
}

func TestRunModelsRequiresKey(t *testing.T) {
	sandbox(t)
	if err := run([]string{"models", "codex"}); err == nil {
		t.Error("models without --key succeeded, want error")
	}
}

func TestRunModelsWithInvalidEndpoint(t *testing.T) {
	sandbox(t)
	if err := run([]string{"models", "codex", "--key", "sk-test", "--endpoint", "not a url"}); err == nil {
		t.Error("models with invalid endpoint succeeded, want error")
	}
}

// seedClaude fakes an installed Claude Code (settings.json makes it "detected").
func seedClaude(t *testing.T, home string) {
	t.Helper()
	dir := filepath.Join(home, ".claude")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(dir, "settings.json"), `{"theme":"dark"}`)
}

// claudePickerIDs reads the model ids registered in the sandboxed settings.json.
func claudePickerIDs(t *testing.T, home string) []string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(home, ".claude", "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	var s struct {
		ModelPicker struct {
			Options []struct {
				Model string `json:"model"`
			} `json:"options"`
		} `json:"modelPicker"`
	}
	if err := json.Unmarshal(data, &s); err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, o := range s.ModelPicker.Options {
		ids = append(ids, o.Model)
	}
	return ids
}

func bindingSlugs(t *testing.T, c *catalog.Catalog, tool, name string) (slug string, slugs []string) {
	t.Helper()
	b, found, err := c.BindingByName(tool, name)
	if err != nil || !found {
		t.Fatalf("binding %s/%s: found=%v err=%v", tool, name, found, err)
	}
	slug, err = c.ModelSlug(b.ModelID)
	if err != nil {
		t.Fatal(err)
	}
	slugs, err = c.ModelSlugs(b.Models)
	if err != nil {
		t.Fatal(err)
	}
	return slug, slugs
}

// TestRunAddWithModelsRegistersPickerList covers `--models a,b,c`: the ids reach the
// tool's own model menu (so switching model mid-session needs no charon round trip)
// and are stored on the binding, with the first one taken as the default model.
func TestRunAddWithModelsRegistersPickerList(t *testing.T) {
	home := sandbox(t)
	seedClaude(t, home)

	args := []string{"add", "claude", "--name", "gw", "--key", "sk-gw-123456789",
		"--endpoint", "https://gateway.example/v1", "--models", "kimi-k2, glm-4.6 ,deepseek-v3,"}
	if err := run(args); err != nil {
		t.Fatalf("add: %v", err)
	}

	if got := strings.Join(claudePickerIDs(t, home), ","); got != "kimi-k2,glm-4.6,deepseek-v3" {
		t.Errorf("modelPicker ids = %q, want the whole --models list", got)
	}
	slug, slugs := bindingSlugs(t, openCatalog(t), "claude", "gw")
	if strings.Join(slugs, ",") != "kimi-k2,glm-4.6,deepseek-v3" {
		t.Errorf("binding models = %v, want the --models list persisted", slugs)
	}
	if slug != "kimi-k2" {
		t.Errorf("default model = %q, want the first --models id promoted to default", slug)
	}
}

// TestRunEditKeepsPickerListWithoutModelsFlag is the regression guard: an edit that
// only rotates the key must leave the tool's model menu intact.
func TestRunEditKeepsPickerListWithoutModelsFlag(t *testing.T) {
	home := sandbox(t)
	seedClaude(t, home)

	if err := run([]string{"add", "claude", "--name", "gw", "--key", "sk-gw-123456789",
		"--endpoint", "https://gateway.example/v1", "--models", "kimi-k2,glm-4.6"}); err != nil {
		t.Fatalf("add: %v", err)
	}
	if err := run([]string{"edit", "claude", "gw", "--key", "sk-gw-987654321"}); err != nil {
		t.Fatalf("edit: %v", err)
	}

	if got := strings.Join(claudePickerIDs(t, home), ","); got != "kimi-k2,glm-4.6" {
		t.Errorf("modelPicker ids = %q, want the list to survive a key rotation", got)
	}
	_, slugs := bindingSlugs(t, openCatalog(t), "claude", "gw")
	if strings.Join(slugs, ",") != "kimi-k2,glm-4.6" {
		t.Errorf("binding models = %v, want the list preserved", slugs)
	}
}

// TestRunEditModelsFlagReplacesPickerList: passing --models explicitly curates the
// menu down, which is how you drop models you no longer want offered. A list of
// one must replace the picker rather than fall through to "keep the existing list".
func TestRunEditModelsFlagReplacesPickerList(t *testing.T) {
	home := sandbox(t)
	seedClaude(t, home)

	if err := run([]string{"add", "claude", "--name", "gw", "--key", "sk-gw-123456789",
		"--endpoint", "https://gateway.example/v1", "--models", "kimi-k2,glm-4.6,deepseek-v3"}); err != nil {
		t.Fatalf("add: %v", err)
	}
	if err := run([]string{"edit", "claude", "gw", "--models", "glm-4.6", "--model", "glm-4.6"}); err != nil {
		t.Fatalf("edit: %v", err)
	}

	if got := strings.Join(claudePickerIDs(t, home), ","); got != "glm-4.6" {
		t.Errorf("modelPicker ids = %q, want the curated-down list", got)
	}
}
