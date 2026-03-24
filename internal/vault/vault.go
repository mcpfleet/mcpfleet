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

// Vault holds decrypted secrets in memory and persists them encrypted on disk.
type Vault struct {
	path    string
	secrets map[string]string // key -> secret value
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

	key, err := getOrCreateKey()
	if err != nil {
		return nil, fmt.Errorf("vault: get key: %w", err)
	}

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

	key, err := getOrCreateKey()
	if err != nil {
		return fmt.Errorf("vault: get key: %w", err)
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

func getOrCreateKey() ([]byte, error) {
	secret, err := keyring.Get(keyringService, keyringUser)
	if err == nil {
		h := sha256.Sum256([]byte(secret))
		return h[:], nil
	}

	// Key doesn't exist yet — generate a new one.
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return nil, fmt.Errorf("generate key: %w", err)
	}
	newSecret := fmt.Sprintf("%x", buf)
	if err := keyring.Set(keyringService, keyringUser, newSecret); err != nil {
		return nil, fmt.Errorf("store key in keyring: %w", err)
	}
	h := sha256.Sum256([]byte(newSecret))
	return h[:], nil
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
