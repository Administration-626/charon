//go:build !darwin

package secret

import "errors"

// ErrKeychainMissing is returned when the requested item does not exist.
var ErrKeychainMissing = errors.New("keychain item not found")

// KeychainSupported reports that the OS keychain is available. Off macOS the
// keychain is unsupported, so KeychainRead/OAuth detection is inert and
// KeychainWrite/Delete are no-ops: charon must not pretend to manage real
// credentials it cannot touch.
func KeychainSupported() bool { return false }

// KeychainRead is unsupported off macOS; callers treat this as "absent".
func KeychainRead(_ string) (string, error) { return "", ErrKeychainMissing }

// KeychainWrite is a no-op off macOS.
func KeychainWrite(_, _, _ string) error { return nil }

// KeychainDelete is a no-op off macOS.
func KeychainDelete(_ string) error { return nil }
