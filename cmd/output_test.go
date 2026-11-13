package cmd

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zalando/go-keyring"

	"github.com/mcpfleet/mcpfleet/internal/registry"
	"github.com/mcpfleet/mcpfleet/internal/vault"
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

// listJSONServers is a registry response for GET /v1/servers: one stdio
// server with a secret and a literal env var, one http server with a secret
// header and a URL holding "&", both with the fields the registry adds to
// its output (id, created_at, updated_at).
const listJSONServers = `[
	{"id":"0001","created_at":"2026-11-01T10:00:00Z","updated_at":"2026-11-02T10:00:00Z",
	 "name":"github","description":"GitHub","transport":"stdio",
	 "install":{"type":"npx","package":"@modelcontextprotocol/server-github"},
	 "command":"npx","args":["-y","@modelcontextprotocol/server-github","my dir"],
	 "env":{"GITHUB_TOKEN":{"secret":"gh"},"LOG_LEVEL":{"value":"debug"}},
	 "tags":["dev"],"platforms":["linux","darwin"]},
	{"id":"0002","created_at":"2026-11-01T10:00:00Z","updated_at":"2026-11-02T10:00:00Z",
	 "name":"api","transport":"http","install":{"type":"","package":""},"command":"",
	 "url":"https://example.com/mcp?a=1&b=2",
	 "headers":{"Authorization":{"secret":"api"}}}
]`

// listJSON runs "list --json" against a registry serving listJSONServers
// and returns stdout, failing the test on a non-zero exit.
func listJSON(t *testing.T) string {
	t.Helper()
	fakeRegistry(t, map[string]struct {
		status int
		body   string
	}{
		"GET /v1/servers": {http.StatusOK, listJSONServers},
	})
	t.Cleanup(func() { resetFlags(rootCmd) })

	var stdout, stderr bytes.Buffer
	code := runForTest(t, []string{"list", "--json"}, &stdout, &stderr)
	require.Equal(t, 0, code, stderr.String())
	return stdout.String()
}

func TestList_JSON(t *testing.T) {
	out := listJSON(t)

	assert.NotContains(t, out, "MCP Servers", "no table header in JSON output")
	assert.Contains(t, out, "https://example.com/mcp?a=1&b=2", "no HTML escaping")

	var got []map[string]any
	require.NoError(t, json.Unmarshal([]byte(out), &got), out)
	require.Len(t, got, 2)

	// Only the keys of the registry's ServerBody, the input of push.
	body := map[string]bool{
		"name": true, "description": true, "transport": true, "install": true,
		"command": true, "url": true, "args": true, "env": true, "headers": true,
		"tags": true, "platforms": true,
	}
	for _, srv := range got {
		for k := range srv {
			assert.True(t, body[k], "unexpected key %q in %v", k, srv["name"])
		}
	}

	assert.Equal(t, "github", got[0]["name"])
	assert.Equal(t, []any{"-y", "@modelcontextprotocol/server-github", "my dir"}, got[0]["args"])
	assert.Equal(t, map[string]any{
		"GITHUB_TOKEN": map[string]any{"secret": "gh"},
		"LOG_LEVEL":    map[string]any{"value": "debug"},
	}, got[0]["env"])
	assert.Equal(t, "https://example.com/mcp?a=1&b=2", got[1]["url"])
	assert.Equal(t, map[string]any{"Authorization": map[string]any{"secret": "api"}}, got[1]["headers"])
}

func TestList_JSONEmpty(t *testing.T) {
	for _, body := range []string{`[]`, `null`} {
		t.Run(body, func(t *testing.T) {
			fakeRegistry(t, map[string]struct {
				status int
				body   string
			}{
				"GET /v1/servers": {http.StatusOK, body},
			})
			t.Cleanup(func() { resetFlags(rootCmd) })

			var stdout, stderr bytes.Buffer
			code := runForTest(t, []string{"list", "--json"}, &stdout, &stderr)
			require.Equal(t, 0, code, stderr.String())
			assert.Equal(t, "[]\n", stdout.String())
		})
	}
}

func TestList_JSONDoesNotResolveSecrets(t *testing.T) {
	keyring.MockInit()
	t.Cleanup(keyring.MockInit)
	out := func() string {
		// fakeRegistry moves HOME, so the vault is filled after it, in
		// the same HOME list runs with.
		fakeRegistry(t, map[string]struct {
			status int
			body   string
		}{
			"GET /v1/servers": {http.StatusOK, listJSONServers},
		})
		v, err := vault.Open()
		require.NoError(t, err)
		v.Set("gh", "supersecret-gh")
		v.Set("api", "supersecret-api")
		require.NoError(t, v.Save())
		got, ok := storedSecret(t, "gh")
		require.True(t, ok)
		require.Equal(t, "supersecret-gh", got)

		t.Cleanup(func() { resetFlags(rootCmd) })
		var stdout, stderr bytes.Buffer
		code := runForTest(t, []string{"list", "--json"}, &stdout, &stderr)
		require.Equal(t, 0, code, stderr.String())
		assert.NotContains(t, stderr.String(), "supersecret")
		return stdout.String()
	}()

	assert.NotContains(t, out, "supersecret")
	assert.Contains(t, out, `"secret": "gh"`)
	assert.Contains(t, out, `"secret": "api"`)
}

// TestList_JSONIsPushInput pins the round trip "list --json > f.json &&
// push f.json": push reads the JSON file (YAML is a superset of JSON) and
// sends each server back unchanged.
func TestList_JSONIsPushInput(t *testing.T) {
	out := listJSON(t)
	file := filepath.Join(t.TempDir(), "servers.json")
	require.NoError(t, os.WriteFile(file, []byte(out), 0o600))

	var (
		mu     sync.Mutex
		posted []map[string]any
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/v1/servers/"):
			w.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(w, `{"detail":"server not found"}`)
		case r.Method == http.MethodPost && r.URL.Path == "/v1/servers":
			raw, _ := io.ReadAll(r.Body)
			var body map[string]any
			if err := json.Unmarshal(raw, &body); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			mu.Lock()
			posted = append(posted, body)
			mu.Unlock()
			_, _ = w.Write(raw)
		default:
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	t.Cleanup(srv.Close)
	t.Setenv("MCPFLEET_REGISTRY_URL", srv.URL)

	var stdout, stderr bytes.Buffer
	code := runForTest(t, []string{"push", file}, &stdout, &stderr)
	require.Equal(t, 0, code, stderr.String())
	assert.Equal(t, "created  github\ncreated  api\n", stdout.String())

	var want []map[string]any
	require.NoError(t, json.Unmarshal([]byte(out), &want))
	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, want, posted, "push sends back exactly what list --json printed")
}
