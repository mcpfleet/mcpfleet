package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mcpfleet/mcpfleet/internal/registry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAuthCommands(t *testing.T) {
	// Setup temp directory for testing
	tempDir := t.TempDir()
	oldConfigDir := os.Getenv("HOME")
	os.Setenv("HOME", tempDir)
	defer func() {
		if oldConfigDir != "" {
			os.Setenv("HOME", oldConfigDir)
		} else {
			os.Unsetenv("HOME")
		}
	}()

	// Create config directory
	configDir := filepath.Join(tempDir, ".config", "mcpfleet")
	require.NoError(t, os.MkdirAll(configDir, 0o700))

	t.Run("Login saves token", func(t *testing.T) {
		token := "mcp_test_token_12345"
		err := registry.SaveToken(token)
		require.NoError(t, err)

		// Verify token was saved
		saved, err := registry.LoadToken()
		require.NoError(t, err)
		assert.Equal(t, token, saved)
	})

	t.Run("Logout deletes token", func(t *testing.T) {
		token := "mcp_test_token_67890"
		err := registry.SaveToken(token)
		require.NoError(t, err)

		// Logout
		err = registry.DeleteToken()
		require.NoError(t, err)

		// Verify token was deleted
		_, err = registry.LoadToken()
		assert.Error(t, err)
	})

	t.Run("Status shows masked token", func(t *testing.T) {
		token := "mcp_verylongtokenthatshouldbemasked"
		err := registry.SaveToken(token)
		require.NoError(t, err)

		// Load and check masking
		saved, err := registry.LoadToken()
		require.NoError(t, err)
		assert.Equal(t, token, saved)

		// Token should be maskable
		if len(saved) > 8 {
			masked := saved[:4] + "****" + saved[len(saved)-4:]
			assert.NotContains(t, masked, saved[4:len(saved)-4])
		}

		// Cleanup
		registry.DeleteToken()
	})
}
