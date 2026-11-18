package cmd

import (
	"fmt"
	"io"

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
			if err := validateSkill(cmd.OutOrStdout(), dir); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "valid: %s\n", dir)
			return nil
		},
	}
}

// validateSkill checks the skill in dir with skillspec.Validate, prints
// every violation as "error: …" and every recommendation as "warning: …" to
// out, and returns an error when the skill has violations. Warnings alone
// return nil.
func validateSkill(out io.Writer, dir string) error {
	res := skillspec.Validate(dir)
	for _, v := range res.Errors {
		fmt.Fprintf(out, "error: %s\n", v.Error())
	}
	for _, v := range res.Warnings {
		fmt.Fprintf(out, "warning: %s\n", v.Error())
	}
	if res.Valid() {
		return nil
	}
	noun := "errors"
	if len(res.Errors) == 1 {
		noun = "error"
	}
	return fmt.Errorf("%s is not a valid skill (%d %s)", dir, len(res.Errors), noun)
}
