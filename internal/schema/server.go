package schema

// Server represents a single MCP server definition.
// This is the core data structure serialized to/from YAML, and to/from JSON
// when talking to the registry API (same key names).
type Server struct {
	Name        string            `yaml:"name" json:"name"`
	Description string            `yaml:"description,omitempty" json:"description,omitempty"`
	Transport   string            `yaml:"transport" json:"transport"` // stdio | sse | http
	Install     InstallConfig     `yaml:"install" json:"install"`
	Command     string            `yaml:"command" json:"command"`
	URL         string            `yaml:"url,omitempty" json:"url,omitempty"` // http | sse only: the remote server address
	Args        []string          `yaml:"args,omitempty" json:"args,omitempty"`
	Env         map[string]EnvVar `yaml:"env,omitempty" json:"env,omitempty"`
	Tags        []string          `yaml:"tags,omitempty" json:"tags,omitempty"`
	Platforms   []string          `yaml:"platforms,omitempty" json:"platforms,omitempty"` // linux, darwin, windows
}

// Transports of a server: stdio starts a local command, http and sse
// connect to a remote server at URL.
const (
	TransportStdio = "stdio"
	TransportHTTP  = "http"
	TransportSSE   = "sse"
)

// IsRemote reports whether the server is reached over the network (http or
// sse) instead of being started as a local command. An empty transport is
// stdio.
func (s Server) IsRemote() bool {
	return s.Transport == TransportHTTP || s.Transport == TransportSSE
}

// InstallConfig describes how to install the server runtime.
type InstallConfig struct {
	Type    string `yaml:"type" json:"type"`                           // npx | uvx | docker | binary | go
	Package string `yaml:"package" json:"package"`                     // e.g. "@modelcontextprotocol/server-github"
	Version string `yaml:"version,omitempty" json:"version,omitempty"` // defaults to "latest"
}

// EnvVar represents an environment variable for the server.
// It can be a literal value or a reference to a vault secret.
type EnvVar struct {
	// Literal value (not recommended for secrets)
	Value string `yaml:"value,omitempty" json:"value,omitempty"`
	// Reference to a named secret in the vault
	Secret string `yaml:"secret,omitempty" json:"secret,omitempty"`
}

// Manifest is the top-level structure of a fleet manifest file.
type Manifest struct {
	Version string   `yaml:"version" json:"version"`
	Servers []Server `yaml:"servers" json:"servers"`
}

// Example server definition (for documentation):
//
// name: github
// description: GitHub MCP server
// transport: stdio
// install:
//   type: npx
//   package: "@modelcontextprotocol/server-github"
//   version: latest
// command: npx
// args: ["-y", "@modelcontextprotocol/server-github"]
// env:
//   GITHUB_TOKEN:
//     secret: github_token
// tags: [dev, vcs]
// platforms: [linux, darwin, windows]
