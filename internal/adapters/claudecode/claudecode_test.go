package claudecode

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/mcpfleet/mcpfleet/internal/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// isolate points HOME at a fresh temp dir and clears CLAUDE_CONFIG_DIR so a
// test never touches the developer's real Claude Code config.
func isolate(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	return home
}

func TestConfigPath_IsClaudeCodeUserConfig(t *testing.T) {
	home := isolate(t)
	assert.Equal(t, filepath.Join(home, ".claude.json"), New().ConfigPath())
}

func TestConfigPath_HonoursClaudeConfigDir(t *testing.T) {
	isolate(t)
	dir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", dir)
	assert.Equal(t, filepath.Join(dir, ".claude.json"), New().ConfigPath())
}

func TestApply_PreservesClaudeCodeState(t *testing.T) {
	isolate(t)
	a := New()
	path := a.ConfigPath()
	orig := `{
  "oauthAccount": {"emailAddress": "me@example.com"},
  "userID": "abc123",
  "projects": {"/tmp/p": {"allowedTools": ["Bash"], "mcpServers": {}}},
  "mcpServers": {"remote": {"type": "http", "url": "https://x"}}
}`
	require.NoError(t, os.WriteFile(path, []byte(orig), 0o600))

	servers := []schema.Server{{
		Name:    "fs",
		Command: "npx",
		Args:    []string{"-y", "pkg"},
		Env:     map[string]schema.EnvVar{"K": {Value: "v"}},
	}}
	require.NoError(t, a.Apply(servers))

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	var got map[string]any
	require.NoError(t, json.Unmarshal(data, &got))

	assert.Equal(t, "abc123", got["userID"])
	assert.Equal(t, map[string]any{"emailAddress": "me@example.com"}, got["oauthAccount"])
	assert.Contains(t, got["projects"], "/tmp/p")

	mcp, _ := got["mcpServers"].(map[string]any)
	assert.Equal(t, map[string]any{"type": "http", "url": "https://x"}, mcp["remote"])
	assert.Equal(t, map[string]any{
		"type":    "stdio",
		"command": "npx",
		"args":    []any{"-y", "pkg"},
		"env":     map[string]any{"K": "v"},
	}, mcp["fs"])

	if runtime.GOOS != "windows" {
		st, err := os.Stat(path)
		require.NoError(t, err)
		assert.Equal(t, os.FileMode(0o600), st.Mode().Perm())
	}
}

func TestApply_NewFileIs0600(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permissions only")
	}
	isolate(t)
	a := New()
	require.NoError(t, a.Apply([]schema.Server{{Name: "fs", Command: "npx"}}))
	st, err := os.Stat(a.ConfigPath())
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), st.Mode().Perm())
}

func TestAdapter(t *testing.T) {
	t.Run("Name returns claude-code", func(t *testing.T) {
		assert.Equal(t, "claude-code", New().Name())
	})

	t.Run("Apply handles empty server list", func(t *testing.T) {
		isolate(t)
		a := New()
		require.NoError(t, a.Apply([]schema.Server{}))
		_, err := os.Stat(a.ConfigPath())
		require.NoError(t, err)
	})

	t.Run("Apply then List round-trips servers", func(t *testing.T) {
		isolate(t)
		a := New()
		servers := []schema.Server{{
			Name:    "test-server",
			Command: "npx",
			Args:    []string{"-y", "@test/server"},
		}}
		require.NoError(t, a.Apply(servers))

		got, err := a.List()
		require.NoError(t, err)
		require.Len(t, got, 1)
		assert.Equal(t, "test-server", got[0].Name)
		assert.Equal(t, "npx", got[0].Command)
		assert.Equal(t, []string{"-y", "@test/server"}, got[0].Args)
	})

	t.Run("List returns error for non-existent config", func(t *testing.T) {
		isolate(t)
		_, err := New().List()
		assert.Error(t, err)
	})
}
