// Package mysql is the MySQL and MariaDB driver. It connects with
// go-sql-driver/mysql and uses the MariaDB client tools (mariadb-dump,
// mariadb), which work with both MySQL 5.7+ and MariaDB 10+ servers, for
// logical backups and restores.
package mysql

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/dbvault/dbvault/backend/internal/engine"
	"github.com/dbvault/dbvault/backend/internal/proc"
)

// Driver implements engine.Driver for MySQL or MariaDB (one flavor each).
type Driver struct {
	flavor string
	// BinDir holds mariadb-dump and mariadb ("" = look them up on PATH).
	BinDir string
	// WorkDir holds short-lived private option files.
	WorkDir string
}

func NewMySQL(binDir, workDir string) *Driver {
	return &Driver{flavor: engine.MySQL, BinDir: binDir, WorkDir: workDir}
}

func NewMariaDB(binDir, workDir string) *Driver {
	return &Driver{flavor: engine.MariaDB, BinDir: binDir, WorkDir: workDir}
}

var _ engine.Driver = (*Driver)(nil)

func (d *Driver) Name() string { return d.flavor }
func (d *Driver) Label() string {
	if d.flavor == engine.MariaDB {
		return "MariaDB"
	}
	return "MySQL"
}
func (d *Driver) DefaultPort() int       { return 3306 }
func (d *Driver) SSLModes() []string     { return sslModes }
func (d *Driver) DefaultSSLMode() string { return "prefer" }
func (d *Driver) FileExtension() string  { return ".sql" }
func (d *Driver) Format() string         { return "sql" }
func (d *Driver) Capabilities() engine.Capabilities {
	return engine.Capabilities{AtomicRestore: false, Schemas: false}
}

func (d *Driver) path(name string) string {
	if d.BinDir != "" {
		return filepath.Join(d.BinDir, name)
	}
	return name
}

var toolVersionRE = regexp.MustCompile(`from (\d+\.\d+\.\d+)`)

// toolVersion returns e.g. "11.4.12" for `mariadb-dump --version`.
func (d *Driver) toolVersion(ctx context.Context, name string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, d.path(name), "--version").Output()
	if err != nil {
		return "", fmt.Errorf("%s is not available on the worker: install the MariaDB client tools (or set MYSQL_BIN_DIR): %w", name, err)
	}
	if m := toolVersionRE.FindSubmatch(out); m != nil {
		return string(m[1]), nil
	}
	return strings.TrimSpace(string(out)), nil
}

func (d *Driver) Inspect(ctx context.Context, t engine.Target) (engine.ServerInfo, error) {
	info, err := d.inspect(ctx, t)
	if err != nil {
		return info, engine.LocalhostHint(t.Host, err)
	}
	return info, nil
}

// DumpArgs are the mariadb-dump options DBVault uses, after the option
// file. --single-transaction gives a consistent snapshot of InnoDB tables
// without locking them.
func DumpArgs() []string {
	return []string{
		"--single-transaction", "--quick",
		"--routines", "--triggers", "--events",
		"--hex-blob", "--no-tablespaces",
		"--default-character-set=utf8mb4",
		"--max-allowed-packet=1G",
	}
}

func (d *Driver) Dump(ctx context.Context, t engine.Target, _ engine.ServerInfo) (*engine.Dump, error) {
	version, err := d.toolVersion(ctx, "mariadb-dump")
	if err != nil {
		return nil, err
	}
	t, err = effectiveTarget(ctx, t)
	if err != nil {
		return nil, err
	}
	opts, err := writeOptionFile(t, d.WorkDir)
	if err != nil {
		return nil, err
	}
	args := append([]string{"--defaults-file=" + opts.path}, DumpArgs()...)
	args = append(args, t.Database)
	cmd := exec.CommandContext(ctx, d.path("mariadb-dump"), args...)
	p, stdout, err := proc.Start(cmd, nil, nil, true)
	if err != nil {
		opts.Close()
		return nil, err
	}
	return &engine.Dump{
		Stream:      stripSandbox(stdout),
		ToolVersion: "mariadb-dump " + version,
		Wait: func() error {
			defer opts.Close()
			if err := p.Wait(); err != nil {
				return withHint(err)
			}
			return nil
		},
	}, nil
}

func (d *Driver) CheckRestoreTool(ctx context.Context) error {
	_, err := d.toolVersion(ctx, "mariadb")
	return err
}

func (d *Driver) Tables(ctx context.Context, archive io.Reader) ([]engine.Table, error) {
	return listTables(ctx, archive)
}

// Restore feeds the dump to the mariadb client. MySQL has no transactional
// DDL, so the restore is not atomic (o.Atomic is ignored); the dump itself
// drops and recreates each table, which covers o.Overwrite.
func (d *Driver) Restore(ctx context.Context, t engine.Target, dbName string, archive io.Reader, _ engine.RestoreOptions, log engine.Logger) error {
	if t.SSLMode == "require" {
		check := t
		check.Database = ""
		if _, err := d.inspect(ctx, check); err != nil {
			return err
		}
	}
	t, err := effectiveTarget(ctx, t)
	if err != nil {
		return err
	}
	opts, err := writeOptionFile(t, d.WorkDir)
	if err != nil {
		return err
	}
	defer opts.Close()
	filter := newRestoreFilter(archive)
	cmd := exec.CommandContext(ctx, d.path("mariadb"), "--defaults-file="+opts.path,
		"--database="+dbName, "--default-character-set=utf8mb4", "--max-allowed-packet=1G", "--batch")
	p, _, err := proc.Start(cmd, nil, filter, false)
	if err != nil {
		return err
	}
	if err := p.Wait(); err != nil {
		return fmt.Errorf("mariadb restore failed: %w", withHint(err))
	}
	if filter.rewritten > 0 && log != nil {
		log.Infof("Assigned %d view/routine/trigger definer(s) to the restoring user", filter.rewritten)
	}
	return nil
}

// withHint adds advice for common MySQL restore/dump failures.
func withHint(err error) error {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "1419") || strings.Contains(msg, "log_bin_trust_function_creators"):
		return fmt.Errorf("%w (with binary logging on, creating triggers and routines needs SUPER, or set log_bin_trust_function_creators=1 on the server)", err)
	case strings.Contains(msg, "1044") && strings.Contains(strings.ToLower(msg), "lock tables"):
		return fmt.Errorf("%w (the backup user needs the LOCK TABLES privilege for non-InnoDB tables)", err)
	case strings.Contains(strings.ToLower(msg), "show events") || strings.Contains(msg, "EVENT command denied"):
		return fmt.Errorf("%w (grant the backup user the EVENT privilege on this database)", err)
	}
	return err
}

// conn opens a connection without a default database.
func (d *Driver) conn(ctx context.Context, t engine.Target) (*sql.DB, error) {
	db, _, err := connect(ctx, t, "")
	return db, FriendlyError(err)
}

func (d *Driver) DatabaseExists(ctx context.Context, t engine.Target, name string) (bool, error) {
	db, err := d.conn(ctx, t)
	if err != nil {
		return false, err
	}
	defer db.Close()
	var n int
	err = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM information_schema.SCHEMATA WHERE LOWER(SCHEMA_NAME) = LOWER(?)`, name).Scan(&n)
	return n > 0, FriendlyError(err)
}

func (d *Driver) CreateDatabase(ctx context.Context, t engine.Target, name string) error {
	db, err := d.conn(ctx, t)
	if err != nil {
		return err
	}
	defer db.Close()
	if _, err := db.ExecContext(ctx, "CREATE DATABASE "+quoteIdent(name)); err != nil {
		return fmt.Errorf("create database %s: %w", name, FriendlyError(err))
	}
	return nil
}

func (d *Driver) DropDatabase(ctx context.Context, t engine.Target, name string) error {
	db, err := d.conn(ctx, t)
	if err != nil {
		return err
	}
	defer db.Close()
	_, err = db.ExecContext(ctx, "DROP DATABASE IF EXISTS "+quoteIdent(name))
	return FriendlyError(err)
}

func (d *Driver) CheckTables(ctx context.Context, t engine.Target, dbName string, tables []engine.Table, countRows bool) (engine.TableCheck, error) {
	res := engine.TableCheck{Expected: len(tables)}
	db, err := d.conn(ctx, t)
	if err != nil {
		return res, err
	}
	defer db.Close()
	for _, tb := range tables {
		var n int
		if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM information_schema.TABLES WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ? AND TABLE_TYPE = 'BASE TABLE'`,
			dbName, tb.Name).Scan(&n); err != nil {
			return res, FriendlyError(err)
		}
		if n == 0 {
			res.Missing = append(res.Missing, tb.String())
			continue
		}
		res.Found++
		if countRows {
			var rows int64
			if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+quoteIdent(dbName)+"."+quoteIdent(tb.Name)).Scan(&rows); err != nil {
				return res, fmt.Errorf("query %s: %w", tb, FriendlyError(err))
			}
			res.Rows += rows
		}
	}
	return res, nil
}

// Sandbox uses the official images: mysql:8 is the 8.4 LTS line and a
// newer server can restore dumps from any older one.
func (d *Driver) Sandbox(major int, password string) engine.SandboxSpec {
	var image string
	var env []string
	if d.flavor == engine.MariaDB {
		if major <= 0 {
			major = 11
		}
		image = "mariadb:" + strconv.Itoa(major)
		env = []string{"MARIADB_ROOT_PASSWORD=" + password, "MARIADB_DATABASE=verify"}
	} else {
		switch {
		case major <= 0:
			image = "mysql:8.4"
		case major == 5:
			image = "mysql:5.7"
		default:
			image = "mysql:" + strconv.Itoa(major)
		}
		env = []string{"MYSQL_ROOT_PASSWORD=" + password, "MYSQL_DATABASE=verify"}
	}
	return engine.SandboxSpec{
		Image:    image,
		Env:      env,
		Port:     3306,
		Username: "root",
		Password: password,
		Database: "verify",
		SSLMode:  "prefer",
	}
}
