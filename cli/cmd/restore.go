package cmd

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/dbvault/dbvault/cli/internal/client"
	"github.com/dbvault/dbvault/cli/internal/ui"
)

var restoreFlags struct {
	target      string
	newDatabase string
	existing    bool
	confirm     string
}

var restoreCmd = &cobra.Command{
	Use:   "restore <backup-id>",
	Short: "Restore a backup into a new or existing database",
	Long: `Restore a backup. Restores run in a single transaction: if anything fails,
the target is left unchanged.

Into a new database on the target server (safe):
  dbvault restore 1f2e3d4c --target production --new-database shop_restored

Over the existing database (destructive — objects in the backup are dropped
and recreated; you must type RESTORE or pass --confirm RESTORE):
  dbvault restore 1f2e3d4c --target staging --existing`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		c, _, err := session()
		if err != nil {
			return err
		}
		backupID, err := resolveBackup(cmd, c, args[0])
		if err != nil {
			return err
		}
		var detail struct {
			Backup client.Backup `json:"backup"`
		}
		if err := c.Get(ctx, "/backups/"+backupID, &detail); err != nil {
			return err
		}
		targetName := restoreFlags.target
		if targetName == "" {
			targetName = detail.Backup.DatabaseName
		}
		target, err := findDatabase(ctx, c, targetName)
		if err != nil {
			return err
		}
		if (restoreFlags.newDatabase == "") == !restoreFlags.existing {
			return errors.New("choose exactly one of --new-database <name> or --existing")
		}
		req := map[string]any{"backup_id": backupID, "target_database_id": target.ID}
		if restoreFlags.existing {
			req["mode"] = "existing"
			confirm := restoreFlags.confirm
			if confirm == "" {
				fmt.Println(ui.Red("Restoring this backup may overwrite existing data."))
				fmt.Printf("Objects in the backup will be dropped and recreated in %s (%s/%s).\n", ui.Bold(target.Name), target.Host, target.Database)
				fmt.Print("Type RESTORE to continue: ")
				line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
				confirm = strings.TrimSpace(line)
			}
			if confirm != "RESTORE" {
				return errors.New("aborted: confirmation did not match RESTORE")
			}
			req["confirmation"] = "RESTORE"
		} else {
			req["mode"] = "new"
			req["new_database_name"] = restoreFlags.newDatabase
		}
		var r client.Restore
		if err := c.Post(ctx, "/restores", req, &r); err != nil {
			return err
		}
		if !flagJSON {
			fmt.Println(ui.Bold("Restoring..."))
			fmt.Println()
		}
		if r.JobID == nil {
			return errors.New("restore has no job id")
		}
		job, err := followJob(ctx, c, *r.JobID, !flagJSON, false)
		if err != nil {
			return err
		}
		var view struct {
			Restore client.Restore `json:"restore"`
		}
		if err := c.Get(ctx, "/restores/"+r.ID, &view); err != nil {
			return err
		}
		if flagJSON {
			return printJSON(view.Restore)
		}
		if view.Restore.Status != "completed" {
			fmt.Println()
			fmt.Println(ui.Red("Restore failed:"), deref(view.Restore.Error))
			if job.Status == "failed" {
				return errors.New("restore failed")
			}
			return fmt.Errorf("restore %s", view.Restore.Status)
		}
		dest := target.Database
		if view.Restore.NewDatabaseName != nil {
			dest = *view.Restore.NewDatabaseName
		}
		fmt.Printf("\n%s Restored into %s on %s\n", ui.Green("✓"), ui.Bold(dest), target.Host)
		return nil
	},
}

func init() {
	f := restoreCmd.Flags()
	f.StringVar(&restoreFlags.target, "target", "", "target database name (default: the backup's database)")
	f.StringVar(&restoreFlags.newDatabase, "new-database", "", "create this database on the target server and restore into it")
	f.BoolVar(&restoreFlags.existing, "existing", false, "restore over the target database (destructive)")
	f.StringVar(&restoreFlags.confirm, "confirm", "", `pass "RESTORE" to confirm a destructive restore non-interactively`)
	rootCmd.AddCommand(restoreCmd)
}
