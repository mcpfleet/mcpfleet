package claudedesktop

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestConfigPath_PerGOOS pins Claude Desktop's config file per OS. It lives
// in the app's data directory: %APPDATA%\Claude on Windows,
// ~/Library/Application Support/Claude on macOS, and
// ${XDG_CONFIG_HOME:-~/.config}/Claude on Linux and other Unix systems.
func TestConfigPath_PerGOOS(t *testing.T) {
	env := func(vars map[string]string) func(string) string {
		return func(key string) string { return vars[key] }
	}
	noXDG := env(map[string]string{"APPDATA": "/appdata"})
	withXDG := env(map[string]string{"APPDATA": "/appdata", "XDG_CONFIG_HOME": "/xdg"})
	for _, tc := range []struct {
		name, goos, home string
		getenv           func(string) string
		want             string
	}{
		{"linux", "linux", "/home/u", noXDG, "/home/u/.config/Claude/claude_desktop_config.json"},
		{"linux+XDG", "linux", "/home/u", withXDG, "/xdg/Claude/claude_desktop_config.json"},
		{"freebsd", "freebsd", "/home/u", noXDG, "/home/u/.config/Claude/claude_desktop_config.json"},
		{"darwin", "darwin", "/Users/u", withXDG, "/Users/u/Library/Application Support/Claude/claude_desktop_config.json"},
		{"windows", "windows", "/c/Users/u", withXDG, "/appdata/Claude/claude_desktop_config.json"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := configPath(tc.goos, filepath.FromSlash(tc.home), tc.getenv)
			assert.Equal(t, tc.want, filepath.ToSlash(got))
		})
	}
}
