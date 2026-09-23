package artifact

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAtomicWriteReplacesFileWithRequestedMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credential.json")
	for _, value := range []string{"first", "second"} {
		if err := AtomicWrite(path, []byte(value), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "second" {
		t.Fatalf("content = %q, err=%v; want second", data, err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("mode = %o, want 0600", got)
	}
}
