package cmd

import (
	"os"

	"github.com/charmbracelet/log"
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
  mcpfleet apply --all cursor
  mcpfleet apply --tag dev claude-code`,
}

func Execute() error {
	return rootCmd.Execute()
}

func init() {
	rootCmd.PersistentFlags().BoolP("verbose", "v", false, "Enable verbose output")
	rootCmd.AddCommand(
		newAuthCmd(),
		newListCmd(),
		newApplyCmd(),
		newPushCmd(),
	)

	// Silence default error output – we handle it ourselves
	rootCmd.SilenceErrors = true
	rootCmd.SilenceUsage = true

	// Setup logger
	log.SetOutput(os.Stderr)
}
