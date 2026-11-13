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

func TestList_ShowsTransportAndTarget(t *testing.T) {
	fakeRegistry(t, map[string]struct {
		status int
		body   string
	}{
		"GET /v1/servers": {http.StatusOK, `[
			{"name":"fs","transport":"stdio","command":"npx","args":["-y","fs"]},
			{"name":"api","transport":"http","url":"https://example.com/mcp"},
			{"name":"events","transport":"sse","url":"https://example.com/sse"},
			{"name":"legacy","command":"uvx","args":["x"]},
			{"name":"broken","transport":"sse"}
		]`},
	})

	var stdout, stderr bytes.Buffer
	code := runForTest(t, []string{"list"}, &stdout, &stderr)
	require.Equal(t, 0, code, stderr.String())

	out := stdout.String()
	assert.Contains(t, out, "NAME    TRANSPORT  TARGET\n")
	assert.Contains(t, out, "----    ---------  ------\n")
	assert.Contains(t, out, "fs      stdio      npx -y fs\n")
	assert.Contains(t, out, "api     http       https://example.com/mcp\n")
	assert.Contains(t, out, "events  sse        https://example.com/sse\n")
	assert.Contains(t, out, "legacy  stdio      uvx x\n")
	assert.Contains(t, out, "broken  sse        -\n")
	assert.NotContains(t, out, "COMMAND")
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
		assert.Contains(t, stdout.String(), "NAME    TRANSPORT  TARGET\n")
		assert.Contains(t, stdout.String(), "github  stdio      npx -y gh\n")
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

func TestShellQuote(t *testing.T) {
	tests := []struct{ in, want string }{
		{"-y", "-y"},
		{"@scope/pkg", "@scope/pkg"},
		{"--flag=x", "--flag=x"},
		{"my dir", "'my dir'"},
		{"it's", `'it'\''s'`},
		{"", "''"},
		{"$HOME", "'$HOME'"},
		{"~/x", "'~/x'"},
		{`a"b`, `'a"b'`},
		{"--flag=x y", "'--flag=x y'"},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, shellQuote(tt.in), "shellQuote(%q)", tt.in)
	}
}

func TestList_QuotesArgs(t *testing.T) {
	fakeRegistry(t, map[string]struct {
		status int
		body   string
	}{
		"GET /v1/servers": {http.StatusOK, `[
			{"name":"fs","command":"npx","args":["-y","fs","my dir","it's",""]},
			{"name":"local","command":"/opt/my tools/srv","args":["--root=/tmp"]},
			{"name":"api","transport":"http","url":"https://example.com/mcp?a=1&b=2"}
		]`},
	})

	var stdout, stderr bytes.Buffer
	code := runForTest(t, []string{"list"}, &stdout, &stderr)
	require.Equal(t, 0, code, stderr.String())

	out := stdout.String()
	assert.Contains(t, out, `npx -y fs 'my dir' 'it'\''s' ''`+"\n")
	assert.Contains(t, out, `'/opt/my tools/srv' --root=/tmp`+"\n")
	assert.Contains(t, out, "https://example.com/mcp?a=1&b=2\n", "a URL is not quoted")
}
