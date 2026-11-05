package cmd

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/x/term"
	"github.com/spf13/cobra"

	"github.com/mcpfleet/mcpfleet/internal/vault"
)

func newSecretCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "secret",
		Short: "Manage secrets in the local vault",
		Long: `Manage secrets in the local encrypted vault (~/.config/mcpfleet/vault.enc).

Registry entries refer to secrets by name (env: GITHUB_TOKEN: {secret: github_token}).
"mcpfleet apply" fills those references from the vault; a missing secret stops it.

Examples:
  mcpfleet secret set github_token
  echo "$GITHUB_TOKEN" | mcpfleet secret set github_token --stdin
  mcpfleet secret list
  mcpfleet secret rm github_token`,
	}
	cmd.AddCommand(newSecretSetCmd(), newSecretListCmd(), newSecretRmCmd())
	return cmd
}

func newSecretSetCmd() *cobra.Command {
	var fromStdin bool

	c := &cobra.Command{
		Use:   "set <name>",
		Short: "Store or update a secret",
		Long: `Store or update a secret in the local vault.

The value is never taken from an argument (it would end up in the shell
history). Without --stdin a hidden prompt asks for it; with --stdin the whole
standard input is the value, minus one trailing newline.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			if err := validateSecretName(name); err != nil {
				return err
			}

			var value string
			if fromStdin {
				data, err := io.ReadAll(cmd.InOrStdin())
				if err != nil {
					return fmt.Errorf("read stdin: %w", err)
				}
				value = trimOneNewline(string(data))
			} else {
				if !isTerminal(cmd.InOrStdin()) {
					return errors.New("stdin is not a terminal; pipe the value with --stdin")
				}
				v, err := promptSecret(name)
				if err != nil {
					return err
				}
				value = v
			}
			if value == "" {
				return errors.New("secret value cannot be empty")
			}

			v, err := vault.Open()
			if err != nil {
				return err
			}
			_, existed := v.Get(name)
			v.Set(name, value)
			if err := v.Save(); err != nil {
				return err
			}
			verb := "Saved"
			if existed {
				verb = "Updated"
			}
			fmt.Fprintf(cmd.OutOrStdout(), "✓ %s secret %s\n", verb, name)
			return nil
		},
	}
	c.Flags().BoolVar(&fromStdin, "stdin", false, "read the value from standard input instead of a hidden prompt")
	return c
}

func newSecretListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List secret names (never values)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			v, err := vault.Open()
			if err != nil {
				return err
			}
			names := v.Names()
			if len(names) == 0 {
				fmt.Fprintln(cmd.ErrOrStderr(), "No secrets stored. Add one with: mcpfleet secret set <name>")
				return nil
			}
			for _, name := range names {
				fmt.Fprintln(cmd.OutOrStdout(), name)
			}
			return nil
		},
	}
}

func newSecretRmCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "rm <name>",
		Short: "Remove a secret",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			v, err := vault.Open()
			if err != nil {
				return err
			}
			if _, ok := v.Get(name); !ok {
				return fmt.Errorf("secret %q not found", name)
			}
			v.Delete(name)
			if err := v.Save(); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "✓ Removed secret %s\n", name)
			return nil
		},
	}
}

// validateSecretName accepts any non-empty name without surrounding
// whitespace. The registry does not restrict secret names, so a stricter rule
// here could make a referenced secret impossible to set.
func validateSecretName(name string) error {
	if name == "" {
		return errors.New("secret name cannot be empty")
	}
	if strings.TrimSpace(name) != name {
		return fmt.Errorf("secret name %q has leading or trailing whitespace", name)
	}
	return nil
}

// trimOneNewline drops a single trailing "\n" or "\r\n", so that
// `echo value | mcpfleet secret set name --stdin` stores "value".
func trimOneNewline(s string) string {
	if strings.HasSuffix(s, "\r\n") {
		return s[:len(s)-2]
	}
	return strings.TrimSuffix(s, "\n")
}

// isTerminal reports whether r is an interactive terminal.
func isTerminal(r io.Reader) bool {
	f, ok := r.(*os.File)
	return ok && term.IsTerminal(f.Fd())
}

// promptSecret asks for a secret value without echoing it.
var promptSecret = func(name string) (string, error) {
	var value string
	err := huh.NewInput().
		Title("Value for secret " + name).
		EchoMode(huh.EchoModePassword).
		Value(&value).
		Run()
	return value, err
}
