package edit

import (
	"os"
	"path/filepath"
	"testing"
)

// readOnlyFile writes content to a file whose directory cannot be written, so
// reads succeed and the atomic write (a temp file in that directory) fails.
func readOnlyFile(t *testing.T, name, content string) string {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("a read-only directory is not enforced for root")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, name)
	write(t, path, content)
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
	return path
}

func TestJSONWriteErrors(t *testing.T) {
	edits := map[string]func(string) error{
		"SetJSON":      func(p string) error { return SetJSON(p, KV{Path: "b", Value: 2}) },
		"ApplyJSON":    func(p string) error { return ApplyJSON(p, []KV{{Path: "b", Value: 2}}, nil) },
		"DelJSON":      func(p string) error { return DelJSON(p, "a") },
		"SetJSONAfter": func(p string) error { return SetJSONAfter(p, "a", "b", 2) },
	}
	for name, edit := range edits {
		t.Run(name, func(t *testing.T) {
			path := readOnlyFile(t, "s.json", "{\"a\":1}\n")
			if err := edit(path); err == nil {
				t.Fatal("writing into a read-only directory must fail")
			}
		})
	}
}

func TestTOMLWriteErrors(t *testing.T) {
	edits := map[string]func(string) error{
		"SetTOMLTop":    func(p string) error { return SetTOMLTop(p, KV{Path: "b", Value: 2}) },
		"PatchTOMLTop":  func(p string) error { return PatchTOMLTop(p, []KV{{Path: "b", Value: 2}}, nil) },
		"DelTOMLTop":    func(p string) error { return DelTOMLTop(p, "a") },
		"SetTOMLKey":    func(p string) error { return SetTOMLKey(p, "t", "x", 2) },
		"DelTOMLKey":    func(p string) error { return DelTOMLKey(p, "t", "x") },
		"SetTOMLTable":  func(p string) error { return SetTOMLTable(p, "u", KV{Path: "y", Value: 1}) },
		"DelTOMLTable":  func(p string) error { return DelTOMLTable(p, "t") },
		"SetTOMLTables": func(p string) error { return SetTOMLTables(p, []string{"t"}, nil) },
	}
	for name, edit := range edits {
		t.Run(name, func(t *testing.T) {
			path := readOnlyFile(t, "config.toml", "a = 1\n\n[t]\nx = 1\n")
			if err := edit(path); err == nil {
				t.Fatal("writing into a read-only directory must fail")
			}
		})
	}
}

func TestWriteAtomicMkdirError(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "not-a-dir")
	write(t, file, "x")
	// The parent "directory" is a regular file, so creating it fails.
	if err := WriteAtomic(filepath.Join(file, "sub", "f"), []byte("x")); err == nil {
		t.Error("a file in place of a parent directory must fail")
	}
}

func TestTOMLReadErrors(t *testing.T) {
	path := filepath.Join(t.TempDir(), "as-dir.toml")
	if err := os.MkdirAll(path, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := TOMLTables(path); err == nil {
		t.Error("TOMLTables on an unreadable path must fail")
	}
	if _, err := GetTOMLTable(path, "t"); err == nil {
		t.Error("GetTOMLTable on an unreadable path must fail")
	}
	if _, ok := GetTOMLTopRaw(path, "a"); ok {
		t.Error("GetTOMLTopRaw on an unreadable path must report false")
	}
	if _, ok := GetTOMLTop(path, "a"); ok {
		t.Error("GetTOMLTop on an unreadable path must report false")
	}
	for name, edit := range map[string]func() error{
		"SetTOMLTop":   func() error { return SetTOMLTop(path, KV{Path: "a", Value: 1}) },
		"DelTOMLTop":   func() error { return DelTOMLTop(path, "a") },
		"SetTOMLKey":   func() error { return SetTOMLKey(path, "t", "x", 1) },
		"DelTOMLKey":   func() error { return DelTOMLKey(path, "t", "x") },
		"SetTOMLTable": func() error { return SetTOMLTable(path, "t", KV{Path: "x", Value: 1}) },
		"DelTOMLTable": func() error { return DelTOMLTable(path, "t") },
		"SetTOMLTables": func() error {
			return SetTOMLTables(path, []string{"t"}, []Table{{Name: "t"}})
		},
		"PatchTOMLTop": func() error { return PatchTOMLTop(path, []KV{{Path: "a", Value: 1}}, nil) },
	} {
		if err := edit(); err == nil {
			t.Errorf("%s on an unreadable path must fail", name)
		}
	}
}
