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
func isolate(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "xdg"))
	return dir
}

// servers reads the "mcpServers" object of the written config.
func servers(t *testing.T, a *crush.Adapter) map[string]map[string]any {
	t.Helper()
	data, err := os.ReadFile(a.ConfigPath())
	require.NoError(t, err)
	var cfg struct {
		McpServers map[string]map[string]any `json:"mcpServers"`
	}
	require.NoError(t, json.Unmarshal(data, &cfg), "config is not valid JSON:\n%s", data)
	return cfg.McpServers
}

func TestName(t *testing.T) {
	assert.Equal(t, "crush", crush.New().Name())
}

// The two ConfigPath tests pin today's location (<XDG or ~/.config>/crush/mcp.json).
// NER-500 moves crush to its real config file: update these two tests there.
func TestConfigPath_XDG_NER500(t *testing.T) {
	dir := isolate(t)
	assert.Equal(t, filepath.Join(dir, "xdg", "crush", "mcp.json"), crush.New().ConfigPath())
}

func TestConfigPath_HomeFallback_NER500(t *testing.T) {
	dir := isolate(t)
	t.Setenv("XDG_CONFIG_HOME", "")
	assert.Equal(t, filepath.Join(dir, ".config", "crush", "mcp.json"), crush.New().ConfigPath())
}

func TestConfigPath_UnderHome(t *testing.T) {
	dir := isolate(t)
	assert.True(t, strings.HasPrefix(crush.New().ConfigPath(), dir), "ConfigPath %q not under %q", crush.New().ConfigPath(), dir)
}

func TestApply_CreatesConfigWithEntryShape(t *testing.T) {
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
		"command": "npx",
		"args":    []any{"-y", "my-mcp-server"},
		"env":     map[string]any{"K": "v"},
	}, got)
}

func TestApply_OmitsEmptyArgsAndEnv(t *testing.T) {
	isolate(t)
	a := crush.New()
	require.NoError(t, a.Apply([]schema.Server{{Name: "bare", Command: "run"}}))
	assert.Equal(t, map[string]any{"command": "run"}, servers(t, a)["bare"])
}

func TestApply_SecretRefWritesEmptyValue(t *testing.T) {
	// Apply writes EnvVar.Value only; secret references are not resolved here.
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
	assert.Equal(t, map[string]any{"command": "new"}, servers(t, a)["s"])
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
	require.NoError(t, os.WriteFile(a.ConfigPath(), []byte(`{"mcpServers":`), 0o644))
	_, err := a.List()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "crush: parse config")
}
