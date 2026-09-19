package database

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dbvault/dbvault/backend/internal/apperr"
	"github.com/dbvault/dbvault/backend/internal/audit"
	"github.com/dbvault/dbvault/backend/internal/db"
	"github.com/dbvault/dbvault/backend/internal/encryption"
	"github.com/dbvault/dbvault/backend/internal/engine"
	"github.com/dbvault/dbvault/backend/internal/validate"
)

// Database is a protected database as exposed by the API. It never carries
// the password: once stored, credentials only leave the server sealed.
type Database struct {
	ID             string     `json:"id"`
	OrganizationID string     `json:"organization_id"`
	Name           string     `json:"name"`
	Engine         string     `json:"engine"`
	Host           string     `json:"host"`
	Port           int        `json:"port"`
	DatabaseName   string     `json:"database"`
	Username       string     `json:"username"`
	SSLMode        string     `json:"ssl_mode"`
	HasSSLRootCert bool       `json:"has_ssl_root_cert"`
	PGVersion      *string    `json:"pg_version"`
	SizeBytes      *int64     `json:"size_bytes"`
	LastTestedAt   *time.Time `json:"last_tested_at"`
	LastTestOK     *bool      `json:"last_test_ok"`
	LastTestError  *string    `json:"last_test_error"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`

	// Aggregates
	Protected       bool       `json:"protected"`
	ScheduleCount   int        `json:"schedule_count"`
	BackupCount     int        `json:"backup_count"`
	LastBackupAt    *time.Time `json:"last_backup_at"`
	LastBackupState *string    `json:"last_backup_status"`
	LastSuccessAt   *time.Time `json:"last_success_at"`
	NextRunAt       *time.Time `json:"next_run_at"`
	StorageBytes    int64      `json:"storage_bytes"`
}

// Health summarises a database's backup history.
type Health struct {
	Status         string     `json:"status"` // healthy | warning | critical | unprotected
	Reason         string     `json:"reason"`
	Completed30d   int        `json:"completed_30d"`
	Failed30d      int        `json:"failed_30d"`
	SuccessRate30d *float64   `json:"success_rate_30d"`
	LastFailureAt  *time.Time `json:"last_failure_at"`
	LastVerifiedAt *time.Time `json:"last_verified_at"`
}

type Service struct {
	Drivers *engine.Registry
	Pool    *pgxpool.Pool
	Sealer  *encryption.Sealer
	WorkDir string
}

func passwordAAD(id string) string { return "database:" + id + ":password" }

const selectDatabase = `SELECT d.id, d.organization_id, d.name, d.engine, d.host, d.port, d.database_name, d.username, d.ssl_mode,
	d.ssl_root_cert IS NOT NULL AND d.ssl_root_cert <> '', d.pg_version, d.size_bytes, d.last_tested_at, d.last_test_ok, d.last_test_error,
	d.created_at, d.updated_at,
	EXISTS (SELECT 1 FROM backup_schedules s WHERE s.database_id = d.id AND s.enabled),
	(SELECT count(*) FROM backup_schedules s WHERE s.database_id = d.id),
	(SELECT count(*) FROM backups b WHERE b.database_id = d.id AND b.status = 'completed'),
	lb.created_at, lb.status,
	(SELECT max(b.completed_at) FROM backups b WHERE b.database_id = d.id AND b.status = 'completed'),
	(SELECT min(s.next_run_at) FROM backup_schedules s WHERE s.database_id = d.id AND s.enabled),
	COALESCE((SELECT sum(b.size_bytes) FROM backups b WHERE b.database_id = d.id AND b.status = 'completed'), 0)::bigint
	FROM databases d
	LEFT JOIN LATERAL (SELECT created_at, status FROM backups b WHERE b.database_id = d.id AND b.status <> 'deleted'
	                   ORDER BY created_at DESC LIMIT 1) lb ON true`

func scanDatabase(row pgx.Row) (Database, error) {
	var d Database
	err := row.Scan(&d.ID, &d.OrganizationID, &d.Name, &d.Engine, &d.Host, &d.Port, &d.DatabaseName, &d.Username, &d.SSLMode, &d.HasSSLRootCert,
		&d.PGVersion, &d.SizeBytes, &d.LastTestedAt, &d.LastTestOK, &d.LastTestError, &d.CreatedAt, &d.UpdatedAt,
		&d.Protected, &d.ScheduleCount, &d.BackupCount, &d.LastBackupAt, &d.LastBackupState, &d.LastSuccessAt, &d.NextRunAt, &d.StorageBytes)
	return d, err
}

func (s *Service) List(ctx context.Context, orgID string) ([]Database, error) {
	rows, err := s.Pool.Query(ctx, selectDatabase+` WHERE d.organization_id = $1 AND d.deleted_at IS NULL ORDER BY lower(d.name)`, orgID)
	if err != nil {
		return nil, err
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Database, error) { return scanDatabase(r) })
	if out == nil {
		out = []Database{}
	}
	return out, err
}

func (s *Service) Get(ctx context.Context, orgID, id string) (Database, error) {
	d, err := scanDatabase(s.Pool.QueryRow(ctx, selectDatabase+` WHERE d.id = $1 AND d.organization_id = $2 AND d.deleted_at IS NULL`, id, orgID))
	if db.IsNotFound(err) {
		return d, apperr.NotFound("Database")
	}
	return d, err
}

// FindByName resolves a database by name (case-insensitive) or id; used by the CLI.
func (s *Service) FindByName(ctx context.Context, orgID, nameOrID string) (Database, error) {
	d, err := scanDatabase(s.Pool.QueryRow(ctx, selectDatabase+` WHERE d.organization_id = $1 AND d.deleted_at IS NULL
		AND (lower(d.name) = lower($2) OR d.id::text = $2)`, orgID, nameOrID))
	if db.IsNotFound(err) {
		return d, apperr.NotFound("Database")
	}
	return d, err
}

// Health computes the backup health of a database.
func (s *Service) Health(ctx context.Context, orgID string, d Database) (Health, error) {
	var h Health
	err := s.Pool.QueryRow(ctx, `SELECT
		count(*) FILTER (WHERE status = 'completed'),
		count(*) FILTER (WHERE status = 'failed'),
		max(completed_at) FILTER (WHERE status = 'failed'),
		(SELECT max(verified_at) FROM backups WHERE database_id = $1 AND verification_status = 'passed')
		FROM backups WHERE database_id = $1 AND organization_id = $2 AND created_at > now() - interval '30 days'`, d.ID, orgID).
		Scan(&h.Completed30d, &h.Failed30d, &h.LastFailureAt, &h.LastVerifiedAt)
	if err != nil {
		return h, err
	}
	if total := h.Completed30d + h.Failed30d; total > 0 {
		rate := float64(h.Completed30d) / float64(total)
		h.SuccessRate30d = &rate
	}
	switch {
	case !d.Protected:
		h.Status, h.Reason = "unprotected", "No enabled backup schedule."
	case d.LastBackupState != nil && *d.LastBackupState == "failed":
		h.Status, h.Reason = "critical", "The most recent backup failed."
	case d.LastSuccessAt == nil:
		h.Status, h.Reason = "warning", "No successful backup yet."
	case time.Since(*d.LastSuccessAt) > 48*time.Hour:
		h.Status, h.Reason = "warning", "Last successful backup is more than 48 hours old."
	case h.LastVerifiedAt == nil:
		h.Status, h.Reason = "healthy", "Backups are succeeding. Run a verification to prove they restore."
	default:
		h.Status, h.Reason = "healthy", "Backups are succeeding and verified."
	}
	return h, nil
}

// Input is the create/update payload.
type Input struct {
	Name         string  `json:"name"`
	Host         string  `json:"host"`
	Port         int     `json:"port"`
	DatabaseName string  `json:"database"`
	Username     string  `json:"username"`
	Password     *string `json:"password"`
	SSLMode      string  `json:"ssl_mode"`
	// Engine is the database type ("postgres"); empty means PostgreSQL.
	Engine      string  `json:"engine"`
	SSLRootCert *string `json:"ssl_root_cert"`
}

// Validate checks an input against the engine's rules; requirePassword is
// true on create.
func (in *Input) Validate(drivers *engine.Registry, requirePassword bool) error {
	in.Name = strings.TrimSpace(in.Name)
	in.Host = strings.TrimSpace(in.Host)
	in.DatabaseName = strings.TrimSpace(in.DatabaseName)
	in.Username = strings.TrimSpace(in.Username)
	if in.Engine == "" {
		in.Engine = engine.Postgres
	}
	drv, err := drivers.Get(in.Engine)
	if err != nil {
		return apperr.Validation(map[string]string{"engine": "Unsupported database type."})
	}
	if in.SSLMode == "" {
		in.SSLMode = drv.DefaultSSLMode()
	}
	if in.Port == 0 {
		in.Port = drv.DefaultPort()
	}
	v := validate.New()
	v.Required("name", in.Name)
	v.Check(v.Has("name") || validate.IsResourceName(in.Name), "name", "Use letters, numbers, spaces, dots, dashes or underscores (max 63).")
	v.Required("host", in.Host)
	v.Check(v.Has("host") || validate.IsHost(in.Host), "host", "Must be a hostname or IP address.")
	v.Range("port", in.Port, 1, 65535)
	v.Required("database", in.DatabaseName)
	v.MaxLen("database", in.DatabaseName, 63)
	// Database names are passed to libpq via PGDATABASE; reject values
	// libpq could interpret as a connection string.
	v.Check(!strings.ContainsAny(in.DatabaseName, "=\x00") && !strings.Contains(in.DatabaseName, "://"), "database", "Contains characters that are not allowed.")
	v.Required("username", in.Username)
	v.MaxLen("username", in.Username, 63)
	if requirePassword {
		v.Check(in.Password != nil && *in.Password != "", "password", "This field is required.")
	}
	if in.Password != nil {
		v.MaxLen("password", *in.Password, 1024)
	}
	v.OneOf("ssl_mode", in.SSLMode, drv.SSLModes()...)
	if in.SSLRootCert != nil && *in.SSLRootCert != "" {
		v.Check(strings.Contains(*in.SSLRootCert, "BEGIN CERTIFICATE"), "ssl_root_cert", "Must be a PEM-encoded certificate.")
		v.MaxLen("ssl_root_cert", *in.SSLRootCert, 64<<10)
	}
	return v.Err()
}

// Target builds a connection target from an input (for unsaved tests).
func (in Input) Target() Target {
	t := Target{Engine: in.Engine, Host: in.Host, Port: in.Port, Database: in.DatabaseName, Username: in.Username, SSLMode: in.SSLMode}
	if in.Password != nil {
		t.Password = *in.Password
	}
	if in.SSLRootCert != nil {
		t.SSLRootCert = *in.SSLRootCert
	}
	return t
}

// Create stores a database with its password sealed.
func (s *Service) Create(ctx context.Context, orgID, userID string, in Input) (Database, error) {
	id := uuid.NewString()
	sealed, err := s.Sealer.SealString(*in.Password, passwordAAD(id))
	if err != nil {
		return Database{}, err
	}
	err = db.WithTx(ctx, s.Pool, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO databases (id, organization_id, name, engine, host, port, database_name, username, password_encrypted, ssl_mode, ssl_root_cert, created_by)
			VALUES ($1, $2, $3, $12, $4, $5, $6, $7, $8, $9, NULLIF($10, ''), $11)`,
			id, orgID, in.Name, in.Host, in.Port, in.DatabaseName, in.Username, sealed, in.SSLMode, deref(in.SSLRootCert), userID, in.Engine)
		if db.IsUniqueViolation(err) {
			return apperr.Validation(map[string]string{"name": "A database with this name already exists."})
		}
		if err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Entry{OrgID: orgID, Action: audit.DatabaseCreated, ResourceType: "database", ResourceID: id,
			Metadata: map[string]any{"name": in.Name, "host": in.Host, "database": in.DatabaseName}})
	})
	if err != nil {
		return Database{}, err
	}
	return s.Get(ctx, orgID, id)
}

// Update modifies a database. A nil/empty password keeps the stored one.
func (s *Service) Update(ctx context.Context, orgID, id string, in Input) (Database, error) {
	err := db.WithTx(ctx, s.Pool, func(tx pgx.Tx) error {
		var sealed *string
		if in.Password != nil && *in.Password != "" {
			v, err := s.Sealer.SealString(*in.Password, passwordAAD(id))
			if err != nil {
				return err
			}
			sealed = &v
		}
		tag, err := tx.Exec(ctx, `UPDATE databases SET name = $3, host = $4, port = $5, database_name = $6, username = $7,
			password_encrypted = COALESCE($8, password_encrypted), ssl_mode = $9,
			ssl_root_cert = CASE WHEN $10::text IS NULL THEN ssl_root_cert ELSE NULLIF($10, '') END,
			last_tested_at = NULL, last_test_ok = NULL, last_test_error = NULL
			WHERE id = $1 AND organization_id = $2 AND deleted_at IS NULL`,
			id, orgID, in.Name, in.Host, in.Port, in.DatabaseName, in.Username, sealed, in.SSLMode, in.SSLRootCert)
		if db.IsUniqueViolation(err) {
			return apperr.Validation(map[string]string{"name": "A database with this name already exists."})
		}
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return apperr.NotFound("Database")
		}
		return audit.Record(ctx, tx, audit.Entry{OrgID: orgID, Action: audit.DatabaseUpdated, ResourceType: "database", ResourceID: id,
			Metadata: map[string]any{"name": in.Name, "password_changed": sealed != nil}})
	})
	if err != nil {
		return Database{}, err
	}
	return s.Get(ctx, orgID, id)
}

// Delete soft-deletes a database and removes its schedules. Existing
// backups are kept: they remain listable, downloadable and restorable.
func (s *Service) Delete(ctx context.Context, orgID, id string) error {
	return db.WithTx(ctx, s.Pool, func(tx pgx.Tx) error {
		var name string
		err := tx.QueryRow(ctx, `UPDATE databases SET deleted_at = now(), name = name || ' (deleted ' || to_char(now(), 'YYYY-MM-DD HH24:MI') || ')'
			WHERE id = $1 AND organization_id = $2 AND deleted_at IS NULL RETURNING name`, id, orgID).Scan(&name)
		if db.IsNotFound(err) {
			return apperr.NotFound("Database")
		}
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM backup_schedules WHERE database_id = $1`, id); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Entry{OrgID: orgID, Action: audit.DatabaseDeleted, ResourceType: "database", ResourceID: id, Metadata: map[string]any{"name": name}})
	})
}

// Target returns the decrypted connection target (worker/test use only;
// never serialise it).
func (s *Service) Target(ctx context.Context, orgID, id string) (Target, error) {
	var t Target
	var sealed string
	var cert *string
	err := s.Pool.QueryRow(ctx, `SELECT engine, host, port, database_name, username, password_encrypted, ssl_mode, ssl_root_cert
		FROM databases WHERE id = $1 AND organization_id = $2`, id, orgID).
		Scan(&t.Engine, &t.Host, &t.Port, &t.Database, &t.Username, &sealed, &t.SSLMode, &cert)
	if db.IsNotFound(err) {
		return t, apperr.NotFound("Database")
	}
	if err != nil {
		return t, err
	}
	t.SSLRootCert = deref(cert)
	t.Password, err = s.Sealer.OpenString(sealed, passwordAAD(id))
	return t, err
}

// TestResult is returned by connection tests.
type TestResult struct {
	OK       bool        `json:"ok"`
	Message  string      `json:"message"`
	Server   *ServerInfo `json:"server,omitempty"`
	TestedAt time.Time   `json:"tested_at"`
}

// TestTarget connects to t and reports the result (never an error for
// connection failures: those are part of the result).
func (s *Service) TestTarget(ctx context.Context, t Target) (TestResult, error) {
	drv, err := s.Drivers.For(t)
	if err != nil {
		return TestResult{}, err
	}
	res := TestResult{TestedAt: time.Now()}
	info, err := drv.Inspect(ctx, t)
	if err != nil {
		res.Message = err.Error()
		return res, nil
	}
	res.OK = true
	res.Message = "Connection successful"
	res.Server = &info
	return res, nil
}

// TestSaved tests a stored database and records the outcome.
func (s *Service) TestSaved(ctx context.Context, orgID, id string) (TestResult, error) {
	t, err := s.Target(ctx, orgID, id)
	if err != nil {
		return TestResult{}, err
	}
	res, err := s.TestTarget(ctx, t)
	if err != nil {
		return res, err
	}
	s.RecordTest(ctx, id, res)
	audit.MustRecord(ctx, s.Pool, audit.Entry{OrgID: orgID, Action: audit.DatabaseTested, ResourceType: "database", ResourceID: id,
		Metadata: map[string]any{"ok": res.OK, "message": res.Message}})
	return res, nil
}

// RecordTest persists a connection test outcome and server facts.
func (s *Service) RecordTest(ctx context.Context, id string, res TestResult) {
	var version *string
	var size *int64
	if res.Server != nil {
		version, size = &res.Server.Version, &res.Server.SizeBytes
	}
	var errMsg *string
	if !res.OK {
		errMsg = &res.Message
	}
	_, _ = s.Pool.Exec(ctx, `UPDATE databases SET last_tested_at = $2, last_test_ok = $3, last_test_error = $4,
		pg_version = COALESCE($5, pg_version), size_bytes = COALESCE($6, size_bytes) WHERE id = $1`,
		id, res.TestedAt, res.OK, errMsg, version, size)
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
