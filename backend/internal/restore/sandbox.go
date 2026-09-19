// Package restore restores backups into target databases and proves
// backups are restorable by restoring them into disposable sandboxes.
package restore

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/dbvault/dbvault/backend/internal/database"
)

// Sandbox provisions disposable PostgreSQL databases for restore tests.
type Sandbox interface {
	// Name describes the sandbox kind for reports ("docker", "server").
	Name() string
	// Check reports whether the sandbox can currently be used.
	Check(ctx context.Context) error
	// Provision creates an empty database for a dump of the given major
	// PostgreSQL version. The instance must always be destroyed.
	Provision(ctx context.Context, major int) (*Instance, error)
}

// Instance is a provisioned, empty sandbox database.
type Instance struct {
	Target      database.Target
	DBName      string
	Description string
	destroy     func(ctx context.Context) error
}

// Destroy tears the sandbox down. It uses a fresh context so cleanup runs
// even when the job was cancelled.
func (i *Instance) Destroy() error {
	if i == nil || i.destroy == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	return i.destroy(ctx)
}

func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// waitForPostgres polls until the server accepts connections.
func waitForPostgres(ctx context.Context, t database.Target, dbName string, timeout time.Duration) error {
	m, err := t.Materialize("")
	if err != nil {
		return err
	}
	defer m.Close()
	deadline := time.Now().Add(timeout)
	var lastErr error
	for time.Now().Before(deadline) {
		cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		conn, err := m.Connect(cctx, dbName)
		if err == nil {
			err = conn.Ping(cctx)
			conn.Close(cctx)
		}
		cancel()
		if err == nil {
			return nil
		}
		lastErr = err
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
	return fmt.Errorf("sandbox PostgreSQL did not become ready: %v", lastErr)
}

// ServerSandbox creates a throwaway database on a dedicated PostgreSQL
// server (VERIFY_POSTGRES_URL) for each test and drops it afterwards. It
// needs no Docker access. Use a server that holds no other data.
type ServerSandbox struct {
	admin database.Target
}

// NewServerSandbox parses the admin connection URL.
func NewServerSandbox(adminURL string) (*ServerSandbox, error) {
	u, err := url.Parse(adminURL)
	if err != nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") {
		return nil, errors.New("VERIFY_POSTGRES_URL must be a postgres:// URL")
	}
	port := 5432
	if p := u.Port(); p != "" {
		port, _ = strconv.Atoi(p)
	}
	pw, _ := u.User.Password()
	sslmode := u.Query().Get("sslmode")
	if sslmode == "" {
		sslmode = "prefer"
	}
	db := strings.TrimPrefix(u.Path, "/")
	if db == "" {
		db = "postgres"
	}
	return &ServerSandbox{admin: database.Target{Host: u.Hostname(), Port: port, Database: db, Username: u.User.Username(), Password: pw, SSLMode: sslmode}}, nil
}

func (s *ServerSandbox) Name() string { return "server" }

func (s *ServerSandbox) Check(ctx context.Context) error {
	m, err := s.admin.Materialize("")
	if err != nil {
		return err
	}
	defer m.Close()
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	conn, err := m.Connect(ctx, "")
	if err != nil {
		return fmt.Errorf("verification server unreachable: %w", err)
	}
	return conn.Close(ctx)
}

func (s *ServerSandbox) Provision(ctx context.Context, major int) (*Instance, error) {
	m, err := s.admin.Materialize("")
	if err != nil {
		return nil, err
	}
	defer m.Close()
	conn, err := m.Connect(ctx, "")
	if err != nil {
		return nil, fmt.Errorf("verification server unreachable: %w", err)
	}
	defer conn.Close(context.WithoutCancel(ctx))
	var serverMajor int
	if err := conn.QueryRow(ctx, `SELECT current_setting('server_version_num')::int / 10000`).Scan(&serverMajor); err != nil {
		return nil, err
	}
	if major > serverMajor {
		return nil, fmt.Errorf("the verification server runs PostgreSQL %d but the backup is from PostgreSQL %d; use a newer VERIFY_POSTGRES_URL server or VERIFY_MODE=docker", serverMajor, major)
	}
	name := "dbvault_verify_" + randomHex(6)
	if _, err := conn.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{name}.Sanitize()); err != nil {
		return nil, fmt.Errorf("create sandbox database: %w", database.FriendlyError(err))
	}
	admin := s.admin
	return &Instance{
		Target:      admin,
		DBName:      name,
		Description: fmt.Sprintf("temporary database on verification server (PostgreSQL %d)", serverMajor),
		destroy: func(ctx context.Context) error {
			m, err := admin.Materialize("")
			if err != nil {
				return err
			}
			defer m.Close()
			c, err := m.Connect(ctx, "")
			if err != nil {
				return err
			}
			defer c.Close(ctx)
			_, err = c.Exec(ctx, "DROP DATABASE IF EXISTS "+pgx.Identifier{name}.Sanitize()+" WITH (FORCE)")
			return err
		},
	}, nil
}
