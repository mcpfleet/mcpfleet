package crush

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/mcpfleet/mcpfleet/internal/schema"
)

// crushConfig mirrors Crush's MCP config file at ~/.crush/mcp.json
type crushConfig struct {
	McpServers map[string]mcpEntry `json:"mcpServers"`
}

type mcpEntry struct {
	Command string            `json:"command"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
}

// Adapter implements adapters.Adapter for Crush (opencode/crush).
type Adapter struct{}

func New() *Adapter { return &Adapter{} }

func (a *Adapter) Name() string { return "crush" }

func (a *Adapter) ConfigPath() string {
	home, _ := os.UserHomeDir()
	// Crush respects XDG_CONFIG_HOME, falling back to ~/.config/crush/mcp.json
	// and also supports ~/.crush/mcp.json for convenience.
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "crush", "mcp.json")
	}
	return filepath.Join(home, ".config", "crush", "mcp.json")
}

func (a *Adapter) Apply(servers []schema.Server) error {
	cfgPath := a.ConfigPath()
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o755); err != nil {
		return fmt.Errorf("crush: create config dir: %w", err)
	}

	cfg := crushConfig{McpServers: make(map[string]mcpEntry)}
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
		return fmt.Errorf("crush: marshal config: %w", err)
	}
	return os.WriteFile(cfgPath, out, 0o644)
}

func (a *Adapter) List() ([]schema.Server, error) {
	data, err := os.ReadFile(a.ConfigPath())
	if err != nil {
		return nil, fmt.Errorf("crush: read config: %w", err)
	}
	var cfg crushConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("crush: parse config: %w", err)
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
