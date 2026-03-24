package zed

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/mcpfleet/mcpfleet/internal/schema"
)

// zedSettings mirrors Zed's ~/.config/zed/settings.json (partial).
// Zed stores MCP context servers under the "context_servers" key.
type zedSettings struct {
	ContextServers map[string]contextServer `json:"context_servers"`
}

type contextServer struct {
	Command contextCommand `json:"command"`
}

type contextCommand struct {
	Path string            `json:"path"`
	Args []string          `json:"args,omitempty"`
	Env  map[string]string `json:"env,omitempty"`
}

// Adapter implements adapters.Adapter for Zed editor.
type Adapter struct{}

func New() *Adapter { return &Adapter{} }

func (a *Adapter) Name() string { return "zed" }

func (a *Adapter) ConfigPath() string {
	home, _ := os.UserHomeDir()
	switch runtime.GOOS {
	case "windows":
		return filepath.Join(os.Getenv("APPDATA"), "Zed", "settings.json")
	default:
		return filepath.Join(home, ".config", "zed", "settings.json")
	}
}

func (a *Adapter) Apply(servers []schema.Server) error {
	cfgPath := a.ConfigPath()
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o755); err != nil {
		return fmt.Errorf("zed: create config dir: %w", err)
	}

	// Zed settings may contain many other keys – we must do a surgical merge
	// using a raw map so we don't clobber unrelated settings.
	raw := make(map[string]json.RawMessage)
	if data, err := os.ReadFile(cfgPath); err == nil {
		_ = json.Unmarshal(data, &raw)
	}

	// Decode existing context_servers (if any).
	contextServers := make(map[string]contextServer)
	if existing, ok := raw["context_servers"]; ok {
		_ = json.Unmarshal(existing, &contextServers)
	}

	for _, s := range servers {
		contextServers[s.Name] = contextServer{
			Command: contextCommand{
				Path: s.Command,
				Args: s.Args,
				Env:  s.Env,
			},
		}
	}

	encoded, err := json.Marshal(contextServers)
	if err != nil {
		return fmt.Errorf("zed: encode context_servers: %w", err)
	}
	raw["context_servers"] = encoded

	out, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return fmt.Errorf("zed: marshal settings: %w", err)
	}
	return os.WriteFile(cfgPath, out, 0o644)
}

func (a *Adapter) List() ([]schema.Server, error) {
	data, err := os.ReadFile(a.ConfigPath())
	if err != nil {
		return nil, fmt.Errorf("zed: read config: %w", err)
	}
	var cfg zedSettings
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("zed: parse config: %w", err)
	}
	var servers []schema.Server
	for name, cs := range cfg.ContextServers {
		servers = append(servers, schema.Server{
			Name:    name,
			Command: cs.Command.Path,
			Args:    cs.Command.Args,
			Env:     cs.Command.Env,
		})
	}
	return servers, nil
}
