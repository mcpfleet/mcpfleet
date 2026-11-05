package zed_test

import (
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mcpfleet/mcpfleet/internal/adapters/zed"
	"github.com/mcpfleet/mcpfleet/internal/schema"
)

// isolateHome points HOME and APPDATA at a fresh temp dir without creating
// a settings file.
func isolateHome(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)
	t.Setenv("APPDATA", dir)
	return dir
}

func contextServers(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	cs, _ := standardize(t, data)["context_servers"].(map[string]any)
	return cs
}

func TestName(t *testing.T) {
	assert.Equal(t, "zed", zed.New().Name())
}

func TestConfigPath(t *testing.T) {
	dir := isolateHome(t)
	want := filepath.Join(dir, ".config", "zed", "settings.json")
	if runtime.GOOS == "windows" {
		want = filepath.Join(dir, "Zed", "settings.json")
	}
	assert.Equal(t, want, zed.New().ConfigPath())
}

func TestApply_EntryShape(t *testing.T) {
	isolateHome(t)
	a := zed.New()
	require.NoError(t, a.Apply([]schema.Server{{
		Name:    "my-server",
		Command: "npx",
		Args:    []string{"-y", "pkg"},
		Env:     map[string]schema.EnvVar{"K": {Value: "v"}},
	}}))

	assert.Equal(t, map[string]any{
		"command": map[string]any{
			"path": "npx",
			"args": []any{"-y", "pkg"},
			"env":  map[string]any{"K": "v"},
		},
	}, contextServers(t, a.ConfigPath())["my-server"])
}

func TestApply_OmitsEmptyArgsAndEnv(t *testing.T) {
	isolateHome(t)
	a := zed.New()
	require.NoError(t, a.Apply([]schema.Server{{Name: "bare", Command: "run"}}))
	assert.Equal(t, map[string]any{"command": map[string]any{"path": "run"}},
		contextServers(t, a.ConfigPath())["bare"])
}

func TestApply_MergesSuccessiveApplies(t *testing.T) {
	isolateHome(t)
	a := zed.New()
	require.NoError(t, a.Apply([]schema.Server{{Name: "server-a", Command: "cmd-a"}}))
	require.NoError(t, a.Apply([]schema.Server{{Name: "server-b", Command: "cmd-b"}}))

	cs := contextServers(t, a.ConfigPath())
	assert.Contains(t, cs, "server-a")
	assert.Contains(t, cs, "server-b")
}

func TestApply_KeepsOtherSettings(t *testing.T) {
	path := setup(t, `{"theme":"One Dark","vim_mode":true}`)
	require.NoError(t, zed.New().Apply([]schema.Server{{Name: "s", Command: "run"}}))

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	m := standardize(t, data)
	assert.Equal(t, "One Dark", m["theme"])
	assert.Equal(t, true, m["vim_mode"])
	assert.Contains(t, m["context_servers"], "s")
}

func TestApply_NameWithSlashAndTilde(t *testing.T) {
	// Names are JSON Pointer–escaped when patching; they must round-trip.
	isolateHome(t)
	a := zed.New()
	require.NoError(t, a.Apply([]schema.Server{{Name: "org/srv~1", Command: "run"}}))
	assert.Contains(t, contextServers(t, a.ConfigPath()), "org/srv~1")
}

func TestList_ReturnsAppliedServers(t *testing.T) {
	isolateHome(t)
	a := zed.New()
	require.NoError(t, a.Apply([]schema.Server{
		{Name: "a", Command: "cmd-a", Args: []string{"1"}, Env: map[string]schema.EnvVar{"K": {Value: "v"}}},
		{Name: "b", Command: "cmd-b"},
	}))

	got, err := a.List()
	require.NoError(t, err)
	sort.Slice(got, func(i, j int) bool { return got[i].Name < got[j].Name })
	assert.Equal(t, []schema.Server{
		{Name: "a", Command: "cmd-a", Args: []string{"1"}, Env: map[string]schema.EnvVar{"K": {Value: "v"}}},
		{Name: "b", Command: "cmd-b", Env: map[string]schema.EnvVar{}},
	}, got)
}

func TestList_MissingFile(t *testing.T) {
	isolateHome(t)
	_, err := zed.New().List()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "zed: read config")
}

func TestList_ReadsJSONCSettings(t *testing.T) {
	setup(t, jsoncSettings)
	got, err := zed.New().List()
	require.NoError(t, err)
	commands := map[string]string{}
	for _, s := range got {
		commands[s.Name] = s.Command
	}
	assert.Equal(t, map[string]string{"old": "a", "replace-me": "stale"}, commands)
}
