package claudecode

import (
	"os"
	"testing"

	"github.com/mcpfleet/mcpfleet/internal/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAdapter(t *testing.T) {
	adapter := New()

	t.Run("Name returns claude-code", func(t *testing.T) {
		assert.Equal(t, "claude-code", adapter.Name())
	})

	t.Run("ConfigPath returns non-empty path", func(t *testing.T) {
		path := adapter.ConfigPath()
		assert.NotEmpty(t, path)
		assert.Contains(t, path, "claude_desktop_config.json")
	})

	t.Run("Apply handles empty server list", func(t *testing.T) {
		// Set temp home
		tempHome := t.TempDir()
		oldHome := os.Getenv("HOME")
		os.Setenv("HOME", tempHome)
		defer func() {
			if oldHome != "" {
				os.Setenv("HOME", oldHome)
			} else {
				os.Unsetenv("HOME")
			}
		}()

		// Create adapter that will use temp home
		testAdapter := New()
		servers := []schema.Server{}

		err := testAdapter.Apply(servers)
		require.NoError(t, err)

		// Config file should be created even with empty server list
		_, err = os.Stat(testAdapter.ConfigPath())
		require.NoError(t, err)
	})

	t.Run("Apply creates valid JSON structure", func(t *testing.T) {
		// Set temp home
		tempHome := t.TempDir()
		oldHome := os.Getenv("HOME")
		os.Setenv("HOME", tempHome)
		defer func() {
			if oldHome != "" {
				os.Setenv("HOME", oldHome)
			} else {
				os.Unsetenv("HOME")
			}
		}()

		testAdapter := New()
		servers := []schema.Server{
			{
				Name:    "test-server",
				Command: "npx",
				Args:    []string{"-y", "@test/server"},
			},
		}

		err := testAdapter.Apply(servers)
		require.NoError(t, err)

		// Verify file was created and contains expected structure
		data, err := os.ReadFile(testAdapter.ConfigPath())
		require.NoError(t, err)
		content := string(data)

		assert.Contains(t, content, "mcpServers")
		assert.Contains(t, content, "test-server")
		assert.Contains(t, content, "npx")
	})

	t.Run("List returns error for non-existent config", func(t *testing.T) {
		// Set temp home to ensure config doesn't exist
		tempHome := t.TempDir()
		oldHome := os.Getenv("HOME")
		os.Setenv("HOME", tempHome)
		defer func() {
			if oldHome != "" {
				os.Setenv("HOME", oldHome)
			} else {
				os.Unsetenv("HOME")
			}
		}()

		testAdapter := New()
		_, err := testAdapter.List()
		assert.Error(t, err)
	})
}
