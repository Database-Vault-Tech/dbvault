// Package engine defines the contract every supported database engine
// implements. Everything else in DBVault (compression, encryption,
// checksums, storage, retention, scheduling, verification reports) is
// engine-agnostic and talks to databases only through a Driver.
package engine

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

// Engine identifiers stored in databases.engine.
const (
	Postgres = "postgres"
	MySQL    = "mysql"
	MariaDB  = "mariadb"
)

// Target holds everything needed to connect to a database. Password is
// plaintext in memory only; it is never serialised.
type Target struct {
	Engine      string
	Host        string
	Port        int
	Database    string
	Username    string
	Password    string `json:"-"`
	SSLMode     string
	SSLRootCert string
}

// ServerInfo describes a server and database, as shown by connection tests.
type ServerInfo struct {
	Version      string `json:"version"`
	VersionNum   int    `json:"version_num"`
	Major        int    `json:"major"`
	SizeBytes    int64  `json:"size_bytes"`
	TableCount   int    `json:"table_count"`
	FullVersion  string `json:"full_version"`
	CurrentUser  string `json:"current_user"`
	IsSuperuser  bool   `json:"is_superuser"`
	InRecovery   bool   `json:"in_recovery"`
	LatencyMilli int64  `json:"latency_ms"`
}

// Table identifies a table contained in a backup archive.
type Table struct {
	Schema string
	Name   string
}

func (t Table) String() string {
	if t.Schema == "" {
		return t.Name
	}
	return t.Schema + "." + t.Name
}

// TableCheck is the result of checking restored tables.
type TableCheck struct {
	Expected int      `json:"tables_expected"`
	Found    int      `json:"tables_found"`
	Rows     int64    `json:"rows"`
	Missing  []string `json:"missing,omitempty"`
}

// Dump is a running native dump. Stream must be read to EOF before Wait.
type Dump struct {
	Stream      io.Reader
	Wait        func() error
	ToolVersion string
}

// RestoreOptions controls a restore.
type RestoreOptions struct {
	// Overwrite drops objects contained in the archive before recreating
	// them (restoring over an existing database).
	Overwrite bool
	// Atomic applies the whole restore in one transaction where the engine
	// supports it.
	Atomic bool
}

// Capabilities describes engine behaviour the API and UI must respect.
type Capabilities struct {
	// AtomicRestore is true when a failed restore leaves the target
	// unchanged (PostgreSQL restores run in one transaction; MySQL DDL
	// commits implicitly, so a failed restore can leave it half-restored).
	AtomicRestore bool `json:"atomic_restore"`
	// Schemas is true when tables live in named schemas within a database.
	Schemas bool `json:"schemas"`
}

// Logger receives user-visible job log lines.
type Logger interface {
	Infof(format string, args ...any)
	Warnf(format string, args ...any)
}

// SandboxSpec describes a disposable container used for restore tests.
type SandboxSpec struct {
	Image string
	Env   []string
	Port  int
	// Username, Password and Database to connect with once it's ready.
	Username string
	Password string
	Database string
	SSLMode  string
}

// Driver is implemented once per database engine.
type Driver interface {
	// Name is the stored identifier ("postgres"); Label is for humans ("PostgreSQL").
	Name() string
	Label() string
	DefaultPort() int
	// SSLModes lists accepted values for Target.SSLMode; DefaultSSLMode is used when empty.
	SSLModes() []string
	DefaultSSLMode() string
	// FileExtension of dump artifacts before compression, e.g. ".dump".
	FileExtension() string
	// Format recorded on backups, e.g. "pg_dump_custom".
	Format() string

	// Inspect connects and reports server facts (the "Test Connection" result).
	Inspect(ctx context.Context, t Target) (ServerInfo, error)
	// Dump starts a consistent logical dump of t.Database.
	Dump(ctx context.Context, t Target, server ServerInfo) (*Dump, error)
	// CheckRestoreTool reports whether the worker can restore this engine.
	CheckRestoreTool(ctx context.Context) error
	// Tables lists the tables contained in a dump archive.
	Tables(ctx context.Context, archive io.Reader) ([]Table, error)
	// Restore applies an archive to database dbName on t's server.
	Restore(ctx context.Context, t Target, dbName string, archive io.Reader, o RestoreOptions, log Logger) error

	DatabaseExists(ctx context.Context, t Target, name string) (bool, error)
	CreateDatabase(ctx context.Context, t Target, name string) error
	DropDatabase(ctx context.Context, t Target, name string) error
	// CheckTables confirms tables exist in dbName (and counts rows when asked).
	CheckTables(ctx context.Context, t Target, dbName string, tables []Table, countRows bool) (TableCheck, error)

	// Sandbox returns the container spec for restore tests of a backup taken
	// from a server with the given major version.
	Sandbox(major int, password string) SandboxSpec

	// Capabilities describes behaviour that differs between engines.
	Capabilities() Capabilities
}

// Registry holds the drivers available in this build.
type Registry struct {
	drivers map[string]Driver
	order   []string
}

func NewRegistry(drivers ...Driver) *Registry {
	r := &Registry{drivers: map[string]Driver{}}
	for _, d := range drivers {
		if _, dup := r.drivers[d.Name()]; !dup {
			r.order = append(r.order, d.Name())
		}
		r.drivers[d.Name()] = d
	}
	return r
}

// ErrUnsupported is returned for engines this build can't handle.
var ErrUnsupported = errors.New("unsupported database engine")

// Get returns the driver for an engine name ("" means PostgreSQL, for rows
// created before engines existed).
func (r *Registry) Get(name string) (Driver, error) {
	if name == "" {
		name = Postgres
	}
	d, ok := r.drivers[name]
	if !ok {
		return nil, fmt.Errorf("%w %q", ErrUnsupported, name)
	}
	return d, nil
}

// For returns the driver for a target.
func (r *Registry) For(t Target) (Driver, error) { return r.Get(t.Engine) }

// Names lists registered engines in registration order.
func (r *Registry) Names() []string { return append([]string(nil), r.order...) }

// Drivers lists registered drivers in registration order.
func (r *Registry) Drivers() []Driver {
	out := make([]Driver, 0, len(r.drivers))
	for _, n := range r.Names() {
		out = append(out, r.drivers[n])
	}
	return out
}

// LocalhostHint explains the most common self-hosting mistake: inside a
// container, "localhost" is the container itself, not the host machine.
func LocalhostHint(host string, err error) error {
	switch strings.ToLower(strings.Trim(host, "[]")) {
	case "localhost", "127.0.0.1", "::1":
	default:
		return err
	}
	if _, statErr := os.Stat("/.dockerenv"); statErr != nil {
		return err
	}
	// Only connectivity failures; an authentication error means the
	// server was reached.
	msg := err.Error()
	if !strings.Contains(msg, "connection refused") && !strings.Contains(msg, "timeout") && !strings.Contains(msg, "could not connect") {
		return err
	}
	return fmt.Errorf("%w. DBVault runs in Docker, so %q refers to the DBVault container itself: "+
		"use host.docker.internal for a database on this machine, or the database container's name if it shares a Docker network", err, host)
}
