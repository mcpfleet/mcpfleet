package windsurf

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestConfigPath_PerGOOS pins Windsurf's config file per OS: it lives under
// the user's home directory everywhere (%USERPROFILE% on Windows), never
// under %APPDATA%.
func TestConfigPath_PerGOOS(t *testing.T) {
	getenv := func(key string) string {
		if key == "APPDATA" {
			return "/appdata"
		}
		return ""
	}
	for _, tc := range []struct {
		goos, home, want string
	}{
		{"linux", "/home/u", "/home/u/.codeium/windsurf/mcp_config.json"},
		{"darwin", "/Users/u", "/Users/u/.codeium/windsurf/mcp_config.json"},
		{"windows", "/c/Users/u", "/c/Users/u/.codeium/windsurf/mcp_config.json"},
	} {
		t.Run(tc.goos, func(t *testing.T) {
			got := configPath(tc.goos, filepath.FromSlash(tc.home), getenv)
			assert.Equal(t, tc.want, filepath.ToSlash(got))
		})
	}
}
