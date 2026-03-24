package cmd

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
)

var (
	headerStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#7C3AED"))
	nameStyle   = lipgloss.NewStyle().Bold(true)
	tagStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("#6B7280")).Italic(true)
)

func newListCmd() *cobra.Command {
	var filterTag string

	return &cobra.Command{
		Use:   "list",
		Short: "List all MCP servers in your registry",
		Aliases: []string{"ls"},
		RunE: func(cmd *cobra.Command, args []string) error {
			// TODO: fetch from registry
			fmt.Println(headerStyle.Render("MCP Servers"))
			fmt.Println()

			// Placeholder output
			servers := []struct {
				Name        string
				Description string
				Tags        []string
				Transport   string
			}{
				{"github", "GitHub MCP server", []string{"dev", "vcs"}, "stdio"},
				{"linear", "Linear project management", []string{"dev", "pm"}, "stdio"},
			}

			for _, s := range servers {
				if filterTag != "" {
					found := false
					for _, t := range s.Tags {
						if t == filterTag {
							found = true
							break
						}
					}
					if !found {
						continue
					}
				}
				fmt.Printf("%s  %s  %s\n",
					nameStyle.Render(s.Name),
					s.Description,
					tagStyle.Render(fmt.Sprintf("[%v]", s.Tags)),
				)
			}
			return nil
		},
	}
}
