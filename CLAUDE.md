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
shellcheck -s sh install.sh scripts/test-install.sh   # lint the install script (CI runs this)
goreleaser release --snapshot --clean && sh scripts/test-install.sh dist   # test install.sh against snapshot archives (needs python3)
sh scripts/skills-ref-parity.sh # skill fixtures get the same verdict from skills-ref and mcpfleet (needs skills-ref on PATH, see the script)
sh scripts/e2e-skills.sh ../mcpfleet-registry   # cross-repo e2e: real registry + CLI, push -> apply --skill claude-code (needs a registry checkout, gcc, jq, skills-ref)
```

`pkg/skillspec` is the one implementation of the Agent Skills spec (parse, validate, hash, and the tar.gz archive format of `Pack`/`Unpack` with its limits); it is public because the registry module imports it, so it depends only on the standard library, `yaml.v3` and `x/text`. Its fixtures live in `pkg/skillspec/testdata/{valid,invalid}/<skill-name>/`: the directory says the verdict, and the CI job `skills-ref-parity` checks that `skills-ref` agrees. Where `skills-ref` and the spec text differ, `skills-ref` sets the verdict and the Go validator reports the spec rule as a warning. The same job runs `TestSkillRoundTrip` with `MCPFLEET_REQUIRE_SKILLS_REF=1`, so the tree `skill pull` writes back must pass `skills-ref validate`; without that variable the test only logs that skills-ref is missing. `TestApplySkillE2E` runs the same way there for the directory `apply --skill` installs into Claude Code's skills dir.

`install.sh` (POSIX sh) is what the README's `curl … | sh` runs. The main address `https://mcpfleet.dev/install.sh` serves the copy the website deploy (`mcpfleet/mcpfleet.github.io`) fetches from `main` on every deploy and on a daily schedule, so a broken commit to it reaches those users at the next deploy or daily run at the latest; the README's fallback via `raw.githubusercontent.com` serves `main` itself, so there it reaches users at once. `scripts/test-install.sh` serves the snapshot archives over a local HTTP server in the GitHub Releases URL layout and points the script at it with `MCPFLEET_REPO_URL`; a fake `uname` selects the platform.

CI (`.github/workflows/ci.yml`) runs a `gofmt -l .` check (it fails on any unformatted Go file), `go mod tidy`, build, vet and tests on Go 1.23 for every PR to `main`, plus the `skills-ref-parity` job and the `install-script` jobs: shellcheck and `scripts/test-install.sh` on Ubuntu (dash), Alpine amd64 and arm64 (busybox, `wget` without `curl`, arm64 via QEMU) and macOS. `.github/workflows/e2e-skills.yml` checks out `mcpfleet/mcpfleet-registry` at `main` and runs `scripts/e2e-skills.sh`: it builds and starts the registry on a fresh database, bootstraps a token, pushes `testdata/e2e/servers.yaml` and the fixture skill `testdata/e2e/skills/gh-notes/` (which requires the server `github`) with the real CLI, runs `apply --skill gh-notes claude-code` with an empty `HOME`, and checks the installed copy against the fixture (`diff -r` plus executable bits), with `skills-ref validate`, and that `~/.claude.json` holds `github`. A registry change on its `main` can turn this job red here. Run `gofmt -w .` before pushing.

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
