---
name: gh-notes
description: Summarize recent GitHub activity of a repository into short release notes. Use when the user asks what changed in a repository or wants draft release notes.
license: MIT
compatibility: Needs the github MCP server.
metadata:
  mcpfleet-requires: "github"
  version: "1.0"
---

# GitHub notes

1. Use the `github` MCP server to list the pull requests merged since the last tag.
2. Group them by the first word of their title, as `references/format.md` shows.
3. Run `scripts/hello.sh` to check that the skill was installed with its scripts.
