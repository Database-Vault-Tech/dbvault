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

	"github.com/dbvault/dbvault/backend/internal/engine"
)

// Sandbox provisions disposable databases for restore tests.
type Sandbox interface {
	// Name describes the sandbox kind for reports ("docker", "server").
	Name() string
	// Check reports whether the sandbox can currently be used.
	Check(ctx context.Context) error
	// Provision creates an empty database able to restore a backup of the
	// given engine and major version. The instance must always be destroyed.
	Provision(ctx context.Context, drv engine.Driver, major int) (*Instance, error)
}

// Instance is a provisioned, empty sandbox database.
type Instance struct {
	Target      engine.Target
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

// waitReady polls until the sandbox accepts connections.
func waitReady(ctx context.Context, drv engine.Driver, t engine.Target, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	var lastErr error
	for time.Now().Before(deadline) {
		cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		_, err := drv.Inspect(cctx, t)
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
	return fmt.Errorf("sandbox %s did not become ready: %v", drv.Label(), lastErr)
}

// ServerSandbox creates a throwaway database on a dedicated server
// (VERIFY_POSTGRES_URL) for each test and drops it afterwards. It needs no
// Docker access. Use a server that holds no other data.
type ServerSandbox struct {
	admin engine.Target
	drv   engine.Driver
}

// NewServerSandbox parses the admin connection URL of a PostgreSQL server.
func NewServerSandbox(adminURL string, drv engine.Driver) (*ServerSandbox, error) {
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
	return &ServerSandbox{drv: drv, admin: engine.Target{Engine: drv.Name(), Host: u.Hostname(), Port: port, Database: db,
		Username: u.User.Username(), Password: pw, SSLMode: sslmode}}, nil
}

func (s *ServerSandbox) Name() string { return "server" }

func (s *ServerSandbox) Check(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if _, err := s.drv.Inspect(ctx, s.admin); err != nil {
		return fmt.Errorf("verification server unreachable: %w", err)
	}
	return nil
}

func (s *ServerSandbox) Provision(ctx context.Context, drv engine.Driver, major int) (*Instance, error) {
	if drv.Name() != s.drv.Name() {
		return nil, fmt.Errorf("the verification server runs %s, so it can't test %s backups; use VERIFY_MODE=docker", s.drv.Label(), drv.Label())
	}
	info, err := s.drv.Inspect(ctx, s.admin)
	if err != nil {
		return nil, fmt.Errorf("verification server unreachable: %w", err)
	}
	if major > info.Major {
		return nil, fmt.Errorf("the verification server runs %s %d but the backup is from %s %d; use a newer VERIFY_POSTGRES_URL server or VERIFY_MODE=docker",
			s.drv.Label(), info.Major, drv.Label(), major)
	}
	name := "dbvault_verify_" + randomHex(6)
	if err := s.drv.CreateDatabase(ctx, s.admin, name); err != nil {
		return nil, fmt.Errorf("create sandbox database: %w", err)
	}
	admin, sdrv := s.admin, s.drv
	return &Instance{
		Target:      admin,
		DBName:      name,
		Description: fmt.Sprintf("temporary database on verification server (%s %d)", s.drv.Label(), info.Major),
		destroy:     func(ctx context.Context) error { return sdrv.DropDatabase(ctx, admin, name) },
	}, nil
}
