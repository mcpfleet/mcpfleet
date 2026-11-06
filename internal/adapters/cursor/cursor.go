package cursor

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/mcpfleet/mcpfleet/internal/adapters/jsonfile"
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
	URL     string            `json:"url,omitempty"` // remote servers only
}

// remoteEntry is the entry Apply writes for a remote server. Cursor takes a
// "url" in place of "command" and detects the transport (Streamable HTTP or
// SSE) itself, so the entry carries no type.
type remoteEntry struct {
	URL string `json:"url"`
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

// path is ConfigPath for reading and writing: it refuses a relative path,
// which an empty APPDATA (Windows) or home directory gives, so the config is
// never resolved against the working directory.
func (a *Adapter) path() (string, error) {
	v := jsonfile.HomeVar(runtime.GOOS)
	if runtime.GOOS == "windows" {
		v = "APPDATA"
	}
	return jsonfile.RequireAbs(a.Name(), a.ConfigPath(), v)
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
			entries[s.Name] = remoteEntry{URL: s.URL}
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
	p, err := a.path()
	if err != nil {
		return err
	}
	if err := jsonfile.UpsertEntries(p, "mcpServers", entries); err != nil {
		return fmt.Errorf("cursor: %w", err)
	}
	return nil
}

func (a *Adapter) List() ([]schema.Server, error) {
	p, err := a.path()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return nil, fmt.Errorf("cursor: read config: %w", err)
	}
	var cfg mcpJSON
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("cursor: parse config: %w", err)
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
		if u := entry.URL; u != "" && entry.Command == "" {
			// A remote entry has no type: report it as http, the
			// transport the agent tries first.
			srv.Transport = schema.TransportHTTP
			srv.URL = u
		}
		servers = append(servers, srv)
	}
	return servers, nil
}
