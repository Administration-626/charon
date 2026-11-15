package edit

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	toml "github.com/pelletier/go-toml/v2"
	"github.com/tidwall/jsonc"
)

// Differential tests for the format-preserving editors.
//
// Every edit here is checked against a second, deliberately different
// implementation: the document is decoded into plain Go values, the same change
// is made to those values, and the two are compared. Bytes are expected to
// differ — keeping comments, key order and indentation is the whole point of
// this package, and a whole-document rewrite loses exactly that — so the
// assertion is on values. A difference means one of the two implementations
// changed the document's meaning.
//
// The oracle is a whole-document rewrite, so it shares no code path with the
// line- and offset-level splicing under test: it cannot inherit the bug it is
// looking for. It is kept small on purpose. Where its model is simpler than the
// file format allows, the case notes it:
//
//   - inline tables decode as maps, indistinguishable from a child table block;
//     real SetTOMLTable keeps child blocks and replaces everything else, so a
//     document with an inline table at a table name the case replaces has no
//     case here.
//   - array tables ([[t]]) decode as slices; the cases avoid editing below one.
//
// Keywords can move: the oracle has no key order, so a reordering bug would
// have to corrupt a value to be caught. Ordering and layout are pinned by the
// golden corpus in corpus_test.go and by the byte-level tests in json_test.go
// and toml_test.go.
//
// Two behaviors are deliberate and are not bugs a case should report:
//
//   - a member deleted and re-set in separate edits (or under a parent that is
//     deleted with it) is inserted as the first member of its object, so it can
//     move to the front once; a repeat converges.
//   - a table that exists only because a child block names it (`[a.b]` with no
//     `[a]`) goes away with that child; the oracle prunes the empty parent for
//     the same reason.

// diffDoc is a document under test: a fixture in testdata, or inline bytes.
type diffDoc struct {
	fixture string // file name under testdata
	raw     string // inline document, used when fixture is empty
}

func (d diffDoc) bytes(t *testing.T) []byte {
	t.Helper()
	if d.fixture == "" {
		return []byte(d.raw)
	}
	b, err := os.ReadFile(filepath.Join("testdata", d.fixture))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// editResult runs edit against a fresh copy of the document and returns the
// bytes the edit left behind.
func editResult(t *testing.T, src []byte, name string, edit func(path string) error) ([]byte, error) {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, src, 0o600); err != nil {
		t.Fatal(err)
	}
	return editOnce(t, path, src, edit)
}

// editOnce runs edit on an existing file and returns what the file holds
// afterwards, whether or not the edit failed.
func editOnce(t *testing.T, path string, _ []byte, edit func(string) error) ([]byte, error) {
	t.Helper()
	err := edit(path)
	out, rerr := os.ReadFile(path)
	if rerr != nil {
		t.Fatal(rerr)
	}
	// An edit that deletes the last thing in a file legitimately leaves it
	// empty; whether the result is right is the value comparison's business.
	return out, err
}

// ---- JSON oracle ----

// decodeJSON reads JSON/JSONC the way both sides are compared: comments and
// trailing commas removed, a byte order mark ignored, numbers as float64.
func decodeJSON(t *testing.T, b []byte) map[string]any {
	t.Helper()
	m := map[string]any{}
	if len(bytes.TrimSpace(TrimBOM(b))) == 0 {
		return m
	}
	plain := jsonc.ToJSONInPlace(append([]byte(nil), TrimBOM(b)...))
	if err := json.Unmarshal(plain, &m); err != nil {
		t.Fatalf("result is not JSON/JSONC: %v\n%s", err, b)
	}
	return m
}

// jsonDocValue is v as a decoded document holds it (int → float64, and so on),
// so a value the editor marshals compares equal to the value put in the oracle.
func jsonDocValue(t *testing.T, v any) any {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshalling %#v: %v", v, err)
	}
	var out any
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshalling %#v: %v", v, err)
	}
	return out
}

// oracleJSONSet sets one dot path, creating missing parents; a parent that
// holds a scalar is replaced by the object the path needs — the documented
// behavior of setJSONBytes.
func oracleJSONSet(t *testing.T, doc map[string]any, path string, v any) {
	t.Helper()
	parts := strings.Split(path, ".")
	cur := doc
	for _, p := range parts[:len(parts)-1] {
		next, ok := cur[p].(map[string]any)
		if !ok {
			next = map[string]any{}
			cur[p] = next
		}
		cur = next
	}
	cur[parts[len(parts)-1]] = jsonDocValue(t, v)
}

// oracleJSONDel removes one dot path, ignoring a path that is not there.
func oracleJSONDel(doc map[string]any, path string) {
	parts := strings.Split(path, ".")
	cur := doc
	for _, p := range parts[:len(parts)-1] {
		next, ok := cur[p].(map[string]any)
		if !ok {
			return
		}
		cur = next
	}
	delete(cur, parts[len(parts)-1])
}

// ---- TOML oracle ----

// decodeTOML parses a document into plain values, the way both sides of a TOML
// case are compared.
func decodeTOML(t *testing.T, b []byte) map[string]any {
	t.Helper()
	m := map[string]any{}
	if err := toml.Unmarshal(TrimBOM(b), &m); err != nil {
		t.Fatalf("result is not TOML: %v\n%s", err, b)
	}
	return m
}

// oracleTOMLPath splits a table name into its parts: dots separate, a quoted
// part is one name however many dots it holds. Written here rather than taken
// from the package under test, so the oracle stays independent.
func oracleTOMLPath(name string) []string {
	var parts []string
	var cur strings.Builder
	for i := 0; i < len(name); i++ {
		switch c := name[i]; c {
		case '"', '\'':
			end := i + 1
			for end < len(name) && name[end] != c {
				if c == '"' && name[end] == '\\' {
					end++
				}
				end++
			}
			part := name[i+1 : min(end, len(name))]
			part = strings.ReplaceAll(part, `\"`, `"`)
			part = strings.ReplaceAll(part, `\\`, `\`)
			cur.WriteString(part)
			i = end
		case '.':
			parts = append(parts, cur.String())
			cur.Reset()
		case ' ', '\t':
			// whitespace around dots is not part of the name
		default:
			cur.WriteByte(c)
		}
	}
	return append(parts, cur.String())
}

// oracleTOMLLookup returns the table at parts, or nil when it is absent or an
// intermediate value is not a table.
func oracleTOMLLookup(doc map[string]any, parts []string) map[string]any {
	cur := doc
	for _, p := range parts {
		next, ok := cur[p].(map[string]any)
		if !ok {
			return nil
		}
		cur = next
	}
	return cur
}

// oracleTOMLTable returns the table at name, creating it and its parents.
func oracleTOMLTable(doc map[string]any, name string) map[string]any {
	parts := oracleTOMLPath(name)
	cur := doc
	for _, p := range parts {
		next, ok := cur[p].(map[string]any)
		if !ok {
			next = map[string]any{}
			cur[p] = next
		}
		cur = next
	}
	return cur
}

// oracleTOMLValue is v as a decoded TOML document holds it: integers come back
// as int64, everything else as it was.
func oracleTOMLValue(v any) any {
	if n, ok := v.(int); ok {
		return int64(n)
	}
	return v
}

// oracleTOMLSetTable replaces a table's own entries with kvs, the way
// SetTOMLTable replaces a block's body: entries that are tables themselves are
// child blocks (a separate header later in the file) and survive.
func oracleTOMLSetTable(doc map[string]any, name string, kvs []KV) {
	tbl := oracleTOMLTable(doc, name)
	for k, v := range tbl {
		if _, child := v.(map[string]any); !child {
			delete(tbl, k)
		}
	}
	for _, kv := range kvs {
		tbl[kv.Path] = oracleTOMLValue(kv.Value)
	}
}

// oracleTOMLDelTable removes one table, leaving child blocks alone.
func oracleTOMLDelTable(doc map[string]any, name string) {
	parts := oracleTOMLPath(name)
	parent := oracleTOMLLookup(doc, parts[:len(parts)-1])
	if parent == nil {
		return
	}
	delete(parent, parts[len(parts)-1])
}

// oracleTOMLDelKey removes one key of a table, and the table with it when
// nothing else is left in it — DelTOMLKey's documented behavior.
func oracleTOMLDelKey(doc map[string]any, table, key string) {
	tbl := oracleTOMLLookup(doc, oracleTOMLPath(table))
	if tbl == nil {
		return
	}
	delete(tbl, key)
	if len(tbl) == 0 {
		oracleTOMLDelTable(doc, table)
	}
}

// oracleTOMLDelPrefix removes every table whose name starts with prefix: the
// keys under the prefix's parent that hold a table (a map) or an array table
// (a slice). Scalars are not tables and stay.
func oracleTOMLDelPrefix(doc map[string]any, prefix string) {
	i := strings.LastIndexByte(prefix, '.')
	if i < 0 {
		return
	}
	parent := oracleTOMLLookup(doc, oracleTOMLPath(prefix[:i]))
	if parent == nil {
		return
	}
	leaf := prefix[i+1:]
	for name, v := range parent {
		if !strings.HasPrefix(name, leaf) {
			continue
		}
		switch v.(type) {
		case map[string]any, []any:
			delete(parent, name)
		}
	}
}

// ---- harnesses ----

// jsonDiff runs one JSON case: the edit's result must decode to the same value
// the oracle produced, the edit must be safe to repeat, and a case expecting an
// error must leave the file byte-identical.
type jsonDiff struct {
	name string
	doc  diffDoc
	edit func(path string) error
	// oracle applies the same change to the decoded document. Nil means the
	// edit is expected to fail.
	oracle func(t *testing.T, doc map[string]any)
}

func runJSONDiffs(t *testing.T, cases []jsonDiff) {
	t.Helper()
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			src := c.doc.bytes(t)
			got, err := editResult(t, src, "settings.json", c.edit)
			if c.oracle == nil {
				if err == nil {
					t.Fatalf("edit succeeded, want an error:\n%s", got)
				}
				if !bytes.Equal(got, src) {
					t.Fatalf("a failed edit changed the file:\n--- got ---\n%s\n--- was ---\n%s", got, src)
				}
				return
			}
			if err != nil {
				t.Fatalf("edit: %v", err)
			}
			want := decodeJSON(t, src)
			c.oracle(t, want)
			if g := decodeJSON(t, got); !reflect.DeepEqual(g, want) {
				t.Fatalf("JSON value semantics differ (bytes differ by design, values must not):\n--- got ---\n%#v\n--- want ---\n%#v\n--- file ---\n%s", g, want, got)
			}
			// Repeating the same edit must not change a byte.
			path := filepath.Join(t.TempDir(), "again.json")
			if err := os.WriteFile(path, got, 0o600); err != nil {
				t.Fatal(err)
			}
			again, err := editOnce(t, path, got, c.edit)
			if err != nil {
				t.Fatalf("second edit: %v", err)
			}
			if !bytes.Equal(again, got) {
				t.Fatalf("edit is not idempotent:\n--- first ---\n%s\n--- second ---\n%s", got, again)
			}
		})
	}
}

// tomlDiff is jsonDiff for TOML documents.
type tomlDiff struct {
	name   string
	doc    diffDoc
	edit   func(path string) error
	oracle func(t *testing.T, doc map[string]any)
}

func runTOMLDiffs(t *testing.T, cases []tomlDiff) {
	t.Helper()
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			src := c.doc.bytes(t)
			got, err := editResult(t, src, "config.toml", c.edit)
			if c.oracle == nil {
				if err == nil {
					t.Fatalf("edit succeeded, want an error:\n%s", got)
				}
				if !bytes.Equal(got, src) {
					t.Fatalf("a failed edit changed the file:\n--- got ---\n%s\n--- was ---\n%s", got, src)
				}
				return
			}
			if err != nil {
				t.Fatalf("edit: %v", err)
			}
			want := decodeTOML(t, src)
			c.oracle(t, want)
			if g := decodeTOML(t, got); !reflect.DeepEqual(g, want) {
				t.Fatalf("TOML value semantics differ (bytes differ by design, values must not):\n--- got ---\n%#v\n--- want ---\n%#v\n--- file ---\n%s", g, want, got)
			}
			path := filepath.Join(t.TempDir(), "again.toml")
			if err := os.WriteFile(path, got, 0o600); err != nil {
				t.Fatal(err)
			}
			again, err := editOnce(t, path, got, c.edit)
			if err != nil {
				t.Fatalf("second edit: %v", err)
			}
			if !bytes.Equal(again, got) {
				t.Fatalf("edit is not idempotent:\n--- first ---\n%s\n--- second ---\n%s", got, again)
			}
		})
	}
}

// ---- JSON cases ----

func TestJSONDifferential(t *testing.T) {
	runJSONDiffs(t, []jsonDiff{
		{
			// The edit claude.go really makes, on the corpus fixture.
			name: "claude gateway apply",
			doc:  diffDoc{fixture: "claude.json"},
			edit: func(path string) error {
				return ApplyJSON(path, []KV{
					{Path: "env.ANTHROPIC_BASE_URL", Value: "https://gw.example.com"},
					{Path: "env.ANTHROPIC_API_KEY", Value: "sk-new"},
					{Path: "model", Value: "claude-opus-4-1"},
				}, []string{"env.ANTHROPIC_AUTH_TOKEN"})
			},
			oracle: func(t *testing.T, doc map[string]any) {
				oracleJSONDel(doc, "env.ANTHROPIC_AUTH_TOKEN")
				oracleJSONSet(t, doc, "env.ANTHROPIC_BASE_URL", "https://gw.example.com")
				oracleJSONSet(t, doc, "env.ANTHROPIC_API_KEY", "sk-new")
				oracleJSONSet(t, doc, "model", "claude-opus-4-1")
			},
		},
		{
			// A JSONC fixture with comments: the oracle cannot see them, so only
			// the values can be compared (corpus_test pins the comments).
			name: "opencode provider on jsonc",
			doc:  diffDoc{fixture: "opencode.jsonc"},
			edit: func(path string) error {
				return SetJSON(path,
					KV{Path: "provider.charon.options.baseURL", Value: "https://gw.example.com/v1"},
					KV{Path: "provider.charon.options.apiKey", Value: "sk-new"},
					KV{Path: "provider.charon.models", Value: map[string]any{"new-model": map[string]any{}}},
				)
			},
			oracle: func(t *testing.T, doc map[string]any) {
				oracleJSONSet(t, doc, "provider.charon.options.baseURL", "https://gw.example.com/v1")
				oracleJSONSet(t, doc, "provider.charon.options.apiKey", "sk-new")
				oracleJSONSet(t, doc, "provider.charon.models", map[string]any{"new-model": map[string]any{}})
			},
		},
		{
			// Compact JSON, the shape a hand-written file often has.
			name: "compact set creates nesting",
			doc:  diffDoc{raw: `{"a":1,"b":{"c":2}}`},
			edit: func(path string) error {
				return SetJSON(path, KV{Path: "b.d.e", Value: true}, KV{Path: "f", Value: "x"})
			},
			oracle: func(t *testing.T, doc map[string]any) {
				oracleJSONSet(t, doc, "b.d.e", true)
				oracleJSONSet(t, doc, "f", "x")
			},
		},
		{
			// A byte order mark and CRLF line ends must survive as a value.
			name: "bom crlf",
			doc:  diffDoc{raw: "\xef\xbb\xbf{\r\n  \"env\": {\r\n    \"A\": \"1\"\r\n  }\r\n}\r\n"},
			edit: func(path string) error {
				return SetJSON(path, KV{Path: "env.B", Value: "2"})
			},
			oracle: func(t *testing.T, doc map[string]any) {
				oracleJSONSet(t, doc, "env.B", "2")
			},
		},
		{
			name: "delete last member",
			doc:  diffDoc{raw: "{\n  \"a\": 1\n}\n"},
			edit: func(path string) error { return DelJSON(path, "a") },
			oracle: func(_ *testing.T, doc map[string]any) {
				oracleJSONDel(doc, "a")
			},
		},
		{
			// Deleting the only key of a nested object must not take the parent.
			name: "delete collapses empty parent",
			doc:  diffDoc{raw: "{\n  \"env\": {\n    \"ONLY\": \"1\"\n  },\n  \"keep\": 2\n}\n"},
			edit: func(path string) error { return DelJSON(path, "env.ONLY") },
			oracle: func(_ *testing.T, doc map[string]any) {
				oracleJSONDel(doc, "env.ONLY")
			},
		},
		{
			name: "delete middle member of compact",
			doc:  diffDoc{raw: `{"a":1,"b":2,"c":3}`},
			edit: func(path string) error { return DelJSON(path, "b") },
			oracle: func(_ *testing.T, doc map[string]any) {
				oracleJSONDel(doc, "b")
			},
		},
		{
			// SetJSONAfter puts a restored key back after its sibling; for the
			// oracle only the resulting value matters.
			name: "set after anchor",
			doc:  diffDoc{raw: "{\n  \"a\": 1,\n  \"b\": 2,\n  \"c\": 3\n}\n"},
			edit: func(path string) error {
				return SetJSONAfter(path, "a", "b2", "restored")
			},
			oracle: func(t *testing.T, doc map[string]any) {
				oracleJSONSet(t, doc, "b2", "restored")
			},
		},
		{
			// A missing anchor falls back to a first-member insert: same value.
			name: "set after missing anchor",
			doc:  diffDoc{raw: `{"a":1,"b":2}`},
			edit: func(path string) error {
				return SetJSONAfter(path, "nope", "c", 3)
			},
			oracle: func(t *testing.T, doc map[string]any) {
				oracleJSONSet(t, doc, "c", 3)
			},
		},
		{
			// An ancestor that is a scalar becomes the object the path needs.
			name: "scalar becomes object",
			doc:  diffDoc{raw: "{\n  \"a\": 1,\n  \"b\": 2\n}\n"},
			edit: func(path string) error {
				return SetJSON(path, KV{Path: "a.b", Value: 2})
			},
			oracle: func(t *testing.T, doc map[string]any) {
				oracleJSONSet(t, doc, "a.b", 2)
			},
		},
		{
			name: "empty file is an empty object",
			doc:  diffDoc{raw: ""},
			edit: func(path string) error {
				return SetJSON(path, KV{Path: "a.b", Value: "v"})
			},
			oracle: func(t *testing.T, doc map[string]any) {
				oracleJSONSet(t, doc, "a.b", "v")
			},
		},
		{
			// A sequence, as catalog edits run one after another.
			name: "sequence",
			doc:  diffDoc{fixture: "claude.json"},
			edit: func(path string) error {
				if err := ApplyJSON(path, []KV{{Path: "env.ANTHROPIC_API_KEY", Value: "sk-1"}}, []string{"env.ANTHROPIC_AUTH_TOKEN"}); err != nil {
					return err
				}
				if err := DelJSON(path, "model"); err != nil {
					return err
				}
				return SetJSONAfter(path, "env.ANTHROPIC_API_KEY", "env.NEW", "n")
			},
			oracle: func(t *testing.T, doc map[string]any) {
				oracleJSONSet(t, doc, "env.ANTHROPIC_API_KEY", "sk-1")
				oracleJSONDel(doc, "env.ANTHROPIC_AUTH_TOKEN")
				oracleJSONDel(doc, "model")
				oracleJSONSet(t, doc, "env.NEW", "n")
			},
		},
		{
			// A top-level array has no keys to edit: refuse, and write nothing.
			name: "array top level is refused",
			doc:  diffDoc{raw: "[]\n"},
			edit: func(path string) error {
				return SetJSON(path, KV{Path: "a", Value: 1})
			},
		},
		{
			// A trailing comma before a comment (JSONC allows it) used to be left
			// behind, orphaned on its own line, and the next edit was refused as
			// invalid — so this case edits twice: delete the member, put it back.
			name: "delete before comment then re-add",
			doc:  diffDoc{raw: "{\n  \"a\": 1,\n  \"model\": \"SET\",\n  // note\n}\n"},
			edit: func(path string) error {
				if err := DelJSON(path, "model"); err != nil {
					return err
				}
				return SetJSON(path, KV{Path: "model", Value: "SET"})
			},
			oracle: func(t *testing.T, doc map[string]any) {
				oracleJSONDel(doc, "model")
				oracleJSONSet(t, doc, "model", "SET")
			},
		},
		{
			name: "invalid json is refused",
			doc:  diffDoc{raw: "{\"a\": ]}\n"},
			edit: func(path string) error {
				return SetJSON(path, KV{Path: "a", Value: 1})
			},
		},
	})
}

// ---- TOML cases ----

func TestTOMLDifferential(t *testing.T) {
	runTOMLDiffs(t, []tomlDiff{
		{
			// The top-level patch codex.go makes, on the corpus fixture.
			name: "codex top patch",
			doc:  diffDoc{fixture: "codex.toml"},
			edit: func(path string) error {
				return PatchTOMLTop(path,
					[]KV{
						{Path: "model", Value: "gpt-5.1"},
						{Path: "model_provider", Value: "charon"},
						{Path: "model_reasoning_effort", Value: "medium"},
					}, nil)
			},
			oracle: func(_ *testing.T, doc map[string]any) {
				doc["model"] = "gpt-5.1"
				doc["model_provider"] = "charon"
				doc["model_reasoning_effort"] = "medium"
			},
		},
		{
			// Prefix-owned tables are replaced and appended, including one whose
			// name holds a dot and therefore needs quoting.
			name: "grok owned model tables",
			doc:  diffDoc{fixture: "grok.toml"},
			edit: func(path string) error {
				return SetTOMLTables(path, []string{"model.charon-"}, []Table{
					{Name: "model.charon-work", KVs: []KV{
						{Path: "name", Value: "work"},
						{Path: "base_url", Value: "https://work.example.com/v1"},
						{Path: "api_key", Value: "sk-new"},
					}},
					{Name: `model."charon-a.b"`, KVs: []KV{{Path: "name", Value: "dotted"}}},
				})
			},
			oracle: func(_ *testing.T, doc map[string]any) {
				oracleTOMLDelPrefix(doc, "model.charon-")
				oracleTOMLSetTable(doc, "model.charon-work", []KV{
					{Path: "name", Value: "work"},
					{Path: "base_url", Value: "https://work.example.com/v1"},
					{Path: "api_key", Value: "sk-new"},
				})
				oracleTOMLSetTable(doc, `model."charon-a.b"`, []KV{{Path: "name", Value: "dotted"}})
			},
		},
		{
			name: "grok set default key",
			doc:  diffDoc{fixture: "grok.toml"},
			edit: func(path string) error {
				return SetTOMLKey(path, "models", "default", "charon-work")
			},
			oracle: func(_ *testing.T, doc map[string]any) {
				oracleTOMLTable(doc, "models")["default"] = "charon-work"
			},
		},
		{
			// A key that does not exist yet is added to the exisiting table.
			name: "grok add key to table",
			doc:  diffDoc{fixture: "grok.toml"},
			edit: func(path string) error {
				return SetTOMLKey(path, "model.mine", "base_url", "https://mine/v1")
			},
			oracle: func(_ *testing.T, doc map[string]any) {
				oracleTOMLTable(doc, "model.mine")["base_url"] = "https://mine/v1"
			},
		},
		{
			name: "set key appends missing table",
			doc:  diffDoc{fixture: "grok.toml"},
			edit: func(path string) error {
				return SetTOMLKey(path, "new.table", "k", 7)
			},
			oracle: func(_ *testing.T, doc map[string]any) {
				oracleTOMLTable(doc, "new.table")["k"] = int64(7)
			},
		},
		{
			// Deleting a table's last key takes the table with it.
			name: "delete key drops empty table",
			doc:  diffDoc{raw: "[t]\nx = 1\n"},
			edit: func(path string) error { return DelTOMLKey(path, "t", "x") },
			oracle: func(_ *testing.T, doc map[string]any) {
				oracleTOMLDelKey(doc, "t", "x")
			},
		},
		{
			// ... but a table with keys left stays.
			name: "delete key keeps table",
			doc:  diffDoc{raw: "[t]\nx = 1\ny = 2\n"},
			edit: func(path string) error { return DelTOMLKey(path, "t", "x") },
			oracle: func(_ *testing.T, doc map[string]any) {
				oracleTOMLDelKey(doc, "t", "x")
			},
		},
		{
			name: "delete table",
			doc:  diffDoc{fixture: "codex.toml"},
			edit: func(path string) error {
				return DelTOMLTable(path, "model_providers.openai")
			},
			oracle: func(_ *testing.T, doc map[string]any) {
				oracleTOMLDelTable(doc, "model_providers.openai")
			},
		},
		{
			// A multi-line array value goes as a whole.
			name: "delete multi line top key",
			doc:  diffDoc{raw: "models = [\n  \"a\",\n  \"b\",\n]\nkeep = 1\n\n[t]\nx = 2\n"},
			edit: func(path string) error { return DelTOMLTop(path, "models") },
			oracle: func(_ *testing.T, doc map[string]any) {
				delete(doc, "models")
			},
		},
		{
			// Replacing a top-level key keeps its place and trailing comment.
			name: "set top preserving",
			doc:  diffDoc{raw: "model = \"old\"   # keep\nother = 1\n\n[t]\nx = 2\n"},
			edit: func(path string) error {
				return SetTOMLTopPreserving(path, KV{Path: "model", Value: "new"})
			},
			oracle: func(_ *testing.T, doc map[string]any) {
				doc["model"] = "new"
			},
		},
		{
			// Array tables and dotted headers sit around the edit and must be
			// left alone while an owned table is replaced.
			name: "array and dotted tables survive",
			doc: diffDoc{raw: `# top
[[jobs]]
name = "a"

[jobs.sub]
x = 1

[a.b]
y = 2

["a.b"]
y = 3

[charon]
base = "old"
`},
			edit: func(path string) error {
				return SetTOMLTables(path, []string{"charon"}, []Table{
					{Name: "charon", KVs: []KV{{Path: "base", Value: "new"}}},
				})
			},
			oracle: func(_ *testing.T, doc map[string]any) {
				oracleTOMLDelPrefix(doc, "charon")
				oracleTOMLSetTable(doc, "charon", []KV{{Path: "base", Value: "new"}})
			},
		},
		{
			name: "bom crlf toml",
			doc:  diffDoc{raw: "\xef\xbb\xbfa = 1\r\n\r\n[t]\r\nx = 2\r\n"},
			edit: func(path string) error {
				return SetTOMLTop(path, KV{Path: "a", Value: 2})
			},
			oracle: func(_ *testing.T, doc map[string]any) {
				doc["a"] = int64(2)
			},
		},
		{
			name: "invalid toml is refused",
			doc:  diffDoc{raw: "a = \n"},
			edit: func(path string) error {
				return SetTOMLTop(path, KV{Path: "a", Value: 1})
			},
		},
		{
			// SetTOMLTables with a parent table next to a child of it (the shape
			// a tool with both a plain and a prefixed table name writes) must
			// survive a second, identical apply: the parent it writes is
			// replaced, not appended again as a duplicate header.
			name: "prefix and parent reapply",
			doc:  diffDoc{raw: "[chr]\nold = 1\n\n[chr.\"a.b\"]\nstale = 2\n"},
			edit: func(path string) error {
				return SetTOMLTables(path, []string{"chr."}, []Table{
					{Name: "chr", KVs: []KV{{Path: "a", Value: 1}}},
					{Name: `chr."a.b"`, KVs: []KV{{Path: "b", Value: 2}}},
				})
			},
			oracle: func(_ *testing.T, doc map[string]any) {
				oracleTOMLDelPrefix(doc, "chr.")
				oracleTOMLSetTable(doc, "chr", []KV{{Path: "a", Value: 1}})
				oracleTOMLSetTable(doc, `chr."a.b"`, []KV{{Path: "b", Value: 2}})
			},
		},
		{
			// An array table is not an ordinary table: replacing it as one is a
			// type error, and nothing is written.
			name: "set table refuses array table",
			doc:  diffDoc{raw: "[[t]]\nx = 1\n"},
			edit: func(path string) error {
				return SetTOMLTable(path, "t", KV{Path: "x", Value: 2})
			},
		},
	})
}
