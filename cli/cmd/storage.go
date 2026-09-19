package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/dbvault/dbvault/cli/internal/client"
	"github.com/dbvault/dbvault/cli/internal/ui"
)

var storageCmd = &cobra.Command{Use: "storage", Short: "Manage storage destinations"}

var storageListCmd = &cobra.Command{
	Use:     "list",
	Aliases: []string{"ls"},
	Short:   "List storage destinations",
	Args:    cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, _, err := session()
		if err != nil {
			return err
		}
		var list []client.Storage
		if err := c.Get(cmd.Context(), "/storage", &list); err != nil {
			return err
		}
		if flagJSON {
			return printJSON(list)
		}
		if len(list) == 0 {
			fmt.Println("No storage destinations yet. Add one in the dashboard under Storage.")
			return nil
		}
		t := ui.NewTable(cmd.OutOrStdout(), "Name", "Type", "Location", "Default", "Backups", "Used", "Last test")
		for _, s := range list {
			def := ""
			if s.IsDefault {
				def = ui.Green("default")
			}
			test := ui.Dim("never")
			if s.LastTestOK != nil {
				if *s.LastTestOK {
					test = ui.Green("ok") + " " + ui.Dim(ui.Ago(s.LastTested))
				} else {
					test = ui.Red("failed") + " " + ui.Dim(ui.Ago(s.LastTested))
				}
			}
			t.Row(ui.Bold(s.Name), s.Type, s.Location, def, fmt.Sprint(s.BackupCount), ui.Bytes(s.UsedBytes), test)
		}
		t.Flush()
		return nil
	},
}

var scheduleCmd = &cobra.Command{Use: "schedule", Aliases: []string{"schedules"}, Short: "Manage backup schedules"}

var scheduleListCmd = &cobra.Command{
	Use:     "list",
	Aliases: []string{"ls"},
	Short:   "List backup schedules",
	Args:    cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, _, err := session()
		if err != nil {
			return err
		}
		var list []client.Schedule
		if err := c.Get(cmd.Context(), "/schedules", &list); err != nil {
			return err
		}
		if flagJSON {
			return printJSON(list)
		}
		if len(list) == 0 {
			fmt.Println("No schedules yet. Create one in the dashboard under Schedules.")
			return nil
		}
		t := ui.NewTable(cmd.OutOrStdout(), "Database", "Schedule", "Cron", "Next run", "Last", "Retention", "Storage", "Enabled")
		for _, s := range list {
			ret := fmt.Sprintf("%dd/%dw/%dm", s.Retention.Daily, s.Retention.Weekly, s.Retention.Monthly)
			if s.Retention.Daily+s.Retention.Weekly+s.Retention.Monthly == 0 {
				ret = "keep all"
			}
			enabled := ui.Green("yes")
			if !s.Enabled {
				enabled = ui.Dim("paused")
			}
			last := ui.Dim("—")
			if s.LastBackupStatus != nil {
				last = ui.Status(*s.LastBackupStatus)
			}
			t.Row(ui.Bold(s.DatabaseName), s.Description, ui.Dim(s.CronExpression+" "+s.Timezone), ui.Ago(s.NextRunAt), last, ret, s.StorageName, enabled)
		}
		t.Flush()
		return nil
	},
}

func init() {
	storageCmd.AddCommand(storageListCmd)
	scheduleCmd.AddCommand(scheduleListCmd)
	rootCmd.AddCommand(storageCmd, scheduleCmd)
}
