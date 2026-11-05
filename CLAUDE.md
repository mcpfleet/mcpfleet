# mcpfleet

Go CLI (with a Bubbletea TUI as the default command) that applies MCP server definitions from the central registry to the config files of AI coding agents (Cursor, Claude Code, Windsurf, Zed, Crush). The registry backend lives in `mcpfleet/mcpfleet-registry`; the website and docs in `mcpfleet/mcpfleet.github.io`.

## Commands

```bash
gofmt -l .                      # list unformatted files (CI fails if any; fix with gofmt -w .)
go build ./...                  # build every package
go vet ./...                    # static checks (CI runs this)
go test ./... -count=1          # unit tests (CI runs them with -v)
go run ./cmd/mcpfleet           # run the CLI / TUI locally
goreleaser check                # validate .goreleaser.yml (needs goreleaser installed)
```

CI (`.github/workflows/ci.yml`) runs a `gofmt -l .` check (it fails on any unformatted Go file), `go mod tidy`, build, vet and tests on Go 1.23 for every PR to `main`. Run `gofmt -w .` before pushing.

## Commit messages

Commits use `<ISSUE-ID>: <summary>` (e.g. `NER-123: add zed adapter`). The release changelog (`changelog.filters` in `.goreleaser.yml`) is built from these subjects and drops some of them, so name commits accordingly:

- a commit that only adds or changes tests starts with `<ISSUE-ID>: test …` (e.g. `NER-123: test zed adapter merge`);
- a commit that only changes CI starts with `<ISSUE-ID>: ci: …`;
- every other commit describes the user-visible change and must not start with the word `test` or `ci` (`NER-123: tests …` is kept, `NER-123: test-only flag` is dropped), or it disappears from the changelog.

Start-of-work commits (`Rozpoczęcie prac nad <ISSUE-ID>`) and merge commits are dropped as well.

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
