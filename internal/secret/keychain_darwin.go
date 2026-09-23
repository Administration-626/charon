//go:build darwin

package secret

import (
	"bytes"
	"errors"
	"os/exec"
	"strings"
)

// ErrKeychainMissing is returned when the requested item does not exist.
var ErrKeychainMissing = errors.New("keychain item not found")

// KeychainRead returns the value under service, or ErrKeychainMissing if absent.
func KeychainRead(service string) (string, error) {
	cmd := exec.Command("security", "find-generic-password", "-s", service, "-w")
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		if strings.Contains(errb.String(), "could not be found") {
			return "", ErrKeychainMissing
		}
		return "", err
	}
	return strings.TrimRight(out.String(), "\n"), nil
}
