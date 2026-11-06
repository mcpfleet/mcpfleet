package claudecode

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

// claudeSettings mirrors the relevant part of Claude Code's user config.
// Claude Code keeps user-scoped MCP servers under the top-level "mcpServers"
// key of ~/.claude.json, next to the rest of its state (account, projects,
// ...), which must be left intact.
type claudeSettings struct {
	McpServers map[string]mcpEntry `json:"mcpServers"`
}

type mcpEntry struct {
	Type    string            `json:"type,omitempty"`
	Command string            `json:"command"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
	URL     string            `json:"url,omitempty"`     // http | sse only
	Headers map[string]string `json:"headers,omitempty"` // http | sse only
}

// remoteEntry is the entry Apply writes for a remote (http or sse) server:
// the transport, its address and the HTTP headers, with no command, args or
// env. Claude Code reads "headers" for http and sse servers
// (https://code.claude.com/docs/en/mcp, "claude mcp add --header") and
// expands ${VAR} in their values.
type remoteEntry struct {
	Type    string            `json:"type"`
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers,omitempty"`
}

// Adapter implements adapters.Adapter for Claude Code (claude.ai CLI).
type Adapter struct{}

func New() *Adapter { return &Adapter{} }

func (a *Adapter) Name() string { return "claude-code" }

// ConfigPath returns Claude Code's user config: $CLAUDE_CONFIG_DIR/.claude.json
// when the variable is set, otherwise ~/.claude.json on every OS.
func (a *Adapter) ConfigPath() string {
	if dir := os.Getenv("CLAUDE_CONFIG_DIR"); dir != "" {
		return filepath.Join(dir, ".claude.json")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claude.json")
}

// path is ConfigPath for reading and writing: it refuses a relative path,
// which an empty home directory or a relative CLAUDE_CONFIG_DIR gives, so the
// config is never resolved against the working directory.
func (a *Adapter) path() (string, error) {
	return jsonfile.RequireAbs(a.Name(), a.ConfigPath(), jsonfile.HomeVar(runtime.GOOS), "CLAUDE_CONFIG_DIR")
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
			Type:    "stdio",
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
	if err := jsonfile.UpsertEntries(p, "mcpServers", entries); err != nil {
		return fmt.Errorf("claude-code: %w", err)
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
		return nil, fmt.Errorf("claude-code: read config: %w", err)
	}
	var cfg claudeSettings
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("claude-code: parse config: %w", err)
	}
	var servers []schema.Server
	for name, entry := range cfg.McpServers {
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
