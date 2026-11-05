package crush_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mcpfleet/mcpfleet/internal/adapters/crush"
	"github.com/mcpfleet/mcpfleet/internal/schema"
)

// isolate points HOME and XDG_CONFIG_HOME at a fresh temp dir and returns it.
// CRUSH_GLOBAL_CONFIG is cleared so a developer's own setting cannot leak in.
func isolate(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "xdg"))
	t.Setenv("CRUSH_GLOBAL_CONFIG", "")
	return dir
}

// servers reads the "mcp" object of the written config.
func servers(t *testing.T, a *crush.Adapter) map[string]map[string]any {
	t.Helper()
	data, err := os.ReadFile(a.ConfigPath())
	require.NoError(t, err)
	var cfg struct {
		MCP map[string]map[string]any `json:"mcp"`
	}
	require.NoError(t, json.Unmarshal(data, &cfg), "config is not valid JSON:\n%s", data)
	return cfg.MCP
}

func TestName(t *testing.T) {
	assert.Equal(t, "crush", crush.New().Name())
}

// ConfigPath is Crush's global crush.json; TestConfigPath_PerEnv covers every
// lookup step with a fake environment, these two read the real one.
func TestConfigPath_XDG(t *testing.T) {
	dir := isolate(t)
	assert.Equal(t, filepath.Join(dir, "xdg", "crush", "crush.json"), crush.New().ConfigPath())
}

func TestConfigPath_HomeFallback(t *testing.T) {
	dir := isolate(t)
	t.Setenv("XDG_CONFIG_HOME", "")
	assert.Equal(t, filepath.Join(dir, ".config", "crush", "crush.json"), crush.New().ConfigPath())
}

func TestConfigPath_CrushGlobalConfig(t *testing.T) {
	dir := isolate(t)
	t.Setenv("CRUSH_GLOBAL_CONFIG", filepath.Join(dir, "global"))
	assert.Equal(t, filepath.Join(dir, "global", "crush.json"), crush.New().ConfigPath())
}

func TestConfigPath_UnderHome(t *testing.T) {
	dir := isolate(t)
	assert.True(t, strings.HasPrefix(crush.New().ConfigPath(), dir), "ConfigPath %q not under %q", crush.New().ConfigPath(), dir)
}

func TestApply_WritesMcpKeyWithStdioType(t *testing.T) {
	isolate(t)
	a := crush.New()
	require.NoError(t, a.Apply([]schema.Server{{
		Name:    "my-server",
		Command: "npx",
		Args:    []string{"-y", "my-mcp-server"},
		Env:     map[string]schema.EnvVar{"K": {Value: "v"}},
	}}))

	got := servers(t, a)["my-server"]
	assert.Equal(t, map[string]any{
		"type":    "stdio",
		"command": "npx",
		"args":    []any{"-y", "my-mcp-server"},
		"env":     map[string]any{"K": "v"},
	}, got)
}

func TestApply_OmitsEmptyArgsAndEnv(t *testing.T) {
	isolate(t)
	a := crush.New()
	require.NoError(t, a.Apply([]schema.Server{{Name: "bare", Command: "run"}}))
	assert.Equal(t, map[string]any{"type": "stdio", "command": "run"}, servers(t, a)["bare"])
}

func TestApply_SecretRefWritesEmptyValue(t *testing.T) {
	// Apply writes EnvVar.Value only: cmd/apply resolves secret references
	// from the vault into Value before calling Apply.
	isolate(t)
	a := crush.New()
	require.NoError(t, a.Apply([]schema.Server{{
		Name: "s", Command: "run",
		Env: map[string]schema.EnvVar{"TOKEN": {Secret: "tok"}},
	}}))
	assert.Equal(t, map[string]any{"TOKEN": ""}, servers(t, a)["s"]["env"])
}

func TestApply_MergesSuccessiveApplies(t *testing.T) {
	isolate(t)
	a := crush.New()
	require.NoError(t, a.Apply([]schema.Server{{Name: "server-a", Command: "cmd-a"}}))
	require.NoError(t, a.Apply([]schema.Server{{Name: "server-b", Command: "cmd-b"}}))

	got := servers(t, a)
	assert.Equal(t, "cmd-a", got["server-a"]["command"])
	assert.Equal(t, "cmd-b", got["server-b"]["command"])
}

func TestApply_OverwritesSameName(t *testing.T) {
	isolate(t)
	a := crush.New()
	require.NoError(t, a.Apply([]schema.Server{{Name: "s", Command: "old", Args: []string{"x"}}}))
	require.NoError(t, a.Apply([]schema.Server{{Name: "s", Command: "new"}}))
	assert.Equal(t, map[string]any{"type": "stdio", "command": "new"}, servers(t, a)["s"])
}

func TestList_ReturnsAppliedServers(t *testing.T) {
	isolate(t)
	a := crush.New()
	in := []schema.Server{
		{Name: "a", Command: "cmd-a", Args: []string{"1"}, Env: map[string]schema.EnvVar{"K": {Value: "v"}}},
		{Name: "b", Command: "cmd-b"},
	}
	require.NoError(t, a.Apply(in))

	got, err := a.List()
	require.NoError(t, err)
	sort.Slice(got, func(i, j int) bool { return got[i].Name < got[j].Name })
	assert.Equal(t, []schema.Server{
		{Name: "a", Command: "cmd-a", Args: []string{"1"}, Env: map[string]schema.EnvVar{"K": {Value: "v"}}},
		{Name: "b", Command: "cmd-b", Env: map[string]schema.EnvVar{}},
	}, got)
}

func TestList_MissingFile(t *testing.T) {
	isolate(t)
	_, err := crush.New().List()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "crush: read config")
}

func TestList_InvalidJSON(t *testing.T) {
	isolate(t)
	a := crush.New()
	require.NoError(t, os.MkdirAll(filepath.Dir(a.ConfigPath()), 0o755))
	require.NoError(t, os.WriteFile(a.ConfigPath(), []byte(`{"mcp":`), 0o644))
	_, err := a.List()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "crush: parse config")
}

func TestList_ReadsMcpKeyOnly(t *testing.T) {
	isolate(t)
	a := crush.New()
	require.NoError(t, os.MkdirAll(filepath.Dir(a.ConfigPath()), 0o755))
	require.NoError(t, os.WriteFile(a.ConfigPath(), []byte(`{
  "mcp": {"real": {"type": "stdio", "command": "run"}},
  "mcpServers": {"stale": {"command": "old"}}
}`), 0o644))

	got, err := a.List()
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "real", got[0].Name)
	assert.Equal(t, "run", got[0].Command)
}

func TestApply_PreservesOtherKeys(t *testing.T) {
	isolate(t)
	a := crush.New()
	require.NoError(t, os.MkdirAll(filepath.Dir(a.ConfigPath()), 0o755))
	require.NoError(t, os.WriteFile(a.ConfigPath(), []byte(`{
  "$schema": "https://charm.land/crush.json",
  "providers": {"openai": {"api_key": "$OPENAI_API_KEY"}},
  "options": {"debug": true},
  "mcp": {"manual": {"type": "http", "url": "https://example.com/mcp", "timeout": 30}}
}`), 0o644))

	require.NoError(t, a.Apply([]schema.Server{{Name: "new", Command: "run"}}))

	data, err := os.ReadFile(a.ConfigPath())
	require.NoError(t, err)
	var cfg map[string]any
	require.NoError(t, json.Unmarshal(data, &cfg))
	assert.Equal(t, "https://charm.land/crush.json", cfg["$schema"])
	assert.Equal(t, map[string]any{"openai": map[string]any{"api_key": "$OPENAI_API_KEY"}}, cfg["providers"])
	assert.Equal(t, map[string]any{"debug": true}, cfg["options"])
	assert.Equal(t, map[string]any{"type": "http", "url": "https://example.com/mcp", "timeout": float64(30)}, servers(t, a)["manual"])
	assert.Equal(t, map[string]any{"type": "stdio", "command": "run"}, servers(t, a)["new"])
}
