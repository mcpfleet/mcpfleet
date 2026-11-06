package windsurf

import (
	"cmp"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/mcpfleet/mcpfleet/internal/adapters/jsonfile"
	"github.com/mcpfleet/mcpfleet/internal/schema"
)

// windsurfSettings mirrors <home>/.codeium/windsurf/mcp_config.json
type windsurfSettings struct {
	McpServers map[string]mcpEntry `json:"mcpServers"`
}

type mcpEntry struct {
	Command   string            `json:"command"`
	Args      []string          `json:"args,omitempty"`
	Env       map[string]string `json:"env,omitempty"`
	ServerURL string            `json:"serverUrl,omitempty"` // remote servers only
	URL       string            `json:"url,omitempty"`       // accepted as an alias of serverUrl
}

// remoteEntry is the entry Apply writes for a remote server. Windsurf takes
// a "serverUrl" in place of "command" and detects the transport (Streamable
// HTTP or SSE) itself, so the entry carries no type.
type remoteEntry struct {
	ServerURL string `json:"serverUrl"`
}

// Adapter implements adapters.Adapter for Windsurf.
type Adapter struct{}

func New() *Adapter { return &Adapter{} }

func (a *Adapter) Name() string { return "windsurf" }

func (a *Adapter) ConfigPath() string {
	home, _ := os.UserHomeDir()
	return configPath(runtime.GOOS, home, os.Getenv)
}

// configPath returns Windsurf's MCP config file for the given OS, home
// directory and environment. Windsurf keeps it under the home directory on
// every OS (%USERPROFILE%\.codeium\windsurf on Windows, not %APPDATA%), so
// goos and getenv do not change the result; they are parameters so the
// per-OS layout is pinned by a test on any runner.
func configPath(goos, home string, getenv func(string) string) string {
	return filepath.Join(home, ".codeium", "windsurf", "mcp_config.json")
}

// SupportsTransport reports whether the config file can hold a server with
// the given transport: stdio (also the empty transport), http and sse.
func (a *Adapter) SupportsTransport(transport string) bool {
	switch transport {
	case "", schema.TransportStdio, schema.TransportHTTP, schema.TransportSSE:
		return true
	}
	return false
}

func (a *Adapter) Apply(servers []schema.Server) error {
	entries := make(map[string]any, len(servers))
	for _, s := range servers {
		if !a.SupportsTransport(s.Transport) || (s.IsRemote() && s.URL == "") {
			continue // never write an entry with an empty command
		}
		if s.IsRemote() {
			entries[s.Name] = remoteEntry{ServerURL: s.URL}
			continue
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
	// Merge into the existing file: only the entries named above change;
	// other keys and servers are kept, and an unparsable file is an error.
	if err := jsonfile.UpsertEntries(a.ConfigPath(), "mcpServers", entries); err != nil {
		return fmt.Errorf("windsurf: %w", err)
	}
	return nil
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
		srv := schema.Server{
			Name:    name,
			Command: entry.Command,
			Args:    entry.Args,
			Env:     env,
		}
		if u := cmp.Or(entry.ServerURL, entry.URL); u != "" && entry.Command == "" {
			// A remote entry has no type: report it as http, the
			// transport the agent tries first.
			srv.Transport = schema.TransportHTTP
			srv.URL = u
		}
		servers = append(servers, srv)
	}
	return servers, nil
}
