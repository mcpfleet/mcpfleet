package cursor

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/mcpfleet/mcpfleet/internal/schema"
)

// mcpJSON mirrors Cursor's ~/.cursor/mcp.json format.
type mcpJSON struct {
	McpServers map[string]mcpEntry `json:"mcpServers"`
}

type mcpEntry struct {
	Command string            `json:"command"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
}

// Adapter implements adapters.Adapter for Cursor.
type Adapter struct{}

func New() *Adapter { return &Adapter{} }

func (a *Adapter) Name() string { return "cursor" }

func (a *Adapter) ConfigPath() string {
	home, _ := os.UserHomeDir()
	switch runtime.GOOS {
	case "windows":
		return filepath.Join(os.Getenv("APPDATA"), "Cursor", "mcp.json")
	default:
		return filepath.Join(home, ".cursor", "mcp.json")
	}
}

func (a *Adapter) Apply(servers []schema.Server) error {
	cfgPath := a.ConfigPath()
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o755); err != nil {
		return fmt.Errorf("cursor: create config dir: %w", err)
	}

	// Load existing config (if any) so we do a merge, not an overwrite.
	cfg := mcpJSON{McpServers: make(map[string]mcpEntry)}
	if data, err := os.ReadFile(cfgPath); err == nil {
		_ = json.Unmarshal(data, &cfg)
		if cfg.McpServers == nil {
			cfg.McpServers = make(map[string]mcpEntry)
		}
	}

	for _, s := range servers {
		entry := mcpEntry{
			Command: s.Command,
			Args:    s.Args,
		}
		if len(s.Env) > 0 {
			entry.Env = s.Env
		}
		cfg.McpServers[s.Name] = entry
	}

	out, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("cursor: marshal config: %w", err)
	}
	return os.WriteFile(cfgPath, out, 0o644)
}

func (a *Adapter) List() ([]schema.Server, error) {
	data, err := os.ReadFile(a.ConfigPath())
	if err != nil {
		return nil, fmt.Errorf("cursor: read config: %w", err)
	}
	var cfg mcpJSON
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("cursor: parse config: %w", err)
	}
	var servers []schema.Server
	for name, entry := range cfg.McpServers {
		servers = append(servers, schema.Server{
			Name:    name,
			Command: entry.Command,
			Args:    entry.Args,
			Env:     entry.Env,
		})
	}
	return servers, nil
}
