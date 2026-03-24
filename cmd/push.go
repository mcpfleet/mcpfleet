package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newPushCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "push [file]",
		Short: "Push an MCP server definition to your registry",
		Example: `  mcpfleet push github.yaml
  mcpfleet push ./servers/linear.yaml`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			file := args[0]

			// TODO: implement
			// 1. Parse and validate YAML file
			// 2. Authenticate with registry
			// 3. Upload server definition

			fmt.Printf("Pushing %s to registry...\n", file)
			return nil
		},
	}
}
