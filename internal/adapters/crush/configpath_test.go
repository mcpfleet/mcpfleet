package crush

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestConfigPath_PerEnv pins Crush's global JSON config, as Crush's own
// GlobalConfig() resolves it: $CRUSH_GLOBAL_CONFIG/crush.json, else
// $XDG_CONFIG_HOME/crush/crush.json, else <home>/.config/crush/crush.json,
// on every OS (Windows included).
func TestConfigPath_PerEnv(t *testing.T) {
	for _, tc := range []struct {
		name, goos, home string
		env              map[string]string
		want             string
	}{
		{"crush global config wins", "linux", "/home/u",
			map[string]string{"CRUSH_GLOBAL_CONFIG": "/etc/crush", "XDG_CONFIG_HOME": "/xdg"},
			"/etc/crush/crush.json"},
		{"xdg config home", "linux", "/home/u",
			map[string]string{"XDG_CONFIG_HOME": "/xdg"},
			"/xdg/crush/crush.json"},
		{"home fallback linux", "linux", "/home/u", nil,
			"/home/u/.config/crush/crush.json"},
		{"home fallback darwin", "darwin", "/Users/u", nil,
			"/Users/u/.config/crush/crush.json"},
		{"home fallback windows", "windows", "/c/Users/u",
			map[string]string{"APPDATA": "/appdata", "LOCALAPPDATA": "/localappdata"},
			"/c/Users/u/.config/crush/crush.json"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			getenv := func(key string) string { return filepath.FromSlash(tc.env[key]) }
			got := configPath(tc.goos, filepath.FromSlash(tc.home), getenv)
			assert.Equal(t, tc.want, filepath.ToSlash(got))
		})
	}
}
