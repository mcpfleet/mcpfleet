package zed_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/tailscale/hujson"

	"github.com/mcpfleet/mcpfleet/internal/adapters/zed"
	"github.com/mcpfleet/mcpfleet/internal/schema"
)

const jsoncSettings = `// Zed settings
{
  "theme": "One Dark", // fav
  "vim_mode": true,
  /* servers below */
  "context_servers": {
    "old": {"command": {"path": "a"}}, // keep me
    "replace-me": {"command": {"path": "stale"}},
  },
}
`

func setup(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("APPDATA", dir)
	path := zed.New().ConfigPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// standardize parses a JSONC document into a plain map.
func standardize(t *testing.T, data []byte) map[string]any {
	t.Helper()
	std, err := hujson.Standardize(data)
	if err != nil {
		t.Fatalf("written settings are not valid JSONC: %v\n%s", err, data)
	}
	var m map[string]any
	if err := json.Unmarshal(std, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestApply_PreservesJSONCSettings(t *testing.T) {
	path := setup(t, jsoncSettings)

	err := zed.New().Apply([]schema.Server{
		{Name: "new-srv", Command: "npx", Args: []string{"-y", "pkg"}},
		{Name: "replace-me", Command: "fresh"},
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	out, _ := os.ReadFile(path)
	text := string(out)

	for _, want := range []string{"// Zed settings", "// fav", "/* servers below */", "// keep me"} {
		if !strings.Contains(text, want) {
			t.Errorf("comment %q lost:\n%s", want, text)
		}
	}
	m := standardize(t, out)
	if m["theme"] != "One Dark" || m["vim_mode"] != true {
		t.Errorf("editor settings lost: %v", m)
	}
	cs := m["context_servers"].(map[string]any)
	if cmd := cs["old"].(map[string]any)["command"].(map[string]any); cmd["path"] != "a" {
		t.Errorf("unrelated server changed: %v", cs["old"])
	}
	if cmd := cs["replace-me"].(map[string]any)["command"].(map[string]any); cmd["path"] != "fresh" {
		t.Errorf("server not replaced: %v", cs["replace-me"])
	}
	if cmd := cs["new-srv"].(map[string]any)["command"].(map[string]any); cmd["path"] != "npx" {
		t.Errorf("server not added: %v", cs["new-srv"])
	}
	if !strings.Contains(text, "\n    \"new-srv\": {\n      \"command\": {") {
		t.Errorf("new server not indented like its siblings:\n%s", text)
	}
	if strings.Contains(text, "stale") {
		t.Errorf("replaced server value still present:\n%s", text)
	}
	bak, err := os.ReadFile(path + ".bak")
	if err != nil || string(bak) != jsoncSettings {
		t.Errorf(".bak = %q, %v; want original", bak, err)
	}
}

func TestApply_AddsContextServersToJSONC(t *testing.T) {
	path := setup(t, "// mine\n{\n  \"theme\": \"x\", // c\n}\n")

	if err := zed.New().Apply([]schema.Server{{Name: "a/b~c", Command: "npx"}}); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	out, _ := os.ReadFile(path)
	if !strings.Contains(string(out), "// c") || !strings.Contains(string(out), "// mine") {
		t.Errorf("comments lost:\n%s", out)
	}
	if !strings.Contains(string(out), "// c\n  \"context_servers\": {\n    \"a/b~c\": {") {
		t.Errorf("context_servers not laid out on its own lines:\n%s", out)
	}
	m := standardize(t, out)
	if m["theme"] != "x" {
		t.Errorf("theme lost: %v", m)
	}
	if _, ok := m["context_servers"].(map[string]any)["a/b~c"]; !ok {
		t.Errorf("server with special characters not written: %v", m)
	}
}

func TestApply_InvalidJSONCFailsWithoutWrite(t *testing.T) {
	for name, content := range map[string]string{
		"syntax":               "{ \"theme\": ",
		"root array":           "[1]",
		"context_servers list": "{\"context_servers\": [] // x\n}",
	} {
		t.Run(name, func(t *testing.T) {
			path := setup(t, content)
			if err := zed.New().Apply([]schema.Server{{Name: "a", Command: "b"}}); err == nil {
				t.Fatal("Apply returned nil error")
			}
			if got, _ := os.ReadFile(path); string(got) != content {
				t.Errorf("file modified: %q", got)
			}
			if _, err := os.Stat(path + ".bak"); !os.IsNotExist(err) {
				t.Errorf(".bak created on failure")
			}
		})
	}
}

func TestApply_CreatesNewSettings(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("APPDATA", dir)
	path := zed.New().ConfigPath()
	if err := zed.New().Apply([]schema.Server{{Name: "a", Command: "b"}}); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	out, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := standardize(t, out)["context_servers"].(map[string]any)["a"]; !ok {
		t.Errorf("server not written:\n%s", out)
	}
	if _, err := os.Stat(path + ".bak"); !os.IsNotExist(err) {
		t.Errorf(".bak created for a new file")
	}
}

func TestApply_RemoteHTTPKeepsJSONC(t *testing.T) {
	path := setup(t, jsoncSettings)
	err := zed.New().Apply([]schema.Server{
		{Name: "replace-me", Transport: "http", URL: "https://example.com/mcp"},
		{Name: "streamed", Transport: "sse", URL: "https://example.com/sse"},
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	data, _ := os.ReadFile(path)
	for _, want := range []string{"// Zed settings", "// fav", "/* servers below */", "// keep me"} {
		if !strings.Contains(string(data), want) {
			t.Errorf("comment %q lost:\n%s", want, data)
		}
	}
	cs, _ := standardize(t, data)["context_servers"].(map[string]any)
	if got, want := cs["replace-me"], map[string]any{"url": "https://example.com/mcp"}; !reflect.DeepEqual(got, want) {
		t.Errorf("replace-me = %v, want %v", got, want)
	}
	if _, ok := cs["streamed"]; ok {
		t.Errorf("sse server written although Zed has no SSE transport: %v", cs["streamed"])
	}
}

func TestList_MapsRemoteHTTPServers(t *testing.T) {
	setup(t, `{"context_servers": {"remote": {"url": "https://example.com/mcp", "headers": {"X": "y"}}}}`)
	got, err := zed.New().List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 1 || got[0].Transport != "http" || got[0].URL != "https://example.com/mcp" || got[0].Command != "" {
		t.Errorf("List = %+v, want one http server at https://example.com/mcp", got)
	}
}
