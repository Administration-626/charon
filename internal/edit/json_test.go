package edit

import (
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
