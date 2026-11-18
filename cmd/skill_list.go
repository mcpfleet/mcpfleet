package cmd

import (
	"fmt"
	"strings"
	"text/tabwriter"
	"unicode/utf8"

	"github.com/spf13/cobra"

	"github.com/mcpfleet/mcpfleet/internal/registry"
)

// maxListDescription is the number of characters of a description that
// "skill list" shows.
const maxListDescription = 60

func newSkillListCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "list",
		Short:   "List the skills in the registry",
		Aliases: []string{"ls"},
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := registry.New()
			if err != nil {
				return fmt.Errorf("registry client: %w", err)
			}
			skills, err := client.ListSkills(cmd.Context())
			if err != nil {
				return fmt.Errorf("list skills: %w", err)
			}

			out := cmd.OutOrStdout()
			if len(skills) == 0 {
				fmt.Fprintln(out, "No skills registered.")
				return nil
			}
			fmt.Fprintln(out, headerStyle.Render("Skills"))
			fmt.Fprintln(out)

			w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "NAME\tDESCRIPTION\tREQUIRES\tSHA256")
			fmt.Fprintln(w, "----\t-----------\t--------\t------")
			for _, s := range skills {
				requires := "-"
				if len(s.Requires) > 0 {
					requires = strings.Join(s.Requires, ",")
				}
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", s.Name, truncate(oneLine(s.Description), maxListDescription),
					requires, shortHash(s.SHA256))
			}
			return w.Flush()
		},
	}
}

// oneLine joins the lines of s with spaces, so a multi-line description
// keeps the table intact.
func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// truncate shortens s to at most n characters, ending with "…" when cut.
func truncate(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n-1]) + "…"
}
