package cmd

import (
	"fmt"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"
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

			// TODO: validate token against registry
			// TODO: store token in keyring
			fmt.Println("Logged in successfully!")
			return nil
		},
	}
}

func newAuthLogoutCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Log out from registry",
		RunE: func(cmd *cobra.Command, args []string) error {
			// TODO: remove token from keyring
			fmt.Println("Logged out.")
			return nil
		},
	}
}

func newAuthStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show current auth status",
		RunE: func(cmd *cobra.Command, args []string) error {
			// TODO: check keyring for token, validate against registry
			fmt.Println("Not logged in.")
			return nil
		},
	}
}
