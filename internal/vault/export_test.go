package vault

import "testing"

// KeyStore mirrors the package's keyStore seam for external tests.
type KeyStore = keyStore

// SetKeyStore replaces the keyring backend for the duration of the test.
func SetKeyStore(t *testing.T, ks KeyStore) {
	t.Helper()
	prev := keys
	keys = ks
	t.Cleanup(func() { keys = prev })
}
