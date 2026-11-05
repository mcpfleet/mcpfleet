package cmd

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zalando/go-keyring"

	"github.com/mcpfleet/mcpfleet/internal/vault"
)

// isolateVault gives the test a fresh HOME (so a fresh vault file) and an
// empty in-memory keyring.
func isolateVault(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	keyring.MockInit()
	t.Cleanup(keyring.MockInit)
}

// runSecret executes the root command with stdin and returns stdout, stderr
// and the error. Flags changed by the run are reset afterwards, because the
// global rootCmd keeps flag values between Execute calls.
func runSecret(t *testing.T, stdin io.Reader, args ...string) (string, string, error) {
	t.Helper()
	var out, errOut bytes.Buffer
	rootCmd.SetIn(stdin)
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&errOut)
	rootCmd.SetArgs(args)
	t.Cleanup(func() {
		rootCmd.SetIn(nil)
		rootCmd.SetOut(nil)
		rootCmd.SetErr(nil)
		rootCmd.SetArgs(nil)
		resetFlags(rootCmd)
	})
	err := rootCmd.Execute()
	return out.String(), errOut.String(), err
}

func resetFlags(c *cobra.Command) {
	c.Flags().VisitAll(func(f *pflag.Flag) {
		if f.Changed {
			_ = f.Value.Set(f.DefValue)
			f.Changed = false
		}
	})
	for _, sub := range c.Commands() {
		resetFlags(sub)
	}
}

func storedSecret(t *testing.T, name string) (string, bool) {
	t.Helper()
	v, err := vault.Open()
	require.NoError(t, err)
	return v.Get(name)
}

func TestSecretSet_StdinSavesValue(t *testing.T) {
	isolateVault(t)

	out, _, err := runSecret(t, strings.NewReader("tok-123\n"), "secret", "set", "github_token", "--stdin")
	require.NoError(t, err)
	assert.Contains(t, out, "Saved secret github_token")

	got, ok := storedSecret(t, "github_token")
	require.True(t, ok)
	assert.Equal(t, "tok-123", got, "one trailing newline is trimmed")
}

func TestSecretSet_StdinKeepsAllButOneTrailingNewline(t *testing.T) {
	isolateVault(t)

	_, _, err := runSecret(t, strings.NewReader("line1\nline2\n\r\n"), "secret", "set", "k", "--stdin")
	require.NoError(t, err)

	got, _ := storedSecret(t, "k")
	assert.Equal(t, "line1\nline2\n", got)
}

func TestSecretSet_OverwriteSaysUpdated(t *testing.T) {
	isolateVault(t)

	_, _, err := runSecret(t, strings.NewReader("one"), "secret", "set", "k", "--stdin")
	require.NoError(t, err)
	out, _, err := runSecret(t, strings.NewReader("two"), "secret", "set", "k", "--stdin")
	require.NoError(t, err)
	assert.Contains(t, out, "Updated secret k")

	got, _ := storedSecret(t, "k")
	assert.Equal(t, "two", got)
}

func TestSecretSet_ValueNeverFromArgument(t *testing.T) {
	isolateVault(t)

	_, _, err := runSecret(t, strings.NewReader(""), "secret", "set", "k", "the-value")
	require.Error(t, err)

	_, ok := storedSecret(t, "k")
	assert.False(t, ok)
}

func TestSecretSet_RefusesWithoutTerminalOrStdinFlag(t *testing.T) {
	isolateVault(t)

	_, _, err := runSecret(t, strings.NewReader("piped"), "secret", "set", "k")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--stdin")

	_, ok := storedSecret(t, "k")
	assert.False(t, ok)
}

func TestSecretSet_EmptyValueFails(t *testing.T) {
	isolateVault(t)

	_, _, err := runSecret(t, strings.NewReader("\n"), "secret", "set", "k", "--stdin")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty")

	_, ok := storedSecret(t, "k")
	assert.False(t, ok)
}

func TestSecretSet_InvalidNameFails(t *testing.T) {
	isolateVault(t)

	for _, name := range []string{"", " padded", "padded "} {
		_, _, err := runSecret(t, strings.NewReader("v"), "secret", "set", name, "--stdin")
		assert.Error(t, err, "name %q", name)
	}
}

func TestSecretList_NamesWithoutValues(t *testing.T) {
	isolateVault(t)

	for name, val := range map[string]string{"zeta": "z-value", "alpha": "a-value"} {
		_, _, err := runSecret(t, strings.NewReader(val), "secret", "set", name, "--stdin")
		require.NoError(t, err)
	}

	out, _, err := runSecret(t, nil, "secret", "list")
	require.NoError(t, err)
	assert.Equal(t, "alpha\nzeta\n", out)
	assert.NotContains(t, out, "value")
}

func TestSecretList_EmptyVaultHintOnStderr(t *testing.T) {
	isolateVault(t)

	out, errOut, err := runSecret(t, nil, "secret", "list")
	require.NoError(t, err)
	assert.Empty(t, out)
	assert.Contains(t, errOut, "secret set")
}

func TestSecretRm_DeletesSecret(t *testing.T) {
	isolateVault(t)

	_, _, err := runSecret(t, strings.NewReader("v"), "secret", "set", "gone", "--stdin")
	require.NoError(t, err)
	_, _, err = runSecret(t, strings.NewReader("v"), "secret", "set", "kept", "--stdin")
	require.NoError(t, err)

	out, _, err := runSecret(t, nil, "secret", "rm", "gone")
	require.NoError(t, err)
	assert.Contains(t, out, "Removed secret gone")

	_, ok := storedSecret(t, "gone")
	assert.False(t, ok)
	_, ok = storedSecret(t, "kept")
	assert.True(t, ok)
}

func TestSecretRm_MissingSecretFails(t *testing.T) {
	isolateVault(t)

	_, _, err := runSecret(t, nil, "secret", "rm", "nope")
	require.Error(t, err)
	assert.Contains(t, err.Error(), `secret "nope" not found`)
}
