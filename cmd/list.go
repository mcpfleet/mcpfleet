package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/charmbracelet/lipgloss"
	"github.com/mcpfleet/mcpfleet/internal/registry"
	"github.com/spf13/cobra"
)

var (
	headerStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#7C3AED"))
)

func newListCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "list",
		Short:   "List all MCP servers in your registry",
		Aliases: []string{"ls"},
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := registry.New()
			if err != nil {
				return fmt.Errorf("registry client: %w", err)
			}

			servers, err := client.ListServers(context.Background())
			if err != nil {
				return fmt.Errorf("list servers: %w", err)
			}

			if len(servers) == 0 {
				fmt.Println("No servers registered.")
				return nil
			}

			fmt.Println(headerStyle.Render("MCP Servers"))
			fmt.Println()

			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "NAME\tCOMMAND\tARGS\tURL")
			fmt.Fprintln(w, "----\t-------\t----\t---")
			for _, s := range servers {
				command := ""
				if s.Command != nil {
					command = *s.Command
				}
				argsStr := strings.Join(s.Args, " ")
				url := ""
				if s.URL != nil {
					url = *s.URL
				}
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", s.Name, command, argsStr, url)
			}
			w.Flush()
			return nil
		},
	}
}

func init() {
	rootCmd.AddCommand(newListCmd())
}
