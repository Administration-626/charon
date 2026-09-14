package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"charon/internal/profile"
)

// sandbox points HOME and the store's XDG_CONFIG_HOME at temp dirs so run()
// never touches real user config (see AGENTS.md).
func sandbox(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USER", "tester")
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
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

func TestRunRejectsUnknownCommandAndTool(t *testing.T) {
	sandbox(t)
	if err := run([]string{"bogus"}); err == nil || !strings.Contains(err.Error(), "unknown command") {
		t.Errorf("run(bogus) = %v, want unknown-command error", err)
	}
	for _, args := range [][]string{
		{"rm", "faketool", "x"},
		{"switch", "faketool", "x"},
		{"undo", "faketool"},
		{"ls", "faketool"},
		{"cp", "faketool", "a", "b"},
	} {
		if err := run(args); err == nil || !strings.Contains(err.Error(), "unknown tool") {
			t.Errorf("run(%v) = %v, want unknown-tool error", args, err)
		}
	}
}

func TestRunProfileLifecycle(t *testing.T) {
	home := sandbox(t)
	seedCodex(t, home)

	if err := run([]string{"add", "codex", "--name", "work", "--key", "sk-test", "--endpoint", "https://example.com/v1"}); err != nil {
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
	if err := run([]string{"restore", "codex"}); err != nil {
		t.Fatalf("restore: %v", err)
	}
	if err := run([]string{"undo", "codex"}); err != nil {
		t.Fatalf("undo: %v", err)
	}
	if err := run([]string{"rm", "codex", "work-2"}); err != nil {
		t.Fatalf("rm: %v", err)
	}

	store, err := profile.Open()
	if err != nil {
		t.Fatal(err)
	}
	if !store.Exists("codex", "work") || !store.Exists("codex", profile.DefaultName) {
		t.Errorf("expected work + default profiles, got %v", store.List("codex"))
	}
	if store.Exists("codex", "work-2") {
		t.Error("work-2 still exists after rm")
	}
}

func TestRunRejectsUnsafeProfileArguments(t *testing.T) {
	home := sandbox(t)
	seedCodex(t, home)

	for _, args := range [][]string{
		{"rm", "codex", "nope"},                             // nonexistent profile
		{"rm", "codex", "../.."},                            // traversal out of the store
		{"save", "codex", "../evil"},                        // credential snapshot outside the store
		{"add", "codex", "--name", "default", "--key", "k"}, // reserved name
		{"rename", "codex", "default", "x"},                 // default is not renamable
	} {
		if err := run(args); err == nil {
			t.Errorf("run(%v) succeeded, want error", args)
		}
	}
	// The traversal attempts must not have deleted the store or the config dir.
	if _, err := os.Stat(filepath.Join(home, ".config", "charon")); err != nil {
		t.Fatalf("store dir damaged: %v", err)
	}
}

func TestRunRejectsInvalidURLAndKey(t *testing.T) {
	home := sandbox(t)
	seedCodex(t, home)

	for _, args := range [][]string{
		{"add", "codex", "--name", "bad-url", "--key", "sk-test", "--endpoint", "not a url"},
		{"add", "codex", "--name", "bad-scheme", "--key", "sk-test", "--endpoint", "ftp://example.com"},
		{"add", "codex", "--name", "no-key", "--key", "  "},
		{"models", "codex", "--key", "sk-test", "--endpoint", "not a url"},
	} {
		if err := run(args); err == nil {
			t.Errorf("run(%v) succeeded, want validation error", args)
		}
	}

	if err := run([]string{"add", "codex", "--name", "good", "--key", "sk-test", "--endpoint", "https://example.com/v1"}); err != nil {
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
// inactive profile via the CLI only updates its stored spec, leaving the
// active profile (and hence the live config) untouched.
func TestRunEditDoesNotSwitchInactiveProfile(t *testing.T) {
	home := sandbox(t)
	seedCodex(t, home)

	if err := run([]string{"add", "codex", "--name", "work", "--key", "sk-work", "--endpoint", "https://work.example.com/v1"}); err != nil {
		t.Fatalf("add work: %v", err)
	}
	if err := run([]string{"add", "codex", "--name", "other", "--key", "sk-other", "--endpoint", "https://other.example.com/v1"}); err != nil {
		t.Fatalf("add other: %v", err)
	}
	if err := run([]string{"switch", "codex", "work"}); err != nil {
		t.Fatalf("switch work: %v", err)
	}

	if err := run([]string{"edit", "codex", "other", "--model", "gpt-5.5"}); err != nil {
		t.Fatalf("edit other: %v", err)
	}

	store, err := profile.Open()
	if err != nil {
		t.Fatal(err)
	}
	if store.Active("codex") != "work" {
		t.Errorf("active = %q, want work (editing an inactive profile must not switch)", store.Active("codex"))
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

func TestRunRefreshUpdatesActiveProfile(t *testing.T) {
	home := sandbox(t)
	seedCodex(t, home)

	if err := run([]string{"add", "codex", "--name", "work", "--key", "sk-test", "--endpoint", "https://example.com/v1"}); err != nil {
		t.Fatalf("add: %v", err)
	}
	// Refresh without an explicit active profile should work after add.
	if err := run([]string{"refresh", "codex"}); err != nil {
		t.Errorf("refresh: %v", err)
	}
}

func TestRunRefreshErrorsOnUnknownTool(t *testing.T) {
	sandbox(t)
	if err := run([]string{"refresh", "nosuchtool"}); err == nil || !strings.Contains(err.Error(), "unknown tool") {
		t.Errorf("refresh unknown tool: %v, want unknown-tool error", err)
	}
}

func TestRunPruneRequiresTool(t *testing.T) {
	sandbox(t)
	if err := run([]string{"prune"}); err == nil || !strings.Contains(err.Error(), "usage") {
		t.Errorf("prune without args: %v, want usage error", err)
	}
}

func TestRunPruneOnDetectedTool(t *testing.T) {
	home := sandbox(t)
	seedCodex(t, home)

	// Add a profile to create some state, then prune.
	if err := run([]string{"add", "codex", "--name", "work", "--key", "sk-test", "--endpoint", "https://example.com/v1"}); err != nil {
		t.Fatalf("add: %v", err)
	}
	if err := run([]string{"prune", "codex"}); err != nil {
		t.Errorf("prune codex: %v", err)
	}
}

func TestRunPruneWithKeepFlag(t *testing.T) {
	home := sandbox(t)
	seedCodex(t, home)

	if err := run([]string{"add", "codex", "--name", "work", "--key", "sk-test", "--endpoint", "https://example.com/v1"}); err != nil {
		t.Fatalf("add: %v", err)
	}
	if err := run([]string{"prune", "codex", "--keep", "5"}); err != nil {
		t.Errorf("prune codex --keep 5: %v", err)
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

func TestRunProfilesListsProfileNames(t *testing.T) {
	home := sandbox(t)
	seedCodex(t, home)

	if err := run([]string{"add", "codex", "--name", "work", "--key", "sk-test", "--endpoint", "https://example.com/v1"}); err != nil {
		t.Fatalf("add: %v", err)
	}
	// __profiles is a hidden command for shell completion.
	if err := run([]string{"__profiles", "codex"}); err != nil {
		t.Errorf("__profiles codex: %v", err)
	}
}

func TestRunSaveRequiresTool(t *testing.T) {
	sandbox(t)
	if err := run([]string{"save"}); err == nil || !strings.Contains(err.Error(), "usage") {
		t.Errorf("save without args: %v, want usage error", err)
	}
}

func TestRunSaveWithName(t *testing.T) {
	home := sandbox(t)
	seedCodex(t, home)

	if err := run([]string{"save", "codex", "snapshot1"}); err != nil {
		t.Errorf("save codex snapshot1: %v", err)
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

// TestRunAddWithModelsRegistersPickerList covers `--models a,b,c`: the ids reach the
// tool's own model menu (so switching model mid-session needs no charon round trip)
// and are stored on the profile, with the first one taken as the default model.
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
	store, err := profile.Open()
	if err != nil {
		t.Fatal(err)
	}
	sp, ok := store.GetSpec("claude", "gw")
	if !ok {
		t.Fatal("profile has no spec")
	}
	if strings.Join(sp.Models, ",") != "kimi-k2,glm-4.6,deepseek-v3" {
		t.Errorf("spec.Models = %v, want the --models list persisted", sp.Models)
	}
	if sp.Model != "kimi-k2" {
		t.Errorf("spec.Model = %q, want the first --models id promoted to default", sp.Model)
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
	store, err := profile.Open()
	if err != nil {
		t.Fatal(err)
	}
	if sp, _ := store.GetSpec("claude", "gw"); strings.Join(sp.Models, ",") != "kimi-k2,glm-4.6" {
		t.Errorf("spec.Models = %v, want the list preserved", sp.Models)
	}
}

// TestRunEditModelsFlagReplacesPickerList: passing --models explicitly curates the
// menu down, which is how you drop models you no longer want offered.
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
