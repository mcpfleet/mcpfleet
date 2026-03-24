package adapters

import (
	"github.com/mcpfleet/mcpfleet/internal/schema"
)

// Adapter writes MCP server configurations for a specific AI coding agent.
type Adapter interface {
	// Name returns the canonical agent name (e.g. "cursor", "crush").
	Name() string

	// ConfigPath returns the absolute path where the config file lives.
	ConfigPath() string

	// Apply writes or merges the given servers into the agent config.
	Apply(servers []schema.Server) error

	// List reads currently configured servers from the agent config.
	List() ([]schema.Server, error)
}
