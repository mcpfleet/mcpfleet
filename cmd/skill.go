package cmd

import "github.com/spf13/cobra"

func newSkillCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "skill",
		Short: "Work with Agent Skills",
		Long: `Work with Agent Skills (https://agentskills.io/specification): directories
with a SKILL.md file that teach an AI coding agent a task.

Examples:
  mcpfleet skill validate ./my-skill
  mcpfleet skill push ./my-skill
  mcpfleet skill list
  mcpfleet skill pull my-skill ~/.claude/skills
  mcpfleet skill delete my-skill`,
	}
	cmd.AddCommand(
		newSkillValidateCmd(),
		newSkillPushCmd(),
		newSkillPullCmd(),
		newSkillListCmd(),
		newSkillDeleteCmd(),
	)
	return cmd
}
