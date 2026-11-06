package cmd

import (
	"context"
	"fmt"
	"strings"
	"text/tabwriter"

	"github.com/charmbracelet/lipgloss"
	"github.com/mcpfleet/mcpfleet/internal/registry"
	"github.com/mcpfleet/mcpfleet/internal/schema"
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

			out := cmd.OutOrStdout()
			if len(servers) == 0 {
				fmt.Fprintln(out, "No servers registered.")
				return nil
			}

			fmt.Fprintln(out, headerStyle.Render("MCP Servers"))
			fmt.Fprintln(out)

			w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "NAME\tTRANSPORT\tTARGET")
			fmt.Fprintln(w, "----\t---------\t------")
			for _, s := range servers {
				fmt.Fprintf(w, "%s\t%s\t%s\n", s.Name, listTransport(s), listTarget(s))
			}
			w.Flush()
			return nil
		},
	}
}

// listTransport returns the server's transport, treating an empty value as
// stdio (the registry's default for servers without a transport).
func listTransport(s schema.Server) string {
	if s.Transport == "" {
		return schema.TransportStdio
	}
	return s.Transport
}

// listTarget returns where the server runs: its URL for remote transports,
// otherwise the command with its arguments. An empty target prints as "-"
// so the missing value stays visible in the table.
func listTarget(s schema.Server) string {
	var target string
	if s.IsRemote() {
		target = s.URL
	} else {
		target = strings.TrimSpace(s.Command + " " + strings.Join(s.Args, " "))
	}
	if target == "" {
		return "-"
	}
	return target
}
