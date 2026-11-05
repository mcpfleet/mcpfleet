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

You use multiple AI coding agents — Cursor, Claude Code, Claude Desktop, Windsurf, Zed, Crush. Each has its own config file in a different location. Every time you add an MCP server or switch machines, you have to update each config manually.

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
- 🤖 **Multi-agent support** — claude-code, claude-desktop, crush, cursor, windsurf, zed
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
| `claude-code` | `~/.claude.json` (`mcpServers`, user scope); `$CLAUDE_CONFIG_DIR/.claude.json` when `CLAUDE_CONFIG_DIR` is set |
| `claude-desktop` | macOS: `~/Library/Application Support/Claude/claude_desktop_config.json`; Windows: `%APPDATA%\Claude\claude_desktop_config.json`; other: `~/.claude/claude_desktop_config.json` |
| `crush` | `~/.config/crush/mcp.json` |
| `cursor` | `~/.cursor/mcp.json` |
| `windsurf` | `~/.codeium/windsurf/mcp_config.json` |
| `zed` | `~/.config/zed/settings.json` |

`claude-code` adds servers to Claude Code's user scope, the same place `claude mcp add -s user` writes to, so they are available in every project. `~/.claude.json` also holds the rest of Claude Code's state (login, projects); `apply` changes only the `mcpServers` entries it writes and keeps the file's `0600` permissions. Claude Code may rewrite this file while it runs, so run `apply claude-code` with Claude Code closed, or start a new session afterwards, and check the result with `claude mcp list`.

> **Upgrading from an earlier version:** `apply claude-code` used to write to Claude Desktop's `claude_desktop_config.json`. Servers applied that way stay there. To keep updating that file, use `mcpfleet apply claude-desktop`.

### How `apply` changes your config

`apply` merges into the agent's existing config file instead of replacing it:

- Only the server entries being applied are added or replaced. Every other setting and every other server in the file stays as it was.
- Before writing, the previous file is saved next to it as `<file>.bak` (for example `~/.cursor/mcp.json.bak`), with the same permissions. Each `apply` overwrites this one backup, so it always holds the state from before the last run.
- If the file cannot be parsed (invalid JSON, or the servers key is not an object), `apply` stops with an error and writes nothing. Fix the file and run it again.
- JSON files are rewritten with 2-space indentation and keys in alphabetical order; the content is the same.
- Zed's `settings.json` may contain comments and trailing commas. They are kept: only the `context_servers` entries being applied change.
- The file is written atomically, and a symlinked config (for example from a dotfiles repo) is updated at the link's target.

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
