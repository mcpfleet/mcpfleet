package cmd

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mcpfleet/mcpfleet/internal/registry"
)

// isolateConfig points the config directory (~/.config/mcpfleet) at a fresh
// temp HOME and clears the registry URL override from the environment.
func isolateConfig(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("MCPFLEET_REGISTRY_URL", "")
}

// runRoot executes the global root command with args and returns its error.
func runRoot(t *testing.T, args ...string) error {
	t.Helper()
	var out bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&out)
	rootCmd.SetArgs(args)
	t.Cleanup(func() {
		rootCmd.SetOut(nil)
		rootCmd.SetErr(nil)
		rootCmd.SetArgs(nil)
	})
	return rootCmd.Execute()
}

func TestRootHelp_ListsDeleteOnce(t *testing.T) {
	n := 0
	for _, c := range rootCmd.Commands() {
		if c.Name() == "delete" {
			n++
		}
	}
	assert.Equal(t, 1, n, "delete registered %d times", n)

	lines := 0
	for _, l := range strings.Split(rootCmd.UsageString(), "\n") {
		if strings.HasPrefix(l, "  delete ") {
			lines++
		}
	}
	assert.Equal(t, 1, lines, "delete listed %d times in help", lines)
}

func TestAuthLogin_FlagsSkipFormAndSaveURL(t *testing.T) {
	isolateConfig(t)

	require.NoError(t, runRoot(t, "auth", "login", "--url", "http://reg.test:8080/", "--token", " tok "))

	tok, err := registry.LoadToken()
	require.NoError(t, err)
	assert.Equal(t, "tok", tok)

	u, err := registry.LoadRegistryURL()
	require.NoError(t, err)
	assert.Equal(t, "http://reg.test:8080", u)
}

func TestAuthLogin_SavedURLUsedByNextCommand(t *testing.T) {
	isolateConfig(t)

	var (
		mu       sync.Mutex
		requests []string
		auth     []string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		requests = append(requests, r.Method+" "+r.URL.Path)
		auth = append(auth, r.Header.Get("Authorization"))
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(srv.Close)

	require.NoError(t, runRoot(t, "auth", "login", "--url", srv.URL, "--token", "tok"))
	require.NoError(t, runRoot(t, "delete", "foo"))

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, []string{"DELETE /v1/servers/foo"}, requests)
	assert.Equal(t, []string{"Bearer tok"}, auth)
}

func TestAuthLogin_RejectsInvalidURL(t *testing.T) {
	for _, bad := range []string{"ftp://x", "not a url", "http://", "://nohost"} {
		t.Run(bad, func(t *testing.T) {
			isolateConfig(t)

			err := runRoot(t, "auth", "login", "--url", bad, "--token", "tok")
			require.Error(t, err)

			_, err = registry.LoadToken()
			assert.Error(t, err, "token must not be saved for an invalid URL")
			_, err = registry.LoadRegistryURL()
			assert.Error(t, err, "URL must not be saved when invalid")
		})
	}
}

func TestAuthLogin_HelpShowsFlags(t *testing.T) {
	login, _, err := rootCmd.Find([]string{"auth", "login"})
	require.NoError(t, err)
	assert.NotNil(t, login.Flags().Lookup("url"))
	assert.NotNil(t, login.Flags().Lookup("token"))
}

func TestAuthStatus_ShowsRegistryURLAndSource(t *testing.T) {
	tests := []struct {
		name     string
		loggedIn bool
		fileURL  string
		envURL   string
		wantLine string
	}{
		{"default, logged in", true, "", "", "Registry: https://registry.mcpfleet.dev (from default)"},
		{"file, logged in", true, "https://file.example", "", "Registry: https://file.example (from ~/.config/mcpfleet/registry_url)"},
		{"env, logged in", true, "https://file.example", "https://env.example/", "Registry: https://env.example (from MCPFLEET_REGISTRY_URL)"},
		{"env, not logged in", false, "", "https://env.example", "Registry: https://env.example (from MCPFLEET_REGISTRY_URL)"},
		{"default, not logged in", false, "", "", "Registry: https://registry.mcpfleet.dev (from default)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isolateConfig(t)
			if tt.loggedIn {
				require.NoError(t, registry.SaveToken("mcp_secret_token_beae"))
			}
			if tt.fileURL != "" {
				require.NoError(t, registry.SaveRegistryURL(tt.fileURL))
			}
			t.Setenv("MCPFLEET_REGISTRY_URL", tt.envURL)

			out, errOut, err := runRootIO(t, nil, "auth", "status")
			require.NoError(t, err)
			assert.Empty(t, errOut)
			if tt.loggedIn {
				assert.Contains(t, out, "✓ Logged in (token: mcp_")
				assert.NotContains(t, out, "secret_token")
			} else {
				assert.Contains(t, out, "Not logged in.")
			}
			assert.Contains(t, out, tt.wantLine+"\n")
		})
	}
}
