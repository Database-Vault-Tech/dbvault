package cmd

import (
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/spf13/cobra"

	"github.com/dbvault/dbvault/cli/internal/client"
	"github.com/dbvault/dbvault/cli/internal/ui"
)

var backupFlags struct {
	storage     string
	compression string
	noEncrypt   bool
	detach      bool
	verbose     bool
}

var backupCmd = &cobra.Command{
	Use:   "backup [database]",
	Short: "Back up a database now (or manage backups)",
	Long: `Back up a database now and follow its progress:

  dbvault backup production

Subcommands:
  dbvault backup list [--database production]
  dbvault backup verify <backup-id>`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 {
			return cmd.Help()
		}
		ctx := cmd.Context()
		c, _, err := session()
		if err != nil {
			return err
		}
		d, err := findDatabase(ctx, c, args[0])
		if err != nil {
			return err
		}
		req := map[string]any{"database_id": d.ID}
		if backupFlags.storage != "" {
			var list []client.Storage
			if err := c.Get(ctx, "/storage", &list); err != nil {
				return err
			}
			found := ""
			for _, s := range list {
				if strings.EqualFold(s.Name, backupFlags.storage) || s.ID == backupFlags.storage {
					found = s.ID
				}
			}
			if found == "" {
				return fmt.Errorf("storage destination %q not found", backupFlags.storage)
			}
			req["storage_destination_id"] = found
		}
		if backupFlags.compression != "" {
			req["compression"] = backupFlags.compression
		}
		if backupFlags.noEncrypt {
			req["encrypted"] = false
		}

		if !flagJSON {
			fmt.Println(ui.Bold("Starting backup..."))
			fmt.Println()
		}
		var q client.Queued
		if err := c.Post(ctx, "/backups", req, &q); err != nil {
			return err
		}
		var detail struct {
			Backup client.Backup `json:"backup"`
		}
		if err := c.Get(ctx, "/backups/"+q.BackupID, &detail); err != nil {
			return err
		}
		if !flagJSON {
			fmt.Println("Database:   ", d.Name)
			fmt.Println("Server:     ", engineVersion(d.Engine, d.PGVersion))
			fmt.Println("Destination:", detail.Backup.StorageName, ui.Dim("("+strings.ToUpper(detail.Backup.StorageType)+")"))
			fmt.Println()
		}
		if backupFlags.detach {
			if flagJSON {
				return printJSON(q)
			}
			fmt.Println(ui.Green("✓"), "Backup queued:", q.BackupID)
			fmt.Println(ui.Dim("  Follow it in the dashboard or with: dbvault backup list"))
			return nil
		}
		if !flagJSON && !backupFlags.verbose {
			fmt.Println("Uploading...")
		}
		job, err := followJob(ctx, c, q.JobID, backupFlags.verbose && !flagJSON, !flagJSON)
		if err != nil {
			return err
		}
		if err := c.Get(ctx, "/backups/"+q.BackupID, &detail); err != nil {
			return err
		}
		b := detail.Backup
		if flagJSON {
			return printJSON(b)
		}
		fmt.Println()
		if job.Status != "completed" {
			fmt.Println(ui.Red("Backup failed."))
			fmt.Println()
			fmt.Println("Database:", d.Name)
			fmt.Println("Reason:  ", jobError(job))
			fmt.Println(ui.Dim("\nLogs: dbvault backup list --database " + d.Name + " · or open the backup in the dashboard"))
			return errors.New("backup failed")
		}
		fmt.Println(ui.Green("Backup completed."))
		fmt.Println()
		size := int64(0)
		if b.SizeBytes != nil {
			size = *b.SizeBytes
		}
		fmt.Println("Size:    ", ui.Bytes(size))
		if b.DurationMS != nil {
			fmt.Println("Duration:", ui.Duration(*b.DurationMS))
		}
		fmt.Println("Checksum:", deref(b.Checksum))
		fmt.Println("Location:", ui.Dim(deref(b.StorageKey)))
		fmt.Println("ID:      ", ui.Dim(b.ID))
		return nil
	},
}

var listDatabase string
var listLimit int

var backupListCmd = &cobra.Command{
	Use:     "list",
	Aliases: []string{"ls"},
	Short:   "List recent backups",
	Args:    cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		c, _, err := session()
		if err != nil {
			return err
		}
		q := url.Values{"limit": {fmt.Sprint(listLimit)}}
		if listDatabase != "" {
			d, err := findDatabase(ctx, c, listDatabase)
			if err != nil {
				return err
			}
			q.Set("database_id", d.ID)
		}
		var list []client.Backup
		if err := c.Get(ctx, "/backups?"+q.Encode(), &list); err != nil {
			return err
		}
		if flagJSON {
			return printJSON(list)
		}
		if len(list) == 0 {
			fmt.Println("No backups yet. Start one with:", ui.Bold("dbvault backup <database>"))
			return nil
		}
		t := ui.NewTable(cmd.OutOrStdout(), "ID", "Database", "Status", "Size", "Duration", "Storage", "Verified", "Created")
		for _, b := range list {
			size, dur := "—", "—"
			if b.SizeBytes != nil {
				size = ui.Bytes(*b.SizeBytes)
			}
			if b.DurationMS != nil {
				dur = ui.Duration(*b.DurationMS)
			}
			verified := ui.Dim("—")
			if b.VerificationStatus != "none" {
				verified = ui.Status(b.VerificationStatus)
			}
			created := b.CreatedAt
			t.Row(b.ID[:8], b.DatabaseName, ui.Status(b.Status), size, dur, b.StorageName, verified, ui.Ago(&created))
		}
		t.Flush()
		return nil
	},
}

// resolveBackup accepts a full id or an unambiguous 8+ character prefix.
func resolveBackup(cmd *cobra.Command, c *client.Client, id string) (string, error) {
	if len(id) == 36 {
		return id, nil
	}
	if len(id) < 8 {
		return "", errors.New("use the full backup id or at least its first 8 characters")
	}
	var list []client.Backup
	if err := c.Get(cmd.Context(), "/backups?limit=200", &list); err != nil {
		return "", err
	}
	match := ""
	for _, b := range list {
		if strings.HasPrefix(b.ID, id) {
			if match != "" {
				return "", fmt.Errorf("backup id prefix %q is ambiguous", id)
			}
			match = b.ID
		}
	}
	if match == "" {
		return "", fmt.Errorf("backup %q not found", id)
	}
	return match, nil
}

var backupVerifyCmd = &cobra.Command{
	Use:   "verify <backup-id>",
	Short: "Prove a backup restores: checksum, decrypt, restore into a sandbox, query",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		c, _, err := session()
		if err != nil {
			return err
		}
		id, err := resolveBackup(cmd, c, args[0])
		if err != nil {
			return err
		}
		var q client.Queued
		if err := c.Post(ctx, "/backups/"+id+"/verify", nil, &q); err != nil {
			return err
		}
		if !flagJSON {
			fmt.Println(ui.Bold("Verifying backup " + id[:8] + "..."))
			fmt.Println()
		}
		if _, err := followJob(ctx, c, q.JobID, !flagJSON, false); err != nil {
			return err
		}
		var detail struct {
			Backup client.Backup `json:"backup"`
		}
		if err := c.Get(ctx, "/backups/"+id, &detail); err != nil {
			return err
		}
		b := detail.Backup
		if flagJSON {
			return printJSON(b.Verification)
		}
		v := b.Verification
		if v == nil {
			return errors.New("verification produced no report")
		}
		fmt.Println()
		fmt.Printf("%-24s %s\n", "Backup integrity", ui.Status(strings.ToUpper(v.Integrity.Status)))
		fmt.Printf("%-24s %s\n", "Restore test", ui.Status(strings.ToUpper(v.Restore.Status)))
		fmt.Printf("%-24s %s\n", "Database verification", ui.Status(strings.ToUpper(v.Database.Status)))
		fmt.Printf("%-24s %s\n", "Recovery test duration", ui.Duration(v.DurationMS))
		if v.Sandbox != "" {
			fmt.Printf("%-24s %s\n", "Sandbox", ui.Dim(v.Sandbox))
		}
		switch b.VerificationStatus {
		case "passed":
			fmt.Printf("\n%s %d tables and %d rows restored and queried successfully.\n", ui.Green("✓"), v.TablesRestored, v.Rows)
		case "unavailable":
			fmt.Printf("\n%s %s\n", ui.Yellow("!"), v.Restore.Message)
		default:
			for _, ch := range []client.Check{v.Integrity, v.Restore, v.Database} {
				if ch.Status == "fail" {
					fmt.Printf("\n%s %s\n", ui.Red("✗"), ch.Message)
				}
			}
			return errors.New("verification failed")
		}
		return nil
	},
}

func init() {
	f := backupCmd.Flags()
	f.StringVar(&backupFlags.storage, "storage", "", "storage destination name (default: the database's schedule or org default)")
	f.StringVar(&backupFlags.compression, "compression", "", "zstd|gzip|none (default zstd)")
	f.BoolVar(&backupFlags.noEncrypt, "no-encrypt", false, "store the backup unencrypted (not recommended)")
	f.BoolVarP(&backupFlags.detach, "detach", "d", false, "queue the backup and exit without waiting")
	f.BoolVarP(&backupFlags.verbose, "verbose", "v", false, "stream the job log instead of a progress bar")
	backupListCmd.Flags().StringVar(&listDatabase, "database", "", "only show backups of this database")
	backupListCmd.Flags().IntVar(&listLimit, "limit", 20, "number of backups to show")
	backupCmd.AddCommand(backupListCmd, backupVerifyCmd)
	rootCmd.AddCommand(backupCmd)
}
