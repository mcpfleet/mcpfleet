package cmd

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/mcpfleet/mcpfleet/internal/registry"
)

func newSkillDeleteCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "delete <name>",
		Short:   "Remove a skill from the registry",
		Aliases: []string{"rm"},
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			client, err := registry.New()
			if err != nil {
				return fmt.Errorf("registry client: %w", err)
			}
			if err := client.DeleteSkill(cmd.Context(), name); err != nil {
				if errors.Is(err, registry.ErrNotFound) {
					return fmt.Errorf("skill %q not found in registry", name)
				}
				return fmt.Errorf("delete skill %q: %w", name, err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Skill %q removed from registry.\n", name)
			return nil
		},
	}
}
