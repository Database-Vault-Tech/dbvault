package restore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dbvault/dbvault/backend/internal/apperr"
	"github.com/dbvault/dbvault/backend/internal/audit"
	"github.com/dbvault/dbvault/backend/internal/backups"
	"github.com/dbvault/dbvault/backend/internal/database"
	"github.com/dbvault/dbvault/backend/internal/db"
	"github.com/dbvault/dbvault/backend/internal/encryption"
	"github.com/dbvault/dbvault/backend/internal/jobs"
	"github.com/dbvault/dbvault/backend/internal/notifications"
	"github.com/dbvault/dbvault/backend/internal/pgtools"
	"github.com/dbvault/dbvault/backend/internal/storage"
	"github.com/dbvault/dbvault/backend/internal/validate"
)

// ConfirmationPhrase must be typed to restore over an existing database.
const ConfirmationPhrase = "RESTORE"

type Service struct {
	Pool         *pgxpool.Pool
	Queue        *jobs.Queue
	Backups      *backups.Service
	Databases    *database.Service
	Destinations *storage.DestinationService
	Keys         *encryption.KeyStore
	Notify       *notifications.Service
	Tools        pgtools.Tools
	WorkDir      string
	AppURL       string
	// Sandbox is nil when restore testing is disabled.
	Sandbox Sandbox
	// SandboxUnavailableReason explains why Sandbox is nil.
	SandboxUnavailableReason string
}

// Job is a restore job as exposed by the API.
type Job struct {
	ID                 string          `json:"id"`
	JobID              *string         `json:"job_id"`
	BackupID           string          `json:"backup_id"`
	BackupCreatedAt    time.Time       `json:"backup_created_at"`
	SourceDatabaseName string          `json:"source_database_name"`
	TargetDatabaseID   string          `json:"target_database_id"`
	TargetDatabaseName string          `json:"target_database_name"`
	Mode               string          `json:"mode"`
	NewDatabaseName    *string         `json:"new_database_name"`
	Status             string          `json:"status"`
	Error              *string         `json:"error"`
	Verification       json.RawMessage `json:"verification"`
	StartedAt          *time.Time      `json:"started_at"`
	CompletedAt        *time.Time      `json:"completed_at"`
	DurationMs         *int64          `json:"duration_ms"`
	RequestedBy        *string         `json:"requested_by"`
	RequestedByEmail   *string         `json:"requested_by_email"`
	CreatedAt          time.Time       `json:"created_at"`
	organizationID     string
}

const selectRestore = `SELECT r.id, r.organization_id, r.job_id, r.backup_id, b.created_at, sd.name, r.target_database_id, td.name, r.mode,
	r.new_database_name, r.status, r.error, r.verification, r.started_at, r.completed_at, r.duration_ms, r.requested_by, u.email, r.created_at
	FROM restore_jobs r
	JOIN backups b ON b.id = r.backup_id
	JOIN databases sd ON sd.id = b.database_id
	JOIN databases td ON td.id = r.target_database_id
	LEFT JOIN users u ON u.id = r.requested_by`

func scanRestore(row pgx.Row) (Job, error) {
	var j Job
	err := row.Scan(&j.ID, &j.organizationID, &j.JobID, &j.BackupID, &j.BackupCreatedAt, &j.SourceDatabaseName, &j.TargetDatabaseID, &j.TargetDatabaseName,
		&j.Mode, &j.NewDatabaseName, &j.Status, &j.Error, &j.Verification, &j.StartedAt, &j.CompletedAt, &j.DurationMs, &j.RequestedBy,
		&j.RequestedByEmail, &j.CreatedAt)
	return j, err
}

func (s *Service) List(ctx context.Context, orgID string, limit int) ([]Job, error) {
	rows, err := s.Pool.Query(ctx, selectRestore+` WHERE r.organization_id = $1 ORDER BY r.created_at DESC LIMIT $2`, orgID, limit)
	if err != nil {
		return nil, err
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Job, error) { return scanRestore(r) })
	if out == nil {
		out = []Job{}
	}
	return out, err
}

func (s *Service) Get(ctx context.Context, orgID, id string) (Job, error) {
	j, err := scanRestore(s.Pool.QueryRow(ctx, selectRestore+` WHERE r.id = $1 AND r.organization_id = $2`, id, orgID))
	if db.IsNotFound(err) {
		return j, apperr.NotFound("Restore")
	}
	return j, err
}

// CreateInput is a restore request.
type CreateInput struct {
	BackupID         string `json:"backup_id"`
	TargetDatabaseID string `json:"target_database_id"`
	Mode             string `json:"mode"`
	NewDatabaseName  string `json:"new_database_name"`
	Confirmation     string `json:"confirmation"`
}

// Create validates and queues a restore. Restoring over an existing
// database is destructive and requires the exact confirmation phrase.
func (s *Service) Create(ctx context.Context, orgID, userID string, in CreateInput) (Job, error) {
	v := validate.New()
	v.Check(isUUID(in.BackupID), "backup_id", "Select a backup.")
	v.Check(isUUID(in.TargetDatabaseID), "target_database_id", "Select a destination database.")
	v.OneOf("mode", in.Mode, "existing", "new")
	if in.Mode == "existing" {
		v.Check(in.Confirmation == ConfirmationPhrase, "confirmation", "Type RESTORE to confirm this destructive operation.")
	}
	if in.Mode == "new" {
		in.NewDatabaseName = strings.TrimSpace(in.NewDatabaseName)
		v.Check(validate.IsPGIdentifier(in.NewDatabaseName), "new_database_name", "Use letters, numbers, underscores or dashes (max 63), starting with a letter.")
	}
	if err := v.Err(); err != nil {
		return Job{}, err
	}
	b, err := s.Backups.Get(ctx, orgID, in.BackupID)
	if err != nil {
		return Job{}, err
	}
	if b.Status != "completed" {
		return Job{}, apperr.Conflict("Only completed backups can be restored.")
	}
	target, err := s.Databases.Get(ctx, orgID, in.TargetDatabaseID)
	if err != nil {
		return Job{}, err
	}
	if in.Mode == "new" && strings.EqualFold(in.NewDatabaseName, target.DatabaseName) {
		return Job{}, apperr.Validation(map[string]string{"new_database_name": "Choose a name different from the existing database."})
	}

	var restoreID, jobID string
	err = db.WithTx(ctx, s.Pool, func(tx pgx.Tx) error {
		// One restore per target at a time.
		if _, err := tx.Exec(ctx, `SELECT 1 FROM databases WHERE id = $1 FOR UPDATE`, target.ID); err != nil {
			return err
		}
		var busy bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM restore_jobs WHERE target_database_id = $1 AND status IN ('queued', 'running', 'verifying'))`, target.ID).Scan(&busy); err != nil {
			return err
		}
		if busy {
			return apperr.Conflict("A restore into this database is already in progress.")
		}
		var newName *string
		if in.Mode == "new" {
			newName = &in.NewDatabaseName
		}
		if err := tx.QueryRow(ctx, `INSERT INTO restore_jobs (organization_id, backup_id, target_database_id, mode, new_database_name, requested_by)
			VALUES ($1, $2, $3, $4, $5, $6) RETURNING id`, orgID, b.ID, target.ID, in.Mode, newName, userID).Scan(&restoreID); err != nil {
			return err
		}
		j, err := s.Queue.Create(ctx, tx, jobs.Spec{OrganizationID: orgID, Type: jobs.TypeRestore, Payload: map[string]string{"restore_id": restoreID, "backup_id": b.ID}, CreatedBy: userID})
		if err != nil {
			return err
		}
		jobID = j.ID
		if _, err := tx.Exec(ctx, `UPDATE restore_jobs SET job_id = $2 WHERE id = $1`, restoreID, j.ID); err != nil {
			return err
		}
		dest := target.DatabaseName
		if newName != nil {
			dest = *newName
		}
		return audit.Record(ctx, tx, audit.Entry{OrgID: orgID, Action: audit.RestoreStarted, ResourceType: "restore", ResourceID: restoreID,
			Metadata: map[string]any{"backup_id": b.ID, "source_database": b.DatabaseName, "target_database": target.Name, "target_db_name": dest, "mode": in.Mode}})
	})
	if err != nil {
		return Job{}, err
	}
	_ = s.Queue.Push(ctx, jobID)
	return s.Get(ctx, orgID, restoreID)
}

func isUUID(s string) bool {
	return len(s) == 36 && strings.Count(s, "-") == 4
}

// archiveSource opens the verified local copy of a backup as a plain
// pg_dump archive stream. Each call re-reads the file from the start.
type archiveSource struct {
	path        string
	compression string
	identity    string
}

func (a archiveSource) open() (io.ReadCloser, error) {
	f, err := os.Open(a.path)
	if err != nil {
		return nil, err
	}
	archive, err := backups.OpenArchive(f, a.compression, a.identity)
	if err != nil {
		f.Close()
		return nil, err
	}
	return &multiCloser{Reader: archive, closers: []io.Closer{archive, f}}, nil
}

type multiCloser struct {
	io.Reader
	closers []io.Closer
}

func (m *multiCloser) Close() error {
	var first error
	for _, c := range m.closers {
		if err := c.Close(); err != nil && first == nil {
			first = err
		}
	}
	return first
}

// fetch downloads a backup, verifies its checksum and prepares an archive
// source. The caller must remove the returned file.
func (s *Service) fetch(ctx context.Context, b backups.Backup, log *jobs.Logger) (archiveSource, error) {
	if b.StorageKey == nil || b.Checksum == nil {
		return archiveSource{}, errors.New("backup has no stored artifact")
	}
	dest, err := s.Destinations.GetIncludingDeleted(ctx, b.OrganizationID, b.StorageDestinationID)
	if err != nil {
		return archiveSource{}, err
	}
	st, err := s.Destinations.Open(ctx, dest)
	if err != nil {
		return archiveSource{}, fmt.Errorf("open storage: %w", err)
	}
	log.Infof("Downloading backup from %s", st.Describe())
	path, n, err := backups.DownloadVerified(ctx, st, *b.StorageKey, *b.Checksum, s.WorkDir)
	if err != nil {
		return archiveSource{}, err
	}
	log.Infof("Downloaded %s; checksum verified (sha256:%s)", backups.HumanBytes(n), (*b.Checksum)[:16])
	src := archiveSource{path: path, compression: b.Compression}
	if b.Encrypted {
		id, err := s.Keys.Identity(ctx, b.OrganizationID, *b.EncryptionKeyID)
		if err != nil {
			os.Remove(path)
			return archiveSource{}, fmt.Errorf("load decryption key: %w", err)
		}
		src.identity = id
	}
	return src, nil
}

// tables lists the ordinary tables in an archive.
func (s *Service) tables(ctx context.Context, src archiveSource) ([]pgtools.TOCEntry, error) {
	rc, err := src.open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	entries, err := s.Tools.ListArchive(ctx, rc)
	if err != nil {
		return nil, err
	}
	var out []pgtools.TOCEntry
	for _, e := range entries {
		if e.Type == "TABLE" {
			out = append(out, e)
		}
	}
	return out, nil
}

// runRestore pipes the archive into pg_restore.
func (s *Service) runRestore(ctx context.Context, src archiveSource, m *database.Materialized, dbName string, opts pgtools.RestoreOptions) error {
	rc, err := src.open()
	if err != nil {
		return err
	}
	defer rc.Close()
	if err := s.describeTarget(ctx, m, dbName, &opts); err != nil {
		return err
	}
	proc, err := s.Tools.StartRestore(ctx, m.Env(dbName), opts, rc)
	if err != nil {
		return err
	}
	if err := proc.Wait(); err != nil {
		return fmt.Errorf("pg_restore failed: %w", err)
	}
	return nil
}

func (s *Service) serverMajor(ctx context.Context, m *database.Materialized, dbName string) (int, error) {
	var o pgtools.RestoreOptions
	err := s.describeTarget(ctx, m, dbName, &o)
	return o.ServerMajor, err
}

// describeTarget records the target server's version and settings so
// pg_restore output can be adapted to older servers.
func (s *Service) describeTarget(ctx context.Context, m *database.Materialized, dbName string, opts *pgtools.RestoreOptions) error {
	conn, err := m.Connect(ctx, dbName)
	if err != nil {
		return err
	}
	defer conn.Close(context.WithoutCancel(ctx))
	var names []string
	if err := conn.QueryRow(ctx, `SELECT current_setting('server_version_num')::int / 10000, array_agg(name) FROM pg_catalog.pg_settings`).
		Scan(&opts.ServerMajor, &names); err != nil {
		return database.FriendlyError(err)
	}
	opts.ServerSettings = make(map[string]bool, len(names))
	for _, n := range names {
		opts.ServerSettings[n] = true
	}
	return nil
}

// TableCheck is the result of checking restored tables.
type TableCheck struct {
	Expected int      `json:"tables_expected"`
	Found    int      `json:"tables_found"`
	Rows     int64    `json:"rows"`
	Missing  []string `json:"missing,omitempty"`
}

// checkTables confirms every table from the archive exists (and, when
// countRows is set, is readable) in the restored database.
func checkTables(ctx context.Context, conn *pgx.Conn, tables []pgtools.TOCEntry, countRows bool) (TableCheck, error) {
	res := TableCheck{Expected: len(tables)}
	if _, err := conn.Exec(ctx, "SET statement_timeout = '5min'"); err != nil {
		return res, err
	}
	for _, t := range tables {
		var exists bool
		if err := conn.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pg_catalog.pg_tables WHERE schemaname = $1 AND tablename = $2)`, t.Schema, t.Name).Scan(&exists); err != nil {
			return res, err
		}
		if !exists {
			res.Missing = append(res.Missing, t.Schema+"."+t.Name)
			continue
		}
		res.Found++
		if countRows {
			var n int64
			if err := conn.QueryRow(ctx, "SELECT count(*) FROM "+pgx.Identifier{t.Schema, t.Name}.Sanitize()).Scan(&n); err != nil {
				return res, fmt.Errorf("query %s.%s: %w", t.Schema, t.Name, err)
			}
			res.Rows += n
		}
	}
	return res, nil
}

type restorePayload struct {
	RestoreID string `json:"restore_id"`
}

// ExecuteRestore runs a restore job (worker side).
func (s *Service) ExecuteRestore(ctx context.Context, job jobs.Job, log *jobs.Logger) (any, error) {
	var p restorePayload
	if err := json.Unmarshal(job.Payload, &p); err != nil {
		return nil, err
	}
	orgID := *job.OrganizationID
	r, err := s.Get(ctx, orgID, p.RestoreID)
	if err != nil {
		return nil, err
	}
	start := time.Now()
	_, _ = s.Pool.Exec(ctx, `UPDATE restore_jobs SET status = 'running', started_at = now(), error = NULL WHERE id = $1`, r.ID)

	result, runErr := s.restore(ctx, r, log)
	duration := time.Since(start)
	if runErr != nil {
		if ctx.Err() != nil {
			log.Warnf("Restore cancelled; the target database was left unchanged (single transaction rolled back)")
			return nil, ctx.Err()
		}
		msg := runErr.Error()
		log.Errorf("Restore failed: %s", msg)
		bg := context.WithoutCancel(ctx)
		_, _ = s.Pool.Exec(bg, `UPDATE restore_jobs SET status = 'failed', error = $2, completed_at = now(), duration_ms = $3 WHERE id = $1`, r.ID, msg, duration.Milliseconds())
		audit.MustRecord(bg, s.Pool, audit.Entry{OrgID: orgID, Action: audit.RestoreFailed, ResourceType: "restore", ResourceID: r.ID, Metadata: map[string]any{"error": msg}})
		s.Notify.Emit(bg, orgID, notifications.Event{Type: notifications.EventRestoreFailed, Severity: "error",
			Title: "Restore into " + r.TargetDatabaseName + " failed", Message: msg,
			Details:  []notifications.Detail{{Label: "Backup of", Value: r.SourceDatabaseName}, {Label: "Target", Value: r.TargetDatabaseName}},
			Resource: map[string]any{"restore_id": r.ID, "backup_id": r.BackupID}, URL: s.AppURL + "/restore"})
		return nil, runErr
	}
	verification, _ := json.Marshal(result)
	_, err = s.Pool.Exec(ctx, `UPDATE restore_jobs SET status = 'completed', verification = $2, completed_at = now(), duration_ms = $3 WHERE id = $1`,
		r.ID, verification, duration.Milliseconds())
	if err != nil {
		return nil, err
	}
	log.Infof("Restore completed in %s", duration.Round(time.Second))
	audit.MustRecord(ctx, s.Pool, audit.Entry{OrgID: orgID, Action: audit.RestoreCompleted, ResourceType: "restore", ResourceID: r.ID,
		Metadata: map[string]any{"backup_id": r.BackupID, "target": r.TargetDatabaseName, "duration_ms": duration.Milliseconds()}})
	s.Notify.Emit(ctx, orgID, notifications.Event{Type: notifications.EventRestoreCompleted, Severity: "success",
		Title:    "Restore into " + r.TargetDatabaseName + " completed",
		Message:  fmt.Sprintf("Backup of %s from %s was restored and %d of %d tables were verified.", r.SourceDatabaseName, r.BackupCreatedAt.Format(time.RFC1123), result.Found, result.Expected),
		Details:  []notifications.Detail{{Label: "Duration", Value: duration.Round(time.Second).String()}},
		Resource: map[string]any{"restore_id": r.ID, "backup_id": r.BackupID}, URL: s.AppURL + "/restore"})
	return result, nil
}

func (s *Service) restore(ctx context.Context, r Job, log *jobs.Logger) (TableCheck, error) {
	b, err := s.Backups.Get(ctx, r.organizationID, r.BackupID)
	if err != nil {
		return TableCheck{}, err
	}
	target, err := s.Databases.Target(ctx, r.organizationID, r.TargetDatabaseID)
	if err != nil {
		return TableCheck{}, err
	}
	if _, err := s.Tools.CheckCompatible(ctx, "pg_restore", 0); err != nil {
		return TableCheck{}, err
	}
	src, err := s.fetch(ctx, b, log)
	if err != nil {
		return TableCheck{}, err
	}
	defer os.Remove(src.path)

	tables, err := s.tables(ctx, src)
	if err != nil {
		return TableCheck{}, err
	}
	log.Infof("Backup contains %d tables", len(tables))

	m, err := target.Materialize(s.WorkDir)
	if err != nil {
		return TableCheck{}, err
	}
	defer m.Close()

	dbName := target.Database
	opts := pgtools.RestoreOptions{SingleTransaction: true}
	createdDB := false
	if r.Mode == "new" {
		dbName = *r.NewDatabaseName
		log.Infof("Creating database %q on %s:%d", dbName, target.Host, target.Port)
		conn, err := m.Connect(ctx, "")
		if err != nil {
			return TableCheck{}, err
		}
		_, err = conn.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{dbName}.Sanitize())
		conn.Close(context.WithoutCancel(ctx))
		if err != nil {
			return TableCheck{}, fmt.Errorf("create database %s: %w", dbName, database.FriendlyError(err))
		}
		createdDB = true
	} else {
		opts.Clean = true
		log.Warnf("Restoring over existing database %q: existing objects contained in the backup are dropped and recreated", dbName)
	}

	log.Infof("Running pg_restore into %q (single transaction: all-or-nothing)", dbName)
	if major, err := s.serverMajor(ctx, m, dbName); err == nil {
		if compat, client := s.Tools.NeedsCompat(ctx, major); compat {
			log.Infof("Target runs PostgreSQL %d; adapting pg_restore %d output for compatibility", major, client)
		}
	}
	if err := s.runRestore(ctx, src, m, dbName, opts); err != nil {
		if createdDB {
			s.dropCreated(m, dbName, log)
		}
		return TableCheck{}, err
	}
	log.Infof("pg_restore completed")

	_, _ = s.Pool.Exec(ctx, `UPDATE restore_jobs SET status = 'verifying' WHERE id = $1`, r.ID)
	log.Infof("Verifying restored database")
	conn, err := m.Connect(ctx, dbName)
	if err != nil {
		return TableCheck{}, err
	}
	defer conn.Close(context.WithoutCancel(ctx))
	check, err := checkTables(ctx, conn, tables, false)
	if err != nil {
		return check, err
	}
	if len(check.Missing) > 0 {
		return check, fmt.Errorf("verification failed: %d tables missing after restore (%s)", len(check.Missing), strings.Join(first(check.Missing, 5), ", "))
	}
	log.Infof("Verified %d of %d tables present", check.Found, check.Expected)
	return check, nil
}

func (s *Service) dropCreated(m *database.Materialized, name string, log *jobs.Logger) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	conn, err := m.Connect(ctx, "")
	if err != nil {
		log.Warnf("Could not remove partially created database %q: %v", name, err)
		return
	}
	defer conn.Close(ctx)
	if _, err := conn.Exec(ctx, "DROP DATABASE IF EXISTS "+pgx.Identifier{name}.Sanitize()); err != nil {
		log.Warnf("Could not remove partially created database %q: %v", name, err)
		return
	}
	log.Infof("Removed database %q created for this restore", name)
}

func first(s []string, n int) []string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

// CheckStatus values for verification reports.
const (
	CheckPass        = "pass"
	CheckFail        = "fail"
	CheckSkipped     = "skipped"
	CheckUnavailable = "unavailable"
)

type Check struct {
	Status  string `json:"status"`
	Message string `json:"message"`
}

// Report is stored on the backup after a verification run.
type Report struct {
	Integrity      Check     `json:"integrity"`
	Restore        Check     `json:"restore"`
	Database       Check     `json:"database"`
	TablesExpected int       `json:"tables_expected"`
	TablesRestored int       `json:"tables_restored"`
	Rows           int64     `json:"rows"`
	Sandbox        string    `json:"sandbox"`
	DurationMs     int64     `json:"duration_ms"`
	StartedAt      time.Time `json:"started_at"`
	CompletedAt    time.Time `json:"completed_at"`
}

// Overall returns passed, failed or unavailable. A backup is only
// "passed" when every check actually ran and passed.
func (r Report) Overall() string {
	if r.Integrity.Status == CheckFail || r.Restore.Status == CheckFail || r.Database.Status == CheckFail {
		return "failed"
	}
	if r.Integrity.Status == CheckPass && r.Restore.Status == CheckPass && r.Database.Status == CheckPass {
		return "passed"
	}
	return "unavailable"
}

// UnavailableMessage is shown when restore testing can't run.
const UnavailableMessage = "Restore testing unavailable in this environment"

// ExecuteVerification proves a backup is restorable (worker side):
// download → checksum → decrypt → decompress → restore into a sandbox →
// verification queries → destroy sandbox.
func (s *Service) ExecuteVerification(ctx context.Context, job jobs.Job, log *jobs.Logger) (any, error) {
	var p struct {
		BackupID string `json:"backup_id"`
	}
	if err := json.Unmarshal(job.Payload, &p); err != nil {
		return nil, err
	}
	orgID := *job.OrganizationID
	b, err := s.Backups.Get(ctx, orgID, p.BackupID)
	if err != nil {
		return nil, err
	}
	if b.Status != "completed" {
		return nil, fmt.Errorf("backup is %s, only completed backups can be verified", b.Status)
	}
	_, _ = s.Pool.Exec(ctx, `UPDATE backups SET verification_status = 'running' WHERE id = $1`, b.ID)

	rep := s.verify(ctx, b, log)
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	overall := rep.Overall()
	raw, _ := json.Marshal(rep)
	var verifiedAt *time.Time
	if overall == "passed" {
		verifiedAt = &rep.CompletedAt
	}
	if _, err := s.Pool.Exec(ctx, `UPDATE backups SET verification_status = $2, verification = $3, verified_at = COALESCE($4, verified_at) WHERE id = $1`,
		b.ID, overall, raw, verifiedAt); err != nil {
		return nil, err
	}
	log.Infof("Backup integrity: %s", strings.ToUpper(rep.Integrity.Status))
	log.Infof("Restore test: %s", strings.ToUpper(rep.Restore.Status))
	log.Infof("Database verification: %s", strings.ToUpper(rep.Database.Status))
	log.Infof("Recovery test duration: %s", (time.Duration(rep.DurationMs) * time.Millisecond).Round(time.Second))

	switch overall {
	case "passed":
		audit.MustRecord(ctx, s.Pool, audit.Entry{OrgID: orgID, Action: audit.BackupVerified, ResourceType: "backup", ResourceID: b.ID,
			Metadata: map[string]any{"tables": rep.TablesRestored, "rows": rep.Rows, "duration_ms": rep.DurationMs}})
		s.Notify.Emit(ctx, orgID, notifications.Event{Type: notifications.EventVerificationPassed, Severity: "success",
			Title:    "Backup of " + b.DatabaseName + " verified",
			Message:  fmt.Sprintf("The backup was restored in a sandbox: %d tables, %d rows.", rep.TablesRestored, rep.Rows),
			Resource: map[string]any{"backup_id": b.ID}, URL: s.AppURL + "/backups/" + b.ID})
	case "failed":
		reason := firstFailure(rep)
		audit.MustRecord(ctx, s.Pool, audit.Entry{OrgID: orgID, Action: audit.BackupVerifyFailed, ResourceType: "backup", ResourceID: b.ID, Metadata: map[string]any{"reason": reason}})
		s.Notify.Emit(ctx, orgID, notifications.Event{Type: notifications.EventVerificationFailed, Severity: "error",
			Title: "Verification of " + b.DatabaseName + " backup failed", Message: reason,
			Resource: map[string]any{"backup_id": b.ID}, URL: s.AppURL + "/backups/" + b.ID})
		return rep, errors.New("verification failed: " + reason)
	}
	return rep, nil
}

func firstFailure(r Report) string {
	for _, c := range []Check{r.Integrity, r.Restore, r.Database} {
		if c.Status == CheckFail {
			return c.Message
		}
	}
	return "unknown failure"
}

func (s *Service) verify(ctx context.Context, b backups.Backup, log *jobs.Logger) Report {
	rep := Report{StartedAt: time.Now().UTC(),
		Integrity: Check{Status: CheckSkipped}, Restore: Check{Status: CheckSkipped}, Database: Check{Status: CheckSkipped}}
	finish := func() Report {
		rep.CompletedAt = time.Now().UTC()
		rep.DurationMs = rep.CompletedAt.Sub(rep.StartedAt).Milliseconds()
		return rep
	}

	// 1-2. Download and verify checksum.
	src, err := s.fetch(ctx, b, log)
	if err != nil {
		rep.Integrity = Check{Status: CheckFail, Message: err.Error()}
		return finish()
	}
	defer os.Remove(src.path)
	rep.Integrity = Check{Status: CheckPass, Message: "SHA-256 matches the value recorded at backup time (" + (*b.Checksum)[:16] + "…)"}

	// 3-4. Decrypt + decompress while reading the archive's table of contents.
	tables, err := s.tables(ctx, src)
	if err != nil {
		rep.Integrity = Check{Status: CheckFail, Message: "Archive could not be decrypted or read: " + err.Error()}
		return finish()
	}
	rep.TablesExpected = len(tables)
	log.Infof("Archive decrypted and decompressed; %d tables in table of contents", len(tables))

	if s.Sandbox == nil {
		msg := UnavailableMessage
		if s.SandboxUnavailableReason != "" {
			msg += ": " + s.SandboxUnavailableReason
		}
		log.Warnf("%s", msg)
		rep.Restore = Check{Status: CheckUnavailable, Message: msg}
		rep.Database = Check{Status: CheckUnavailable, Message: "Requires a restore test"}
		return finish()
	}
	if err := s.Sandbox.Check(ctx); err != nil {
		msg := UnavailableMessage + ": " + err.Error()
		log.Warnf("%s", msg)
		rep.Restore = Check{Status: CheckUnavailable, Message: msg}
		rep.Database = Check{Status: CheckUnavailable, Message: "Requires a restore test"}
		return finish()
	}

	// 5. Provision a disposable PostgreSQL.
	major := 0
	if b.PGVersion != nil {
		major, _ = strconv.Atoi(strings.SplitN(*b.PGVersion, ".", 2)[0])
	}
	log.Infof("Starting restore sandbox (%s, PostgreSQL %d)", s.Sandbox.Name(), major)
	inst, err := s.Sandbox.Provision(ctx, major)
	if err != nil {
		rep.Restore = Check{Status: CheckFail, Message: "Could not start the restore sandbox: " + err.Error()}
		return finish()
	}
	rep.Sandbox = inst.Description
	// 9. Always destroy the sandbox.
	defer func() {
		if err := inst.Destroy(); err != nil {
			log.Warnf("Could not remove restore sandbox: %v", err)
		} else {
			log.Infof("Restore sandbox destroyed")
		}
	}()
	log.Infof("Sandbox ready: %s", inst.Description)

	m, err := inst.Target.Materialize(s.WorkDir)
	if err != nil {
		rep.Restore = Check{Status: CheckFail, Message: err.Error()}
		return finish()
	}
	defer m.Close()

	// 6. Restore.
	restoreStart := time.Now()
	log.Infof("Restoring backup into sandbox")
	if err := s.runRestore(ctx, src, m, inst.DBName, pgtools.RestoreOptions{SingleTransaction: true}); err != nil {
		rep.Restore = Check{Status: CheckFail, Message: err.Error()}
		return finish()
	}
	rep.Restore = Check{Status: CheckPass, Message: "pg_restore completed without errors in " + time.Since(restoreStart).Round(time.Second).String()}
	log.Infof("Restore into sandbox completed")

	// 7. Verification queries.
	conn, err := m.Connect(ctx, inst.DBName)
	if err != nil {
		rep.Database = Check{Status: CheckFail, Message: err.Error()}
		return finish()
	}
	defer conn.Close(context.WithoutCancel(ctx))
	check, err := checkTables(ctx, conn, tables, true)
	rep.TablesRestored, rep.Rows = check.Found, check.Rows
	switch {
	case err != nil:
		rep.Database = Check{Status: CheckFail, Message: err.Error()}
	case len(check.Missing) > 0:
		rep.Database = Check{Status: CheckFail, Message: fmt.Sprintf("%d of %d tables missing after restore: %s", len(check.Missing), check.Expected, strings.Join(first(check.Missing, 5), ", "))}
	default:
		rep.Database = Check{Status: CheckPass, Message: fmt.Sprintf("%d of %d tables restored and queryable, %d rows counted", check.Found, check.Expected, check.Rows)}
	}
	return finish()
}
