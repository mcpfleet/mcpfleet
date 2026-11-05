package vault_test

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zalando/go-keyring"

	"github.com/mcpfleet/mcpfleet/internal/vault"
)

// TestMain swaps the OS keyring for go-keyring's in-memory mock, so the
// tests never read or write the developer's keyring.
func TestMain(m *testing.M) {
	keyring.MockInit()
	os.Exit(m.Run())
}

// freshKeyring gives the test an empty in-memory keyring (and so a new
// vault key on first use).
func freshKeyring(t *testing.T) {
	t.Helper()
	keyring.MockInit()
	t.Cleanup(keyring.MockInit)
}

func vaultPath(t *testing.T) string {
	return filepath.Join(t.TempDir(), "nested", "vault.enc")
}

func saved(t *testing.T, path string, secrets map[string]string) {
	t.Helper()
	v, err := vault.OpenAt(path)
	require.NoError(t, err)
	for k, val := range secrets {
		v.Set(k, val)
	}
	require.NoError(t, v.Save())
}

func TestOpenAt_MissingFileIsEmptyVault(t *testing.T) {
	v, err := vault.OpenAt(vaultPath(t))
	require.NoError(t, err)
	_, ok := v.Get("anything")
	assert.False(t, ok)
}

func TestSetGetDelete(t *testing.T) {
	v, err := vault.OpenAt(vaultPath(t))
	require.NoError(t, err)

	v.Set("k", "v1")
	got, ok := v.Get("k")
	assert.True(t, ok)
	assert.Equal(t, "v1", got)

	v.Set("k", "v2")
	got, _ = v.Get("k")
	assert.Equal(t, "v2", got)

	v.Delete("k")
	_, ok = v.Get("k")
	assert.False(t, ok)

	v.Delete("never-set") // no panic
}

func TestSave_CreatesParentDirAndPrivateFile(t *testing.T) {
	freshKeyring(t)
	path := vaultPath(t)
	saved(t, path, map[string]string{"k": "v"})

	st, err := os.Stat(path)
	require.NoError(t, err)
	if runtime.GOOS != "windows" {
		assert.Equal(t, os.FileMode(0o600), st.Mode().Perm())
		dst, err := os.Stat(filepath.Dir(path))
		require.NoError(t, err)
		assert.Equal(t, os.FileMode(0o700), dst.Mode().Perm())
	}
}

func TestSave_EncryptsOnDisk(t *testing.T) {
	freshKeyring(t)
	path := vaultPath(t)
	saved(t, path, map[string]string{"api_key": "super-secret-value"})

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.False(t, bytes.Contains(data, []byte("super-secret-value")), "plaintext secret on disk")
	assert.False(t, bytes.Contains(data, []byte("api_key")), "plaintext key name on disk")
}

func TestSave_UsesFreshNonce(t *testing.T) {
	freshKeyring(t)
	path := vaultPath(t)
	saved(t, path, map[string]string{"k": "v"})
	first, err := os.ReadFile(path)
	require.NoError(t, err)
	saved(t, path, nil)
	second, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.NotEqual(t, first, second, "same content encrypted twice gave identical bytes")
}

func TestSaveOpenAt_RoundTrip(t *testing.T) {
	freshKeyring(t)
	path := vaultPath(t)
	saved(t, path, map[string]string{"a": "1", "b": "two", "empty": ""})

	v, err := vault.OpenAt(path)
	require.NoError(t, err)
	for k, want := range map[string]string{"a": "1", "b": "two", "empty": ""} {
		got, ok := v.Get(k)
		assert.True(t, ok, k)
		assert.Equal(t, want, got, k)
	}
}

func TestOpenAt_TooShortFile(t *testing.T) {
	freshKeyring(t)
	path := vaultPath(t)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
	require.NoError(t, os.WriteFile(path, []byte("short"), 0o600))

	_, err := vault.OpenAt(path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "vault: decrypt")
}

func TestOpenAt_TamperedCiphertext(t *testing.T) {
	freshKeyring(t)
	path := vaultPath(t)
	saved(t, path, map[string]string{"k": "v"})

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	data[len(data)-1] ^= 0xff
	require.NoError(t, os.WriteFile(path, data, 0o600))

	_, err = vault.OpenAt(path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "vault: decrypt")
}

func TestOpenAt_DifferentKey(t *testing.T) {
	freshKeyring(t)
	path := vaultPath(t)
	saved(t, path, map[string]string{"k": "v"})

	keyring.MockInit() // empty keyring: OpenAt generates a new, different key
	_, err := vault.OpenAt(path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "vault: decrypt")
}

func TestOpenAt_MissingKeyIsNotRegenerated(t *testing.T) {
	freshKeyring(t)
	path := vaultPath(t)
	saved(t, path, map[string]string{"k": "v"})

	keyring.MockInit() // the vault file stays, its key is gone
	_, err := vault.OpenAt(path)
	require.Error(t, err)
	assert.True(t, errors.Is(err, vault.ErrKeyMissing), "got %v", err)
	assert.Contains(t, err.Error(), path)
	assert.Contains(t, err.Error(), "restore the key, or move the file aside")

	_, err = keyring.Get("mcpfleet", "vault-key")
	assert.ErrorIs(t, err, keyring.ErrNotFound, "OpenAt stored a new key")
}

// flakyKeyring fails every Get with a non-"not found" error and counts Set.
type flakyKeyring struct{ sets int }

func (f *flakyKeyring) Get(string, string) (string, error) {
	return "", errors.New("dbus: no session")
}

func (f *flakyKeyring) Set(string, string, string) error {
	f.sets++
	return nil
}

func TestOpenAt_KeyringErrorKeepsKey(t *testing.T) {
	freshKeyring(t)
	path := vaultPath(t)
	saved(t, path, map[string]string{"k": "v"})

	fk := &flakyKeyring{}
	vault.SetKeyStore(t, fk)
	_, err := vault.OpenAt(path)
	require.Error(t, err)
	assert.True(t, errors.Is(err, vault.ErrKeyringUnavailable), "got %v", err)
	assert.Contains(t, err.Error(), "dbus: no session")
	assert.Contains(t, err.Error(), "unlock the keyring")
	assert.Zero(t, fk.sets, "OpenAt overwrote the key")
}

func TestSave_KeyringErrorKeepsKey(t *testing.T) {
	freshKeyring(t)
	path := vaultPath(t)
	v, err := vault.OpenAt(path) // no file yet: no key needed
	require.NoError(t, err)
	saved(t, path, map[string]string{"k": "v"}) // file and key appear meanwhile

	fk := &flakyKeyring{}
	vault.SetKeyStore(t, fk)
	v.Set("other", "x")
	err = v.Save()
	require.Error(t, err)
	assert.True(t, errors.Is(err, vault.ErrKeyringUnavailable), "got %v", err)
	assert.Contains(t, err.Error(), "unlock the keyring")
	assert.Zero(t, fk.sets, "Save overwrote the key")
}

func TestSave_NewVaultKeyringErrorKeepsKey(t *testing.T) {
	fk := &flakyKeyring{}
	vault.SetKeyStore(t, fk)
	v, err := vault.OpenAt(vaultPath(t))
	require.NoError(t, err)
	v.Set("k", "v")
	err = v.Save()
	require.Error(t, err)
	assert.True(t, errors.Is(err, vault.ErrKeyringUnavailable), "got %v", err)
	assert.Zero(t, fk.sets, "Save created a key although the keyring could not be read")
}

func TestKeyringError(t *testing.T) {
	path := vaultPath(t)
	freshKeyring(t)
	saved(t, path, map[string]string{"k": "v"})

	keyring.MockInitWithError(errors.New("keyring locked"))

	_, err := vault.OpenAt(path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "vault: get key")

	v, err := vault.OpenAt(vaultPath(t)) // missing file needs no key
	require.NoError(t, err)
	v.Set("k", "v")
	err = v.Save()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "vault: get key")
	assert.Contains(t, err.Error(), "keyring locked")
}

func TestResolve(t *testing.T) {
	v, err := vault.OpenAt(vaultPath(t))
	require.NoError(t, err)
	v.Set("A", "secret-a")
	v.Set("", "empty-name")

	got := v.Resolve(map[string]string{
		"ref":      "${A}",
		"missing":  "${MISSING}",
		"literal":  "plain",
		"empty":    "${}",
		"partial":  "x${A}",
		"unclosed": "${A",
		"blank":    "",
	})
	assert.Equal(t, map[string]string{
		"ref":      "secret-a",
		"missing":  "${MISSING}",
		"literal":  "plain",
		"empty":    "${}",
		"partial":  "x${A}",
		"unclosed": "${A",
		"blank":    "",
	}, got)
}

func TestResolve_DoesNotModifyInput(t *testing.T) {
	v, err := vault.OpenAt(vaultPath(t))
	require.NoError(t, err)
	v.Set("A", "secret-a")

	in := map[string]string{"k": "${A}"}
	_ = v.Resolve(in)
	assert.Equal(t, map[string]string{"k": "${A}"}, in)
}

func TestResolve_NilAndEmpty(t *testing.T) {
	v, err := vault.OpenAt(vaultPath(t))
	require.NoError(t, err)

	assert.Nil(t, v.Resolve(nil))
	empty := map[string]string{}
	got := v.Resolve(empty)
	assert.NotNil(t, got)
	assert.Empty(t, got)
}

func TestOpen_DefaultPath(t *testing.T) {
	freshKeyring(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	v, err := vault.Open()
	require.NoError(t, err)
	v.Set("k", "v")
	require.NoError(t, v.Save())

	_, err = os.Stat(filepath.Join(home, ".config", "mcpfleet", "vault.enc"))
	require.NoError(t, err)

	v2, err := vault.Open()
	require.NoError(t, err)
	got, ok := v2.Get("k")
	assert.True(t, ok)
	assert.Equal(t, "v", got)
}
