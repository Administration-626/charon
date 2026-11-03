package edit

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	toml "github.com/pelletier/go-toml/v2"
	"github.com/tidwall/jsonc"
)

// These targets check the contract every caller relies on: an edit either
// fails without touching the file or leaves a config the tool can still parse,
// and applying it twice is the same as applying it once.

func FuzzPatchJSON(f *testing.F) {
	for _, seed := range []string{
		"{\"a\":1}\n",
		"{\n  // c\n  \"a\": \"b\"\n}\n",
		"{\"a\":{\"b\":1},\"c\":2}",
		"{}\n",
		"[]\n",
		"\xef\xbb\xbf{\"a\":1}\r\n",
		"",
	} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, raw []byte) {
		set := []KV{{Path: "b.c", Value: 2}}
		out, err := PatchJSON(raw, []string{"a"}, set)
		if err != nil {
			return
		}
		plain := jsonc.ToJSONInPlace(bytes.Clone(TrimBOM(out)))
		if !json.Valid(plain) {
			t.Fatalf("PatchJSON produced invalid JSON from %q:\n%q", raw, out)
		}
		var m map[string]any
		if err := json.Unmarshal(plain, &m); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		b, ok := m["b"].(map[string]any)
		if !ok || b["c"] != float64(2) {
			t.Fatalf("set value missing from %v", m)
		}
		again, err := PatchJSON(out, nil, set)
		if err != nil {
			t.Fatalf("second patch of %q: %v", out, err)
		}
		if !bytes.Equal(again, out) {
			t.Fatalf("edit is not idempotent:\nfirst  %q\nsecond %q", out, again)
		}
	})
}

func FuzzSetTOMLTop(f *testing.F) {
	for _, seed := range []string{
		"a = 1\n",
		"# c\nmodel = \"gpt\"   # keep\n\n[t]\nx = 1\n",
		"models = [\n  \"a\",\n]\n",
		"\xef\xbb\xbfa = 1\r\n",
		"",
		"a = \n",
	} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, raw []byte) {
		path := filepath.Join(t.TempDir(), "config.toml")
		if err := os.WriteFile(path, raw, 0o600); err != nil {
			t.Fatal(err)
		}
		err := SetTOMLTop(path, KV{Path: "zzz", Value: "v"})
		got, rerr := os.ReadFile(path)
		if rerr != nil {
			t.Fatal(rerr)
		}
		if err != nil {
			if !bytes.Equal(got, raw) {
				t.Fatalf("failed edit changed the file:\nwas %q\ngot %q", raw, got)
			}
			return
		}
		var m map[string]any
		if err := toml.Unmarshal(TrimBOM(got), &m); err != nil {
			t.Fatalf("edited file is not valid TOML: %v\n%s", err, got)
		}
		if m["zzz"] != "v" {
			t.Fatalf("set key missing from %v", m)
		}
		if err := SetTOMLTop(path, KV{Path: "zzz", Value: "v"}); err != nil {
			t.Fatalf("second set: %v", err)
		}
		again, rerr := os.ReadFile(path)
		if rerr != nil {
			t.Fatal(rerr)
		}
		if !bytes.Equal(again, got) {
			t.Fatalf("edit is not idempotent:\nfirst  %q\nsecond %q", got, again)
		}
	})
}

func FuzzDelTOMLTable(f *testing.F) {
	for _, seed := range []string{
		"[t]\nx = 1\n",
		"a = 1\n\n[t]\nx = 1\n\n[u]\ny = 2\n",
		"[[t]]\nx = 1\n",
		"[t]\nx = [\n  1,\n]\n",
		"",
		"a = \n",
	} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, raw []byte) {
		// Start from a document the tool could load; how invalid input is
		// handled is covered by the parse and read tests.
		if err := validateTOML(TrimBOM(raw)); err != nil {
			return
		}
		path := filepath.Join(t.TempDir(), "config.toml")
		if err := os.WriteFile(path, raw, 0o600); err != nil {
			t.Fatal(err)
		}
		err := DelTOMLTable(path, "t")
		got, rerr := os.ReadFile(path)
		if rerr != nil {
			t.Fatal(rerr)
		}
		if err != nil {
			if !bytes.Equal(got, raw) {
				t.Fatalf("failed delete changed the file:\nwas %q\ngot %q", raw, got)
			}
			return
		}
		var m map[string]any
		if err := toml.Unmarshal(TrimBOM(got), &m); err != nil {
			t.Fatalf("edited file is not valid TOML: %v\n%s", err, got)
		}
		tables, err := TOMLTables(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, name := range tables {
			if name == "t" {
				t.Fatalf("table t survived deletion:\n%s", got)
			}
		}
	})
}
