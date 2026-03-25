package cursor_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/mcpfleet/mcpfleet/internal/adapters/cursor"
	"github.com/mcpfleet/mcpfleet/internal/schema"
)

func TestAdapter_Name(t *testing.T) {
	a := cursor.New()
	if a.Name() != "cursor" {
		t.Errorf("Name() = %q, want \"cursor\"", a.Name())
	}
}

func TestAdapter_ConfigPath_NotEmpty(t *testing.T) {
	a := cursor.New()
	if a.ConfigPath() == "" {
		t.Error("ConfigPath() returned empty string")
	}
}

func TestAdapter_Apply_WritesConfig(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	a := cursor.New()
	servers := []schema.Server{
		{
			Name:    "my-server",
			Command: "npx",
			Args:    []string{"-y", "my-mcp-server"},
		},
	}

	if err := a.Apply(servers); err != nil {
		t.Fatalf("Apply() returned unexpected error: %v", err)
	}

	cfgPath := filepath.Join(tmpDir, ".cursor", "mcp.json")
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("config file not written at %s: %v", cfgPath, err)
	}

	var cfg struct {
		McpServers map[string]interface{} `json:"mcpServers"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("failed to parse config JSON: %v", err)
	}
	if _, ok := cfg.McpServers["my-server"]; !ok {
		t.Errorf("server \"my-server\" not found in written config; got: %v", cfg.McpServers)
	}
}

func TestAdapter_Apply_MergesExisting(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	a := cursor.New()

	// First apply: write server-a
	if err := a.Apply([]schema.Server{{Name: "server-a", Command: "cmd-a"}}); err != nil {
		t.Fatalf("first Apply() error: %v", err)
	}
	// Second apply: write server-b
	if err := a.Apply([]schema.Server{{Name: "server-b", Command: "cmd-b"}}); err != nil {
		t.Fatalf("second Apply() error: %v", err)
	}

	cfgPath := filepath.Join(tmpDir, ".cursor", "mcp.json")
	data, _ := os.ReadFile(cfgPath)
	var cfg struct {
		McpServers map[string]interface{} `json:"mcpServers"`
	}
	json.Unmarshal(data, &cfg)

	if _, ok := cfg.McpServers["server-a"]; !ok {
		t.Error("server-a missing after second apply (merge failed)")
	}
	if _, ok := cfg.McpServers["server-b"]; !ok {
		t.Error("server-b missing after second apply")
	}
}
