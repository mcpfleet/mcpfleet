package crush

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/mcpfleet/mcpfleet/internal/adapters/jsonfile"
	"github.com/mcpfleet/mcpfleet/internal/schema"
)

// crushConfig mirrors the MCP part of Crush's global crush.json: servers
// live under the top-level "mcp" key, and each entry needs a "type"
// (stdio, sse or http). See internal/config/config.go in charmbracelet/crush.
type crushConfig struct {
	MCP map[string]mcpEntry `json:"mcp"`
}

type mcpEntry struct {
	Type    string            `json:"type"`
	Command string            `json:"command"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
}

// mcpTypeStdio is Crush's type for a server started as a local command.
const mcpTypeStdio = "stdio"

// Adapter implements adapters.Adapter for Crush (charmbracelet/crush).
type Adapter struct{}

func New() *Adapter { return &Adapter{} }

func (a *Adapter) Name() string { return "crush" }

func (a *Adapter) ConfigPath() string {
	home, _ := os.UserHomeDir()
	return configPath(runtime.GOOS, home, os.Getenv)
}

// configPath returns Crush's global JSON config the way Crush's own
// GlobalConfig() resolves it: $CRUSH_GLOBAL_CONFIG/crush.json, else
// $XDG_CONFIG_HOME/crush/crush.json, else <home>/.config/crush/crush.json.
// Crush uses <home>/.config on Windows too, so goos does not change the
// result; it is a parameter so the per-OS layout is pinned by a test.
func configPath(goos, home string, getenv func(string) string) string {
	if dir := getenv("CRUSH_GLOBAL_CONFIG"); dir != "" {
		return filepath.Join(dir, "crush.json")
	}
	configHome := getenv("XDG_CONFIG_HOME")
	if configHome == "" {
		configHome = filepath.Join(home, ".config")
	}
	return filepath.Join(configHome, "crush", "crush.json")
}

func (a *Adapter) Apply(servers []schema.Server) error {
	entries := make(map[string]any, len(servers))
	for _, s := range servers {
		entry := mcpEntry{
			Type:    mcpTypeStdio,
			Command: s.Command,
			Args:    s.Args,
		}
		if len(s.Env) > 0 {
			entry.Env = make(map[string]string, len(s.Env))
			for k, v := range s.Env {
				entry.Env[k] = v.Value
			}
		}
		entries[s.Name] = entry
	}
	// Merge into the existing file: only the entries named above change;
	// other keys and servers are kept, and an unparsable file is an error.
	if err := jsonfile.UpsertEntries(a.ConfigPath(), "mcp", entries, 0o644); err != nil {
		return fmt.Errorf("crush: %w", err)
	}
	return nil
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
	for name, entry := range cfg.MCP {
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
