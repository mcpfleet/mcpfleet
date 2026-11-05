package cmd

import (
	"fmt"
	"os"

	"github.com/charmbracelet/log"
	"github.com/mcpfleet/mcpfleet/internal/tui"
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "mcpfleet",
	Short: "Manage and apply MCP server definitions across AI coding agents",
	Long: `mcpfleet is a vendor-agnostic CLI for managing MCP (Model Context Protocol)
server definitions. Define your servers once, apply them to any AI coding agent.

Examples:
  mcpfleet auth login
  mcpfleet list
  mcpfleet secret set github_token
  mcpfleet apply --all cursor
  mcpfleet apply --tag dev claude-code`,
	// When no subcommand is provided, show the TUI
	RunE: func(cmd *cobra.Command, args []string) error {
		return tui.Run()
	},
}

// SetVersionInfo sets the build information printed by "mcpfleet --version".
// main passes the values injected by GoReleaser through -ldflags.
func SetVersionInfo(version, commit, date string) {
	rootCmd.Version = fmt.Sprintf("%s (commit %s, built %s)", version, commit, date)
}

func init() {
	rootCmd.PersistentFlags().BoolP("verbose", "v", false, "Enable verbose output")
	rootCmd.AddCommand(
		newAuthCmd(),
		newListCmd(),
		newApplyCmd(),
		newPushCmd(),
		newDeleteCmd(),
		newSecretCmd(),
	)
	// Silence default error output – we handle it ourselves
	rootCmd.SilenceErrors = true
	rootCmd.SilenceUsage = true
	// Setup logger
	log.SetOutput(os.Stderr)
}
