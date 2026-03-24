# mcpfleet

> Vendor-agnostic CLI to manage and apply MCP server definitions across AI coding agents

[![Go](https://img.shields.io/badge/go-1.23-blue)](https://go.dev)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

## What is mcpfleet?

`mcpfleet` solves a simple but painful problem: when you use multiple AI coding agents (Cursor, Claude Code, Windsurf, Zed...) across multiple machines, keeping your MCP server configurations in sync is a mess.

With mcpfleet, you define your MCP servers once in a central registry, store secrets securely in a vault, and apply them to any agent with a single command.

```
new VPS / machine
    |
    v
curl -fsSL https://mcpfleet.dev/install.sh | sh
    |
    v
mcpfleet auth login
    |
    v
mcpfleet apply --all crush
    |
    v
All your MCP servers are ready.
```

## Quick Start

```bash
# Install
curl -fsSL https://mcpfleet.dev/install.sh | sh

# Authenticate with your registry
mcpfleet auth login

# List available MCP servers
mcpfleet list

# Apply all servers to an agent
mcpfleet apply --all cursor

# Apply only tagged servers
mcpfleet apply --tag dev claude-code

# Push a new server definition
mcpfleet push github.yaml

# Manage secrets
mcpfleet secret set github_token ghp_xxxx
```

## Server Definition Format

```yaml
name: github
description: GitHub MCP server
transport: stdio

install:
  type: npx
  package: "@modelcontextprotocol/server-github"
  version: latest

command: npx
args: ["-y", "@modelcontextprotocol/server-github"]

env:
  GITHUB_TOKEN:
    secret: github_token   # pulled from mcpfleet vault

tags: [dev, vcs]
platforms: [linux, darwin, windows]
```

## Supported Agents

| Agent | Config path | Status |
|---|---|---|
| Cursor | `~/.cursor/mcp.json` | Planned |
| Claude Code | `~/.claude.json` | Planned |
| Windsurf | `~/.codeium/windsurf/mcp_config.json` | Planned |
| Zed | `~/.config/zed/settings.json` | Planned |
| Kilo Code | VS Code `settings.json` | Planned |
| Crush | TBD | Planned |

## Architecture

```
mcpfleet/
├── cmd/mcpfleet/     # CLI entrypoint
├── cmd/              # Cobra commands (apply, auth, list, push, secret)
├── internal/
│   ├── schema/       # Server definition types
│   ├── adapters/     # Per-agent config writers (planned)
│   ├── registry/     # Registry HTTP client (planned)
│   ├── vault/        # Secret management (planned)
│   └── installer/    # Runtime installer - npx, uvx, docker (planned)
└── tui/              # Bubble Tea TUI components (planned)
```

## Tech Stack

- **Go 1.23** – single static binary, zero runtime dependencies
- **[Cobra](https://github.com/spf13/cobra)** – CLI framework
- **[Bubble Tea](https://github.com/charmbracelet/bubbletea)** – TUI framework
- **[Lip Gloss](https://github.com/charmbracelet/lipgloss)** – terminal styling
- **[Huh](https://github.com/charmbracelet/huh)** – interactive forms
- **[go-keyring](https://github.com/zalando/go-keyring)** – secure token storage

## License

MIT © [Paweł Wlazło](https://github.com/pawelwlazlo)
