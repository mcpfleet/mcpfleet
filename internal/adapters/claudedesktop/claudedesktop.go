package claudedesktop

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/mcpfleet/mcpfleet/internal/adapters/jsonfile"
	"github.com/mcpfleet/mcpfleet/internal/schema"
)

// claudeSettings mirrors the relevant part of Claude Desktop's
// claude_desktop_config.json.
type claudeSettings struct {
	McpServers map[string]mcpEntry `json:"mcpServers"`
}

type mcpEntry struct {
	Command string            `json:"command"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
}

// Adapter implements adapters.Adapter for the Claude Desktop app.
type Adapter struct{}

func New() *Adapter { return &Adapter{} }

func (a *Adapter) Name() string { return "claude-desktop" }

// ConfigPath returns Claude Desktop's claude_desktop_config.json for the
// current OS and environment.
func (a *Adapter) ConfigPath() string {
	home, _ := os.UserHomeDir()
	return configPath(runtime.GOOS, home, os.Getenv)
}

// configPath returns claude_desktop_config.json in Claude Desktop's data
// directory for the given OS, home directory and environment:
// %APPDATA%\Claude on Windows, ~/Library/Application Support/Claude on macOS,
// and $XDG_CONFIG_HOME/Claude (~/.config/Claude when the variable is unset)
// on Linux and other Unix systems, where Electron puts the app's data.
func configPath(goos, home string, getenv func(string) string) string {
	switch goos {
	case "windows":
		return filepath.Join(getenv("APPDATA"), "Claude", "claude_desktop_config.json")
	case "darwin":
		return filepath.Join(home, "Library", "Application Support", "Claude", "claude_desktop_config.json")
	}
	dir := getenv("XDG_CONFIG_HOME")
	if dir == "" {
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "Claude", "claude_desktop_config.json")
}

func (a *Adapter) Apply(servers []schema.Server) error {
	entries := make(map[string]any, len(servers))
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
		entries[s.Name] = entry
	}
	// Merge into the existing file: only the entries named above change;
	// other keys and servers are kept, and an unparsable file is an error.
	if err := jsonfile.UpsertEntries(a.ConfigPath(), "mcpServers", entries); err != nil {
		return fmt.Errorf("claude-desktop: %w", err)
	}
	return nil
}

func (a *Adapter) List() ([]schema.Server, error) {
	data, err := os.ReadFile(a.ConfigPath())
	if err != nil {
		return nil, fmt.Errorf("claude-desktop: read config: %w", err)
	}
	var cfg claudeSettings
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("claude-desktop: parse config: %w", err)
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
