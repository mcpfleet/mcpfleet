// Package vault provides secure local storage for MCP server secrets.
// Secrets are stored in an encrypted JSON file using AES-256-GCM.
// The encryption key is derived from the user's OS keyring via go-keyring.
package vault

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/zalando/go-keyring"
)

const (
	keyringService = "mcpfleet"
	keyringUser    = "vault-key"
)

var (
	// ErrKeyMissing means the OS keyring holds no vault key. The vault never
	// creates a new key while a vault file exists, because that file could
	// then never be decrypted again.
	ErrKeyMissing = errors.New("key missing from the OS keyring")
	// ErrKeyringUnavailable means the OS keyring could not be read (for
	// example a locked keychain or no D-Bus session). The vault never creates
	// a new key in that case.
	ErrKeyringUnavailable = errors.New("cannot read the vault key from the OS keyring")
)

// keyStore is the slice of the OS keyring the vault uses. Tests swap it via
// export_test.go to simulate selective keyring failures.
type keyStore interface {
	Get(service, user string) (string, error)
	Set(service, user, secret string) error
}

type osKeyring struct{}

func (osKeyring) Get(service, user string) (string, error) { return keyring.Get(service, user) }
func (osKeyring) Set(service, user, secret string) error   { return keyring.Set(service, user, secret) }

var keys keyStore = osKeyring{}

// Vault holds decrypted secrets in memory and persists them encrypted on disk.
type Vault struct {
	path    string
	secrets map[string]string // key -> secret value
	key     []byte            // AES key, nil until first loaded or created
}

// Open loads (or creates) the vault at the default path.
func Open() (*Vault, error) {
	path, err := defaultPath()
	if err != nil {
		return nil, err
	}
	return OpenAt(path)
}

// OpenAt loads (or creates) the vault at a specific path.
func OpenAt(path string) (*Vault, error) {
	v := &Vault{path: path, secrets: make(map[string]string)}

	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		// New vault — nothing to load yet.
		return v, nil
	}

	key, err := loadKey()
	if errors.Is(err, ErrKeyMissing) {
		return nil, missingKeyError(path)
	}
	if err != nil {
		return nil, err
	}
	v.key = key

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("vault: read: %w", err)
	}

	plain, err := decrypt(key, data)
	if err != nil {
		return nil, fmt.Errorf("vault: decrypt: %w", err)
	}

	if err := json.Unmarshal(plain, &v.secrets); err != nil {
		return nil, fmt.Errorf("vault: parse: %w", err)
	}
	return v, nil
}

// Get returns a stored secret by key.
func (v *Vault) Get(key string) (string, bool) {
	val, ok := v.secrets[key]
	return val, ok
}

// Set stores or updates a secret.
func (v *Vault) Set(key, value string) {
	v.secrets[key] = value
}

// Delete removes a secret.
func (v *Vault) Delete(key string) {
	delete(v.secrets, key)
}

// Save encrypts and writes the vault to disk.
func (v *Vault) Save() error {
	if err := os.MkdirAll(filepath.Dir(v.path), 0o700); err != nil {
		return fmt.Errorf("vault: mkdir: %w", err)
	}

	key, err := v.keyForSave()
	if err != nil {
		return err
	}

	plain, err := json.Marshal(v.secrets)
	if err != nil {
		return fmt.Errorf("vault: marshal: %w", err)
	}

	cipher, err := encrypt(key, plain)
	if err != nil {
		return fmt.Errorf("vault: encrypt: %w", err)
	}

	return os.WriteFile(v.path, cipher, 0o600)
}

// Resolve substitutes ${VAR} placeholders in env maps with vault values.
func (v *Vault) Resolve(env map[string]string) map[string]string {
	if len(env) == 0 {
		return env
	}
	out := make(map[string]string, len(env))
	for k, val := range env {
		if len(val) > 3 && val[0] == '$' && val[1] == '{' && val[len(val)-1] == '}' {
			ref := val[2 : len(val)-1]
			if secret, ok := v.secrets[ref]; ok {
				val = secret
			}
		}
		out[k] = val
	}
	return out
}

// --- helpers ---

func defaultPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "mcpfleet", "vault.enc"), nil
}

// keyForSave returns the vault key, creating one only when the keyring has
// none and no vault file exists yet (a first save on a fresh machine).
func (v *Vault) keyForSave() ([]byte, error) {
	if v.key != nil {
		return v.key, nil
	}
	key, err := loadKey()
	if errors.Is(err, ErrKeyMissing) {
		_, statErr := os.Stat(v.path)
		switch {
		case statErr == nil:
			return nil, missingKeyError(v.path)
		case !errors.Is(statErr, os.ErrNotExist):
			return nil, fmt.Errorf("vault: stat: %w", statErr)
		}
		key, err = createKey()
	}
	if err != nil {
		return nil, err
	}
	v.key = key
	return key, nil
}

// loadKey reads the vault key from the keyring. It never creates one: a
// missing key is ErrKeyMissing, any other keyring failure wraps
// ErrKeyringUnavailable.
func loadKey() ([]byte, error) {
	secret, err := keys.Get(keyringService, keyringUser)
	switch {
	case err == nil:
		h := sha256.Sum256([]byte(secret))
		return h[:], nil
	case errors.Is(err, keyring.ErrNotFound):
		return nil, ErrKeyMissing
	default:
		return nil, fmt.Errorf("vault: %w: %w; unlock the keyring (macOS Keychain, GNOME Keyring/KWallet over D-Bus, Windows Credential Manager) and retry",
			ErrKeyringUnavailable, err)
	}
}

// createKey generates a new vault key and stores it in the keyring.
func createKey() ([]byte, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return nil, fmt.Errorf("vault: generate key: %w", err)
	}
	secret := fmt.Sprintf("%x", buf)
	if err := keys.Set(keyringService, keyringUser, secret); err != nil {
		return nil, fmt.Errorf("vault: store key in keyring: %w", err)
	}
	h := sha256.Sum256([]byte(secret))
	return h[:], nil
}

func missingKeyError(path string) error {
	return fmt.Errorf("vault: %w: %s exists but its key (service %q, account %q) is gone; restore the key, or move the file aside to start an empty vault",
		ErrKeyMissing, path, keyringService, keyringUser)
}

func encrypt(key, plaintext []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	return gcm.Seal(nonce, nonce, plaintext, nil), nil
}

func decrypt(key, ciphertext []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nsize := gcm.NonceSize()
	if len(ciphertext) < nsize {
		return nil, fmt.Errorf("ciphertext too short")
	}
	nonce, ct := ciphertext[:nsize], ciphertext[nsize:]
	return gcm.Open(nil, nonce, ct, nil)
}
