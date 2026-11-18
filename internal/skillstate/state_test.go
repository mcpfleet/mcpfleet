package skillstate

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDecide(t *testing.T) {
	tests := []struct {
		name                 string
		local, state, remote string
		force                bool
		want                 Action
	}{
		{"fresh install", "", "", "r", false, Install},
		{"fresh install with stale state", "", "s", "r", false, Install},
		{"already current", "r", "", "r", false, Skip},
		{"already current and recorded", "r", "r", "r", false, Skip},
		{"unchanged since last apply", "s", "s", "r", false, Overwrite},
		{"locally modified", "l", "s", "r", false, Conflict},
		{"local copy never applied by mcpfleet", "l", "", "r", false, Conflict},
		{"locally modified with force", "l", "s", "r", true, Overwrite},
		{"unmanaged copy with force", "l", "", "r", true, Overwrite},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, Decide(tt.local, tt.state, tt.remote, tt.force))
		})
	}
}

func TestLoad_MissingFileIsEmpty(t *testing.T) {
	s, err := Load(filepath.Join(t.TempDir(), "skills-state.json"))
	require.NoError(t, err)
	assert.Equal(t, "", s.Hash("claude-code", "notes"))
}

func TestSaveLoad_RoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cfg", "skills-state.json")
	s, err := Load(path)
	require.NoError(t, err)
	at := time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)
	s.Record("claude-code", "notes", "abc", at)
	s.Record("windsurf", "notes", "def", at)
	require.NoError(t, s.Save())

	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())

	back, err := Load(path)
	require.NoError(t, err)
	assert.Equal(t, "abc", back.Hash("claude-code", "notes"))
	assert.Equal(t, "def", back.Hash("windsurf", "notes"))
	assert.Equal(t, "", back.Hash("cursor", "notes"))

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.JSONEq(t, `{"claude-code":{"notes":{"sha256":"abc","installed_at":"2026-11-18T12:00:00Z"}},
		"windsurf":{"notes":{"sha256":"def","installed_at":"2026-11-18T12:00:00Z"}}}`, string(data))
}

func TestLoad_CorruptFileIsError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "skills-state.json")
	require.NoError(t, os.WriteFile(path, []byte("{"), 0o600))
	_, err := Load(path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), path)
}

func TestStatusOf(t *testing.T) {
	tests := []struct {
		name                 string
		local, state, remote string
		want                 Status
	}{
		{"no local copy", "", "", "r", NotInstalled},
		{"local copy is the registry's", "r", "s", "r", UpToDate},
		{"unchanged since last apply, registry moved on", "s", "s", "r", OutOfDate},
		{"changed since last apply", "l", "s", "r", Modified},
		{"local copy mcpfleet never installed", "l", "", "r", Modified},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, StatusOf(tt.local, tt.state, tt.remote))
		})
	}
}

func TestStatusString(t *testing.T) {
	assert.Equal(t, "not installed", NotInstalled.String())
	assert.Equal(t, "installed", UpToDate.String())
	assert.Equal(t, "out of date", OutOfDate.String())
	assert.Equal(t, "modified locally", Modified.String())
	assert.Equal(t, "not spec-conformant", NotConformant.String())
	assert.Equal(t, "no skills support", Unsupported.String())
}
