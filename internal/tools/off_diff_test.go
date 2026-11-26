package tools

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	editpkg "charon/internal/edit"
)

// Off is the only destructive thing charon does to a config a user wrote: it
// takes charon's entries out and puts back what was there before charon first
// touched the file. The promise `charon off` makes is that the file comes back
// byte for byte — a comment, an indent, a key's place in its object — because
// the alternative is charon quietly rewriting the user's own config on the way
// out.
//
// The tests here generate that promise instead of spelling it out per-fixture:
// each tool gets a family of pre-charon documents (a user-only file, charon's
// keys already present with the user's values, those keys at the beginning, in
// the middle and at the end of the object) written in each layout a hand-edited
// config really has (2 or 4 spaces, tabs, compact, CRLF, a byte order mark, no
// trailing newline). Capture the stash, apply a binding, take charon back out,
// and the bytes must match.
//
// Two classes of entry are excluded on purpose, and a fixture that pre-existed
// one would fail for the right reason rather than a bug:
//
//   - Reserved namespaces are charon's, not the user's: TOML tables and
//     table-name prefixes (Codex's [model_providers.charon], Grok's
//     [model.charon-*], Pi's providers.charon-*) and JSON key prefixes (Pi's
//     providers.charon-<host>) are deleted by off, never restored, and
//     CaptureStash does not record them. Pre-exist only the restorable entries:
//     OwnedFile.JSON, TOMLTop and TOMLKeys.
//   - A file holding nothing but blank lines comes back empty rather than with
//     its exact count of newlines: charon's entries were the only content, and
//     the line surgery that removes them has no content to place a blank line
//     against. A truly empty file, and a file with real content, both restore
//     exactly.
//   - Claude Code's customApiKeyResponses, written when an official-endpoint
//     binding approves its key, stays behind by design (it is harmless without
//     the key), so the official endpoint has no byte-exact case here. The
//     gateway path below writes no such entry.

// layout is a way a config file can be written on disk.
type layout struct {
	name    string
	indent  string // replaces each leading "  " pair
	crlf    bool
	bom     bool
	nonl    bool // no trailing newline
	compact bool // JSON only: the whole document on one line
}

func (l layout) apply(body string) string {
	if body == "" {
		return "" // a file with nothing in it, in any layout
	}
	out := body
	if l.compact {
		var b strings.Builder
		for _, line := range strings.Split(out, "\n") {
			b.WriteString(strings.TrimSpace(line))
		}
		out = b.String()
	} else if l.indent != "" && l.indent != "  " {
		lines := strings.Split(out, "\n")
		for i, line := range lines {
			depth := 0
			for strings.HasPrefix(line, "  ") {
				depth++
				line = line[2:]
			}
			lines[i] = strings.Repeat(l.indent, depth) + line
		}
		out = strings.Join(lines, "\n")
	}
	out = strings.TrimSuffix(out, "\n")
	if !l.nonl {
		out += "\n"
	}
	if l.crlf {
		out = strings.ReplaceAll(out, "\n", "\r\n")
	}
	if l.bom {
		out = "\ufeff" + out
	}
	return out
}

// jsonLayouts covers the shapes a JSON/JSONC config is saved in.
var jsonLayouts = []layout{
	{name: "2sp"},
	{name: "4sp", indent: "    "},
	{name: "tab", indent: "\t"},
	{name: "crlf", crlf: true},
	{name: "bom", bom: true},
	{name: "compact", compact: true},
	{name: "compact-nonl", compact: true, nonl: true},
	{name: "no-final-newline", nonl: true},
	{name: "crlf-bom-4sp", crlf: true, bom: true, indent: "    "},
}

// tomlLayouts covers a TOML config; it has no compact form, but an indent
// change is real for the multi-line arrays a fixture carries.
var tomlLayouts = []layout{
	{name: "plain"},
	{name: "tab", indent: "\t"},
	{name: "crlf", crlf: true},
	{name: "bom", bom: true},
	{name: "nonl", nonl: true},
	{name: "crlf-bom-tab", crlf: true, bom: true, indent: "\t"},
}

// offFixture is one pre-charon document set to round-trip.
type offFixture struct {
	name  string
	tool  string
	files map[string]string // path relative to the sandboxed home
}

// expand multiplies one document set by every layout it can be written in. A
// compact layout is skipped for a document with a line comment: squeezing the
// lines together would comment out the rest of the file.
func expand(tool, name string, docs map[string]string, layouts []layout) []offFixture {
	var out []offFixture
	for _, l := range layouts {
		skip := false
		files := map[string]string{}
		for rel, body := range docs {
			if l.compact && strings.Contains(body, "//") {
				skip = true
				break
			}
			files[rel] = l.apply(body)
		}
		if skip {
			continue
		}
		out = append(out, offFixture{name: name + "/" + l.name, tool: tool, files: files})
	}
	return out
}

// offSpec is the binding each tool is applied with: a gateway endpoint (which
// is what makes charon write a full set of entries), a key, and two models so
// the picker lists are written too.
func offSpec(tool string) AuthSpec {
	spec := AuthSpec{
		Endpoint: "https://gw.example.com/v1",
		Key:      "sk-charon",
		Model:    "m-1",
		Models: []ModelSpec{
			{Slug: "m-1", ContextWindow: 128000},
			{Slug: "m-2", ContextWindow: 64000},
		},
	}
	if tool == "claude" {
		spec.Endpoint = "https://gw.example.com"
	}
	if tool == "pi" {
		spec.Headers = map[string]string{"Originator": "custom-client", "X-Key": "!literal-$KEY"}
	}
	return spec
}

// offRoundTrip applies a binding to the fixture's files and takes charon back
// out, then checks that the tool's config is byte-identical to what it held
// before: a file that was there is unchanged, and a file that was not is gone
// again.
func offRoundTrip(t *testing.T, toolName string, files map[string]string) {
	t.Helper()
	home := sandboxHome(t)
	for rel, body := range files {
		writeFile(t, filepath.Join(home, rel), body)
	}
	tool := Find(toolName)
	if tool == nil {
		t.Fatalf("unknown tool %q", toolName)
		return
	}
	// What the tool's files held before charon ran, nil meaning "no file".
	before := map[string][]byte{}
	for _, f := range tool.Files {
		data, err := os.ReadFile(f)
		if err == nil {
			before[f] = data
		}
	}
	if len(before) != len(files) {
		t.Fatalf("fixture wrote %d files, the tool knows %d", len(files), len(before))
	}

	stash := tool.CaptureStash()
	if err := tool.ApplyAuth(offSpec(toolName)); err != nil {
		t.Fatalf("apply: %v", err)
	}
	// The round trip is only meaningful if charon really wrote something.
	changed := false
	for f, want := range before {
		got, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("%s after apply: %v", f, err)
		}
		if !bytes.Equal(got, want) {
			changed = true
		}
	}
	for _, f := range tool.Files {
		if _, existed := before[f]; !existed {
			if _, err := os.Stat(f); err == nil {
				changed = true
			}
		}
	}
	if !changed {
		t.Fatal("apply changed nothing; the fixture does not exercise charon")
	}

	if err := tool.Off(stash); err != nil {
		t.Fatalf("off: %v", err)
	}
	for f, want := range before {
		got, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("%s after off: %v", f, err)
		}
		if !bytes.Equal(got, want) {
			t.Errorf("%s not restored byte-exact\n--- want ---\n%q\n--- got ---\n%q", f, want, got)
		}
	}
	for _, f := range tool.Files {
		if _, existed := before[f]; existed {
			continue
		}
		if _, err := os.Stat(f); !os.IsNotExist(err) {
			t.Errorf("file charon created survived off: %s", f)
		}
	}
	for _, created := range tool.Created {
		if _, err := os.Stat(created); !os.IsNotExist(err) {
			t.Errorf("file charon created survived off: %s", created)
		}
	}
}

// ---- Codex ----

func codexFixtures() []offFixture {
	config := ".codex/config.toml"
	var out []offFixture
	add := func(name, body string) {
		out = append(out, expand("codex", name, map[string]string{config: body}, tomlLayouts)...)
	}
	add("user only", `# my codex config
approval_policy = "on-request"

[model_providers.mine]
name = "mine"
base_url = "https://mine.example/v1"

[profiles.fast]
model = "gpt-5-mini"
`)
	// A user who pointed codex at a gateway by hand: every owned top key is
	// already there, and off has to put all of them back.
	add("owned keys present", `model = "gpt-5.6"   # daily driver
model_provider = "mine"
model_reasoning_effort = "high"

[model_providers.mine]
name = "mine"
base_url = "https://mine.example/v1"
`)
	// The owned keys interleaved with the user's own, so a restore has to land
	// each one between the right neighbours rather than appending them.
	add("owned keys interleaved", `approval_policy = "on-request"
model = "gpt-5.6"
sandbox_mode = "workspace-write"
model_provider = "mine"
disable_response_storage = true
model_reasoning_effort = "low"
model_context_window = 111111

[model_providers.mine]
name = "mine"
`)
	// Only the last owned key of the set is present: a fixture where the
	// pre-charon value is not "first member of the object".
	add("one owned key last", `approval_policy = "on-request"
sandbox_mode = "workspace-write"
model_reasoning_effort = "low"
`)
	add("empty file", "")
	add("only a comment", "# nothing configured yet\n")
	return out
}

// ---- Claude ----

func claudeFixtures() []offFixture {
	settings := ".claude/settings.json"
	var out []offFixture
	add := func(name, body string) {
		out = append(out, expand("claude", name, map[string]string{settings: body}, jsonLayouts)...)
	}
	add("user only", `{
  "theme": "dark",
  "permissions": {
    "allow": ["Bash(ls:*)"]
  }
}
`)
	// A user with hand-written gateway settings: charon's keys exist with the
	// user's values and must come back exactly.
	add("gateway set by hand", `{
  "theme": "dark",
  "env": {
    "ANTHROPIC_BASE_URL": "https://user-gw.example.com",
    "ANTHROPIC_AUTH_TOKEN": "sk-user-own",
    "KEEP_ME": "1"
  },
  "model": "claude-sonnet-4-5",
  "permissions": {
    "allow": ["Bash(ls:*)"]
  }
}
`)
	// Owned keys at the start of the object, at the end, and around a user key,
	// so the restore order is exercised in each position.
	add("owned keys around", `{
  "model": "claude-opus-4-1",
  "env": {
    "ANTHROPIC_API_KEY": "sk-user-own",
    "KEEP_ME": "1",
    "ANTHROPIC_BASE_URL": "https://user-gw.example.com"
  },
  "modelPicker": {
    "mine": {
      "name": "mine"
    }
  },
  "theme": "dark"
}
`)
	add("jsonc comments", `{
  // hand written, keep the comments
  "theme": "dark",
  "env": {
    // the gateway I use at home
    "ANTHROPIC_BASE_URL": "https://user-gw.example.com",
    "KEEP_ME": "1"
  }
}
`)
	// Nested siblings where an alphabetically-earlier key follows an
	// alphabetically-later owned sibling, before a user key.
	add("nested reverse alpha order", `{
  "env": {
    "ANTHROPIC_BASE_URL": "https://user-gw.example.com",
    "ANTHROPIC_AUTH_TOKEN": "sk-user-own",
    "KEEP_ME": "1"
  },
  "theme": "dark"
}
`)
	add("empty object", "{}\n")
	return out
}

// ---- OpenCode ----

func opencodeFixtures() []offFixture {
	config := ".config/opencode/opencode.jsonc"
	var out []offFixture
	add := func(name, body string) {
		out = append(out, expand("opencode", name, map[string]string{config: body}, jsonLayouts)...)
	}
	add("user only", `{
  // my config
  "theme": "dark",
  "provider": {
    "mine": {
      "options": {
        "baseURL": "https://mine.example.com"
      }
    }
  }
}
`)
	add("charon provider set by hand", `{
  "$schema": "https://opencode.ai/config.json",
  "provider": {
    "charon": {
      "npm": "@ai-sdk/openai-compatible",
      "options": {
        "baseURL": "https://user-gw.example.com/v1",
        "apiKey": "sk-user-own"
      },
      "models": {
        "user-model": {}
      }
    },
    "mine": {
      "options": {
        "baseURL": "https://mine.example.com"
      }
    }
  },
  "model": "charon/user-model",
  "theme": "dark"
}
`)
	add("model last", `{
  "theme": "dark",
  "model": "mine/own"
}
`)
	return out
}

// ---- Pi ----

func piFixtures() []offFixture {
	models := ".pi/agent/models.json"
	settings := ".pi/agent/settings.json"
	var out []offFixture
	add := func(name, modelsBody, settingsBody string) {
		out = append(out, expand("pi", name, map[string]string{
			models:   modelsBody,
			settings: settingsBody,
		}, jsonLayouts)...)
	}
	add("user only", `{
  "providers": {
    "mine": {
      "baseUrl": "https://mine.example.com/v1",
      "api": "openai-completions"
    }
  }
}
`, `{
  "defaultProvider": "mine",
  "defaultModel": "own-1",
  "theme": "dark"
}
`)
	// providers.charon is a restorable key; the aggregated charon-* entries are
	// charon's namespace and must not be pre-existed.
	add("charon provider set by hand", `{
  "providers": {
    "charon": {
      "baseUrl": "https://user-gw.example.com/v1",
      "api": "openai-completions",
      "apiKey": "sk-user-own",
      "models": [
        {
          "id": "user-model"
        }
      ]
    },
    "mine": {
      "baseUrl": "https://mine.example.com/v1"
    }
  }
}
`, `{
  "defaultProvider": "charon",
  "defaultModel": "user-model"
}
`)
	return out
}

// ---- Grok ----

func grokFixtures() []offFixture {
	config := ".grok/config.toml"
	var out []offFixture
	add := func(name, body string) {
		out = append(out, expand("grok", name, map[string]string{config: body}, tomlLayouts)...)
	}
	add("user only", `# grok config
[ui]
yolo = true

[models]
default = "mine"
web_search = "mine"

[model.mine]
name = "mine"
base_url = "https://mine.example.com/v1"
`)
	// models.default is restorable; a pre-existing [model.charon-*] table is
	// charon's namespace and is exercised by the deletion cases instead.
	add("default set by hand", `[models]
default = "charon-hand-written"

[model.hand]
name = "hand"
`)
	add("tables around models", `[ui]
yolo = false

[models]
default = "mine"

[model.mine]
name = "mine"

[extra]
keep = "yes"
`)
	add("empty file", "")
	return out
}

// TestOffRemovesWhatCharonCreated is the other half of the same promise: a tool
// the user never configured has no config file at all, and off has to leave it
// that way rather than behind an empty object or a file with only a table in it.
func TestOffRemovesWhatCharonCreated(t *testing.T) {
	for _, tool := range All() {
		t.Run(tool.Name, func(t *testing.T) {
			offRoundTrip(t, tool.Name, nil)
		})
	}
}

func TestOffRestoresGeneratedConfigs(t *testing.T) {
	var cases []offFixture
	cases = append(cases, codexFixtures()...)
	cases = append(cases, claudeFixtures()...)
	cases = append(cases, opencodeFixtures()...)
	cases = append(cases, piFixtures()...)
	cases = append(cases, grokFixtures()...)
	if len(cases) < 80 {
		t.Fatalf("only %d cases generated; the layout matrix collapsed", len(cases))
	}
	for _, c := range cases {
		t.Run(c.tool+"/"+c.name, func(t *testing.T) {
			offRoundTrip(t, c.tool, c.files)
		})
	}
}

// TestOffKeepsWhatTheUserAddedWhileActive: off removes what charon wrote, not
// what the user wrote next to it. A config key added inside an object charon
// created is the case that makes this delicate — the object is charon's, the key
// in it is not.
// editSet writes a key the way another tool would: through the editors charon
// itself uses, so the file stays as charon left it apart from this key.
func editSet(path, key string, value any) error {
	return editpkg.SetJSON(path, editpkg.KV{Path: key, Value: value})
}

func TestOffKeepsWhatTheUserAddedWhileActive(t *testing.T) {
	home := sandboxHome(t)
	settings := filepath.Join(home, ".claude", "settings.json")
	writeFile(t, settings, "{\n  \"theme\": \"dark\"\n}\n")

	tool := Find("claude")
	stash := tool.CaptureStash()
	if err := tool.ApplyAuth(offSpec("claude")); err != nil {
		t.Fatal(err)
	}
	// The user edits the file charon is managing: env is charon's, MINE is not.
	if err := editSet(settings, "env.MINE", "keep-me"); err != nil {
		t.Fatal(err)
	}
	if err := tool.Off(stash); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(settings)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(got, []byte(`"MINE": "keep-me"`)) {
		t.Errorf("off deleted a key the user added:\n%s", got)
	}
	for _, gone := range []string{"ANTHROPIC_BASE_URL", "ANTHROPIC_AUTH_TOKEN"} {
		if bytes.Contains(got, []byte(gone)) {
			t.Errorf("off left charon's %s behind:\n%s", gone, got)
		}
	}
}

// TestOffRemovesTheObjectItCreated: charon writes env.* into a settings file
// that has no env object, so it creates one. Taking charon out must not leave
// that object behind as an empty {} — a husk the user never wrote, and one more
// difference between the file before the first switch and after off.
func TestOffRemovesTheObjectItCreated(t *testing.T) {
	for _, c := range []struct {
		tool, path, content, left string
	}{
		{"claude", ".claude/settings.json", "{\n  \"theme\": \"dark\"\n}\n", "\"env\""},
		{"opencode", ".config/opencode/opencode.jsonc", "{\n  \"theme\": \"dark\"\n}\n", "\"provider\""},
		{"pi", ".pi/agent/models.json", "{\n  \"theme\": \"dark\"\n}\n", "\"providers\""},
	} {
		t.Run(c.tool, func(t *testing.T) {
			home := sandboxHome(t)
			path := filepath.Join(home, c.path)
			writeFile(t, path, c.content)
			tool := Find(c.tool)
			stash := tool.CaptureStash()
			if err := tool.ApplyAuth(offSpec(c.tool)); err != nil {
				t.Fatal(err)
			}
			if got, _ := os.ReadFile(path); !bytes.Contains(got, []byte(c.left)) {
				t.Fatalf("charon did not create %s to begin with:\n%s", c.left, got)
			}
			if err := tool.Off(stash); err != nil {
				t.Fatal(err)
			}
			got, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if bytes.Contains(got, []byte(c.left+" : {}")) || bytes.Contains(got, []byte(c.left+": {}")) {
				t.Errorf("off left an empty %s behind:\n%s", c.left, got)
			}
			if string(got) != c.content {
				t.Errorf("not restored byte-exact\n--- want ---\n%q\n--- got ---\n%q", c.content, got)
			}
		})
	}
}
