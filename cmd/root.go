package cmd

import (
	"fmt"

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
  mcpfleet apply cursor
  mcpfleet apply --tag dev claude-code`,
	// Every command logs to its own stderr stream, so logs follow
	// cmd.Run's writers instead of the process's os.Stderr, at debug level
	// with --verbose and warning level without it.
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		log.SetOutput(cmd.ErrOrStderr())
		verbose, err := cmd.Flags().GetBool("verbose")
		if err != nil {
			return err
		}
		if verbose {
			log.SetLevel(log.DebugLevel)
		} else {
			// Warnings and errors stay visible without --verbose.
			log.SetLevel(log.WarnLevel)
		}
		return nil
	},
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
	rootCmd.PersistentFlags().BoolP("verbose", "v", false, "Print debug logs to stderr")
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
}
