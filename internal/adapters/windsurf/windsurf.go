package windsurf

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/mcpfleet/mcpfleet/internal/schema"
)

// windsurfSettings mirrors ~/.codeium/windsurf/mcp_config.json
type windsurfSettings struct {
	McpServers map[string]mcpEntry `json:"mcpServers"`
}

type mcpEntry struct {
	Command string            `json:"command"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
}

// Adapter implements adapters.Adapter for Windsurf.
type Adapter struct{}

func New() *Adapter { return &Adapter{} }

func (a *Adapter) Name() string { return "windsurf" }

func (a *Adapter) ConfigPath() string {
	home, _ := os.UserHomeDir()
	switch runtime.GOOS {
	case "windows":
		return filepath.Join(os.Getenv("APPDATA"), "Windsurf", "mcp_config.json")
	default:
		return filepath.Join(home, ".codeium", "windsurf", "mcp_config.json")
	}
}

func (a *Adapter) Apply(servers []schema.Server) error {
	cfgPath := a.ConfigPath()
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o755); err != nil {
		return fmt.Errorf("windsurf: create config dir: %w", err)
	}
	cfg := windsurfSettings{McpServers: make(map[string]mcpEntry)}
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
			entry.Env = make(map[string]string, len(s.Env))
			for k, v := range s.Env {
				entry.Env[k] = v.Value
			}
		}
		cfg.McpServers[s.Name] = entry
	}
	out, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("windsurf: marshal config: %w", err)
	}
	return os.WriteFile(cfgPath, out, 0o644)
}

func (a *Adapter) List() ([]schema.Server, error) {
	data, err := os.ReadFile(a.ConfigPath())
	if err != nil {
		return nil, fmt.Errorf("windsurf: read config: %w", err)
	}
	var cfg windsurfSettings
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("windsurf: parse config: %w", err)
	}
	var servers []schema.Server
	for name, entry := range cfg.McpServers {
		env := make(map[string]schema.EnvVar, len(entry.Env))
		for k, v := range entry.Env {
			env[k] = schema.EnvVar{Value: v}
		}
		servers = append(servers, schema.Server{
			Name:    name,
			Command: entry.Command,
			Args:    entry.Args,
			Env:     env,
		})
	}
	return servers, nil
}
