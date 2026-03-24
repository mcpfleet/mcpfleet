package cmd

import (
	"errors"
	"fmt"
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

	return &cobra.Command{
		Use:   "login",
		Short: "Log in to your mcpfleet registry",
		RunE: func(cmd *cobra.Command, args []string) error {
			if registryURL == "" || token == "" {
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

			// Persist token to disk.
			if err := registry.SaveToken(token); err != nil {
				return fmt.Errorf("save token: %w", err)
			}

			fmt.Println("✓ Logged in successfully!")
			return nil
		},
	}
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
		Short: "Show current auth status",
		RunE: func(cmd *cobra.Command, args []string) error {
			token, err := registry.LoadToken()
			if err != nil {
				fmt.Println("Not logged in.")
				return nil
			}
			// Show a masked version of the token.
			masked := token
			if len(token) > 8 {
				masked = token[:4] + strings.Repeat("*", len(token)-8) + token[len(token)-4:]
			}
			fmt.Printf("✓ Logged in (token: %s)\n", masked)
			return nil
		},
	}
}
