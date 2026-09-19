package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/dbvault/dbvault/cli/internal/client"
	"github.com/dbvault/dbvault/cli/internal/ui"
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show server, worker and backup health",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		c, cfg, err := session()
		if err != nil {
			return err
		}
		var me client.Me
		var sys client.SystemStatus
		var dash struct {
			Stats client.Stats `json:"stats"`
		}
		if err := c.Get(ctx, "/me", &me); err != nil {
			return err
		}
		if err := c.Get(ctx, "/system/status", &sys); err != nil {
			return err
		}
		if err := c.Get(ctx, "/dashboard", &dash); err != nil {
			return err
		}
		if flagJSON {
			return printJSON(map[string]any{"server": cfg.Server, "user": me.User, "system": sys, "stats": dash.Stats})
		}
		orgName := ""
		for _, o := range me.Organizations {
			if o.ID == cfg.Organization || o.Slug == cfg.Organization {
				orgName = o.Name + " (" + o.Role + ")"
			}
		}
		if orgName == "" && len(me.Organizations) > 0 {
			orgName = me.Organizations[0].Name
		}
		s := dash.Stats
		workers := ui.Green(fmt.Sprintf("%d online", sys.WorkersOnline))
		if sys.WorkersOnline == 0 {
			workers = ui.Red("none online — jobs will stay queued")
		}
		verify := ui.Green("available (" + sys.Verification.Mode + ")")
		if !sys.Verification.Available {
			verify = ui.Yellow("unavailable")
		}
		fmt.Printf("%s  %s\n", ui.Bold("Server"), cfg.Server+" "+ui.Dim("("+sys.Version+")"))
		fmt.Printf("%s    %s\n", ui.Bold("User"), me.User.Email)
		fmt.Printf("%s     %s\n", ui.Bold("Org"), orgName)
		fmt.Printf("%s %s\n", ui.Bold("Workers"), workers)
		fmt.Printf("%s  %s\n\n", ui.Bold("Verify"), verify)

		protected := fmt.Sprintf("%d/%d", s.ProtectedDatabases, s.Databases)
		if s.ProtectedDatabases < s.Databases {
			protected = ui.Yellow(protected)
		}
		failed := fmt.Sprint(s.FailedBackups7d)
		if s.FailedBackups7d > 0 {
			failed = ui.Red(failed)
		}
		t := ui.NewTable(cmd.OutOrStdout(), "Databases", "Protected", "Backups today", "Storage", "Failed (7d)", "Last success")
		t.Row(fmt.Sprint(s.Databases), protected, fmt.Sprint(s.BackupsToday), ui.Bytes(s.StorageUsedBytes), failed, ui.Ago(s.LastSuccessfulBackup))
		t.Flush()
		return nil
	},
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print CLI and server versions",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Println("dbvault CLI", Version)
		c, cfg, err := session()
		if err != nil {
			return nil
		}
		var sys client.SystemStatus
		if err := c.Get(cmd.Context(), "/system/status", &sys); err == nil {
			fmt.Println("server     ", sys.Version, ui.Dim("("+cfg.Server+")"))
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(statusCmd, versionCmd)
}
