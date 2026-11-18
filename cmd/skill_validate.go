package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/mcpfleet/mcpfleet/pkg/skillspec"
)

func newSkillValidateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "validate <dir>",
		Short: "Check a skill directory against the Agent Skills spec",
		Long: `Check a skill directory against the Agent Skills specification, with the
same verdict as the reference validator skills-ref.

Every violation is printed on its own line as "error: <field>: <message>",
and the command exits with 1. Recommendations of the spec, such as keeping
SKILL.md under 500 lines, are printed as "warning: …" and keep exit code 0.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir := args[0]
			res := skillspec.Validate(dir)
			out := cmd.OutOrStdout()
			for _, v := range res.Errors {
				fmt.Fprintf(out, "error: %s\n", v.Error())
			}
			for _, v := range res.Warnings {
				fmt.Fprintf(out, "warning: %s\n", v.Error())
			}
			if !res.Valid() {
				noun := "errors"
				if len(res.Errors) == 1 {
					noun = "error"
				}
				return fmt.Errorf("%s is not a valid skill (%d %s)", dir, len(res.Errors), noun)
			}
			fmt.Fprintf(out, "valid: %s\n", dir)
			return nil
		},
	}
}
