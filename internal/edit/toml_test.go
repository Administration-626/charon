package edit

import (
	"path/filepath"
	"reflect"
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
		"bs\bhere":    `"bs\bhere"`,
		"ff\fhere":    `"ff\fhere"`,
		"cr\rhere":    `"cr\rhere"`,
		"del\x7f":     `"del\u007F"`,
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
	if v, ok := GetTOMLTop(path, "nope"); ok || v != "" {
		t.Errorf("GetTOMLTop(nope) = %q, %v; want empty, false", v, ok)
	}
	tbl, err := GetTOMLTable(path, "model_providers.charon")
	if err != nil {
		t.Fatal(err)
	}
	if tbl["base_url"] != "https://gw" {
		t.Errorf("GetTOMLTable base_url = %q", tbl["base_url"])
	}
}

func TestTOMLTablesListsOrdinaryHeaders(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	write(t, path, "# pickers\n\n[a]\nx = 1\n\n[[arr]]\ny = 2\n\n[model.\"charon-a.b\"]\nz = 3\n")
	got, err := TOMLTables(path)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"a", `model."charon-a.b"`}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("TOMLTables = %#v, want %#v", got, want)
	}
	if got, err := TOMLTables(filepath.Join(t.TempDir(), "missing.toml")); err != nil || got != nil {
		t.Errorf("TOMLTables(missing) = %#v, %v; want nil, nil", got, err)
	}
}

func TestDelTOMLKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	write(t, path, "top = 1\n\n[t]\nkeep = \"k\"\n# note\ndrop = \"d\"\n")
	if err := DelTOMLKey(path, "t", "drop"); err != nil {
		t.Fatal(err)
	}
	out := read(t, path)
	if strings.Contains(out, "drop") {
		t.Errorf("key not removed:\n%s", out)
	}
	if !strings.Contains(out, "keep = \"k\"") || !strings.Contains(out, "# note") || !strings.Contains(out, "[t]") {
		t.Errorf("siblings or comments lost:\n%s", out)
	}
	// Removing the last key takes the table with it.
	solo := filepath.Join(t.TempDir(), "solo.toml")
	write(t, solo, "top = 1\n\n[solo]\nonly = 1\n")
	if err := DelTOMLKey(solo, "solo", "only"); err != nil {
		t.Fatal(err)
	}
	if out := read(t, solo); strings.Contains(out, "[solo]") || strings.Contains(out, "only = 1") {
		t.Errorf("empty table not removed:\n%s", out)
	} else if !strings.Contains(out, "top = 1") {
		t.Errorf("top-level key lost:\n%s", out)
	}
	// A missing table or key is a no-op, not an error.
	if err := DelTOMLKey(path, "nope", "x"); err != nil {
		t.Fatal(err)
	}
	if err := DelTOMLKey(path, "missing", "x"); err != nil {
		t.Fatal(err)
	}
}

func TestDelTOMLTable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	write(t, path, "top = 1\n\n[a]\nx = 1\n\n[b]\ny = 2\n")
	if err := DelTOMLTable(path, "a"); err != nil {
		t.Fatal(err)
	}
	out := read(t, path)
	if strings.Contains(out, "[a]") || strings.Contains(out, "x = 1") {
		t.Errorf("table not removed:\n%s", out)
	}
	if !strings.Contains(out, "top = 1") || !strings.Contains(out, "[b]\ny = 2") {
		t.Errorf("other content lost:\n%s", out)
	}
	if strings.Contains(out, "\n\n\n") {
		t.Errorf("blank line left behind:\n%q", out)
	}
	before := out
	if err := DelTOMLTable(path, "nope"); err != nil {
		t.Fatal(err)
	}
	if read(t, path) != before {
		t.Error("deleting a missing table must not touch the file")
	}
}

func TestGetTOMLTopRaw(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	write(t, path, "model = \"gpt\"   # note\nn = 2\n\n[t]\na = 1\n")
	if got, ok := GetTOMLTopRaw(path, "model"); !ok || got != `"gpt"` {
		t.Errorf("GetTOMLTopRaw(model) = %q, %v; want the quoted text", got, ok)
	}
	if got, ok := GetTOMLTopRaw(path, "n"); !ok || got != "2" {
		t.Errorf("GetTOMLTopRaw(n) = %q, %v", got, ok)
	}
	if _, ok := GetTOMLTopRaw(path, "t.a"); ok {
		t.Error("GetTOMLTopRaw must not reach into a table")
	}
	// A file the parser refuses has nothing trustworthy to put back.
	broken := filepath.Join(t.TempDir(), "broken.toml")
	write(t, broken, "model = \n")
	if _, ok := GetTOMLTopRaw(broken, "model"); ok {
		t.Error("invalid TOML must report false")
	}
}

func TestGetTOMLTopFallsBackOnInvalidFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	write(t, path, "model = \"gpt\"   # note\nbroken = \n[t]\na = 1\n")
	if got, ok := GetTOMLTop(path, "model"); !ok || got != "gpt" {
		t.Errorf("GetTOMLTop(model) = %q, %v; want %q", got, ok, "gpt")
	}
	if got, ok := GetTOMLTop(path, "nope"); ok || got != "" {
		t.Errorf("GetTOMLTop(nope) = %q, %v; want empty, false", got, ok)
	}
}

func TestDelTOMLTop(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	write(t, path, "model = \"a\"\nmodels = [\n  \"x\",\n  \"y\",\n]\nkeep = 1\n\n[t]\nx = 1\n")
	if err := DelTOMLTop(path, "models"); err != nil {
		t.Fatal(err)
	}
	out := read(t, path)
	if strings.Contains(out, "models") || strings.Contains(out, `"x"`) {
		t.Errorf("multiline top-level key not removed whole:\n%s", out)
	}
	if !strings.Contains(out, "model = \"a\"") || !strings.Contains(out, "keep = 1") || !strings.Contains(out, "[t]\nx = 1") {
		t.Errorf("other content lost:\n%s", out)
	}
	if err := DelTOMLTop(path, "nope"); err != nil {
		t.Fatal(err)
	}
}

func TestSpellTOMLTableNameAndBareKey(t *testing.T) {
	cases := []struct {
		parts []string
		want  string
	}{
		{[]string{"model", "charon"}, "model.charon"},
		{[]string{"model", "charon-a.b"}, `model."charon-a.b"`},
		{[]string{"a_b", "c-d", "E9"}, "a_b.c-d.E9"},
	}
	for _, c := range cases {
		if got := SpellTOMLTableName(c.parts...); got != c.want {
			t.Errorf("SpellTOMLTableName(%q) = %q, want %q", c.parts, got, c.want)
		}
	}
	bare := map[string]bool{"": false, "abc": true, "a_b-c9": true, "a.b": false, "ü": false, "a b": false}
	for s, want := range bare {
		if got := isBareKey(s); got != want {
			t.Errorf("isBareKey(%q) = %v, want %v", s, got, want)
		}
	}
}

func TestTOMLErrorsCarryPosition(t *testing.T) {
	path := filepath.Join(t.TempDir(), "broken.toml")
	write(t, path, "a = 1\nb = \n")
	err := SetTOMLTop(path, KV{Path: "a", Value: 2})
	if err == nil {
		t.Fatal("invalid TOML must be refused")
	}
	if !strings.Contains(err.Error(), "line 2") {
		t.Errorf("error lacks a position: %v", err)
	}
	// A value left open at EOF reports a position too, without panicking.
	eof := filepath.Join(t.TempDir(), "eof.toml")
	write(t, eof, `model = "x`)
	if err := DelTOMLTop(eof, "model"); err == nil || !strings.Contains(err.Error(), "line 1") {
		t.Errorf("EOF error = %v; want a line-1 position", err)
	}
}

func TestTOMLArrayTableRejected(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	write(t, path, "[[model.charon]]\nname = \"x\"\n")
	if _, err := GetTOMLTable(path, "model.charon"); err == nil || !strings.Contains(err.Error(), "array table") {
		t.Errorf("GetTOMLTable on an array table = %v; want a type error", err)
	}
	for name, edit := range map[string]func() error{
		"SetTOMLTable": func() error { return SetTOMLTable(path, "model.charon", KV{Path: "name", Value: "y"}) },
		"SetTOMLKey":   func() error { return SetTOMLKey(path, "model.charon", "name", "y") },
		"DelTOMLKey":   func() error { return DelTOMLKey(path, "model.charon", "name") },
		"DelTOMLTable": func() error { return DelTOMLTable(path, "model.charon") },
	} {
		if err := edit(); err == nil || !strings.Contains(err.Error(), "array table") {
			t.Errorf("%s on an array table = %v; want a type error", name, err)
		}
	}
	if out := read(t, path); out != "[[model.charon]]\nname = \"x\"\n" {
		t.Errorf("refused edits must leave the file untouched:\n%s", out)
	}
}

func TestGetTOMLTopNonScalar(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	write(t, path, "models = [\"a\", \"b\"]   # note\ninline = { a = 1 }\n")
	if got, ok := GetTOMLTop(path, "models"); !ok || got != `["a", "b"]` {
		t.Errorf("GetTOMLTop(models) = %q, %v; want the literal array", got, ok)
	}
	if got, ok := GetTOMLTop(path, "inline"); !ok || got != "{ a = 1 }" {
		t.Errorf("GetTOMLTop(inline) = %q, %v; want the literal inline table", got, ok)
	}
}

func TestTOMLDecodeErrorRefused(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	write(t, path, "a = 1\na = 2\n")
	// Syntactically fine, semantically a duplicate: the pre-write decode check
	// must refuse it and leave the file as it was.
	if err := SetTOMLTop(path, KV{Path: "a", Value: 3}); err == nil {
		t.Fatal("duplicate key must be refused")
	}
	if out := read(t, path); out != "a = 1\na = 2\n" {
		t.Errorf("refused edit must leave the file untouched:\n%s", out)
	}
}

func TestBareTOMLName(t *testing.T) {
	cases := map[string]string{
		`model."charon-a.b"`: "model.charon-a.b",
		`model.charon-a.b`:   "model.charon-a.b",
		`"a.b".c`:            "a.b.c",
		`a."b\"c"`:           `a.b"c`,
	}
	for in, want := range cases {
		if got := BareTOMLName(in); got != want {
			t.Errorf("BareTOMLName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestTomlLiteralScalars(t *testing.T) {
	cases := []struct {
		in   any
		want string
	}{
		{Raw(`["a"]`), `["a"]`},
		{true, "true"},
		{3, "3"},
	}
	for _, c := range cases {
		if got := tomlLiteral(c.in); got != c.want {
			t.Errorf("tomlLiteral(%#v) = %q, want %q", c.in, got, c.want)
		}
	}
	// A byte that is not valid UTF-8 has no TOML escape; it is written as it
	// is and the pre-write validation refuses the file.
	bad := tomlLiteral("\xff")
	if !strings.Contains(bad, "\xff") {
		t.Errorf("invalid UTF-8 byte should be left as-is, got %q", bad)
	}
	if err := validateTOML([]byte("a = " + bad + "\n")); err == nil {
		t.Error("a file with invalid UTF-8 must be refused")
	}
}

func TestGetTOMLTableMissing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	write(t, path, "a = 1\n\n[t]\nx = 1\n")
	tbl, err := GetTOMLTable(path, "nope")
	if err != nil || tbl != nil {
		t.Errorf("GetTOMLTable(missing) = %#v, %v; want nil, nil", tbl, err)
	}
	if tbl, err := GetTOMLTable(filepath.Join(t.TempDir(), "missing.toml"), "t"); err != nil || tbl != nil {
		t.Errorf("GetTOMLTable on a missing file = %#v, %v; want nil, nil", tbl, err)
	}
}

func TestSetTOMLKeyAddsKeyAndTable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	write(t, path, "a = 1\n\n[t]\nx = 1   # keep\n")
	// A new key goes after the last complete value, keeping the comment above.
	if err := SetTOMLKey(path, "t", "y", 2); err != nil {
		t.Fatal(err)
	}
	out := read(t, path)
	if !strings.Contains(out, "x = 1   # keep\ny = 2") {
		t.Errorf("new key not appended after the last value:\n%s", out)
	}
	// A missing table is appended with the key in it.
	if err := SetTOMLKey(path, "u", "z", "v"); err != nil {
		t.Fatal(err)
	}
	out = read(t, path)
	if !strings.Contains(out, "[u]\nz = \"v\"") {
		t.Errorf("missing table not appended:\n%s", out)
	}
}

func TestSetTOMLTablesNoMatchIsNoOp(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	orig := "# keep\na = 1\n\n[t]\nx = 1\n"
	write(t, path, orig)
	if err := SetTOMLTables(path, []string{"zzz"}, nil); err != nil {
		t.Fatal(err)
	}
	if out := read(t, path); out != orig {
		t.Errorf("no matching table and nothing to add must not touch the file:\n%q", out)
	}
}

func TestTOMLInvalidFileRejectedEverywhere(t *testing.T) {
	path := filepath.Join(t.TempDir(), "broken.toml")
	write(t, path, "a = \n")
	for name, edit := range map[string]func() error{
		"SetTOMLTable":  func() error { return SetTOMLTable(path, "t", KV{Path: "x", Value: 1}) },
		"DelTOMLTable":  func() error { return DelTOMLTable(path, "t") },
		"PatchTOMLTop":  func() error { return PatchTOMLTop(path, nil, nil) },
		"SetTOMLTables": func() error { return SetTOMLTables(path, []string{"t"}, nil) },
	} {
		if err := edit(); err == nil {
			t.Errorf("%s must refuse invalid TOML", name)
		}
	}
	if _, err := GetTOMLTable(path, "t"); err == nil {
		t.Error("GetTOMLTable must report invalid TOML")
	}
}

func TestSetTOMLTableSeparatesFromNextHeader(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	// No blank line between the replaced table and the next header.
	write(t, path, "[a]\nx = 1\n[b]\ny = 2\n")
	if err := SetTOMLTable(path, "a", KV{Path: "z", Value: 3}); err != nil {
		t.Fatal(err)
	}
	if out := read(t, path); !strings.Contains(out, "[a]\nz = 3\n\n[b]") {
		t.Errorf("replaced table not separated from the next header:\n%q", out)
	}
}
