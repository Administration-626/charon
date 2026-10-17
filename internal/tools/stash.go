package tools

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"charon/internal/edit"
)

// OwnedFile is one config file's worth of what charon may own in it. JSON
// files name dot paths; TOML files name top-level keys, "table.key" entries,
// exact table names, and whole-table prefixes (every table whose name starts
// with one is charon's, deleted rather than restored). JSON prefixes work the
// same way on keys: the leaf names charon's entry, and every key that extends
// it with "-" is charon's generated entry too, so off deletes both instead of
// restoring anything.
type OwnedFile struct {
	Path         string
	JSON         []string
	JSONPrefixes []string // "providers.charon": the key plus every key prefixed with "charon-"
	TOMLTop      []string
	TOMLKeys     []string // "table.key"
	TOMLTables   []string // exact table names
	Prefixes     []string // table-name prefixes
}

// StashVal is one owned path's pre-charon value: Raw as the file wrote it
// (JSON raw text, or TOML value text), or Absent when the file had no such
// path and charon's entry is simply removed. Prev is the key before this one
// in its JSON parent object, the anchor that puts a restored key back where
// it was.
type StashVal struct {
	Absent bool   `json:"absent,omitempty"`
	Raw    string `json:"raw,omitempty"`
	Prev   string `json:"prev,omitempty"`
}

// FileStash is what one file's owned paths held before charon first wrote.
type FileStash struct {
	JSON map[string]StashVal `json:"json,omitempty"`
	Top  map[string]StashVal `json:"tomlTop,omitempty"`
	Keys map[string]StashVal `json:"tomlKeys,omitempty"` // "table.key"
}

// ToolStash is a tool's whole pre-charon state, keyed by file path.
type ToolStash struct {
	Files map[string]FileStash `json:"files"`
}

// CaptureStash records what every owned path of t holds right now, before
// charon writes to it. A file that cannot be read is stashed as absent
// everywhere: off still removes charon's entries, restoring nothing there.
func (t *Tool) CaptureStash() ToolStash {
	st := ToolStash{Files: map[string]FileStash{}}
	for _, of := range t.Owned {
		fs := FileStash{}
		if len(of.JSON) > 0 {
			fs.JSON = map[string]StashVal{}
			for _, p := range of.JSON {
				if raw, ok := edit.GetJSONRaw(of.Path, p); ok {
					fs.JSON[p] = StashVal{Raw: raw, Prev: edit.PrevJSONKey(of.Path, p)}
				} else {
					fs.JSON[p] = StashVal{Absent: true}
				}
			}
		}
		if len(of.TOMLTop) > 0 {
			fs.Top = map[string]StashVal{}
			for _, k := range of.TOMLTop {
				if raw, ok := edit.GetTOMLTopRaw(of.Path, k); ok {
					fs.Top[k] = StashVal{Raw: raw}
				} else {
					fs.Top[k] = StashVal{Absent: true}
				}
			}
		}
		if len(of.TOMLKeys) > 0 {
			fs.Keys = map[string]StashVal{}
			for _, tk := range of.TOMLKeys {
				table, key, _ := strings.Cut(tk, ".")
				vals, err := edit.GetTOMLTable(of.Path, table)
				if err != nil || key == "" {
					fs.Keys[tk] = StashVal{Absent: true}
					continue
				}
				if v, ok := vals[key]; ok {
					// GetTOMLTable decodes strings and keeps other scalars as
					// their literal text; both restore as a string value.
					fs.Keys[tk] = StashVal{Raw: v}
				} else {
					fs.Keys[tk] = StashVal{Absent: true}
				}
			}
		}
		st.Files[of.Path] = fs
	}
	return st
}

// Off takes every owned entry of t back out: charon's keys, tables and files
// are removed, and paths that held something before charon (in st) are put
// back as they were written. Stale beyond restore — Claude Code's
// customApiKeyResponses keeps an approved id of a key charon used, and a Pi
// legacy extension charon migrated away stays gone — both are harmless.
// An empty st removes without restoring (the pre-feature story).
func (t *Tool) Off(st ToolStash) error {
	for _, of := range t.Owned {
		fs := st.Files[of.Path]
		if len(of.JSON) > 0 || len(of.JSONPrefixes) > 0 {
			restored := map[string]bool{}
			var restore []string
			for _, p := range of.JSON {
				if v, ok := fs.JSON[p]; ok && !v.Absent {
					restore = append(restore, p)
					restored[p] = true
				}
			}
			var dels []string
			for _, p := range of.JSON {
				if !restored[p] {
					dels = append(dels, p)
				}
			}
			// Restore first — a key that still exists is replaced in place,
			// one that apply removed is inserted after its stashed previous
			// sibling, so it goes back where it was — then remove what stays
			// charon's. The Prev chain also orders siblings restored
			// together.
			if len(restore) > 0 {
				if _, err := os.Stat(of.Path); err == nil {
					for _, p := range orderJSONByPrev(restore, fs.JSON) {
						v := fs.JSON[p]
						if err := edit.SetJSONAfter(of.Path, prevPath(p, v.Prev), p, json.RawMessage(v.Raw)); err != nil {
							return err
						}
					}
				}
			}
			if len(dels) > 0 {
				if err := edit.DelJSON(of.Path, dels...); err != nil {
					return err
				}
			}
			// Prefix-owned keys are charon's generated entries (a tool that
			// registers several endpoints writes one per endpoint). They are
			// deleted, never restored; a key that was just restored keeps its
			// pre-charon value.
			for _, prefix := range of.JSONPrefixes {
				var extra []string
				for _, p := range jsonKeysUnder(of.Path, prefix) {
					if !restored[p] {
						extra = append(extra, p)
					}
				}
				if len(extra) > 0 {
					if err := edit.DelJSON(of.Path, extra...); err != nil {
						return err
					}
				}
			}
		}
		if err := t.offTOML(of, fs); err != nil {
			return err
		}
	}
	for _, created := range t.Created {
		if err := os.Remove(created); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("removing %s: %w", created, err)
		}
	}
	return nil
}

func (t *Tool) offTOML(of OwnedFile, fs FileStash) error {
	if len(of.TOMLTop) == 0 && len(of.TOMLKeys) == 0 && len(of.TOMLTables) == 0 && len(of.Prefixes) == 0 {
		return nil
	}
	// Exact tables and every table under a prefix go first, whole.
	if len(of.TOMLTables) > 0 || len(of.Prefixes) > 0 {
		names, err := edit.TOMLTables(of.Path)
		if err != nil {
			return err
		}
		for _, name := range names {
			owned := false
			for _, exact := range of.TOMLTables {
				if name == exact {
					owned = true
				}
			}
			for _, p := range of.Prefixes {
				if strings.HasPrefix(name, p) || strings.HasPrefix(edit.BareTOMLName(name), p) {
					owned = true
				}
			}
			if owned {
				if err := edit.DelTOMLTable(of.Path, name); err != nil {
					return err
				}
			}
		}
	}
	// Table keys: restore in place, then remove what stays charon's.
	var restoreKeys []struct{ table, key, val string }
	for _, tk := range of.TOMLKeys {
		table, key, _ := strings.Cut(tk, ".")
		if v, ok := fs.Keys[tk]; ok && !v.Absent {
			restoreKeys = append(restoreKeys, struct{ table, key, val string }{table, key, v.Raw})
		} else if err := edit.DelTOMLKey(of.Path, table, key); err != nil {
			return err
		}
	}
	for _, r := range restoreKeys {
		if err := edit.SetTOMLKey(of.Path, r.table, r.key, r.val); err != nil {
			return err
		}
	}
	// Top keys: restore in place (spacing and trailing comment kept), then
	// remove what stays charon's.
	var restoreTop []edit.KV
	for _, k := range of.TOMLTop {
		if v, ok := fs.Top[k]; ok && !v.Absent {
			restoreTop = append(restoreTop, edit.KV{Path: k, Value: edit.Raw(v.Raw)})
		}
	}
	if len(restoreTop) > 0 {
		if _, err := os.Stat(of.Path); err == nil {
			if err := edit.SetTOMLTopPreserving(of.Path, restoreTop...); err != nil {
				return err
			}
		}
	}
	var delTop []string
	for _, k := range of.TOMLTop {
		if v, ok := fs.Top[k]; !ok || v.Absent {
			delTop = append(delTop, k)
		}
	}
	if len(delTop) > 0 {
		if err := edit.DelTOMLTop(of.Path, delTop...); err != nil {
			return err
		}
	}
	return nil
}

// prevPath is the dot path of key's stashed previous sibling: the parent
// path of key joined with prev, "" when the key was first.
func prevPath(key, prev string) string {
	if prev == "" {
		return ""
	}
	if i := strings.LastIndex(key, "."); i >= 0 {
		return key[:i+1] + prev
	}
	return prev
}

// orderJSONByPrev sorts paths by their stashed Prev chain, so a run of
// restored siblings is inserted in the order the file had. Paths not in vals
// keep their given order after the chained ones.
func orderJSONByPrev(paths []string, vals map[string]StashVal) []string {
	known := map[string]bool{}
	for _, p := range paths {
		known[p] = true
	}
	var out []string
	done := map[string]bool{}
	for round := 0; round < len(paths); round++ {
		progressed := false
		for _, p := range paths {
			if done[p] {
				continue
			}
			if v := vals[p]; v.Prev != "" && known[v.Prev] && !done[v.Prev] {
				continue // its predecessor is still to come
			}
			out, done[p], progressed = append(out, p), true, true
		}
		if !progressed {
			for _, p := range paths {
				if !done[p] {
					out, done[p] = append(out, p), true
				}
			}
			break
		}
	}
	return out
}

// jsonKeysUnder lists the full dot paths prefix owns in path: the leaf itself
// plus every key that extends it with "-". The names come from the file's
// parent object, so only entries that are really there are returned; they are
// sorted to keep the delete deterministic. An unreadable parent or one that
// is not a JSON object yields nothing.
func jsonKeysUnder(path, prefix string) []string {
	i := strings.LastIndex(prefix, ".")
	if i < 0 {
		return nil
	}
	parent, leaf := prefix[:i], prefix[i+1:]
	raw, ok := edit.GetJSONRaw(path, parent)
	if !ok {
		return nil
	}
	var obj map[string]json.RawMessage
	if json.Unmarshal([]byte(raw), &obj) != nil {
		return nil
	}
	keys := make([]string, 0, len(obj))
	for key := range obj {
		if key == leaf || strings.HasPrefix(key, leaf+"-") {
			keys = append(keys, parent+"."+key)
		}
	}
	sort.Strings(keys)
	return keys
}
