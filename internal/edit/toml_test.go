package edit

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestSetTOMLKeyPreservesCommentsAndSpacing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	orig := "# my config\nmodel = \"gpt-5\"   # keep me\n\n[model_providers.charon]\nname = \"charon\"\n"
	write(t, path, orig)
	if err := SetTOMLKey(path, "model_providers.charon", "name", "x"); err != nil {
		t.Fatal(err)
	}
	if err := SetTOMLTopPreserving(path, KV{Path: "model", Value: "qwen"}); err != nil {
		t.Fatal(err)
	}
	out := read(t, path)
	if !strings.Contains(out, "# my config") || !strings.Contains(out, "# keep me") {
		t.Errorf("comments lost:\n%s", out)
	}
	if !strings.Contains(out, "model = \"qwen\"   # keep me") {
		t.Errorf("trailing comment not preserved:\n%s", out)
	}
	if !strings.Contains(out, "name = \"x\"") {
		t.Errorf("table key not replaced:\n%s", out)
	}
}

func TestPatchTOMLTopSetsDeletesAndAdds(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	orig := "# top\nmodel = \"old\"\nmodel_provider = \"other\"\nmodel_catalog_json = \"/old/path\"   # stale\n\n[model_providers.other]\nname = \"user\"\n"
	write(t, path, orig)
	if err := PatchTOMLTop(path,
		[]KV{
			{Path: "model", Value: "new"},
			{Path: "model_provider", Value: "charon"},
			{Path: "model_reasoning_effort", Value: "high"},
		},
		[]string{"model_catalog_json"},
	); err != nil {
		t.Fatal(err)
	}
	out := read(t, path)
	if !strings.Contains(out, "# top") {
		t.Errorf("comment lost:\n%s", out)
	}
	if !strings.Contains(out, "model = \"new\"") || strings.Contains(out, "model = \"old\"") {
		t.Errorf("model not replaced:\n%s", out)
	}
	if strings.Contains(out, "model_catalog_json") {
		t.Errorf("deleted key still present:\n%s", out)
	}
	if !strings.Contains(out, "model_provider = \"charon\"") {
		t.Errorf("provider not set:\n%s", out)
	}
	if !strings.Contains(out, "model_reasoning_effort = \"high\"") {
		t.Errorf("new key not added after the last root key:\n%s", out)
	}
	if !strings.Contains(out, "[model_providers.other]") || !strings.Contains(out, "name = \"user\"") {
		t.Errorf("table section must be untouched:\n%s", out)
	}
	if strings.Index(out, "model_reasoning_effort") > strings.Index(out, "[model_providers.other]") {
		t.Errorf("new root key must stay before the first table:\n%s", out)
	}
}

func TestSetTOMLTablesReplacesOwnedOnly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	orig := `# pickers

[model.charon-old]
name = "old"
base_url = "https://old"

[model.mine]  # user's own
name = "mine"

[models]
default = "charon-old"
`
	write(t, path, orig)
	if err := SetTOMLTables(path, []string{"model.charon-"}, []Table{
		{Name: "model.charon-a", KVs: []KV{{Path: "name", Value: "a"}, {Path: "base_url", Value: "https://gw"}}},
		{Name: "model.charon-b", KVs: []KV{{Path: "name", Value: "b"}, {Path: "base_url", Value: "https://gw"}}},
	}); err != nil {
		t.Fatal(err)
	}
	out := read(t, path)
	if strings.Contains(out, "[model.charon-old]") {
		t.Errorf("stale owned table survived:\n%s", out)
	}
	if !strings.Contains(out, "[model.mine]") || !strings.Contains(out, "name = \"mine\"") {
		t.Errorf("user table lost:\n%s", out)
	}
	if !strings.Contains(out, "# pickers") {
		t.Errorf("comment lost:\n%s", out)
	}
	if !strings.Contains(out, "[model.charon-a]") || !strings.Contains(out, "[model.charon-b]") {
		t.Errorf("new tables missing:\n%s", out)
	}
	if !strings.Contains(out, "[models]\ndefault = \"charon-old\"") {
		t.Errorf("other table must stay as written:\n%s", out)
	}
}

func TestSetTOMLTableAppendsAndReplaces(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	write(t, path, "# codex config\nmodel = \"gpt\"\n\n[model_providers.user]\nname = \"user\"\n")
	if err := SetTOMLTable(path, "model_providers.charon",
		KV{Path: "name", Value: "charon"},
		KV{Path: "base_url", Value: "https://gw/v1"},
		KV{Path: "experimental_bearer_token", Value: "sk"},
	); err != nil {
		t.Fatal(err)
	}
	out := read(t, path)
	if !strings.Contains(out, "# codex config") || !strings.Contains(out, "[model_providers.user]") {
		t.Errorf("user content lost:\n%s", out)
	}
	if !strings.Contains(out, "[model_providers.charon]\nname = \"charon\"\nbase_url = \"https://gw/v1\"\nexperimental_bearer_token = \"sk\"") {
		t.Errorf("table not appended:\n%s", out)
	}
	// replace in place, same location
	if err := SetTOMLTable(path, "model_providers.charon", KV{Path: "name", Value: "charon2"}); err != nil {
		t.Fatal(err)
	}
	out = read(t, path)
	if strings.Index(out, "[model_providers.charon]") < strings.Index(out, "[model_providers.user]") {
		t.Errorf("table should replace in place (before user's):\n%s", out)
	}
}

func TestTOMLEditRefusesInvalidResult(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	write(t, path, "model = \"x\"\n\n[models]\ndefault = \"a\"\n")
	// [models] exists as a table; a duplicate table with the same name is invalid
	if err := SetTOMLTable(path, "models", KV{Path: "default", Value: "b"}); err != nil {
		t.Fatal(err)
	}
	// a key that would clash with the table's name at top level is refused
	if err := PatchTOMLTop(path, []KV{{Path: "models", Value: "scalar"}}, nil); err == nil {
		t.Fatal("duplicate key must be refused")
	}
	if !strings.Contains(read(t, path), "default = \"b\"") {
		t.Errorf("first edit missing after refused second:\n%s", read(t, path))
	}
}

func TestTOMLKeepsCRLF(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	write(t, path, "# top\r\nmodel = \"x\"\r\n")
	if err := SetTOMLTop(path, KV{Path: "model", Value: "y"}); err != nil {
		t.Fatal(err)
	}
	out := read(t, path)
	if !strings.Contains(out, "model = \"y\"\r\n") {
		t.Errorf("CRLF not kept:\n%q", out)
	}
}

func TestTomlLiteralEscapes(t *testing.T) {
	cases := map[string]string{
		"plain":       `"plain"`,
		"qu\"ote":     `"qu\"ote"`,
		"back\\slash": `"back\\slash"`,
		"tab\there":   `"tab\there"`,
		"nl\nhere":    `"nl\nhere"`,
		"ctl\x01":     `"ctl\u0001"`,
	}
	for in, want := range cases {
		if got := tomlLiteral(in); got != want {
			t.Errorf("tomlLiteral(%q) = %s, want %s", in, got, want)
		}
	}
	if err := validateTOML([]byte("a = " + tomlLiteral("ctl\x01") + "\n")); err != nil {
		t.Errorf("control char must be escapable: %v", err)
	}
}

func TestGetTOMLTableAndTop(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	write(t, path, "# top\nmodel = \"gpt\"\n\n[model_providers.charon]\nname = \"charon\"\nbase_url = \"https://gw\"\n")
	if v, ok := GetTOMLTop(path, "model"); !ok || v != "gpt" {
		t.Fatalf("GetTOMLTop(model) = %q, %v", v, ok)
	}
	tbl, err := GetTOMLTable(path, "model_providers.charon")
	if err != nil {
		t.Fatal(err)
	}
	if tbl["base_url"] != "https://gw" {
		t.Errorf("GetTOMLTable base_url = %q", tbl["base_url"])
	}
}
