package cmd

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/dbvault/dbvault/cli/internal/client"
	"github.com/dbvault/dbvault/cli/internal/config"
	"github.com/dbvault/dbvault/cli/internal/ui"
)

var (
	initToken string
	initEmail string
	initCode  string
)

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Connect this CLI to a DBVault server",
	Long: `Connect this CLI to a DBVault server.

Authenticate with your email and password (an API token is created for this
machine), or pass an existing token from Settings → Security → API tokens:

  dbvault init --server https://dbvault.example.com --token dbv_...`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		cfg, err := config.Load()
		if err != nil {
			return err
		}
		server := flagServer
		if server == "" {
			def := cfg.Server
			if def == "" {
				def = "http://localhost:3000"
			}
			server = ui.Prompt("DBVault server URL", def)
		}
		server = strings.TrimRight(server, "/")

		token := initToken
		if token == "" {
			email := initEmail
			if email == "" {
				email = ui.Prompt("Email", cfg.Email)
			}
			password, err := ui.PromptSecret("Password")
			if err != nil {
				return err
			}
			var res struct {
				Token string      `json:"token"`
				User  client.User `json:"user"`
			}
			body := map[string]string{"email": email, "password": password, "name": "CLI on " + hostname(), "code": initCode}
			anon := client.New(server, "", "")
			err = anon.Post(ctx, "/auth/token", body, &res)
			// Accounts with two-factor authentication need a code as well.
			if apiErr := (*client.Error)(nil); errors.As(err, &apiErr) && apiErr.Code == "mfa_required" && initCode == "" {
				body["code"] = ui.Prompt("Authentication code (or a recovery code)", "")
				err = anon.Post(ctx, "/auth/token", body, &res)
			}
			if err != nil {
				return err
			}
			token = res.Token
			cfg.Email = email
		}

		c := client.New(server, token, "")
		var me client.Me
		if err := c.Get(ctx, "/me", &me); err != nil {
			return fmt.Errorf("token check failed: %w", err)
		}
		cfg.Server, cfg.Token = server, token
		cfg.Email = me.User.Email

		switch {
		case flagOrg != "":
			cfg.Organization = flagOrg
		case len(me.Organizations) == 1:
			cfg.Organization = me.Organizations[0].ID
		case len(me.Organizations) > 1:
			fmt.Println("\nOrganizations:")
			for i, o := range me.Organizations {
				fmt.Printf("  %d) %s %s\n", i+1, o.Name, ui.Dim("("+o.Role+")"))
			}
			choice := ui.Prompt("Default organization", "1")
			idx := 0
			fmt.Sscanf(choice, "%d", &idx)
			if idx < 1 || idx > len(me.Organizations) {
				idx = 1
			}
			cfg.Organization = me.Organizations[idx-1].ID
		}
		path, err := cfg.Save()
		if err != nil {
			return err
		}
		fmt.Println()
		fmt.Println(ui.Green("✓"), "Connected to", ui.Bold(server), "as", me.User.Email)
		fmt.Println(ui.Dim("  Config saved to " + path + " (mode 0600)"))
		fmt.Println("\nNext:", ui.Bold("dbvault status"), "·", ui.Bold("dbvault database list"), "·", ui.Bold("dbvault backup <database>"))
		return nil
	},
}

func init() {
	initCmd.Flags().StringVar(&initToken, "token", "", "existing API token (skips the password prompt)")
	initCmd.Flags().StringVar(&initEmail, "email", "", "account email")
	initCmd.Flags().StringVar(&initCode, "code", "", "two-factor authentication code, if enabled (prompted for when needed)")
	rootCmd.AddCommand(initCmd)
}
