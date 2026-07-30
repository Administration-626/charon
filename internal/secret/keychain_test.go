package secret

import (
	"runtime"
	"testing"
)

// KeychainSupported must report true only on macOS, where the OS keychain (and
// thus real OAuth management for tools like Claude) is actually available.
func TestKeychainSupported(t *testing.T) {
	got := KeychainSupported()
	want := runtime.GOOS == "darwin"
	if got != want {
		t.Errorf("KeychainSupported() = %v on %s, want %v", got, runtime.GOOS, want)
	}
}
