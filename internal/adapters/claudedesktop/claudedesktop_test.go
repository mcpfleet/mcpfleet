package claudedesktop

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

func isolate(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("APPDATA", filepath.Join(home, "AppData"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	return home
}

func TestName(t *testing.T) {
	assert.Equal(t, "claude-desktop", New().Name())
}

func TestConfigPath_ClaudeDesktopConfig(t *testing.T) {
	home := isolate(t)
	var want string
	switch runtime.GOOS {
	case "windows":
		want = filepath.Join(home, "AppData", "Claude", "claude_desktop_config.json")
	case "darwin":
		want = filepath.Join(home, "Library", "Application Support", "Claude", "claude_desktop_config.json")
	default:
		want = filepath.Join(home, ".config", "Claude", "claude_desktop_config.json")
	}
	assert.Equal(t, want, New().ConfigPath())
}

func TestApply_WritesEntryWithoutType(t *testing.T) {
	isolate(t)
	a := New()
	servers := []schema.Server{{
		Name:    "fs",
		Command: "npx",
		Args:    []string{"-y", "pkg"},
		Env:     map[string]schema.EnvVar{"K": {Value: "v"}},
	}}
	require.NoError(t, a.Apply(servers))

	data, err := os.ReadFile(a.ConfigPath())
	require.NoError(t, err)
	var got struct {
		McpServers map[string]map[string]any `json:"mcpServers"`
	}
	require.NoError(t, json.Unmarshal(data, &got))
	assert.Equal(t, map[string]any{
		"command": "npx",
		"args":    []any{"-y", "pkg"},
		"env":     map[string]any{"K": "v"},
	}, got.McpServers["fs"])
}

func TestApplyThenList(t *testing.T) {
	isolate(t)
	a := New()
	require.NoError(t, a.Apply([]schema.Server{{Name: "s", Command: "run", Args: []string{"x"}}}))
	got, err := a.List()
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "s", got[0].Name)
	assert.Equal(t, "run", got[0].Command)
	assert.Equal(t, []string{"x"}, got[0].Args)
}

func TestList_MissingConfigIsError(t *testing.T) {
	isolate(t)
	_, err := New().List()
	assert.Error(t, err)
}

// TestApply_RefusesRelativeConfigPath pins that without any home or config
// directory in the environment Apply and List fail instead of resolving the
// config file against the working directory.
func TestApply_RefusesRelativeConfigPath(t *testing.T) {
	for _, k := range []string{"HOME", "USERPROFILE", "APPDATA", "XDG_CONFIG_HOME", "home"} {
		t.Setenv(k, "")
	}
	wd, err := os.Getwd()
	require.NoError(t, err)
	dir := t.TempDir()
	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() { _ = os.Chdir(wd) })

	err = New().Apply([]schema.Server{{Name: "fs", Command: "npx"}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot determine config directory")

	_, err = New().List()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot determine config directory")

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	assert.Empty(t, entries, "nothing may be written into the working directory")
}
