package main

import (
	"os"

	"github.com/mcpfleet/mcpfleet/cmd"
)

// Set by GoReleaser through -ldflags "-X main.version=... -X main.commit=... -X main.date=...".
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	cmd.SetVersionInfo(version, commit, date)
	os.Exit(cmd.Run(os.Args[1:], os.Stdout, os.Stderr))
}
