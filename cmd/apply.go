package cmd

import (
	"context"
	"fmt"

	"github.com/charmbracelet/log"
	"github.com/spf13/cobra"

	"github.com/mcpfleet/mcpfleet/internal/adapters"
	"github.com/mcpfleet/mcpfleet/internal/registry"
	"github.com/mcpfleet/mcpfleet/internal/schema"
	"github.com/mcpfleet/mcpfleet/internal/vault"
)

func newApplyCmd() *cobra.Command {
	var applyAll bool
	var tags []string

	cmd := &cobra.Command{
		Use:   "apply [agent]",
		Short: "Apply MCP server definitions to an agent",
		Long: `Apply MCP server definitions from your registry to a specific AI coding agent.

Supported agents: cursor, claude-code, windsurf, zed, crush

Examples:
  mcpfleet apply cursor
  mcpfleet apply --all cursor
  mcpfleet apply --tag dev cursor
  mcpfleet apply --tag dev --tag vcs claude-code`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			agentName := args[0]
			log.Info("Applying MCP servers", "agent", agentName, "all", applyAll, "tags", tags)

			// 1. Resolve adapter.
			adapter, err := adapters.Get(agentName)
			if err != nil {
				return err
			}

			// 2. Fetch server definitions from registry.
			client, err := registry.New()
			if err != nil {
				return err
			}
			servers, err := client.ListServers(context.Background())
			if err != nil {
				return err
			}

			// 3. Filter by tags if provided.
			if len(tags) > 0 {
				servers = filterByTags(servers, tags)
			}

			// 4. Resolve secrets from vault.
			v, err := vault.Open()
			if err != nil {
				log.Warn("Could not open vault, secrets will not be resolved", "err", err)
			} else {
				for i := range servers {
					servers[i].Env = v.Resolve(servers[i].Env)
				}
			}

			// 5. Write config via agent adapter.
			if err := adapter.Apply(servers); err != nil {
				return fmt.Errorf("apply: %w", err)
			}

			fmt.Printf("✓ Applied %d MCP server(s) to %s\n", len(servers), agentName)
			return nil
		},
	}

	cmd.Flags().BoolVar(&applyAll, "all", false, "Apply all servers from registry")
	cmd.Flags().StringSliceVar(&tags, "tag", nil, "Filter servers by tag (can be used multiple times)")
	return cmd
}

// filterByTags returns servers that have at least one of the requested tags.
func filterByTags(servers []schema.Server, tags []string) []schema.Server {
	tagSet := make(map[string]struct{}, len(tags))
	for _, t := range tags {
		tagSet[t] = struct{}{}
	}
	var out []schema.Server
	for _, s := range servers {
		for _, t := range s.Tags {
			if _, ok := tagSet[t]; ok {
				out = append(out, s)
				break
			}
		}
	}
	return out
}
