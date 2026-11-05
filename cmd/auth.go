package cmd

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"

	"github.com/mcpfleet/mcpfleet/internal/registry"
)

func newAuthCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "auth",
		Short: "Authenticate with your mcpfleet registry",
	}
	cmd.AddCommand(newAuthLoginCmd(), newAuthLogoutCmd(), newAuthStatusCmd())
	return cmd
}

func newAuthLoginCmd() *cobra.Command {
	var registryURL string
	var token string

	c := &cobra.Command{
		Use:   "login",
		Short: "Log in to your mcpfleet registry",
		Long: `Log in to your mcpfleet registry.

Without --token an interactive form asks for the registry URL and the token.
Pass --url and --token for non-interactive use (scripts, CI). The registry URL
is saved to ~/.config/mcpfleet/registry_url and used by later commands unless
MCPFLEET_REGISTRY_URL is set.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if token == "" {
				// Interactive login via Huh form
				form := huh.NewForm(
					huh.NewGroup(
						huh.NewInput().
							Title("Registry URL").
							Placeholder("https://registry.mcpfleet.dev").
							Value(&registryURL),
						huh.NewInput().
							Title("Auth Token").
							EchoMode(huh.EchoModePassword).
							Value(&token),
					),
				)
				if err := form.Run(); err != nil {
					return err
				}
			}

			token = strings.TrimSpace(token)
			if token == "" {
				return errors.New("auth token cannot be empty")
			}

			// Validate the URL before writing anything, so a bad URL leaves
			// no half-saved login behind.
			u := strings.TrimRight(strings.TrimSpace(registryURL), "/")
			if u != "" {
				if err := validateRegistryURL(u); err != nil {
					return err
				}
			}

			// Persist token to disk.
			if err := registry.SaveToken(token); err != nil {
				return fmt.Errorf("save token: %w", err)
			}

			// An empty URL keeps the previously saved one (or the default).
			if u != "" {
				if err := registry.SaveRegistryURL(u); err != nil {
					return fmt.Errorf("save registry url: %w", err)
				}
			}

			fmt.Println("✓ Logged in successfully!")
			return nil
		},
	}
	c.Flags().StringVar(&registryURL, "url", "", "registry URL, e.g. https://registry.mcpfleet.dev (saved for later commands)")
	c.Flags().StringVar(&token, "token", "", "auth token for non-interactive login (scripts, CI); prefer the form when typing by hand")
	return c
}

// validateRegistryURL accepts only absolute http(s) URLs with a host.
func validateRegistryURL(raw string) error {
	parsed, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("invalid registry URL %q: %w", raw, err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("invalid registry URL %q: scheme must be http or https", raw)
	}
	if parsed.Host == "" {
		return fmt.Errorf("invalid registry URL %q: missing host", raw)
	}
	return nil
}

func newAuthLogoutCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Log out from registry",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := registry.DeleteToken(); err != nil {
				if os.IsNotExist(err) {
					fmt.Println("Not logged in.")
					return nil
				}
				return fmt.Errorf("logout: %w", err)
			}
			fmt.Println("✓ Logged out.")
			return nil
		},
	}
}

func newAuthStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show current auth status and the registry URL in use",
		Long: `Show whether a token is saved and which registry later commands talk to.

The registry URL comes from MCPFLEET_REGISTRY_URL, else from the URL saved by
"mcpfleet auth login" (~/.config/mcpfleet/registry_url), else the default.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			if token, err := registry.LoadToken(); err != nil {
				fmt.Fprintln(out, "Not logged in.")
			} else {
				fmt.Fprintf(out, "✓ Logged in (token: %s)\n", maskToken(token))
			}
			u, src := registry.BaseURL()
			fmt.Fprintf(out, "Registry: %s (from %s)\n", u, src)
			return nil
		},
	}
}

// maskToken keeps the first and last four characters of a token.
func maskToken(token string) string {
	if len(token) <= 8 {
		return token
	}
	return token[:4] + strings.Repeat("*", len(token)-8) + token[len(token)-4:]
}
