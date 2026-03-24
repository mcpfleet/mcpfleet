package cmd

import (
	"fmt"
	"os"

	"github.com/mcpfleet/mcpfleet/internal/registry"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

// pullCmd fetches all server definitions from the registry and writes them to a YAML file.
var pullCmd = &cobra.Command{
	Use:   "pull [file]",
	Short: "Pull server definitions from the mcpfleet registry",
	Args:  cobra.ExactArgs(1),
	RunE:  runPull,
}

func init() {
	rootCmd.AddCommand(pullCmd)
}

func runPull(cmd *cobra.Command, args []string) error {
	path := args[0]

	client, err := registry.New()
	if err != nil {
		return fmt.Errorf("registry client: %w", err)
	}

	servers, err := client.ListServers(cmd.Context())
	if err != nil {
		return fmt.Errorf("list servers: %w", err)
	}

	out, err := yaml.Marshal(servers)
	if err != nil {
		return fmt.Errorf("marshal yaml: %w", err)
	}

	if err := os.WriteFile(path, out, 0o644); err != nil {
		return fmt.Errorf("write file: %w", err)
	}

	fmt.Printf("pulled %d server(s) -> %s\n", len(servers), path)
	return nil
}
