package cmd

import "github.com/spf13/cobra"

func newSkillCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "skill",
		Short: "Work with Agent Skills",
		Long: `Work with Agent Skills (https://agentskills.io/specification): directories
with a SKILL.md file that teach an AI coding agent a task.

Examples:
  mcpfleet skill validate ./my-skill`,
	}
	cmd.AddCommand(newSkillValidateCmd())
	return cmd
}
