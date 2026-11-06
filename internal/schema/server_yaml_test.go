package schema

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// exampleManifest is the server example from the comment in server.go,
// wrapped in a manifest.
const exampleManifest = `version: "1"
servers:
  - name: github
    description: GitHub MCP server
    transport: stdio
    install:
      type: npx
      package: "@modelcontextprotocol/server-github"
      version: latest
    command: npx
    args: ["-y", "@modelcontextprotocol/server-github"]
    env:
      GITHUB_TOKEN:
        secret: github_token
      LOG_LEVEL:
        value: debug
    tags: [dev, vcs]
    platforms: [linux, darwin, windows]
`

func TestManifest_UnmarshalYAMLExample(t *testing.T) {
	var m Manifest
	require.NoError(t, yaml.Unmarshal([]byte(exampleManifest), &m))

	assert.Equal(t, Manifest{
		Version: "1",
		Servers: []Server{{
			Name:        "github",
			Description: "GitHub MCP server",
			Transport:   "stdio",
			Install:     InstallConfig{Type: "npx", Package: "@modelcontextprotocol/server-github", Version: "latest"},
			Command:     "npx",
			Args:        []string{"-y", "@modelcontextprotocol/server-github"},
			Env: map[string]EnvVar{
				"GITHUB_TOKEN": {Secret: "github_token"},
				"LOG_LEVEL":    {Value: "debug"},
			},
			Tags:      []string{"dev", "vcs"},
			Platforms: []string{"linux", "darwin", "windows"},
		}},
	}, m)
}

func TestManifest_YAMLRoundTrip(t *testing.T) {
	var in Manifest
	require.NoError(t, yaml.Unmarshal([]byte(exampleManifest), &in))

	out, err := yaml.Marshal(in)
	require.NoError(t, err)
	var back Manifest
	require.NoError(t, yaml.Unmarshal(out, &back))
	assert.Equal(t, in, back)
}

func TestServer_YAMLKeysAndOmitEmpty(t *testing.T) {
	out, err := yaml.Marshal(Server{
		Name:      "min",
		Transport: "stdio",
		Install:   InstallConfig{Type: "binary", Package: "min"},
		Command:   "min",
	})
	require.NoError(t, err)

	var got map[string]any
	require.NoError(t, yaml.Unmarshal(out, &got))
	assert.Equal(t, map[string]any{
		"name":      "min",
		"transport": "stdio",
		"install":   map[string]any{"type": "binary", "package": "min"},
		"command":   "min",
	}, got, "required keys only; description/url/args/env/headers/tags/platforms/install.version omitted:\n%s", out)
}

func TestServer_YAMLURLRoundTrip(t *testing.T) {
	in := Server{Name: "remote", Transport: "sse", URL: "https://example.com/sse"}
	out, err := yaml.Marshal(in)
	require.NoError(t, err)

	var keys map[string]any
	require.NoError(t, yaml.Unmarshal(out, &keys))
	assert.Equal(t, "https://example.com/sse", keys["url"], "url key:\n%s", out)

	var back Server
	require.NoError(t, yaml.Unmarshal(out, &back))
	assert.Equal(t, in, back)
}

func TestEnvVar_YAMLOmitEmpty(t *testing.T) {
	out, err := yaml.Marshal(map[string]EnvVar{
		"LIT": {Value: "v"},
		"REF": {Secret: "s"},
	})
	require.NoError(t, err)

	var got map[string]map[string]any
	require.NoError(t, yaml.Unmarshal(out, &got))
	assert.Equal(t, map[string]map[string]any{
		"LIT": {"value": "v"},
		"REF": {"secret": "s"},
	}, got)
}

func TestServer_YAMLHeaders(t *testing.T) {
	src := `
name: remote-api
transport: http
url: https://example.com/mcp
headers:
  Authorization: {secret: example_auth}
  X-Client: {value: mcpfleet}
`
	var srv Server
	require.NoError(t, yaml.Unmarshal([]byte(src), &srv))
	assert.Equal(t, map[string]EnvVar{
		"Authorization": {Secret: "example_auth"},
		"X-Client":      {Value: "mcpfleet"},
	}, srv.Headers)

	out, err := yaml.Marshal(srv)
	require.NoError(t, err)
	var back Server
	require.NoError(t, yaml.Unmarshal(out, &back))
	assert.Equal(t, srv, back)
}
