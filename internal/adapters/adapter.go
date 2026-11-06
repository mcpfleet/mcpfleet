package adapters

import (
	"fmt"

	"github.com/mcpfleet/mcpfleet/internal/schema"
)

// Adapter writes MCP server configurations for a specific AI coding agent.
type Adapter interface {
	// Name returns the canonical agent name (e.g. "cursor", "crush").
	Name() string

	// ConfigPath returns the absolute path where the config file lives.
	ConfigPath() string

	// SupportsTransport reports whether the agent's config file can hold a
	// server with the given transport ("stdio", "http", "sse"). An empty
	// transport means stdio, which every adapter supports.
	SupportsTransport(transport string) bool

	// Apply writes or merges the given servers into the agent config.
	// Servers the adapter cannot write (an unsupported transport, a remote
	// server without a URL) are left out; when every given server is left
	// out, the config file is not touched.
	Apply(servers []schema.Server) error

	// List reads currently configured servers from the agent config.
	List() ([]schema.Server, error)
}

// Skipped is a server that apply leaves out of an agent's config, with the
// reason shown to the user.
type Skipped struct {
	Server schema.Server
	Reason string
}

// Partition splits servers into the ones a can write and the ones it skips:
// servers whose transport a does not support, and remote servers without a
// URL. Both lists keep the order of servers.
func Partition(a Adapter, servers []schema.Server) (apply []schema.Server, skipped []Skipped) {
	for _, s := range servers {
		switch {
		case !a.SupportsTransport(s.Transport):
			skipped = append(skipped, Skipped{s, fmt.Sprintf("%s does not support %s servers in its config file", a.Name(), s.Transport)})
		case s.IsRemote() && s.URL == "":
			skipped = append(skipped, Skipped{s, fmt.Sprintf("server has transport %s but no url", s.Transport)})
		default:
			apply = append(apply, s)
		}
	}
	return apply, skipped
}
