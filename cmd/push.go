package cmd

import (
	"fmt"
	"os"

	"github.com/mcpfleet/mcpfleet/internal/registry"
	"github.com/mcpfleet/mcpfleet/internal/schema"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

// pushCmd reads a servers YAML file and upserts each server in the registry.
var pushCmd = &cobra.Command{
	Use:   "push [file]",
	Short: "Push server definitions to the mcpfleet registry",
	Args:  cobra.ExactArgs(1),
	RunE:  runPush,
}

func init() {
	rootCmd.AddCommand(pushCmd)
}

func runPush(cmd *cobra.Command, args []string) error {
	path := args[0]

	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read file: %w", err)
	}

	var servers []schema.Server
	if err := yaml.Unmarshal(data, &servers); err != nil {
		return fmt.Errorf("parse yaml: %w", err)
	}

	client, err := registry.New()
	if err != nil {
		return fmt.Errorf("registry client: %w", err)
	}

	for _, srv := range servers {
		existing, err := client.GetServer(cmd.Context(), srv.Name)
		if err != nil && err.Error() != "server not found" {
			return fmt.Errorf("get server %q: %w", srv.Name, err)
		}

		if existing != nil {
			if _, err := client.UpdateServer(cmd.Context(), srv); err != nil {
				return fmt.Errorf("update server %q: %w", srv.Name, err)
			}
			fmt.Printf("updated  %s\n", srv.Name)
		} else {
			if _, err := client.CreateServer(cmd.Context(), srv); err != nil {
				return fmt.Errorf("create server %q: %w", srv.Name, err)
			}
			fmt.Printf("created  %s\n", srv.Name)
		}
	}

	fmt.Printf("pushed %d server(s)\n", len(servers))
	return nil
}
