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
	"github.com/dbvault/dbvault/backend/internal/engine"
	"github.com/dbvault/dbvault/backend/internal/jobs"
	"github.com/dbvault/dbvault/backend/internal/notifications"
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
	Drivers      *engine.Registry
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
	Engine             string          `json:"engine"`
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

const selectRestore = `SELECT r.id, r.organization_id, r.job_id, r.backup_id, b.created_at, sd.name, r.target_database_id, td.name, td.engine, r.mode,
	r.new_database_name, r.status, r.error, r.verification, r.started_at, r.completed_at, r.duration_ms, r.requested_by, u.email, r.created_at
	FROM restore_jobs r
	JOIN backups b ON b.id = r.backup_id
	JOIN databases sd ON sd.id = b.database_id
	JOIN databases td ON td.id = r.target_database_id
	LEFT JOIN users u ON u.id = r.requested_by`

func scanRestore(row pgx.Row) (Job, error) {
	var j Job
	err := row.Scan(&j.ID, &j.organizationID, &j.JobID, &j.BackupID, &j.BackupCreatedAt, &j.SourceDatabaseName, &j.TargetDatabaseID, &j.TargetDatabaseName,
		&j.Engine, &j.Mode, &j.NewDatabaseName, &j.Status, &j.Error, &j.Verification, &j.StartedAt, &j.CompletedAt, &j.DurationMs, &j.RequestedBy,
		&j.RequestedByEmail, &j.CreatedAt)
	return j, err
}

// ListFilter narrows and pages the restore history (newest first).
type ListFilter struct {
	TargetDatabaseID string
	Status           string
	Mode             string
	Before           *time.Time
	Limit            int
}

func (s *Service) List(ctx context.Context, orgID string, f ListFilter) ([]Job, error) {
	if f.Limit <= 0 || f.Limit > 200 {
		f.Limit = 50
	}
	q := selectRestore + ` WHERE r.organization_id = $1`
	args := []any{orgID}
	add := func(clause string, v any) {
		args = append(args, v)
		q += fmt.Sprintf(clause, len(args))
	}
	if f.TargetDatabaseID != "" {
		add(` AND r.target_database_id = $%d`, f.TargetDatabaseID)
	}
	if f.Status != "" {
		add(` AND r.status = $%d`, f.Status)
	}
	if f.Mode != "" {
		add(` AND r.mode = $%d`, f.Mode)
	}
	if f.Before != nil {
		add(` AND r.created_at < $%d`, *f.Before)
	}
	args = append(args, f.Limit)
	q += fmt.Sprintf(` ORDER BY r.created_at DESC LIMIT $%d`, len(args))
	rows, err := s.Pool.Query(ctx, q, args...)
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
	in.NewDatabaseName = strings.TrimSpace(in.NewDatabaseName)
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
	if target.Engine != b.Engine {
		return Job{}, apperr.Validation(map[string]string{"target_database_id": fmt.Sprintf(
			"This backup was taken from a %s database and can only be restored into a %s database.", engineLabel(s.Drivers, b.Engine), engineLabel(s.Drivers, b.Engine))})
	}
	if in.Mode == "new" {
		// A new SQLite "database" is a file path in the SQLite folder;
		// everything else creates a database on the target's server.
		if fileBased(s.Drivers, target.Engine) {
			if !validate.IsRelativeFilePath(in.NewDatabaseName) {
				return Job{}, apperr.Validation(map[string]string{"new_database_name": "Enter a file path inside the SQLite folder, like app/restored.db."})
			}
		} else if !validate.IsPGIdentifier(in.NewDatabaseName) {
			return Job{}, apperr.Validation(map[string]string{"new_database_name": "Use letters, numbers, underscores or dashes (max 63), starting with a letter."})
		}
		if exists, err := s.databaseExists(ctx, orgID, target.ID, in.NewDatabaseName); err == nil && exists {
			return Job{}, apperr.Validation(map[string]string{"new_database_name": fmt.Sprintf(
				"A database named %q already exists on this server. Choose another name, or pick \"Restore into the existing database\" to overwrite it.", in.NewDatabaseName)})
		}
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

// databaseExists checks the target server for a database name. Connection
// problems are not reported here; the restore job surfaces them with context.
func (s *Service) databaseExists(ctx context.Context, orgID, targetID, name string) (bool, error) {
	t, err := s.Databases.Target(ctx, orgID, targetID)
	if err != nil {
		return false, err
	}
	drv, err := s.Drivers.For(t)
	if err != nil {
		return false, err
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return drv.DatabaseExists(ctx, t, name)
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

// tables lists the tables contained in an archive.
func tables(ctx context.Context, drv engine.Driver, src archiveSource) ([]engine.Table, error) {
	rc, err := src.open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return drv.Tables(ctx, rc)
}

// runRestore streams the archive into the engine's restore tool.
func runRestore(ctx context.Context, drv engine.Driver, src archiveSource, t engine.Target, dbName string, o engine.RestoreOptions, log engine.Logger) error {
	rc, err := src.open()
	if err != nil {
		return err
	}
	defer rc.Close()
	return drv.Restore(ctx, t, dbName, rc, o, log)
}

// TableCheck is the result of checking restored tables.
type TableCheck = engine.TableCheck

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
	drv, err := s.Drivers.For(target)
	if err != nil {
		return TableCheck{}, err
	}
	if b.Engine != drv.Name() {
		return TableCheck{}, fmt.Errorf("a %s backup can't be restored into a %s database", b.Engine, drv.Label())
	}
	if err := drv.CheckRestoreTool(ctx); err != nil {
		return TableCheck{}, err
	}
	src, err := s.fetch(ctx, b, log)
	if err != nil {
		return TableCheck{}, err
	}
	defer os.Remove(src.path)

	tbls, err := tables(ctx, drv, src)
	if err != nil {
		return TableCheck{}, err
	}
	log.Infof("Backup contains %d tables", len(tbls))

	dbName := target.Database
	opts := engine.RestoreOptions{Atomic: true}
	createdDB := false
	if r.Mode == "new" {
		dbName = *r.NewDatabaseName
		if drv.Capabilities().FileBased {
			log.Infof("Restoring into new file %q", dbName)
		} else {
			log.Infof("Creating database %q on %s:%d", dbName, target.Host, target.Port)
		}
		if err := drv.CreateDatabase(ctx, target, dbName); err != nil {
			return TableCheck{}, err
		}
		createdDB = true
	} else {
		opts.Overwrite = true
		log.Warnf("Restoring over existing database %q: existing objects contained in the backup are dropped and recreated", dbName)
	}

	switch {
	case drv.Capabilities().FileBased:
		log.Infof("Running %s restore into %q (written to a temporary file, then swapped in: all-or-nothing)", drv.Label(), dbName)
	case drv.Capabilities().AtomicRestore:
		log.Infof("Running %s restore into %q (single transaction: all-or-nothing)", drv.Label(), dbName)
	default:
		log.Infof("Running %s restore into %q", drv.Label(), dbName)
	}
	if err := runRestore(ctx, drv, src, target, dbName, opts, log); err != nil {
		if createdDB {
			dropCreated(drv, target, dbName, log)
		}
		return TableCheck{}, err
	}
	log.Infof("Restore tool completed")

	_, _ = s.Pool.Exec(ctx, `UPDATE restore_jobs SET status = 'verifying' WHERE id = $1`, r.ID)
	log.Infof("Verifying restored database")
	check, err := drv.CheckTables(ctx, target, dbName, tbls, false)
	if err != nil {
		return check, err
	}
	if len(check.Missing) > 0 {
		return check, fmt.Errorf("verification failed: %d tables missing after restore (%s)", len(check.Missing), strings.Join(first(check.Missing, 5), ", "))
	}
	log.Infof("Verified %d of %d tables present", check.Found, check.Expected)
	return check, nil
}

func dropCreated(drv engine.Driver, t engine.Target, name string, log *jobs.Logger) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := drv.DropDatabase(ctx, t, name); err != nil {
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
	drv, err := s.Drivers.Get(b.Engine)
	if err != nil {
		rep.Integrity = Check{Status: CheckFail, Message: err.Error()}
		return finish()
	}
	tbls, err := tables(ctx, drv, src)
	if err != nil {
		rep.Integrity = Check{Status: CheckFail, Message: "Archive could not be decrypted or read: " + err.Error()}
		return finish()
	}
	rep.TablesExpected = len(tbls)
	log.Infof("Archive decrypted and decompressed; %d tables found in the archive", len(tbls))

	// File-based engines restore into a temporary directory on the worker:
	// no container or verification server is needed.
	sandbox := s.Sandbox
	if drv.Capabilities().FileBased {
		sandbox = FileSandbox{WorkDir: s.WorkDir}
	}
	if sandbox == nil {
		msg := UnavailableMessage
		if s.SandboxUnavailableReason != "" {
			msg += ": " + s.SandboxUnavailableReason
		}
		log.Warnf("%s", msg)
		rep.Restore = Check{Status: CheckUnavailable, Message: msg}
		rep.Database = Check{Status: CheckUnavailable, Message: "Requires a restore test"}
		return finish()
	}
	if err := sandbox.Check(ctx, drv); err != nil {
		msg := UnavailableMessage + ": " + err.Error()
		log.Warnf("%s", msg)
		rep.Restore = Check{Status: CheckUnavailable, Message: msg}
		rep.Database = Check{Status: CheckUnavailable, Message: "Requires a restore test"}
		return finish()
	}

	// 5. Provision a disposable database of the backup's engine.
	major := 0
	if b.PGVersion != nil {
		major, _ = strconv.Atoi(strings.SplitN(*b.PGVersion, ".", 2)[0])
	}
	log.Infof("Starting restore sandbox (%s, %s %d)", sandbox.Name(), drv.Label(), major)
	inst, err := sandbox.Provision(ctx, drv, major)
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

	// 6. Restore.
	restoreStart := time.Now()
	log.Infof("Restoring backup into sandbox")
	if err := runRestore(ctx, drv, src, inst.Target, inst.DBName, engine.RestoreOptions{Atomic: true}, log); err != nil {
		rep.Restore = Check{Status: CheckFail, Message: err.Error()}
		return finish()
	}
	rep.Restore = Check{Status: CheckPass, Message: "Restore completed without errors in " + time.Since(restoreStart).Round(time.Second).String()}
	log.Infof("Restore into sandbox completed")

	// 7. Verification queries.
	check, err := drv.CheckTables(ctx, inst.Target, inst.DBName, tbls, true)
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

func fileBased(r *engine.Registry, name string) bool {
	d, err := r.Get(name)
	return err == nil && d.Capabilities().FileBased
}

func engineLabel(r *engine.Registry, name string) string {
	if r != nil {
		if d, err := r.Get(name); err == nil {
			return d.Label()
		}
	}
	return name
}
