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

	// Zed's flat stdio entry: "command" is the executable, args and env
	// sit beside it.
	assert.Equal(t, map[string]any{
		"command": "npx",
		"args":    []any{"-y", "pkg"},
		"env":     map[string]any{"K": "v"},
	}, contextServers(t, a.ConfigPath())["my-server"])
}

func TestApply_OmitsEmptyArgsAndEnv(t *testing.T) {
	isolateHome(t)
	a := zed.New()
	require.NoError(t, a.Apply([]schema.Server{{Name: "bare", Command: "run"}}))
	assert.Equal(t, map[string]any{"command": "run"},
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

func TestList_ReadsFlatFormat(t *testing.T) {
	setup(t, `{"context_servers": {"gh": {"command": "npx", "args": ["-y", "gh"], "env": {"K": "v"}}}}`)
	got, err := zed.New().List()
	require.NoError(t, err)
	assert.Equal(t, []schema.Server{{
		Name:    "gh",
		Command: "npx",
		Args:    []string{"-y", "gh"},
		Env:     map[string]schema.EnvVar{"K": {Value: "v"}},
	}}, got)
}

func TestList_ReadsNestedFormat(t *testing.T) {
	setup(t, `{"context_servers": {"gh": {"command": {"path": "npx", "args": ["-y", "gh"], "env": {"K": "v"}}}}}`)
	got, err := zed.New().List()
	require.NoError(t, err)
	assert.Equal(t, []schema.Server{{
		Name:    "gh",
		Command: "npx",
		Args:    []string{"-y", "gh"},
		Env:     map[string]schema.EnvVar{"K": {Value: "v"}},
	}}, got)
}

func TestList_ReadsMixedFormats(t *testing.T) {
	setup(t, `// Zed settings
{
  "context_servers": {
    "flat": {"command": "npx", "args": ["-y", "flat"]}, // new format
    /* old format */
    "nested": {"command": {"path": "uvx", "args": ["nested"]}},
    "remote": {"url": "https://example.com/mcp"},
  },
}
`)
	got, err := zed.New().List()
	require.NoError(t, err)
	sort.Slice(got, func(i, j int) bool { return got[i].Name < got[j].Name })
	assert.Equal(t, []schema.Server{
		{Name: "flat", Command: "npx", Args: []string{"-y", "flat"}, Env: map[string]schema.EnvVar{}},
		{Name: "nested", Command: "uvx", Args: []string{"nested"}, Env: map[string]schema.EnvVar{}},
		{Name: "remote", Transport: schema.TransportHTTP, URL: "https://example.com/mcp", Env: map[string]schema.EnvVar{}},
	}, got)
}

func TestList_RejectsNonStringCommand(t *testing.T) {
	for name, cmd := range map[string]string{"number": `42`, "array": `["npx"]`, "bool": `true`} {
		t.Run(name, func(t *testing.T) {
			setup(t, `{"context_servers": {"bad": {"command": `+cmd+`}}}`)
			_, err := zed.New().List()
			require.Error(t, err)
			assert.Contains(t, err.Error(), `zed: parse config: server "bad": "command" is neither a string nor an object`)
		})
	}
}

// A remote server replaces a local entry of a JSONC settings file with
// Zed's HTTP context server, headers included; comments elsewhere stay, and
// List reads the headers back.
func TestApply_RemoteHeadersInJSONCSettings(t *testing.T) {
	path := setup(t, jsoncSettings)
	a := zed.New()
	require.NoError(t, a.Apply([]schema.Server{{
		Name: "replace-me", Transport: "http", URL: "https://example.com/mcp",
		Headers: map[string]schema.EnvVar{"Authorization": {Value: "Bearer tok"}},
	}}))

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(data), "// keep me")
	assert.Equal(t, map[string]any{
		"url":     "https://example.com/mcp",
		"headers": map[string]any{"Authorization": "Bearer tok"},
	}, contextServers(t, path)["replace-me"])

	got, err := a.List()
	require.NoError(t, err)
	byName := map[string]schema.Server{}
	for _, s := range got {
		byName[s.Name] = s
	}
	assert.Equal(t, map[string]schema.EnvVar{"Authorization": {Value: "Bearer tok"}}, byName["replace-me"].Headers)
}
