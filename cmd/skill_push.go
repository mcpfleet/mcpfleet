package cmd

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/mcpfleet/mcpfleet/internal/registry"
	"github.com/mcpfleet/mcpfleet/pkg/skillspec"
)

// shortHash is the prefix of a skill hash the CLI prints.
func shortHash(sha string) string {
	if len(sha) > 12 {
		return sha[:12]
	}
	return sha
}

func newSkillPushCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "push <dir>",
		Short: "Validate a skill directory and upload it to the registry",
		Long: `Validate a skill directory against the Agent Skills specification and
upload it to the registry under its name, replacing an earlier version.

A skill with violations is not uploaded: they are printed the way
"mcpfleet skill validate" prints them. Warnings are printed and do not stop
the upload. The directory is sent as a tar.gz archive (at most 1 MiB) that
keeps the executable bit of every file and leaves .git directories out.
Every server the skill lists in metadata.mcpfleet-requires must already be
in the registry.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir := args[0]
			if err := validateSkill(cmd.OutOrStdout(), dir); err != nil {
				return fmt.Errorf("%w, nothing was pushed", err)
			}
			skill, err := skillspec.Parse(dir)
			if err != nil {
				return fmt.Errorf("read skill %s: %w", dir, err)
			}
			archive, err := skillspec.Pack(dir, skill.Name)
			if err != nil {
				return fmt.Errorf("pack skill %q: %w", skill.Name, err)
			}

			client, err := registry.New()
			if err != nil {
				return fmt.Errorf("registry client: %w", err)
			}
			res, err := client.PutSkill(cmd.Context(), skill.Name, archive)
			if err != nil {
				var apiErr *registry.APIError
				if errors.As(err, &apiErr) {
					if missing := apiErr.MissingServers(); len(missing) > 0 {
						return fmt.Errorf("push skill %q: the registry does not have the servers it requires: %s "+
							"(push them first with 'mcpfleet push <file>')", skill.Name, strings.Join(missing, ", "))
					}
				}
				return fmt.Errorf("push skill %q: %w", skill.Name, err)
			}
			if res.SHA256 != skill.SHA256 {
				return fmt.Errorf("push skill %q: the registry stored sha256 %s, but the local directory has %s",
					skill.Name, res.SHA256, skill.SHA256)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "pushed %s %s\n", skill.Name, shortHash(res.SHA256))
			return nil
		},
	}
}
