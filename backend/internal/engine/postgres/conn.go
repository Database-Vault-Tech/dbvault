// Package postgres is the PostgreSQL driver: it connects with pgx and uses
// the official pg_dump / pg_restore tools for backups and restores.
package postgres

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/dbvault/dbvault/backend/internal/engine"
)

// Materialized is a target prepared for use, possibly with a temp CA file.
type Materialized struct {
	target   engine.Target
	certPath string
}

// Materialize writes the optional CA certificate to a private temp file.
// Call Close to remove it.
func Materialize(t engine.Target, workDir string) (*Materialized, error) {
	m := &Materialized{target: t}
	if strings.TrimSpace(t.SSLRootCert) != "" {
		if err := os.MkdirAll(workDir, 0o700); err != nil {
			return nil, err
		}
		f, err := os.CreateTemp(workDir, "ca-*.pem")
		if err != nil {
			return nil, err
		}
		if _, err := f.WriteString(t.SSLRootCert); err != nil {
			f.Close()
			os.Remove(f.Name())
			return nil, err
		}
		f.Close()
		m.certPath = f.Name()
	}
	return m, nil
}

func (m *Materialized) Close() {
	if m.certPath != "" {
		_ = os.Remove(m.certPath)
	}
}

// URL builds a connection URL for dbName (defaults to the target database).
// url.URL escapes every component, so credentials containing special
// characters can't inject extra connection parameters.
func (m *Materialized) URL(dbName string) string {
	t := m.target
	if dbName == "" {
		dbName = t.Database
	}
	q := url.Values{}
	sslMode := t.SSLMode
	if sslMode == "" {
		sslMode = "prefer"
	}
	q.Set("sslmode", sslMode)
	if m.certPath != "" {
		q.Set("sslrootcert", m.certPath)
	}
	q.Set("connect_timeout", "10")
	q.Set("application_name", "dbvault")
	u := url.URL{
		Scheme:   "postgres",
		User:     url.UserPassword(t.Username, t.Password),
		Host:     net.JoinHostPort(t.Host, strconv.Itoa(t.Port)),
		Path:     "/" + dbName,
		RawQuery: q.Encode(),
	}
	return u.String()
}

// Env returns libpq environment variables for child processes
// (pg_dump/pg_restore). Credentials go through the environment of the
// child only: never through argv, which is visible to every local user.
func (m *Materialized) Env(dbName string) []string {
	t := m.target
	if dbName == "" {
		dbName = t.Database
	}
	sslMode := t.SSLMode
	if sslMode == "" {
		sslMode = "prefer"
	}
	env := []string{
		"PGHOST=" + t.Host,
		"PGPORT=" + strconv.Itoa(t.Port),
		"PGUSER=" + t.Username,
		"PGPASSWORD=" + t.Password,
		"PGDATABASE=" + dbName,
		"PGSSLMODE=" + sslMode,
		"PGCONNECT_TIMEOUT=10",
		"PGAPPNAME=dbvault",
	}
	if m.certPath != "" {
		env = append(env, "PGSSLROOTCERT="+m.certPath)
	}
	return env
}

// Connect opens a single connection to dbName.
func (m *Materialized) Connect(ctx context.Context, dbName string) (*pgx.Conn, error) {
	cfg, err := pgx.ParseConfig(m.URL(dbName))
	if err != nil {
		return nil, errors.New("invalid connection settings")
	}
	conn, err := pgx.ConnectConfig(ctx, cfg)
	if err != nil {
		return nil, FriendlyError(err)
	}
	return conn, nil
}

// Inspect connects and gathers server information.
func (m *Materialized) Inspect(ctx context.Context) (engine.ServerInfo, error) {
	info, err := m.inspect(ctx)
	if err != nil {
		return info, engine.LocalhostHint(m.target.Host, err)
	}
	return info, nil
}

func (m *Materialized) inspect(ctx context.Context) (engine.ServerInfo, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	start := time.Now()
	conn, err := m.Connect(ctx, "")
	if err != nil {
		return engine.ServerInfo{}, err
	}
	defer conn.Close(context.WithoutCancel(ctx))

	var info engine.ServerInfo
	var superuser string
	err = conn.QueryRow(ctx, `SELECT current_setting('server_version'),
	                                 current_setting('server_version_num')::int,
	                                 version(),
	                                 pg_database_size(current_database()),
	                                 current_user,
	                                 current_setting('is_superuser'),
	                                 pg_is_in_recovery(),
	                                 (SELECT count(*) FROM pg_catalog.pg_tables
	                                   WHERE schemaname NOT IN ('pg_catalog', 'information_schema'))`).
		Scan(&info.Version, &info.VersionNum, &info.FullVersion, &info.SizeBytes, &info.CurrentUser, &superuser, &info.InRecovery, &info.TableCount)
	if err != nil {
		return engine.ServerInfo{}, FriendlyError(err)
	}
	info.Version = strings.Fields(info.Version)[0]
	info.Major = info.VersionNum / 10000
	info.IsSuperuser = superuser == "on"
	info.LatencyMilli = time.Since(start).Milliseconds()
	return info, nil
}

// FriendlyError converts driver errors into actionable messages that never
// include credentials.
func FriendlyError(err error) error {
	if err == nil {
		return nil
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "28P01", "28000":
			return errors.New("authentication failed: check the username and password")
		case "3D000":
			return errors.New("database does not exist")
		case "42501":
			return fmt.Errorf("permission denied: %s", pgErr.Message)
		case "53300":
			return errors.New("the server has too many connections")
		case "57P03":
			return errors.New("the database server is starting up or shutting down")
		}
		return fmt.Errorf("%s (SQLSTATE %s)", pgErr.Message, pgErr.Code)
	}
	var netErr net.Error
	msg := err.Error()
	switch {
	case errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &netErr) && netErr.Timeout()) || strings.Contains(msg, "timeout"):
		return errors.New("connection timeout: the server did not respond (check host, port and firewall rules)")
	case strings.Contains(msg, "connection refused"):
		return errors.New("connection refused: nothing is listening on that host and port")
	case strings.Contains(msg, "no such host"):
		return errors.New("host not found: check the hostname")
	case strings.Contains(msg, "server refused TLS") || strings.Contains(msg, "SSL is not enabled"):
		return errors.New("the server does not support SSL: use ssl mode 'disable' or 'prefer'")
	case strings.Contains(msg, "certificate"):
		return errors.New("TLS certificate verification failed: check the CA certificate and ssl mode")
	case strings.Contains(msg, "no pg_hba.conf entry"):
		return errors.New("the server rejected this client (no pg_hba.conf entry)")
	}
	return errors.New("could not connect to the database server")
}
