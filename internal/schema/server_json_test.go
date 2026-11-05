package schema

import (
	"encoding/json"
	"testing"
)

// TestServer_JSONKeys pins the JSON shape the registry API expects:
// lowercase keys, env entries as {value}/{secret} objects, empty optional
// fields omitted.
func TestServer_JSONKeys(t *testing.T) {
	srv := Server{
		Name:      "github",
		Transport: "stdio",
		Install:   InstallConfig{Type: "npx", Package: "@modelcontextprotocol/server-github"},
		Command:   "npx",
		Env: map[string]EnvVar{
			"GITHUB_TOKEN": {Secret: "github_token"},
			"LOG_LEVEL":    {Value: "debug"},
		},
	}
	b, err := json.Marshal(srv)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}

	for _, k := range []string{"name", "transport", "install", "command", "env"} {
		if _, ok := got[k]; !ok {
			t.Errorf("missing key %q in %s", k, b)
		}
	}
	for _, k := range []string{"Name", "description", "url", "URL", "args", "tags", "platforms"} {
		if _, ok := got[k]; ok {
			t.Errorf("unexpected key %q in %s", k, b)
		}
	}
	install, _ := got["install"].(map[string]any)
	if install["type"] != "npx" || install["package"] != "@modelcontextprotocol/server-github" {
		t.Errorf("install = %v, want lowercase type/package", got["install"])
	}
	if _, ok := install["version"]; ok {
		t.Errorf("empty install.version should be omitted: %s", b)
	}
	env, _ := got["env"].(map[string]any)
	tok, _ := env["GITHUB_TOKEN"].(map[string]any)
	if tok["secret"] != "github_token" || len(tok) != 1 {
		t.Errorf("env.GITHUB_TOKEN = %v, want {secret: github_token}", env["GITHUB_TOKEN"])
	}
	lvl, _ := env["LOG_LEVEL"].(map[string]any)
	if lvl["value"] != "debug" || len(lvl) != 1 {
		t.Errorf("env.LOG_LEVEL = %v, want {value: debug}", env["LOG_LEVEL"])
	}
}

// TestServer_JSONURL pins the key of the remote (http/sse) server address.
func TestServer_JSONURL(t *testing.T) {
	b, err := json.Marshal(Server{Name: "remote", Transport: "http", URL: "https://example.com/mcp"})
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if got["url"] != "https://example.com/mcp" {
		t.Errorf("url = %v, want https://example.com/mcp in %s", got["url"], b)
	}
}

func TestManifest_JSONKeys(t *testing.T) {
	b, err := json.Marshal(Manifest{Version: "1", Servers: []Server{{Name: "x"}}})
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if got["version"] != "1" || got["servers"] == nil {
		t.Errorf("manifest JSON = %s, want lowercase version/servers", b)
	}
}
