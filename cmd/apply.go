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
	var tags []string

	cmd := &cobra.Command{
		Use:   "apply <agent>",
		Short: "Apply MCP server definitions to an agent",
		Long: `Apply MCP server definitions from your registry to a specific AI coding agent.

Supported agents: ` + strings.Join(adapters.Names(), ", ") + `

claude-code writes to Claude Code's user config (~/.claude.json, or
$CLAUDE_CONFIG_DIR/.claude.json); claude-desktop writes to the Claude Desktop
app's claude_desktop_config.json.

Remote servers (transport http or sse, with a url) are written in the format
each agent reads. An agent whose config file cannot hold a server's transport
(claude-desktop: http and sse; zed: sse), or a remote server without a url,
is skipped with a "warning: skipping <name>: <reason>" line on stderr.

Env entries of stdio servers and headers of remote servers that reference a
secret ({secret: name}) are filled from the local vault. Store them first with
"mcpfleet secret set <name>"; a missing secret stops apply before any config
file is written. A secret holds the whole header value, e.g. "Bearer <token>".
The env of a remote server is not written to any agent, and its secrets are
not read.

Examples:
  mcpfleet apply cursor
  mcpfleet apply --tag dev cursor
  mcpfleet apply --tag dev --tag vcs claude-code
  mcpfleet apply claude-desktop`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			agentName := args[0]
			log.Debug("Applying MCP servers", "agent", agentName, "tags", tags)

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
			registryURL, registrySource := registry.BaseURL()
			log.Debug("Using registry", "url", registryURL, "source", registrySource)
			servers, err := client.ListServers(context.Background())
			if err != nil {
				return err
			}
			log.Debug("Fetched servers from registry", "count", len(servers))

			// 3. Filter by tags if provided.
			if len(tags) > 0 {
				servers = filterByTags(servers, tags)
				log.Debug("Filtered servers by tag", "count", len(servers))
			}

			// 4. Leave out the servers this agent cannot hold, with a
			// warning, before secrets are resolved: a skipped server's
			// missing secret does not stop apply.
			servers, skipped := adapters.Partition(adapter, servers)
			for _, s := range skipped {
				fmt.Fprintf(cmd.ErrOrStderr(), "warning: skipping %s: %s\n", s.Server.Name, s.Reason)
			}

			// 5. Resolve secrets from vault. Any failure stops here, before
			// the agent config is touched.
			servers, err = resolveSecrets(servers, openVault)
			if err != nil {
				return err
			}

			// 6. Write config via agent adapter, unless every server was
			// skipped.
			if len(servers) > 0 || len(skipped) == 0 {
				log.Debug("Writing agent config", "path", adapter.ConfigPath())
				if err := adapter.Apply(servers); err != nil {
					return fmt.Errorf("apply: %w", err)
				}
			}
			fmt.Fprintf(cmd.OutOrStdout(), "\u2713 Applied %d MCP server(s) to %s\n", len(servers), agentName)
			return nil
		},
	}
	cmd.Flags().StringSliceVar(&tags, "tag", nil, "Filter servers by tag (can be used multiple times)")
	return cmd
}

// secretStore is the read side of the vault that apply needs.
type secretStore interface {
	Get(name string) (string, bool)
}

func openVault() (secretStore, error) { return vault.Open() }

// secretField is a map of a server whose secret references apply fills in:
// env for a stdio server, headers for a remote one.
type secretField struct {
	kind string // "env" or "header", as error messages name it
	get  func(*schema.Server) *map[string]schema.EnvVar
}

var (
	envField    = secretField{"env", func(s *schema.Server) *map[string]schema.EnvVar { return &s.Env }}
	headerField = secretField{"header", func(s *schema.Server) *map[string]schema.EnvVar { return &s.Headers }}
)

// usedFields returns the field whose secrets s needs resolved, and the one
// no adapter writes for it: a stdio server uses env, a remote server
// headers.
func usedFields(s schema.Server) (used, unused secretField) {
	if s.IsRemote() {
		return headerField, envField
	}
	return envField, headerField
}

// resolveSecrets returns a copy of servers with every secret reference of
// the field each server uses (env for stdio, headers for http and sse)
// replaced by its value from the vault. The other field is dropped from the
// copy, unread, since no adapter writes it: its secrets neither open the
// vault nor stop apply. The vault is opened only when some server references
// a secret. A vault that cannot be opened, or any secret missing from it, is
// an error naming the affected secrets; nothing is resolved partially.
func resolveSecrets(servers []schema.Server, open func() (secretStore, error)) ([]schema.Server, error) {
	var refs []string // "name (server s, env X)", in server order, keys sorted
	for _, s := range servers {
		used, _ := usedFields(s)
		m := *used.get(&s)
		for _, k := range sortedKeys(m) {
			if ref := m[k].Secret; ref != "" {
				refs = append(refs, describeRef(s.Name, used.kind, k, ref))
			}
		}
	}

	var store secretStore
	if len(refs) > 0 {
		var err error
		store, err = open()
		if err != nil {
			return nil, fmt.Errorf("cannot read secrets %s from the vault: %w", strings.Join(refs, ", "), err)
		}
	}

	out := make([]schema.Server, len(servers))
	var missing []string
	for i, s := range servers {
		out[i] = s
		used, unused := usedFields(s)
		*unused.get(&out[i]) = nil
		in := *used.get(&s)
		if len(in) == 0 {
			continue
		}
		resolved := make(map[string]schema.EnvVar, len(in))
		for _, k := range sortedKeys(in) {
			ev := in[k]
			if ev.Secret == "" {
				resolved[k] = schema.EnvVar{Value: ev.Value}
				continue
			}
			val, ok := store.Get(ev.Secret)
			if !ok {
				missing = append(missing, describeRef(s.Name, used.kind, k, ev.Secret))
				continue
			}
			resolved[k] = schema.EnvVar{Value: val}
		}
		*used.get(&out[i]) = resolved
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("missing secrets in vault: %s; set them with: mcpfleet secret set <name>", strings.Join(missing, ", "))
	}
	return out, nil
}

// describeRef names a secret reference for error messages, e.g.
// "example_auth (server remote-api, header Authorization)".
func describeRef(server, kind, key, secret string) string {
	return fmt.Sprintf("%s (server %s, %s %s)", secret, server, kind, key)
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
