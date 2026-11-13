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

// managedFields are the entry fields Apply owns: they are removed from an
// existing entry before the new one is laid over it, so the fields of both
// transports are listed and a switch drops the old transport's fields. Any
// other field (set by the user in the agent) is kept. Claude Desktop only
// runs local (stdio) servers.
var managedFields = []string{"command", "args", "env"}

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

// path is ConfigPath for reading and writing: it refuses a relative path,
// which an environment without APPDATA, USERPROFILE or HOME would give, so
// the config is never resolved against the working directory.
func (a *Adapter) path() (string, error) {
	return jsonfile.RequireAbs(a.Name(), a.ConfigPath(), "APPDATA or USERPROFILE on Windows, HOME elsewhere")
}

// configPath returns claude_desktop_config.json in Claude Desktop's data
// directory for the given OS, home directory and environment:
// %APPDATA%\Claude on Windows (%USERPROFILE%\AppData\Roaming\Claude when
// APPDATA is empty), ~/Library/Application Support/Claude on macOS,
// and $XDG_CONFIG_HOME/Claude (~/.config/Claude when the variable is unset)
// on Linux and other Unix systems, where Electron puts the app's data.
func configPath(goos, home string, getenv func(string) string) string {
	switch goos {
	case "windows":
		// Electron resolves appData from the Roaming known folder, not from
		// the APPDATA variable; its default is %USERPROFILE%\AppData\Roaming,
		// which os.UserHomeDir returns as home on Windows. A redirected
		// Roaming folder sets APPDATA, so the fallback only covers an
		// environment stripped of it.
		dir := getenv("APPDATA")
		if dir == "" {
			dir = filepath.Join(home, "AppData", "Roaming")
		}
		return filepath.Join(dir, "Claude", "claude_desktop_config.json")
	case "darwin":
		return filepath.Join(home, "Library", "Application Support", "Claude", "claude_desktop_config.json")
	}
	dir := getenv("XDG_CONFIG_HOME")
	if dir == "" {
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "Claude", "claude_desktop_config.json")
}

// SupportsTransport reports whether the config file can hold a server with
// the given transport. claude_desktop_config.json holds local (stdio) servers
// only; Claude Desktop adds remote servers in Settings → Connectors instead.
func (a *Adapter) SupportsTransport(transport string) bool {
	return transport == "" || transport == schema.TransportStdio
}

func (a *Adapter) Apply(servers []schema.Server) error {
	entries := make(map[string]any, len(servers))
	for _, s := range servers {
		if !a.SupportsTransport(s.Transport) {
			continue // remote servers have no command to write
		}
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
	if len(entries) == 0 && len(servers) > 0 {
		return nil // every server was skipped: leave the file alone
	}
	path, err := a.path()
	if err != nil {
		return err
	}
	// Merge into the existing file: only the entries named above change;
	// other keys and servers are kept, and an unparsable file is an error.
	if err := jsonfile.UpsertEntries(path, "mcpServers", entries, managedFields); err != nil {
		return fmt.Errorf("claude-desktop: %w", err)
	}
	return nil
}

func (a *Adapter) List() ([]schema.Server, error) {
	path, err := a.path()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
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
