package cmd

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/mcpfleet/mcpfleet/internal/registry"
	"github.com/mcpfleet/mcpfleet/pkg/skillspec"
)

func newSkillPullCmd() *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:   "pull <name> [dir]",
		Short: "Download a skill from the registry into <dir>/<name>",
		Long: `Download a skill from the registry and unpack it into <dir>/<name>/
(<dir> defaults to the current directory), keeping the executable bit of
every file.

The skill's sha256 is checked against the hash the registry sends before
anything is written to <dir>/<name>; an archive that does not match, or that
tries to write outside the skill directory, is rejected and leaves nothing
behind. An existing <dir>/<name> is replaced only with --force.`,
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			dir := "."
			if len(args) == 2 {
				dir = args[1]
			}
			if name == "" || name == "." || name == ".." || strings.ContainsAny(name, "/\\\x00") {
				return fmt.Errorf("skill name %q cannot be a directory name", name)
			}
			target := filepath.Join(dir, name)
			if _, err := os.Lstat(target); err == nil && !force {
				return fmt.Errorf("%s already exists (use --force to replace it)", target)
			}

			client, err := registry.New()
			if err != nil {
				return fmt.Errorf("registry client: %w", err)
			}
			archive, sha, err := client.GetSkillArchive(cmd.Context(), name)
			if err != nil {
				if errors.Is(err, registry.ErrNotFound) {
					return fmt.Errorf("skill %q not found in registry", name)
				}
				return fmt.Errorf("pull skill %q: %w", name, err)
			}
			if sha == "" {
				return fmt.Errorf("pull skill %q: the registry sent no sha256 (X-Skill-Sha256 header), so the archive cannot be verified", name)
			}
			if err := unpackVerified(archive, name, sha, dir, target, force); err != nil {
				return fmt.Errorf("pull skill %q: %w", name, err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "pulled %s %s -> %s\n", name, shortHash(sha), target)
			return nil
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "Replace an existing <dir>/<name>")
	return cmd
}

// unpackVerified unpacks archive into a temporary directory inside dir,
// checks its skillspec hash against sha and only then moves it to target,
// replacing an existing target when force is set.
func unpackVerified(archive []byte, name, sha, dir, target string, force bool) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.MkdirTemp(dir, ".mcpfleet-pull-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)

	root, err := skillspec.Unpack(bytes.NewReader(archive), name, tmp)
	if err != nil {
		return err
	}
	got, err := skillspec.Hash(root)
	if err != nil {
		return err
	}
	if got != sha {
		return fmt.Errorf("sha256 mismatch: the registry says %s, the archive holds %s", sha, got)
	}
	if force {
		if err := os.RemoveAll(target); err != nil {
			return err
		}
	}
	return os.Rename(root, target)
}
