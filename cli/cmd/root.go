// Package cmd implements the dbvault CLI commands.
package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strings"

	"github.com/spf13/cobra"

	"github.com/dbvault/dbvault/cli/internal/client"
	"github.com/dbvault/dbvault/cli/internal/config"
	"github.com/dbvault/dbvault/cli/internal/ui"
)

// Version is set at build time: -ldflags "-X github.com/dbvault/dbvault/cli/cmd.Version=v1.0.0".
var Version = "dev"

var (
	flagServer  string
	flagOrg     string
	flagJSON    bool
	flagNoColor bool
)

var rootCmd = &cobra.Command{
	Use:   "dbvault",
	Short: "Open-source SQL database backups that just work",
	Long: `dbvault manages PostgreSQL, MySQL and MariaDB backups on a DBVault server.

Get started:
  dbvault init                 connect this CLI to your DBVault server
  dbvault database add         register a PostgreSQL, MySQL or MariaDB database
  dbvault backup production    back up the "production" database now`,
	SilenceUsage:  true,
	SilenceErrors: true,
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		if flagNoColor {
			ui.DisableColor()
		}
	},
}

func init() {
	rootCmd.PersistentFlags().StringVar(&flagServer, "server", "", "DBVault server URL (default from config or DBVAULT_SERVER)")
	rootCmd.PersistentFlags().StringVar(&flagOrg, "org", "", "organization id or slug (default from config or DBVAULT_ORG)")
	rootCmd.PersistentFlags().BoolVar(&flagJSON, "json", false, "print machine-readable JSON")
	rootCmd.PersistentFlags().BoolVar(&flagNoColor, "no-color", false, "disable colored output")
}

// Execute runs the CLI and returns the process exit code.
func Execute() int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := rootCmd.ExecuteContext(ctx); err != nil {
		var apiErr *client.Error
		if errors.As(err, &apiErr) && apiErr.Status == 401 {
			fmt.Fprintln(os.Stderr, ui.Red("Error:"), "not authenticated. Run", ui.Bold("dbvault init"), "to connect this CLI.")
			return 1
		}
		fmt.Fprintln(os.Stderr, ui.Red("Error:"), err)
		return 1
	}
	return 0
}

// session loads config and builds an authenticated client.
func session() (*client.Client, *config.Config, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, nil, err
	}
	if flagServer != "" {
		cfg.Server = flagServer
	}
	if flagOrg != "" {
		cfg.Organization = flagOrg
	}
	if cfg.Server == "" || cfg.Token == "" {
		return nil, nil, errors.New("this CLI is not connected to a DBVault server yet. Run: dbvault init")
	}
	return client.New(cfg.Server, cfg.Token, cfg.Organization), cfg, nil
}

func printJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// findDatabase resolves a database by name or id.
func findDatabase(ctx context.Context, c *client.Client, nameOrID string) (client.Database, error) {
	var list []client.Database
	if err := c.Get(ctx, "/databases", &list); err != nil {
		return client.Database{}, err
	}
	for _, d := range list {
		if strings.EqualFold(d.Name, nameOrID) || d.ID == nameOrID {
			return d, nil
		}
	}
	names := make([]string, 0, len(list))
	for _, d := range list {
		names = append(names, d.Name)
	}
	if len(names) == 0 {
		return client.Database{}, fmt.Errorf("no databases yet. Add one with: dbvault database add")
	}
	return client.Database{}, fmt.Errorf("database %q not found (available: %s)", nameOrID, strings.Join(names, ", "))
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// engineLabel returns e.g. "MySQL" ("" means PostgreSQL, for older servers).
func engineLabel(engine string) string {
	if e, ok := engines[engine]; ok {
		return e.label
	}
	return "PostgreSQL"
}

// engineVersion renders e.g. "PostgreSQL 17" or "MySQL 8".
func engineVersion(engine string, v *string) string {
	return engineLabel(engine) + " " + pgMajor(v)
}

func pgMajor(v *string) string {
	if v == nil || *v == "" {
		return "unknown"
	}
	return strings.SplitN(*v, ".", 2)[0]
}
