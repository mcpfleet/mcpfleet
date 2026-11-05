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
  mcpfleet apply cursor
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

The formula is a Homebrew cask in the [`mcpfleet/homebrew-tap`](https://github.com/mcpfleet/homebrew-tap) repository, updated on every release. The binaries are not signed or notarized yet; on macOS the cask removes the quarantine attribute so Gatekeeper does not block `mcpfleet`.

### curl (Linux / macOS)

```bash
curl -fsSL https://mcpfleet.dev/install.sh | sh
```

### Go install

```bash
go install github.com/mcpfleet/mcpfleet/cmd/mcpfleet@latest
```

### Download binary

Every [GitHub Release](https://github.com/mcpfleet/mcpfleet/releases) has archives for Linux, macOS and Windows on amd64 and arm64 (`mcpfleet_<version>_<os>_<arch>.tar.gz`, `.zip` on Windows) plus `checksums.txt`. Download the one for your platform, check it and put the binary on your `PATH`:

```bash
VERSION=0.1.0
curl -fsSLO "https://github.com/mcpfleet/mcpfleet/releases/download/v${VERSION}/mcpfleet_${VERSION}_linux_amd64.tar.gz"
curl -fsSLO "https://github.com/mcpfleet/mcpfleet/releases/download/v${VERSION}/checksums.txt"
sha256sum --ignore-missing -c checksums.txt
tar -xzf "mcpfleet_${VERSION}_linux_amd64.tar.gz" mcpfleet
sudo install mcpfleet /usr/local/bin/
```

### Check the installation

```bash
mcpfleet --version
# mcpfleet version 0.1.0 (commit <sha>, built <date>)
```

## Quick start

Describe your MCP servers in a YAML file. The file is a list of server definitions; only `name` is required:

```yaml
# servers.yaml
- name: filesystem
  transport: stdio
  install:
    type: npx
    package: "@modelcontextprotocol/server-filesystem"
  command: npx
  args: ["-y", "@modelcontextprotocol/server-filesystem", "/home/me/projects"]
  tags: [dev]

- name: github
  transport: stdio
  install:
    type: npx
    package: "@modelcontextprotocol/server-github"
  command: npx
  args: ["-y", "@modelcontextprotocol/server-github"]
  env:
    GITHUB_TOKEN:
      secret: github_token
  tags: [dev, vcs]
```

`env` maps each variable to either a `value` (a literal) or a `secret` (the name of a secret in your local vault, filled in when you apply).

```bash
# 1. Authenticate with the registry
mcpfleet auth login

# 2. Push the server definitions (creates new servers, updates existing ones)
mcpfleet push servers.yaml

# 3. Store the secrets your servers reference (env: GITHUB_TOKEN: {secret: github_token})
mcpfleet secret set github_token

# 4. Apply all servers to Cursor
mcpfleet apply cursor

# 5. Apply only 'dev'-tagged servers to Claude Code
mcpfleet apply --tag dev claude-code

# 6. List all servers in the registry
mcpfleet list

# 7. Save every server from the registry to a YAML file (same format as servers.yaml)
mcpfleet pull backup.yaml

# 8. Remove a server
mcpfleet delete filesystem
```

`secret set` asks for the value with a hidden prompt; it never takes the value as an argument. In scripts, pipe it in with `--stdin` (one trailing newline is dropped):

```bash
printf '%s' "$GITHUB_TOKEN" | mcpfleet secret set github_token --stdin
```

## Supported agents

| Agent | Config path |
|-------|-------------|
| `claude-code` | `~/.claude.json` (`mcpServers`, user scope); `$CLAUDE_CONFIG_DIR/.claude.json` when `CLAUDE_CONFIG_DIR` is set |
| `claude-desktop` | macOS: `~/Library/Application Support/Claude/claude_desktop_config.json`; Windows: `%APPDATA%\Claude\claude_desktop_config.json`; Linux and other systems: `~/.config/Claude/claude_desktop_config.json`, or `$XDG_CONFIG_HOME/Claude/claude_desktop_config.json` when `XDG_CONFIG_HOME` is set |
| `crush` | `~/.config/crush/crush.json` (`mcp`); `$XDG_CONFIG_HOME/crush/crush.json` when `XDG_CONFIG_HOME` is set; `$CRUSH_GLOBAL_CONFIG/crush.json` when `CRUSH_GLOBAL_CONFIG` is set. The same `~/.config` location is used on Windows (`%USERPROFILE%\.config\crush\crush.json`) |
| `cursor` | `~/.cursor/mcp.json` |
| `windsurf` | `~/.codeium/windsurf/mcp_config.json` (`mcpServers`); Windows: `%USERPROFILE%\.codeium\windsurf\mcp_config.json` |
| `zed` | `~/.config/zed/settings.json` |

`claude-code` adds servers to Claude Code's user scope, the same place `claude mcp add -s user` writes to, so they are available in every project. `~/.claude.json` also holds the rest of Claude Code's state (login, projects); `apply` changes only the `mcpServers` entries it writes and keeps the file's `0600` permissions. Claude Code may rewrite this file while it runs, so run `apply claude-code` with Claude Code closed, or start a new session afterwards, and check the result with `claude mcp list`.

> **Upgrading from an earlier version:** `apply claude-code` used to write to Claude Desktop's `claude_desktop_config.json`. Servers applied that way stay there. To keep updating that file, use `mcpfleet apply claude-desktop`.

`crush` writes Crush's global `crush.json`, each server under `mcp` with `"type": "stdio"`. Crush still reads `crush.json` but marks the JSON format as deprecated in favour of its `crushrc` script; if a `crushrc` in the same directory defines a server with the same name, that one wins. Crush expands `$VAR` and `$(...)` in `args` and `env`, so a value containing `$` is interpreted by Crush. Earlier versions of mcpfleet wrote `~/.config/crush/mcp.json`, which Crush never reads; run `mcpfleet apply crush` again and delete the old file.

On Linux, `claude-desktop` used to write `~/.claude/claude_desktop_config.json`, which Claude Desktop does not read. Run `mcpfleet apply claude-desktop` again; the old file can be deleted.

On Windows, `windsurf` used to write `%APPDATA%\Windsurf\mcp_config.json`, which Windsurf does not read. Run `mcpfleet apply windsurf` again; the old file can be deleted.

### How `apply` changes your config

`apply` merges into the agent's existing config file instead of replacing it:

- Only the server entries being applied are added or replaced. Every other setting and every other server in the file stays as it was. An applied entry is replaced as a whole, so options you added by hand to a server that mcpfleet manages (for example a timeout or disabled tools) are dropped on the next `apply`.
- Before writing, the previous file is saved next to it as `<file>.bak` (for example `~/.cursor/mcp.json.bak`), with the same permissions. Each `apply` overwrites this one backup, so it always holds the state from before the last run.
- A config file that does not exist yet is created with `0600` permissions (readable by you only), because it holds `env` values resolved from the vault. Directories `apply` has to create for it get `0700`. An existing file keeps its permissions, and so do existing directories.
- An `env` entry that references a secret (`{secret: github_token}`) is filled from the local vault. If a referenced secret is not in the vault, or the vault cannot be opened, `apply` stops with an error naming the secret and writes nothing. Store it with `mcpfleet secret set <name>` and run `apply` again.
- If the file cannot be parsed (invalid JSON, or the servers key is not an object), `apply` stops with an error and writes nothing. Fix the file and run it again.
- JSON files are rewritten with 2-space indentation and keys in alphabetical order; the content is the same.
- Zed's `settings.json` may contain comments and trailing commas. They are kept: only the `context_servers` entries being applied change.
- The file is written atomically, and a symlinked config (for example from a dotfiles repo) is updated at the link's target.

## Commands

```
mcpfleet auth login          Authenticate with the registry
mcpfleet auth status         Show whether you are logged in and the registry URL in use
mcpfleet auth logout         Remove the stored token
mcpfleet list                List all MCP servers in the registry
mcpfleet push <file>         Add or update the server definitions from a YAML file
mcpfleet pull <file>         Write every server definition from the registry to a YAML file
mcpfleet apply <agent>       Apply registry servers to an agent config
  --tag <tag>                  Apply only servers with the given tag (default: all servers)
mcpfleet delete <name>       Remove an MCP server from the registry
mcpfleet secret set <name>   Store or update a secret in the local vault (hidden prompt)
  --stdin                      Read the value from standard input
mcpfleet secret list         List secret names (never values)
mcpfleet secret rm <name>    Remove a secret from the local vault

Global flags:
  -v, --verbose                Print debug logs to stderr
```

Without `--verbose`, commands print only warnings and errors to stderr; their results go to stdout.

## Configuration

mcpfleet reads its registry URL from:
1. `MCPFLEET_REGISTRY_URL` environment variable
2. `~/.config/mcpfleet/registry_url` file
3. Default: `https://registry.mcpfleet.dev`

`mcpfleet auth status` shows the URL in use and where it came from:

```
✓ Logged in (token: mcp_************beae)
Registry: https://registry.example.com (from MCPFLEET_REGISTRY_URL)
```

The auth token is stored in `~/.config/mcpfleet/token` after `mcpfleet auth login`.

Secrets set with `mcpfleet secret set` are stored encrypted (AES-256-GCM) in `~/.config/mcpfleet/vault.enc`. The encryption key is kept in the OS keyring (macOS Keychain, GNOME Keyring/KWallet, Windows Credential Manager). They never leave your machine: the registry holds only the secret names.

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
