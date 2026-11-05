package cmd

import (
	"errors"
	"fmt"

	"github.com/mcpfleet/mcpfleet/internal/registry"
	"github.com/spf13/cobra"
)

func newDeleteCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "delete <name>",
		Short:   "Remove an MCP server from the registry",
		Aliases: []string{"rm"},
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]

			client, err := registry.New()
			if err != nil {
				return fmt.Errorf("registry client: %w", err)
			}

			if err := client.DeleteServer(cmd.Context(), name); err != nil {
				if errors.Is(err, registry.ErrNotFound) {
					return fmt.Errorf("server %q not found in registry", name)
				}
				return fmt.Errorf("delete server %q: %w", name, err)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Server %q removed from registry.\n", name)
			return nil
		},
	}
}
