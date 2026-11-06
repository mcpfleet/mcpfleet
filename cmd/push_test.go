package cmd

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeRegistry answers each "METHOD /path" with a fixed status and body and
// records the requests it saw. Unknown routes answer 500.
func fakeRegistry(t *testing.T, routes map[string]struct {
	status int
	body   string
}) (*[]string, *sync.Mutex) {
	t.Helper()
	var (
		mu   sync.Mutex
		seen []string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		key := r.Method + " " + r.URL.Path
		mu.Lock()
		seen = append(seen, key)
		mu.Unlock()
		route, ok := routes[key]
		if !ok {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		if route.body != "" {
			w.Header().Set("Content-Type", "application/problem+json")
		}
		w.WriteHeader(route.status)
		_, _ = io.WriteString(w, route.body)
	}))
	t.Cleanup(srv.Close)

	isolateConfig(t)
	t.Setenv("MCPFLEET_REGISTRY_URL", srv.URL)
	home, _ := os.UserHomeDir()
	dir := filepath.Join(home, ".config", "mcpfleet")
	require.NoError(t, os.MkdirAll(dir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "token"), []byte("tok"), 0o600))
	return &seen, &mu
}

func TestPush_CreatesWhenServerMissing(t *testing.T) {
	seen, mu := fakeRegistry(t, map[string]struct {
		status int
		body   string
	}{
		"GET /v1/servers/github": {http.StatusNotFound, `{"detail":"server not found"}`},
		"POST /v1/servers":       {http.StatusOK, `{"name":"github"}`},
	})
	file := filepath.Join(t.TempDir(), "servers.yaml")
	require.NoError(t, os.WriteFile(file, []byte("- name: github\n  command: npx\n"), 0o600))

	var stdout, stderr bytes.Buffer
	code := runForTest(t, []string{"push", file}, &stdout, &stderr)
	assert.Equal(t, 0, code, stderr.String())

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, []string{"GET /v1/servers/github", "POST /v1/servers"}, *seen)
}

// pushNameError runs push on a file with one server named by yamlName
// against a registry that rejects the name, and returns stderr and the
// requests the registry saw.
func pushNameError(t *testing.T, yamlName string, routes map[string]struct {
	status int
	body   string
}) (string, []string) {
	t.Helper()
	seen, mu := fakeRegistry(t, routes)
	file := filepath.Join(t.TempDir(), "servers.yaml")
	require.NoError(t, os.WriteFile(file, []byte("- name: "+yamlName+"\n  command: npx\n"), 0o600))

	var stdout, stderr bytes.Buffer
	code := runForTest(t, []string{"push", file}, &stdout, &stderr)
	assert.Equal(t, 1, code, stdout.String())

	mu.Lock()
	defer mu.Unlock()
	return stderr.String(), append([]string(nil), *seen...)
}

// "." and ".." cannot be sent as one path segment (proxies normalize them
// away), so push creates them without a lookup and shows the registry's 422.
func TestPush_InvalidNameShowsRegistryError(t *testing.T) {
	for _, name := range []string{".", ".."} {
		t.Run(name, func(t *testing.T) {
			stderr, seen := pushNameError(t, `"`+name+`"`, map[string]struct {
				status int
				body   string
			}{
				"POST /v1/servers": {http.StatusUnprocessableEntity,
					`{"status":422,"detail":"validation failed","errors":[{"location":"body.name","message":"name must not be \".\" or \"..\""}]}`},
			})
			assert.Contains(t, stderr, `create server "`+name+`"`)
			assert.Contains(t, stderr, `validation failed: body.name: name must not be "." or ".."`)
			assert.Equal(t, []string{"POST /v1/servers"}, seen)
		})
	}
}

// A name with a control character is escaped in the path, so the lookup
// runs as usual (404) and the create shows the registry's 422.
func TestPush_ControlCharNameShowsRegistryError(t *testing.T) {
	stderr, seen := pushNameError(t, `"a\nb"`, map[string]struct {
		status int
		body   string
	}{
		"GET /v1/servers/a\nb": {http.StatusNotFound, `{"detail":"server not found"}`},
		"POST /v1/servers": {http.StatusUnprocessableEntity,
			`{"status":422,"detail":"validation failed","errors":[{"location":"body.name","message":"name must not contain control characters (found U+000A at byte 1)"}]}`},
	})
	assert.Contains(t, stderr, `create server "a\nb"`)
	assert.Contains(t, stderr, "validation failed: body.name: name must not contain control characters (found U+000A at byte 1)")
	assert.Equal(t, []string{"GET /v1/servers/a\nb", "POST /v1/servers"}, seen)
}
