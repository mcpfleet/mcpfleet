package cmd

import (
	"bytes"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mcpfleet/mcpfleet/internal/registry"
)

// These tests pin that commands write through cmd.OutOrStdout(), so the
// stdout passed to Run receives their output instead of the process stdout.

func TestAuthLogin_WritesToCommandOut(t *testing.T) {
	isolateConfig(t)

	var stdout, stderr bytes.Buffer
	code := runForTest(t, []string{"auth", "login", "--url", "http://reg.test", "--token", "tok"}, &stdout, &stderr)
	require.Equal(t, 0, code, stderr.String())
	assert.Equal(t, "✓ Logged in successfully!\n", stdout.String())
	assert.Empty(t, stderr.String())
}

func TestAuthLogout_WritesToCommandOut(t *testing.T) {
	tests := []struct {
		name     string
		loggedIn bool
		want     string
	}{
		{"logged in", true, "✓ Logged out.\n"},
		{"not logged in", false, "Not logged in.\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isolateConfig(t)
			if tt.loggedIn {
				require.NoError(t, registry.SaveToken("tok"))
			}

			var stdout, stderr bytes.Buffer
			code := runForTest(t, []string{"auth", "logout"}, &stdout, &stderr)
			require.Equal(t, 0, code, stderr.String())
			assert.Equal(t, tt.want, stdout.String())
			assert.Empty(t, stderr.String())
		})
	}
}

func TestPush_WritesToCommandOut(t *testing.T) {
	fakeRegistry(t, map[string]struct {
		status int
		body   string
	}{
		"GET /v1/servers/github": {http.StatusNotFound, `{"detail":"server not found"}`},
		"POST /v1/servers":       {http.StatusOK, `{"name":"github"}`},
		"GET /v1/servers/fs":     {http.StatusOK, `{"name":"fs"}`},
		"PUT /v1/servers/fs":     {http.StatusOK, `{"name":"fs"}`},
	})
	file := filepath.Join(t.TempDir(), "servers.yaml")
	require.NoError(t, os.WriteFile(file, []byte("- name: github\n  command: npx\n- name: fs\n  command: npx\n"), 0o600))

	var stdout, stderr bytes.Buffer
	code := runForTest(t, []string{"push", file}, &stdout, &stderr)
	require.Equal(t, 0, code, stderr.String())
	assert.Equal(t, "created  github\nupdated  fs\n", stdout.String())
}

func TestPull_WritesToCommandOut(t *testing.T) {
	fakeRegistry(t, map[string]struct {
		status int
		body   string
	}{
		"GET /v1/servers": {http.StatusOK, `[{"name":"github","command":"npx"}]`},
	})
	file := filepath.Join(t.TempDir(), "servers.yaml")

	var stdout, stderr bytes.Buffer
	code := runForTest(t, []string{"pull", file}, &stdout, &stderr)
	require.Equal(t, 0, code, stderr.String())
	assert.Equal(t, "pulled 1 server(s) -> "+file+"\n", stdout.String())
}

func TestList_WritesToCommandOut(t *testing.T) {
	t.Run("servers", func(t *testing.T) {
		fakeRegistry(t, map[string]struct {
			status int
			body   string
		}{
			"GET /v1/servers": {http.StatusOK, `[{"name":"github","command":"npx","args":["-y","gh"]}]`},
		})

		var stdout, stderr bytes.Buffer
		code := runForTest(t, []string{"list"}, &stdout, &stderr)
		require.Equal(t, 0, code, stderr.String())
		assert.Contains(t, stdout.String(), "MCP Servers")
		assert.Contains(t, stdout.String(), "NAME    COMMAND  ARGS\n")
		assert.Contains(t, stdout.String(), "github  npx      -y gh\n")
	})
	t.Run("empty", func(t *testing.T) {
		fakeRegistry(t, map[string]struct {
			status int
			body   string
		}{
			"GET /v1/servers": {http.StatusOK, `[]`},
		})

		var stdout, stderr bytes.Buffer
		code := runForTest(t, []string{"list"}, &stdout, &stderr)
		require.Equal(t, 0, code, stderr.String())
		assert.Equal(t, "No servers registered.\n", stdout.String())
	})
}
