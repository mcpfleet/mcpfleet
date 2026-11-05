package windsurf_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mcpfleet/mcpfleet/internal/adapters/windsurf"
	"github.com/mcpfleet/mcpfleet/internal/schema"
)

// isolate points HOME and APPDATA at a fresh temp dir and returns it.
func isolate(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)
	t.Setenv("APPDATA", dir)
	return dir
}

// servers reads the "mcpServers" object of the written config.
func servers(t *testing.T, a *windsurf.Adapter) map[string]map[string]any {
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
	assert.Equal(t, "windsurf", windsurf.New().Name())
}

// TestConfigPath_NonWindows pins ~/.codeium/windsurf/mcp_config.json. On
// Windows the adapter uses %APPDATA%\Windsurf, which NER-500 changes, so the
// Windows path is deliberately not asserted here; update it in NER-500.
func TestConfigPath_NonWindows(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows path is changed by NER-500")
	}
	dir := isolate(t)
	assert.Equal(t, filepath.Join(dir, ".codeium", "windsurf", "mcp_config.json"), windsurf.New().ConfigPath())
}

func TestConfigPath_UnderHome(t *testing.T) {
	dir := isolate(t)
	assert.True(t, strings.HasPrefix(windsurf.New().ConfigPath(), dir), "ConfigPath %q not under %q", windsurf.New().ConfigPath(), dir)
}

func TestApply_CreatesConfigWithEntryShape(t *testing.T) {
	isolate(t)
	a := windsurf.New()
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
	a := windsurf.New()
	require.NoError(t, a.Apply([]schema.Server{{Name: "bare", Command: "run"}}))
	assert.Equal(t, map[string]any{"command": "run"}, servers(t, a)["bare"])
}

func TestApply_SecretRefWritesEmptyValue(t *testing.T) {
	// Apply writes EnvVar.Value only: cmd/apply resolves secret references
	// from the vault into Value before calling Apply.
	isolate(t)
	a := windsurf.New()
	require.NoError(t, a.Apply([]schema.Server{{
		Name: "s", Command: "run",
		Env: map[string]schema.EnvVar{"TOKEN": {Secret: "tok"}},
	}}))
	assert.Equal(t, map[string]any{"TOKEN": ""}, servers(t, a)["s"]["env"])
}

func TestApply_MergesSuccessiveApplies(t *testing.T) {
	isolate(t)
	a := windsurf.New()
	require.NoError(t, a.Apply([]schema.Server{{Name: "server-a", Command: "cmd-a"}}))
	require.NoError(t, a.Apply([]schema.Server{{Name: "server-b", Command: "cmd-b"}}))

	got := servers(t, a)
	assert.Equal(t, "cmd-a", got["server-a"]["command"])
	assert.Equal(t, "cmd-b", got["server-b"]["command"])
}

func TestApply_OverwritesSameName(t *testing.T) {
	isolate(t)
	a := windsurf.New()
	require.NoError(t, a.Apply([]schema.Server{{Name: "s", Command: "old", Args: []string{"x"}}}))
	require.NoError(t, a.Apply([]schema.Server{{Name: "s", Command: "new"}}))
	assert.Equal(t, map[string]any{"command": "new"}, servers(t, a)["s"])
}

func TestList_ReturnsAppliedServers(t *testing.T) {
	isolate(t)
	a := windsurf.New()
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
	_, err := windsurf.New().List()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "windsurf: read config")
}

func TestList_InvalidJSON(t *testing.T) {
	isolate(t)
	a := windsurf.New()
	require.NoError(t, os.MkdirAll(filepath.Dir(a.ConfigPath()), 0o755))
	require.NoError(t, os.WriteFile(a.ConfigPath(), []byte(`{"mcpServers":`), 0o644))
	_, err := a.List()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "windsurf: parse config")
}
