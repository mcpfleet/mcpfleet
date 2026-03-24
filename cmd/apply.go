package cmd

import (
	"fmt"

	"github.com/charmbracelet/log"
	"github.com/spf13/cobra"
)

func newApplyCmd() *cobra.Command {
	var applyAll bool
	var tags []string

	cmd := &cobra.Command{
		Use:   "apply [agent]",
		Short: "Apply MCP server definitions to an agent",
		Long: `Apply MCP server definitions from your registry to a specific AI coding agent.

Supported agents: cursor, claude-code, windsurf, zed, kilo, crush

Examples:
  mcpfleet apply cursor
  mcpfleet apply --all cursor
  mcpfleet apply --tag dev cursor
  mcpfleet apply --tag dev --tag vcs claude-code`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			agent := args[0]

			log.Info("Applying MCP servers", "agent", agent, "all", applyAll, "tags", tags)

			// TODO: implement
			// 1. Load config (registry URL, auth token)
			// 2. Fetch server definitions from registry
			// 3. Filter by tags if provided
			// 4. For each server: resolve secrets, check/install runtime
			// 5. Write config via agent adapter

			fmt.Printf("Applying servers to %s...\n", agent)
			return nil
		},
	}

	cmd.Flags().BoolVar(&applyAll, "all", false, "Apply all servers from registry")
	cmd.Flags().StringSliceVar(&tags, "tag", nil, "Filter servers by tag (can be used multiple times)")

	return cmd
}
