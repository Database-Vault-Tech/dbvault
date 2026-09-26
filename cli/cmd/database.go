package cmd

import (
	"bufio"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/dbvault/dbvault/cli/internal/client"
	"github.com/dbvault/dbvault/cli/internal/ui"
)

// engines holds per-engine defaults for `database add`.
var engines = map[string]struct {
	label, database, user string
	port                  int
}{
	"postgres": {"PostgreSQL", "postgres", "postgres", 5432},
	"mysql":    {"MySQL", "", "root", 3306},
	"mariadb":  {"MariaDB", "", "root", 3306},
	"sqlite":   {"SQLite", "", "", 0},
}

var databaseCmd = &cobra.Command{
	Use:     "database",
	Aliases: []string{"databases", "db"},
	Short:   "Manage protected databases",
}

var databaseListCmd = &cobra.Command{
	Use:     "list",
	Aliases: []string{"ls"},
	Short:   "List databases",
	Args:    cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, _, err := session()
		if err != nil {
			return err
		}
		var list []client.Database
		if err := c.Get(cmd.Context(), "/databases", &list); err != nil {
			return err
		}
		if flagJSON {
			return printJSON(list)
		}
		if len(list) == 0 {
			fmt.Println("No databases yet. Add one with:", ui.Bold("dbvault database add"))
			return nil
		}
		t := ui.NewTable(cmd.OutOrStdout(), "Name", "Connection", "Engine", "Protected", "Last backup", "Next run", "Stored")
		for _, d := range list {
			prot := ui.Green("yes")
			if !d.Protected {
				prot = ui.Yellow("no schedule")
			}
			last := ui.Dim("never")
			if d.LastBackupAt != nil {
				last = ui.Status(deref(d.LastBackupStatus)) + " " + ui.Dim(ui.Ago(d.LastBackupAt))
			}
			next := ui.Dim("—")
			if d.NextRunAt != nil {
				next = ui.Ago(d.NextRunAt)
			}
			where := fmt.Sprintf("%s@%s:%d/%s", d.Username, d.Host, d.Port, d.Database)
			if d.Engine == "sqlite" {
				where = d.Database // a file in the server's SQLite folder
			}
			t.Row(ui.Bold(d.Name), where, engineVersion(d.Engine, d.PGVersion), prot, last, next, ui.Bytes(d.StorageBytes))
		}
		t.Flush()
		return nil
	},
}

var addFlags struct {
	engine, name, host, database, username, sslMode, connURL, file string
	port                                                           int
	passwordStdin                                                  bool
}

var databaseAddCmd = &cobra.Command{
	Use:   "add",
	Short: "Add a PostgreSQL, MySQL, MariaDB or SQLite database",
	Long: `Add a PostgreSQL, MySQL, MariaDB or SQLite database. The connection is tested before it's saved.

  dbvault database add --name production --url "postgres://app@db.internal:5432/shop?sslmode=require"
  dbvault database add --name shop --url "mysql://app@mysql.internal:3306/shop"
  echo "$PGPASSWORD" | dbvault database add --name production --host db.internal --database shop --username app --password-stdin
  dbvault database add --engine mariadb --name shop --host maria.internal --database shop --username app
  dbvault database add --name blog --file myapp/app.db   # SQLite: a path inside the server's SQLite folder

Missing values are prompted for. Passwords are never accepted as flags (they
would leak into shell history and process listings).`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		c, _, err := session()
		if err != nil {
			return err
		}
		engine, host, port, dbname, user, pass, ssl := addFlags.engine, addFlags.host, addFlags.port, addFlags.database, addFlags.username, "", addFlags.sslMode
		if addFlags.connURL != "" {
			u, err := url.Parse(addFlags.connURL)
			if err != nil {
				return errors.New("--url must be a postgres://, mysql:// or mariadb:// connection string")
			}
			switch u.Scheme {
			case "postgres", "postgresql":
				engine = "postgres"
			case "mysql", "mariadb":
				engine = u.Scheme
			default:
				return errors.New("--url must be a postgres://, mysql:// or mariadb:// connection string")
			}
			host = u.Hostname()
			if p, err := strconv.Atoi(u.Port()); err == nil {
				port = p
			}
			dbname = strings.TrimPrefix(u.Path, "/")
			user = u.User.Username()
			pass, _ = u.User.Password()
			if s := u.Query().Get("sslmode"); s != "" {
				ssl = s
			}
		}
		if engine == "" && addFlags.file != "" {
			engine = "sqlite"
		}
		if engine == "" {
			engine = "postgres"
		}
		eng, ok := engines[engine]
		if !ok {
			return fmt.Errorf("--engine must be postgres, mysql, mariadb or sqlite (got %q)", engine)
		}
		name := addFlags.name
		if name == "" {
			name = ui.Prompt("Name (e.g. production)", "")
		}
		var in map[string]any
		if engine == "sqlite" {
			// A file inside the folder mounted into DBVault (SQLITE_ROOT): no
			// host, login or password.
			file := addFlags.file
			if file == "" {
				file = dbname
			}
			if file == "" {
				file = ui.Prompt("File (path inside the server's SQLite folder, e.g. myapp/app.db)", "")
			}
			in = map[string]any{"engine": engine, "name": name, "database": file}
		} else {
			if host == "" {
				host = ui.Prompt("Host", "localhost")
			}
			if port == 0 {
				port = eng.port
			}
			if dbname == "" {
				dbname = ui.Prompt("Database", eng.database)
			}
			if user == "" {
				user = ui.Prompt("Username", eng.user)
			}
			if pass == "" {
				if addFlags.passwordStdin {
					line, err := bufio.NewReader(os.Stdin).ReadString('\n')
					if err != nil && line == "" {
						return errors.New("no password on stdin")
					}
					pass = strings.TrimRight(line, "\r\n")
				} else {
					pass, err = ui.PromptSecret("Password")
					if err != nil {
						return err
					}
				}
			}
			if ssl == "" {
				ssl = "prefer"
			}
			in = map[string]any{"engine": engine, "name": name, "host": host, "port": port, "database": dbname, "username": user, "password": pass, "ssl_mode": ssl}
		}

		fmt.Println(ui.Dim("Testing connection…"))
		var test client.ConnectionTest
		if err := c.Post(ctx, "/databases/test", in, &test); err != nil {
			return err
		}
		if !test.OK {
			return fmt.Errorf("connection failed: %s", test.Message)
		}
		fmt.Printf("%s Connection successful — %s %s, %s, %d tables\n", ui.Green("✓"), eng.label, test.Server.Version, ui.Bytes(test.Server.SizeBytes), test.Server.TableCount)
		var res struct {
			Database client.Database `json:"database"`
		}
		if err := c.Post(ctx, "/databases", in, &res); err != nil {
			return err
		}
		if flagJSON {
			return printJSON(res.Database)
		}
		fmt.Printf("%s Added %s %s\n", ui.Green("✓"), ui.Bold(res.Database.Name), ui.Dim("("+res.Database.ID+")"))
		fmt.Println(ui.Dim("  Credentials are encrypted at rest and never returned by the API."))
		fmt.Println("\nBack it up now:", ui.Bold("dbvault backup "+res.Database.Name))
		return nil
	},
}

var removeYes bool

var databaseRemoveCmd = &cobra.Command{
	Use:     "remove <name>",
	Aliases: []string{"rm", "delete"},
	Short:   "Remove a database (existing backups are kept)",
	Args:    cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		c, _, err := session()
		if err != nil {
			return err
		}
		d, err := findDatabase(ctx, c, args[0])
		if err != nil {
			return err
		}
		if !removeYes {
			fmt.Printf("Remove %s and its schedules? Existing backups are kept. Type the database name to confirm: ", ui.Bold(d.Name))
			line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
			if strings.TrimSpace(line) != d.Name {
				return errors.New("aborted")
			}
		}
		if err := c.Delete(ctx, "/databases/"+d.ID); err != nil {
			return err
		}
		fmt.Println(ui.Green("✓"), "Removed", d.Name)
		return nil
	},
}

var databaseTestCmd = &cobra.Command{
	Use:   "test <name>",
	Short: "Test a database connection",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		c, _, err := session()
		if err != nil {
			return err
		}
		d, err := findDatabase(ctx, c, args[0])
		if err != nil {
			return err
		}
		var test client.ConnectionTest
		if err := c.Post(ctx, "/databases/"+d.ID+"/test", nil, &test); err != nil {
			return err
		}
		if flagJSON {
			return printJSON(test)
		}
		if !test.OK {
			return fmt.Errorf("connection to %s failed: %s", d.Name, test.Message)
		}
		fmt.Printf("%s Connection successful\n\n", ui.Green("✓"))
		fmt.Println("Database:  ", d.Name)
		fmt.Println("Server:    ", engineLabel(d.Engine), test.Server.Version)
		fmt.Println("Size:      ", ui.Bytes(test.Server.SizeBytes))
		fmt.Println("Tables:    ", test.Server.TableCount)
		fmt.Println("Latency:   ", fmt.Sprintf("%dms", test.Server.LatencyMS))
		return nil
	},
}

func init() {
	f := databaseAddCmd.Flags()
	f.StringVar(&addFlags.engine, "engine", "", "postgres|mysql|mariadb|sqlite (default postgres, or taken from --url)")
	f.StringVar(&addFlags.name, "name", "", "display name, e.g. production")
	f.StringVar(&addFlags.connURL, "url", "", "postgres://, mysql:// or mariadb:// connection string")
	f.StringVar(&addFlags.host, "host", "", "hostname or IP")
	f.IntVar(&addFlags.port, "port", 0, "port (default 5432 for PostgreSQL, 3306 for MySQL/MariaDB)")
	f.StringVar(&addFlags.database, "database", "", "database name")
	f.StringVar(&addFlags.file, "file", "", "SQLite: path of the database file inside the server's SQLite folder (implies --engine sqlite)")
	f.StringVar(&addFlags.username, "username", "", "username")
	f.StringVar(&addFlags.sslMode, "ssl-mode", "", "disable|allow|prefer|require|verify-ca|verify-full (default prefer; MySQL/MariaDB: disable|prefer|require|verify-full)")
	f.BoolVar(&addFlags.passwordStdin, "password-stdin", false, "read the password from stdin")
	databaseRemoveCmd.Flags().BoolVarP(&removeYes, "yes", "y", false, "skip confirmation")
	databaseCmd.AddCommand(databaseListCmd, databaseAddCmd, databaseRemoveCmd, databaseTestCmd)
	rootCmd.AddCommand(databaseCmd)
}
