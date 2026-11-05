package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mcpfleet/mcpfleet/internal/schema"
	"github.com/mcpfleet/mcpfleet/internal/vault"
)

type fakeStore map[string]string

func (f fakeStore) Get(name string) (string, bool) {
	v, ok := f[name]
	return v, ok
}

func openFake(store fakeStore, opened *int) func() (secretStore, error) {
	return func() (secretStore, error) {
		*opened++
		return store, nil
	}
}

func TestResolveSecrets_FillsReferencesAndKeepsLiterals(t *testing.T) {
	servers := []schema.Server{{
		Name: "github",
		Env: map[string]schema.EnvVar{
			"GITHUB_TOKEN": {Secret: "github_token"},
			"LOG_LEVEL":    {Value: "debug"},
		},
	}}
	opened := 0

	got, err := resolveSecrets(servers, openFake(fakeStore{"github_token": "tok"}, &opened))
	require.NoError(t, err)
	assert.Equal(t, 1, opened)
	assert.Equal(t, map[string]schema.EnvVar{
		"GITHUB_TOKEN": {Value: "tok"},
		"LOG_LEVEL":    {Value: "debug"},
	}, got[0].Env)
	assert.Equal(t, schema.EnvVar{Secret: "github_token"}, servers[0].Env["GITHUB_TOKEN"], "input is not modified")
}

func TestResolveSecrets_NoReferencesDoesNotOpenVault(t *testing.T) {
	servers := []schema.Server{
		{Name: "plain", Env: map[string]schema.EnvVar{"A": {Value: "1"}}},
		{Name: "noenv"},
	}
	opened := 0

	got, err := resolveSecrets(servers, openFake(nil, &opened))
	require.NoError(t, err)
	assert.Equal(t, 0, opened)
	assert.Equal(t, servers, got)
}

func TestResolveSecrets_MissingSecretsReportedTogether(t *testing.T) {
	servers := []schema.Server{
		{Name: "github", Env: map[string]schema.EnvVar{"GITHUB_TOKEN": {Secret: "github_token"}}},
		{Name: "ok", Env: map[string]schema.EnvVar{"K": {Secret: "present"}}},
		{Name: "slack", Env: map[string]schema.EnvVar{"SLACK_TOKEN": {Secret: "slack_token"}}},
	}
	opened := 0

	_, err := resolveSecrets(servers, openFake(fakeStore{"present": "v"}, &opened))
	require.Error(t, err)
	msg := err.Error()
	assert.Contains(t, msg, "github_token (server github, env GITHUB_TOKEN)")
	assert.Contains(t, msg, "slack_token (server slack, env SLACK_TOKEN)")
	assert.Contains(t, msg, "mcpfleet secret set <name>")
	assert.NotContains(t, msg, "present")
}

func TestResolveSecrets_VaultOpenErrorFails(t *testing.T) {
	for _, sentinel := range []error{vault.ErrKeyMissing, vault.ErrKeyringUnavailable} {
		t.Run(sentinel.Error(), func(t *testing.T) {
			servers := []schema.Server{
				{Name: "github", Env: map[string]schema.EnvVar{"GITHUB_TOKEN": {Secret: "github_token"}}},
			}
			open := func() (secretStore, error) {
				return nil, fmt.Errorf("vault: %w: details", sentinel)
			}

			_, err := resolveSecrets(servers, open)
			require.Error(t, err)
			assert.True(t, errors.Is(err, sentinel), "sentinel kept for callers: %v", err)
			assert.Contains(t, err.Error(), "github_token")
		})
	}
}

func TestApply_MissingSecretFailsWithoutWrite(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("cursor config lives under %APPDATA% on Windows")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("MCPFLEET_REGISTRY_URL", "")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/servers" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]schema.Server{{
			Name:    "github",
			Command: "npx",
			Env:     map[string]schema.EnvVar{"GITHUB_TOKEN": {Secret: "github_token"}},
		}})
	}))
	t.Cleanup(srv.Close)
	_, _, err := runRootIO(t, nil, "auth", "login", "--url", srv.URL, "--token", "tok")
	require.NoError(t, err)

	_, _, err = runRootIO(t, nil, "apply", "cursor")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "github_token")

	_, statErr := os.Stat(filepath.Join(home, ".cursor", "mcp.json"))
	assert.True(t, errors.Is(statErr, os.ErrNotExist), "agent config must not be written: %v", statErr)
}

// loginToFakeRegistry starts a registry that serves servers on GET
// /v1/servers, isolates HOME and logs in to it.
func loginToFakeRegistry(t *testing.T, servers []schema.Server) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("APPDATA", filepath.Join(home, "AppData", "Roaming"))
	t.Setenv("MCPFLEET_REGISTRY_URL", "")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/servers" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(servers)
	}))
	t.Cleanup(srv.Close)
	_, _, err := runRootIO(t, nil, "auth", "login", "--url", srv.URL, "--token", "tok")
	require.NoError(t, err)
	return home
}

func TestApply_VerbosePrintsDebugLogs(t *testing.T) {
	loginToFakeRegistry(t, []schema.Server{{Name: "fs", Command: "npx"}})

	out, errOut, err := runRootIO(t, nil, "apply", "-v", "cursor")
	require.NoError(t, err)
	assert.Contains(t, errOut, "Applying MCP servers")
	assert.Contains(t, out, "Applied 1 MCP server(s) to cursor")
}

func TestApply_NoLogsWithoutVerbose(t *testing.T) {
	loginToFakeRegistry(t, []schema.Server{{Name: "fs", Command: "npx"}})

	out, errOut, err := runRootIO(t, nil, "apply", "cursor")
	require.NoError(t, err)
	assert.Empty(t, errOut)
	assert.Contains(t, out, "Applied 1 MCP server(s) to cursor")
}

func TestApply_AllFlagIsUnknown(t *testing.T) {
	isolateConfig(t)
	var stdout, stderr bytes.Buffer
	code := runForTest(t, []string{"apply", "--all", "cursor"}, &stdout, &stderr)
	assert.Equal(t, 1, code)
	assert.Contains(t, stderr.String(), "Error: unknown flag: --all")
}

func TestApplyHelp_HasNoAllFlag(t *testing.T) {
	out, _, err := runRootIO(t, nil, "apply", "--help")
	require.NoError(t, err)
	assert.Contains(t, out, "--tag")
	assert.NotContains(t, out, "--all")
}
