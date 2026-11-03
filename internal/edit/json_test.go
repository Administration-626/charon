package edit

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestSetJSONPreservesCommentsOrderAndIndent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	orig := "{\n  // which model\n  \"model\": \"a\",\n  \"env\": {\n    \"KEEP\": \"me\"\n  },\n  \"zzz\": 1\n}\n"
	write(t, path, orig)
	if err := SetJSON(path,
		KV{Path: "env.ANTHROPIC_BASE_URL", Value: "https://gw"},
		KV{Path: "model", Value: "b"},
	); err != nil {
		t.Fatal(err)
	}
	out := read(t, path)
	if !strings.Contains(out, "// which model") {
		t.Errorf("comment lost:\n%s", out)
	}
	if !strings.Contains(out, "\"KEEP\": \"me\"") {
		t.Errorf("sibling key lost:\n%s", out)
	}
	if !strings.Contains(out, "\"model\": \"b\"") || strings.Contains(out, "\"model\": \"a\"") {
		t.Errorf("model not replaced in place:\n%s", out)
	}
	if strings.Index(out, "\"model\"") > strings.Index(out, "\"env\"") {
		t.Errorf("key order not preserved:\n%s", out)
	}
	if !strings.Contains(out, "\"ANTHROPIC_BASE_URL\": \"https://gw\"") {
		t.Errorf("nested key not added:\n%s", out)
	}
}

func TestSetJSONCreatesMissingFileAndParents(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "dir", "settings.json")
	if err := SetJSON(path, KV{Path: "a.b", Value: "c"}); err != nil {
		t.Fatal(err)
	}
	out := read(t, path)
	if !strings.Contains(out, "\"a\"") || !strings.Contains(out, "\"b\"") {
		t.Errorf("nested keys not written:\n%s", out)
	}
}

func TestApplyJSONDeletesThenSets(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.json")
	write(t, path, "{\n  \"env\": {\n    \"ANTHROPIC_API_KEY\": \"old\",\n    \"KEEP\": 1\n  }\n}\n")
	if err := ApplyJSON(path,
		[]KV{{Path: "env.ANTHROPIC_API_KEY", Value: "new"}},
		[]string{"env.ANTHROPIC_API_KEY"},
	); err != nil {
		t.Fatal(err)
	}
	out := read(t, path)
	if !strings.Contains(out, "\"ANTHROPIC_API_KEY\": \"new\"") {
		t.Errorf("delete should not win over set:\n%s", out)
	}
	if !strings.Contains(out, "\"KEEP\"") {
		t.Errorf("sibling lost:\n%s", out)
	}
}

func TestDelJSONCollapsesEmptyParent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.json")
	write(t, path, "{\"a\": {\"b\": 1}, \"c\": 2}\n")
	if err := DelJSON(path, "a.b"); err != nil {
		t.Fatal(err)
	}
	out := read(t, path)
	if !strings.Contains(out, "\"a\": {}") {
		t.Errorf("empty parent not collapsed:\n%s", out)
	}
	if !strings.Contains(out, "\"c\": 2") {
		t.Errorf("sibling lost:\n%s", out)
	}
}

func TestJSONKeepsCRLFAndBOM(t *testing.T) {
	dir := t.TempDir()
	crlf := filepath.Join(dir, "crlf.json")
	write(t, crlf, "{\r\n  \"a\": 1\r\n}\r\n")
	if err := SetJSON(crlf, KV{Path: "b", Value: 2}); err != nil {
		t.Fatal(err)
	}
	out := read(t, crlf)
	if !strings.Contains(out, "\"b\": 2,") {
		t.Errorf("added line missing:\n%q", out)
	}
	if strings.Contains(strings.ReplaceAll(out, "\r\n", "\n"), "\"b\": 2\n") {
		t.Errorf("file ended up with mixed endings:\n%q", out)
	}
	bomPath := filepath.Join(dir, "bom.json")
	if err := os.WriteFile(bomPath, append([]byte("\xef\xbb\xbf"), []byte("{\"a\":1}\n")...), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := SetJSON(bomPath, KV{Path: "b", Value: 2}); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(bomPath)
	if len(raw) < 4 || string(raw[:3]) != "\xef\xbb\xbf" {
		t.Errorf("BOM lost: %q", raw)
	}
}

func TestSetJSONRefusesInvalidFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "broken.json")
	write(t, path, "{\"a\": }\n")
	if err := SetJSON(path, KV{Path: "b", Value: 1}); err == nil {
		t.Fatal("invalid JSON must be refused")
	}
	if read(t, path) != "{\"a\": }\n" {
		t.Errorf("file must be untouched, got %q", read(t, path))
	}
}

func TestGetJSONRawDollarKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "opencode.jsonc")
	write(t, path, "{\n  // config\n  \"$schema\": \"https://opencode.ai/config.json\"\n}\n")
	raw, ok := GetJSONRaw(path, "$schema")
	if !ok || raw != `"https://opencode.ai/config.json"` {
		t.Fatalf("GetJSONRaw($schema) = %q, %v", raw, ok)
	}
	if _, ok := GetJSONRaw(path, "nope"); ok {
		t.Fatal("absent key must report false")
	}
}

func TestGetJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	write(t, path, "{\n  // comment\n  \"model\": \"gpt-5\",\n  \"n\": 2,\n  \"env\": {\n    \"KEEP\": \"me\"\n  }\n}\n")
	cases := []struct {
		key  string
		want string
		ok   bool
	}{
		{"model", "gpt-5", true},
		{"n", "2", true},
		{"env.KEEP", "me", true},
		{"env.NOPE", "", false},
		{"nope", "", false},
	}
	for _, c := range cases {
		got, ok := GetJSON(path, c.key)
		if ok != c.ok {
			t.Errorf("GetJSON(%q) ok = %v, want %v", c.key, ok, c.ok)
			continue
		}
		if got != c.want {
			t.Errorf("GetJSON(%q) = %q, want %q", c.key, got, c.want)
		}
	}
	if _, ok := GetJSON(filepath.Join(dir, "missing.json"), "model"); ok {
		t.Error("missing file must report false")
	}
}

func TestPrevJSONKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	write(t, path, "{\n  \"a\": 1,\n  \"b\": {\n    \"x\": 2,\n    \"y\": 3\n  },\n  \"c\": 4\n}\n")
	cases := map[string]string{
		"a":   "",
		"b":   "a",
		"c":   "b",
		"b.x": "",
		"b.y": "x",
		"zzz": "",
	}
	for key, want := range cases {
		if got := PrevJSONKey(path, key); got != want {
			t.Errorf("PrevJSONKey(%q) = %q, want %q", key, got, want)
		}
	}
	if got := PrevJSONKey(filepath.Join(t.TempDir(), "missing.json"), "a"); got != "" {
		t.Errorf("PrevJSONKey on missing file = %q, want empty", got)
	}
}

func TestSetJSONAfterKeepsPosition(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	write(t, path, "{\n  \"model\": \"a\",\n  \"env\": {\n    \"KEEP\": \"me\"\n  }\n}\n")
	if err := SetJSONAfter(path, "model", "provider", "openai"); err != nil {
		t.Fatal(err)
	}
	out := read(t, path)
	if !strings.Contains(out, "\"model\": \"a\",\n  \"provider\": \"openai\",\n  \"env\"") {
		t.Errorf("new key not placed after its anchor:\n%s", out)
	}
	// The anchor is the last member: the new key follows its value, before the
	// closing brace keeps its own line.
	if err := SetJSONAfter(path, "env", "zzz", 1); err != nil {
		t.Fatal(err)
	}
	out = read(t, path)
	if !strings.Contains(out, "\"KEEP\": \"me\"\n  },\n  \"zzz\": 1\n}") {
		t.Errorf("key after last member not placed before the brace:\n%s", out)
	}
	// An existing key is replaced in place; a missing anchor inserts first.
	if err := SetJSONAfter(path, "model", "provider", "anthropic"); err != nil {
		t.Fatal(err)
	}
	if out = read(t, path); !strings.Contains(out, "\"provider\": \"anthropic\"") || strings.Contains(out, "\"provider\": \"openai\"") {
		t.Errorf("existing key not replaced in place:\n%s", out)
	}
	if err := SetJSONAfter(path, "nope", "first", true); err != nil {
		t.Fatal(err)
	}
	out = read(t, path)
	if !strings.HasPrefix(out, "{\n  \"first\": true,") {
		t.Errorf("missing anchor must insert as first member:\n%s", out)
	}
}

func TestSetJSONAfterCompact(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "s.json")
	write(t, path, `{"a":1,"b":2}`) // no trailing newline: a truly compact file
	if err := SetJSONAfter(path, "a", "c", 3); err != nil {
		t.Fatal(err)
	}
	if out := read(t, path); out != `{"a":1,"c":3,"b":2}` {
		t.Errorf("insert after anchor = %q", out)
	}
	if err := SetJSONAfter(path, "b", "d", 4); err != nil {
		t.Fatal(err)
	}
	if out := read(t, path); out != `{"a":1,"c":3,"b":2,"d":4}` {
		t.Errorf("insert after last member = %q", out)
	}
}

func TestSetJSONAfterRefusesNonObject(t *testing.T) {
	path := filepath.Join(t.TempDir(), "array.json")
	write(t, path, "[1,2]\n")
	if err := SetJSONAfter(path, "", "a", 1); err == nil {
		t.Fatal("a top-level array must be refused")
	}
	if read(t, path) != "[1,2]\n" {
		t.Error("refused edit must leave the file untouched")
	}
}

func TestJSONEditErrors(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.json")
	write(t, path, "{\"a\": [1, 2]}\n")
	// An array element is addressed by a non-quote token: deleting it cannot
	// locate a key to remove.
	if err := DelJSON(path, "a.0"); err == nil {
		t.Error("deleting an array element must fail, not guess")
	}
	// The empty path names the document itself (index 0): nothing to delete.
	before := read(t, path)
	if err := DelJSON(path, ""); err != nil {
		t.Errorf("empty path = %v, want no-op", err)
	}
	if read(t, path) != before {
		t.Error("empty path must not rewrite the file")
	}
	if err := DelJSON(filepath.Join(t.TempDir(), "missing.json"), "a"); err != nil {
		t.Errorf("missing file = %v, want nil", err)
	}

	broken := filepath.Join(t.TempDir(), "broken.json")
	write(t, broken, "{\"a\": }\n")
	if err := DelJSON(broken, "a"); err == nil {
		t.Error("DelJSON must refuse invalid JSON")
	}
	if err := ApplyJSON(broken, nil, []string{"a"}); err == nil {
		t.Error("ApplyJSON must refuse invalid JSON")
	}
	if err := SetJSONAfter(broken, "", "a", 1); err == nil {
		t.Error("SetJSONAfter must refuse invalid JSON")
	}
}

func TestJSONReadErrors(t *testing.T) {
	path := filepath.Join(t.TempDir(), "as-dir.json")
	if err := os.MkdirAll(path, 0o700); err != nil {
		t.Fatal(err)
	}
	for name, edit := range map[string]func() error{
		"SetJSON":      func() error { return SetJSON(path, KV{Path: "a", Value: 1}) },
		"ApplyJSON":    func() error { return ApplyJSON(path, []KV{{Path: "a", Value: 1}}, nil) },
		"DelJSON":      func() error { return DelJSON(path, "a") },
		"SetJSONAfter": func() error { return SetJSONAfter(path, "", "a", 1) },
	} {
		if err := edit(); err == nil {
			t.Errorf("%s on an unreadable path must fail", name)
		}
	}
	if _, ok := GetJSON(path, "a"); ok {
		t.Error("GetJSON on an unreadable path must report false")
	}
	if _, ok := GetJSONRaw(path, "a"); ok {
		t.Error("GetJSONRaw on an unreadable path must report false")
	}
}

func TestJSONCompactAndTabs(t *testing.T) {
	dir := t.TempDir()
	compact := filepath.Join(dir, "compact.json")
	write(t, compact, `{"a":1}`) // no newline: the compact insertion path
	if err := SetJSON(compact, KV{Path: "b", Value: 2}); err != nil {
		t.Fatal(err)
	}
	if out := read(t, compact); out != `{"b":2,"a":1}` {
		t.Errorf("compact insert = %q", out)
	}
	if err := SetJSON(compact, KV{Path: "a", Value: 3}); err != nil {
		t.Fatal(err)
	}
	if out := read(t, compact); out != `{"b":2,"a":3}` {
		t.Errorf("compact replace = %q", out)
	}

	tabs := filepath.Join(dir, "tabs.json")
	write(t, tabs, "{\n\t\"a\": 1\n}\n")
	if err := SetJSON(tabs, KV{Path: "b", Value: 2}); err != nil {
		t.Fatal(err)
	}
	if out := read(t, tabs); out != "{\n\t\"b\": 2,\n\t\"a\": 1\n}\n" {
		t.Errorf("tab indentation not followed:\n%q", out)
	}
}

func TestSetJSONEscapedDotAndBraceInString(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vscode.json")
	write(t, path, `{"chat.defaultModel":1,"c":2}`)
	// The key is one part: VS Code keeps a dot inside a settings key.
	if err := SetJSON(path, KV{Path: `chat\.defaultModel`, Value: 3}); err != nil {
		t.Fatal(err)
	}
	if out := read(t, path); out != `{"chat.defaultModel":3,"c":2}` {
		t.Errorf("escaped-dot key = %q", out)
	}

	// A '}' and an escaped quote inside a string must not end the object.
	tricky := filepath.Join(dir, "tricky.json")
	write(t, tricky, `{"k":"a}\"b"}`)
	if err := SetJSON(tricky, KV{Path: "z", Value: 1}); err != nil {
		t.Fatal(err)
	}
	if out := read(t, tricky); out != `{"z":1,"k":"a}\"b"}` {
		t.Errorf("string-aware brace matching = %q", out)
	}
}

func TestPatchJSONKeepsBOMAndHandlesEmpty(t *testing.T) {
	raw := append(append([]byte(nil), bom...), []byte("{\"a\":1}\n")...)
	out, err := PatchJSON(raw, nil, []KV{{Path: "b", Value: 2}})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(out, bom) {
		t.Errorf("BOM not kept: %q", out)
	}
	var got map[string]int
	if err := json.Unmarshal(TrimBOM(out), &got); err != nil {
		t.Fatalf("patched output is not valid JSON: %v\n%s", err, out)
	}
	if got["b"] != 2 || got["a"] != 1 {
		t.Errorf("patched map = %v", got)
	}
	empty, err := PatchJSON(nil, nil, []KV{{Path: "a", Value: 1}})
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(empty, &got); err != nil || got["a"] != 1 {
		t.Errorf("empty input = %q, %v", empty, err)
	}
}

func TestPrevJSONKeyNonObjectParent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.json")
	write(t, path, "{\"a\": 1}\n")
	if got := PrevJSONKey(path, "a.x"); got != "" {
		t.Errorf("PrevJSONKey under a scalar = %q, want empty", got)
	}
}

func TestDelJSONPrettyCRLFAndCollapse(t *testing.T) {
	// A member alone on its line takes the whole line with it; the empty
	// parent collapses.
	path := filepath.Join(t.TempDir(), "s.json")
	write(t, path, "{\n  \"a\": 1\n}\n")
	if err := DelJSON(path, "a"); err != nil {
		t.Fatal(err)
	}
	if out := read(t, path); out != "{}\n" {
		t.Errorf("deleting the only member = %q, want %q", out, "{}\n")
	}

	// The same, with no line end after the value.
	noNL := filepath.Join(t.TempDir(), "no-nl.json")
	write(t, noNL, "{\n  \"a\": 1}")
	if err := DelJSON(noNL, "a"); err != nil {
		t.Fatal(err)
	}
	if out := read(t, noNL); out != "{}" {
		t.Errorf("deleting the last line of a file without a newline = %q", out)
	}

	// The last of two members: drop its line, then the comma before it, with
	// CRLF endings preserved.
	crlf := filepath.Join(t.TempDir(), "crlf.json")
	write(t, crlf, "{\r\n  \"a\": 1,\r\n  \"b\": 2\r\n}\r\n")
	if err := DelJSON(crlf, "b"); err != nil {
		t.Fatal(err)
	}
	out := read(t, crlf)
	if strings.Contains(out, "\"b\"") || strings.Contains(out, "\"a\": 1,\r\n") {
		t.Errorf("comma or member left behind:\n%q", out)
	}
	if strings.Contains(strings.ReplaceAll(out, "\r\n", ""), "\n") {
		t.Errorf("CRLF not kept:\n%q", out)
	}

	// Removing the last key of a nested object collapses that object too.
	nested := filepath.Join(t.TempDir(), "nested.json")
	write(t, nested, "{\n  \"a\": {\n    \"b\": 1\n  },\n  \"c\": 2\n}\n")
	if err := DelJSON(nested, "a.b"); err != nil {
		t.Fatal(err)
	}
	out = read(t, nested)
	if !strings.Contains(out, "\"a\": {}") || !strings.Contains(out, "\"c\": 2") {
		t.Errorf("nested object not collapsed while keeping siblings:\n%s", out)
	}
}

func TestSetJSONAfterCreatesMissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "new.json")
	if err := SetJSONAfter(path, "", "a", 1); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(read(t, path), "\"a\": 1") {
		t.Errorf("missing file not created:\n%s", read(t, path))
	}
}

func TestPatchJSONRefusesNonObject(t *testing.T) {
	if _, err := PatchJSON([]byte("[1]\n"), nil, []KV{{Path: "a", Value: 1}}); err == nil {
		t.Error("a top-level array must be refused")
	}
}
