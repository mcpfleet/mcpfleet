package crush

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
	URL     string            `json:"url,omitempty"`     // http | sse only
	Headers map[string]string `json:"headers,omitempty"` // http | sse only
}

// remoteEntry is the entry Apply writes for a remote (http or sse) server:
// the transport, its address and the HTTP headers, with no command, args or
// env. Crush reads "headers" for http and sse servers (MCPConfig.Headers in
// internal/config/config.go of charmbracelet/crush) and runs their values
// through shell expansion, so $VAR and $(cmd) are expanded.
type remoteEntry struct {
	Type    string            `json:"type"`
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers,omitempty"`
}

// mcpTypeStdio is Crush's type for a server started as a local command.
const mcpTypeStdio = "stdio"

// managedFields are the entry fields Apply owns: they are removed from an
// existing entry before the new one is laid over it, so the fields of both
// transports are listed and a switch drops the old transport's fields. Any
// other field (set by the user in the agent) is kept. Crush reads "type" for every transport.
var managedFields = []string{"type", "command", "args", "env", "url", "headers"}

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

// path is ConfigPath for reading and writing: it refuses a relative path,
// which an empty home directory (or a relative XDG_CONFIG_HOME or
// CRUSH_GLOBAL_CONFIG) gives, so the config is never resolved against the
// working directory.
func (a *Adapter) path() (string, error) {
	return jsonfile.RequireAbs(a.Name(), a.ConfigPath(), jsonfile.HomeVar(runtime.GOOS), "XDG_CONFIG_HOME")
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
			entries[s.Name] = remoteEntry{Type: s.Transport, URL: s.URL, Headers: schema.Values(s.Headers)}
			continue
		}
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
	if len(entries) == 0 && len(servers) > 0 {
		return nil // every server was skipped: leave the file alone
	}
	p, err := a.path()
	if err != nil {
		return err
	}
	// Merge into the existing file: only the entries named above change;
	// other keys and servers are kept, and an unparsable file is an error.
	if err := jsonfile.UpsertEntries(p, "mcp", entries, managedFields); err != nil {
		return fmt.Errorf("crush: %w", err)
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
			Name:      name,
			Transport: cmp.Or(entry.Type, "stdio"),
			URL:       entry.URL,
			Headers:   schema.Literals(entry.Headers),
			Command:   entry.Command,
			Args:      entry.Args,
			Env:       env,
		})
	}
	return servers, nil
}
