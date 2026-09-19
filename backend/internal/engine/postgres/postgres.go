package postgres

import (
	"context"
	"fmt"
	"io"
	"strconv"

	"github.com/jackc/pgx/v5"

	"github.com/dbvault/dbvault/backend/internal/engine"
	"github.com/dbvault/dbvault/backend/internal/pgtools"
)

// Driver implements engine.Driver for PostgreSQL.
type Driver struct {
	Tools pgtools.Tools
	// WorkDir holds short-lived private files (CA certificates).
	WorkDir string
}

func New(tools pgtools.Tools, workDir string) *Driver { return &Driver{Tools: tools, WorkDir: workDir} }

var _ engine.Driver = (*Driver)(nil)

func (d *Driver) Name() string           { return engine.Postgres }
func (d *Driver) Label() string          { return "PostgreSQL" }
func (d *Driver) DefaultPort() int       { return 5432 }
func (d *Driver) DefaultSSLMode() string { return "prefer" }
func (d *Driver) FileExtension() string  { return ".dump" }
func (d *Driver) Format() string         { return "pg_dump_custom" }
func (d *Driver) SSLModes() []string {
	return []string{"disable", "allow", "prefer", "require", "verify-ca", "verify-full"}
}

// connect opens a connection to dbName ("" = the target's database).
func (d *Driver) connect(ctx context.Context, t engine.Target, dbName string) (*pgx.Conn, func(), error) {
	m, err := Materialize(t, d.WorkDir)
	if err != nil {
		return nil, nil, err
	}
	conn, err := m.Connect(ctx, dbName)
	if err != nil {
		m.Close()
		return nil, nil, err
	}
	return conn, func() { conn.Close(context.WithoutCancel(ctx)); m.Close() }, nil
}

func (d *Driver) Inspect(ctx context.Context, t engine.Target) (engine.ServerInfo, error) {
	m, err := Materialize(t, d.WorkDir)
	if err != nil {
		return engine.ServerInfo{}, err
	}
	defer m.Close()
	return m.Inspect(ctx)
}

// Dump runs pg_dump in custom format. pg_dump refuses servers newer than
// itself, so the client version is checked first.
func (d *Driver) Dump(ctx context.Context, t engine.Target, server engine.ServerInfo) (*engine.Dump, error) {
	version, err := d.Tools.CheckCompatible(ctx, "pg_dump", server.Major)
	if err != nil {
		return nil, err
	}
	m, err := Materialize(t, d.WorkDir)
	if err != nil {
		return nil, err
	}
	proc, stdout, err := d.Tools.StartDump(ctx, m.Env(""))
	if err != nil {
		m.Close()
		return nil, err
	}
	return &engine.Dump{
		Stream:      stdout,
		ToolVersion: version,
		Wait: func() error {
			defer m.Close()
			return proc.Wait()
		},
	}, nil
}

func (d *Driver) CheckRestoreTool(ctx context.Context) error {
	_, err := d.Tools.CheckCompatible(ctx, "pg_restore", 0)
	return err
}

func (d *Driver) Tables(ctx context.Context, archive io.Reader) ([]engine.Table, error) {
	entries, err := d.Tools.ListArchive(ctx, archive)
	if err != nil {
		return nil, err
	}
	var out []engine.Table
	for _, e := range entries {
		if e.Type == "TABLE" {
			out = append(out, engine.Table{Schema: e.Schema, Name: e.Name})
		}
	}
	return out, nil
}

// Restore runs pg_restore. When the target server is older than
// pg_restore, output is adapted for it (see pgtools/compat.go).
func (d *Driver) Restore(ctx context.Context, t engine.Target, dbName string, archive io.Reader, o engine.RestoreOptions, log engine.Logger) error {
	m, err := Materialize(t, d.WorkDir)
	if err != nil {
		return err
	}
	defer m.Close()
	opts := pgtools.RestoreOptions{Clean: o.Overwrite, SingleTransaction: o.Atomic}
	if err := d.describeTarget(ctx, m, dbName, &opts); err != nil {
		return err
	}
	if compat, client := d.Tools.NeedsCompat(ctx, opts.ServerMajor); compat && log != nil {
		log.Infof("Target runs PostgreSQL %d; adapting pg_restore %d output for compatibility", opts.ServerMajor, client)
	}
	proc, err := d.Tools.StartRestore(ctx, m.Env(dbName), opts, archive)
	if err != nil {
		return err
	}
	if err := proc.Wait(); err != nil {
		return fmt.Errorf("pg_restore failed: %w", err)
	}
	return nil
}

// describeTarget records the target server's version and settings so
// pg_restore output can be adapted to older servers.
func (d *Driver) describeTarget(ctx context.Context, m *Materialized, dbName string, opts *pgtools.RestoreOptions) error {
	conn, err := m.Connect(ctx, dbName)
	if err != nil {
		return err
	}
	defer conn.Close(context.WithoutCancel(ctx))
	var names []string
	if err := conn.QueryRow(ctx, `SELECT current_setting('server_version_num')::int / 10000, array_agg(name) FROM pg_catalog.pg_settings`).
		Scan(&opts.ServerMajor, &names); err != nil {
		return FriendlyError(err)
	}
	opts.ServerSettings = make(map[string]bool, len(names))
	for _, n := range names {
		opts.ServerSettings[n] = true
	}
	return nil
}

func (d *Driver) DatabaseExists(ctx context.Context, t engine.Target, name string) (bool, error) {
	conn, done, err := d.connect(ctx, t, "")
	if err != nil {
		return false, err
	}
	defer done()
	var exists bool
	err = conn.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pg_catalog.pg_database WHERE lower(datname) = lower($1))`, name).Scan(&exists)
	return exists, err
}

func (d *Driver) CreateDatabase(ctx context.Context, t engine.Target, name string) error {
	conn, done, err := d.connect(ctx, t, "")
	if err != nil {
		return err
	}
	defer done()
	if _, err := conn.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{name}.Sanitize()); err != nil {
		return fmt.Errorf("create database %s: %w", name, FriendlyError(err))
	}
	return nil
}

func (d *Driver) DropDatabase(ctx context.Context, t engine.Target, name string) error {
	conn, done, err := d.connect(ctx, t, "")
	if err != nil {
		return err
	}
	defer done()
	var version int
	if err := conn.QueryRow(ctx, `SELECT current_setting('server_version_num')::int`).Scan(&version); err != nil {
		return err
	}
	stmt := "DROP DATABASE IF EXISTS " + pgx.Identifier{name}.Sanitize()
	if version >= 130000 {
		stmt += " WITH (FORCE)" // disconnects leftover sessions (PostgreSQL 13+)
	}
	_, err = conn.Exec(ctx, stmt)
	return err
}

func (d *Driver) CheckTables(ctx context.Context, t engine.Target, dbName string, tables []engine.Table, countRows bool) (engine.TableCheck, error) {
	res := engine.TableCheck{Expected: len(tables)}
	conn, done, err := d.connect(ctx, t, dbName)
	if err != nil {
		return res, err
	}
	defer done()
	if _, err := conn.Exec(ctx, "SET statement_timeout = '5min'"); err != nil {
		return res, err
	}
	for _, tb := range tables {
		var exists bool
		if err := conn.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pg_catalog.pg_tables WHERE schemaname = $1 AND tablename = $2)`, tb.Schema, tb.Name).Scan(&exists); err != nil {
			return res, err
		}
		if !exists {
			res.Missing = append(res.Missing, tb.String())
			continue
		}
		res.Found++
		if countRows {
			var n int64
			if err := conn.QueryRow(ctx, "SELECT count(*) FROM "+pgx.Identifier{tb.Schema, tb.Name}.Sanitize()).Scan(&n); err != nil {
				return res, fmt.Errorf("query %s: %w", tb, err)
			}
			res.Rows += n
		}
	}
	return res, nil
}

func (d *Driver) Sandbox(major int, password string) engine.SandboxSpec {
	if major <= 0 {
		major = 17
	}
	return engine.SandboxSpec{
		Image:    "postgres:" + strconv.Itoa(major) + "-alpine",
		Env:      []string{"POSTGRES_PASSWORD=" + password, "POSTGRES_DB=verify"},
		Port:     5432,
		Username: "postgres",
		Password: password,
		Database: "verify",
		SSLMode:  "disable",
	}
}
