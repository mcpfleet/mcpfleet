package schema

// Server represents a single MCP server definition.
// This is the core data structure serialized to/from YAML.
type Server struct {
	Name        string            `yaml:"name"`
	Description string            `yaml:"description,omitempty"`
	Transport   string            `yaml:"transport"` // stdio | sse | http
	Install     InstallConfig     `yaml:"install"`
	Command     string            `yaml:"command"`
	Args        []string          `yaml:"args,omitempty"`
	Env         map[string]EnvVar `yaml:"env,omitempty"`
	Tags        []string          `yaml:"tags,omitempty"`
	Platforms   []string          `yaml:"platforms,omitempty"` // linux, darwin, windows
}

// InstallConfig describes how to install the server runtime.
type InstallConfig struct {
	Type    string `yaml:"type"`    // npx | uvx | docker | binary | go
	Package string `yaml:"package"` // e.g. "@modelcontextprotocol/server-github"
	Version string `yaml:"version,omitempty"` // defaults to "latest"
}

// EnvVar represents an environment variable for the server.
// It can be a literal value or a reference to a vault secret.
type EnvVar struct {
	// Literal value (not recommended for secrets)
	Value string `yaml:"value,omitempty"`
	// Reference to a named secret in the vault
	Secret string `yaml:"secret,omitempty"`
}

// Manifest is the top-level structure of a fleet manifest file.
type Manifest struct {
	Version string   `yaml:"version"`
	Servers []Server `yaml:"servers"`
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
