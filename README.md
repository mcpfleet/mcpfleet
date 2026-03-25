<div align="center">

# mcpfleet

**Vendor-agnostic CLI to manage and apply MCP server definitions across AI coding agents**

[![CI](https://github.com/mcpfleet/mcpfleet/actions/workflows/ci.yml/badge.svg)](https://github.com/mcpfleet/mcpfleet/actions/workflows/ci.yml)
[![Go](https://img.shields.io/badge/go-1.23-blue)](https://go.dev)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)
[![Release](https://img.shields.io/github/v/release/mcpfleet/mcpfleet)](https://github.com/mcpfleet/mcpfleet/releases)

[Website](https://mcpfleet.dev) · [Registry API](https://github.com/mcpfleet/mcpfleet-registry) · [Report a bug](https://github.com/mcpfleet/mcpfleet/issues)

</div>

---

## The problem

You use multiple AI coding agents — Cursor, Claude Code, Windsurf, Zed, Crush. Each has its own config file in a different location. Every time you add an MCP server or switch machines, you have to update each config manually.

**mcpfleet** fixes this. Define your MCP servers once in a central registry, then apply them everywhere with a single command.

```
new machine setup:
  curl -fsSL https://mcpfleet.dev/install.sh | sh
  mcpfleet auth login
  mcpfleet apply --all cursor
  # done.
```

## Features

- 📦 **Central registry** — all your MCP server definitions in one place
- 🔐 **Encrypted vault** — secrets stored with AES-256-GCM, keys in OS keyring
- 🤖 **Multi-agent support** — cursor, claude-code, windsurf, zed, crush
- 🏷️ **Tag filtering** — apply only servers tagged `dev`, `vcs`, etc.
- 🔄 **Non-destructive merge** — existing agent configs are preserved
- ↔️ **Cross-platform** — macOS, Linux, Windows (amd64 + arm64)

## Installation

### Homebrew (macOS / Linux)

```bash
brew install mcpfleet/tap/mcpfleet
```

### curl (Linux / macOS)

```bash
curl -fsSL https://mcpfleet.dev/install.sh | sh
```

### Go install

```bash
go install github.com/mcpfleet/mcpfleet/cmd/mcpfleet@latest
```

### Download binary

Grab the latest binary from [GitHub Releases](https://github.com/mcpfleet/mcpfleet/releases).

## Quick start

```bash
# 1. Authenticate with the registry
mcpfleet auth login

# 2. Push an MCP server definition
mcpfleet push my-server \
  --command npx \
  --args "-y,@modelcontextprotocol/server-filesystem" \
  --tag dev

# 3. Apply all servers to Cursor
mcpfleet apply --all cursor

# 4. Apply only 'dev'-tagged servers to Claude Code
mcpfleet apply --tag dev claude-code

# 5. List all servers in the registry
mcpfleet list

# 6. Remove a server
mcpfleet delete my-server
```

## Supported agents

| Agent | Config path |
|-------|-------------|
| `cursor` | `~/.cursor/mcp.json` |
| `claude-code` | `~/Library/Application Support/Claude/claude_desktop_config.json` |
| `windsurf` | `~/.codeium/windsurf/mcp_config.json` |
| `zed` | `~/.config/zed/settings.json` |
| `crush` | `~/.config/crush/mcp.json` |

## Commands

```
mcpfleet auth login          Authenticate with the registry
mcpfleet list                List all MCP servers in the registry
mcpfleet push <name>         Add or update an MCP server definition
mcpfleet pull                Pull latest server definitions from registry
mcpfleet apply <agent>       Apply registry servers to an agent config
  --all                        Apply all servers (ignore tag filter)
  --tag <tag>                  Apply only servers with the given tag
mcpfleet delete <name>       Remove an MCP server from the registry
```

## Configuration

mcpfleet reads its registry URL from:
1. `MCPFLEET_REGISTRY_URL` environment variable
2. `~/.config/mcpfleet/registry_url` file
3. Default: `https://registry.mcpfleet.dev`

The auth token is stored in `~/.config/mcpfleet/token` after `mcpfleet auth login`.

## Self-hosting

You can run your own registry with [mcpfleet-registry](https://github.com/mcpfleet/mcpfleet-registry):

```bash
git clone https://github.com/mcpfleet/mcpfleet-registry
cd mcpfleet-registry
docker compose up -d

# Point mcpfleet at your instance
export MCPFLEET_REGISTRY_URL=http://localhost:8080
mcpfleet auth login
```

## Development

```bash
git clone https://github.com/mcpfleet/mcpfleet
cd mcpfleet
go mod tidy
go test ./...
go build -o mcpfleet ./cmd/mcpfleet
```

## Contributing

Pull requests are welcome! Please open an issue first to discuss major changes.

## License

[MIT](LICENSE)
