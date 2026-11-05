package cmd

import (
	"context"
	"fmt"
	"sort"
	"strings"

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

Supported agents: ` + strings.Join(adapters.Names(), ", ") + `

claude-code writes to Claude Code's user config (~/.claude.json, or
$CLAUDE_CONFIG_DIR/.claude.json); claude-desktop writes to the Claude Desktop
app's claude_desktop_config.json.

Env entries that reference a secret ({secret: name}) are filled from the local
vault. Store them first with "mcpfleet secret set <name>"; a missing secret
stops apply before any config file is written.

Examples:
  mcpfleet apply cursor
  mcpfleet apply --all cursor
  mcpfleet apply --tag dev cursor
  mcpfleet apply --tag dev --tag vcs claude-code
  mcpfleet apply claude-desktop`,
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

			// 4. Resolve secrets from vault. Any failure stops here, before
			// the agent config is touched.
			servers, err = resolveSecrets(servers, openVault)
			if err != nil {
				return err
			}

			// 5. Write config via agent adapter.
			if err := adapter.Apply(servers); err != nil {
				return fmt.Errorf("apply: %w", err)
			}
			fmt.Printf("\u2713 Applied %d MCP server(s) to %s\n", len(servers), agentName)
			return nil
		},
	}
	cmd.Flags().BoolVar(&applyAll, "all", false, "Apply all servers from registry")
	cmd.Flags().StringSliceVar(&tags, "tag", nil, "Filter servers by tag (can be used multiple times)")
	return cmd
}

// secretStore is the read side of the vault that apply needs.
type secretStore interface {
	Get(name string) (string, bool)
}

func openVault() (secretStore, error) { return vault.Open() }

// resolveSecrets returns a copy of servers with every env secret reference
// replaced by its value from the vault. The vault is opened only when some
// server references a secret. A vault that cannot be opened, or any secret
// missing from it, is an error naming the affected secrets; nothing is
// resolved partially.
func resolveSecrets(servers []schema.Server, open func() (secretStore, error)) ([]schema.Server, error) {
	var refs []string // "name (server s, env X)", in server order, env keys sorted
	for _, s := range servers {
		for _, k := range sortedKeys(s.Env) {
			if ref := s.Env[k].Secret; ref != "" {
				refs = append(refs, fmt.Sprintf("%s (server %s, env %s)", ref, s.Name, k))
			}
		}
	}
	if len(refs) == 0 {
		return servers, nil
	}

	store, err := open()
	if err != nil {
		return nil, fmt.Errorf("cannot read secrets %s from the vault: %w", strings.Join(refs, ", "), err)
	}

	out := make([]schema.Server, len(servers))
	var missing []string
	for i, s := range servers {
		out[i] = s
		if len(s.Env) == 0 {
			continue
		}
		env := make(map[string]schema.EnvVar, len(s.Env))
		for _, k := range sortedKeys(s.Env) {
			ev := s.Env[k]
			if ev.Secret == "" {
				env[k] = schema.EnvVar{Value: ev.Value}
				continue
			}
			val, ok := store.Get(ev.Secret)
			if !ok {
				missing = append(missing, fmt.Sprintf("%s (server %s, env %s)", ev.Secret, s.Name, k))
				continue
			}
			env[k] = schema.EnvVar{Value: val}
		}
		out[i].Env = env
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("missing secrets in vault: %s; set them with: mcpfleet secret set <name>", strings.Join(missing, ", "))
	}
	return out, nil
}

func sortedKeys(env map[string]schema.EnvVar) []string {
	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
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
