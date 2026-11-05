# mcpfleet

Go CLI (with a Bubbletea TUI as the default command) that applies MCP server definitions from the central registry to the config files of AI coding agents (Cursor, Claude Code, Windsurf, Zed, Crush). The registry backend lives in `mcpfleet/mcpfleet-registry`; the website and docs in `mcpfleet/mcpfleet.github.io`.

## Commands

```bash
go build ./...                  # build every package
go vet ./...                    # static checks (CI runs this)
go test ./... -count=1          # unit tests (CI runs them with -v)
go run ./cmd/mcpfleet           # run the CLI / TUI locally
goreleaser check                # validate .goreleaser.yml (needs goreleaser installed)
```

CI (`.github/workflows/ci.yml`) runs `go mod tidy`, build, vet and tests on Go 1.23 for every PR to `main`.

## Platform

```yaml
tracker: linear
vcs: github
linear:
  team: NER
  project: 056e7cde-601b-4c2b-a981-5015d37a3de4
github:
  owner: mcpfleet
  repo: mcpfleet
```
