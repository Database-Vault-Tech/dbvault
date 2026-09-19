// Package backups manages backup records, runs backup and cleanup jobs,
// and streams downloads.
package backups

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dbvault/dbvault/backend/internal/apperr"
	"github.com/dbvault/dbvault/backend/internal/audit"
	"github.com/dbvault/dbvault/backend/internal/compress"
	"github.com/dbvault/dbvault/backend/internal/database"
	"github.com/dbvault/dbvault/backend/internal/db"
	"github.com/dbvault/dbvault/backend/internal/encryption"
	"github.com/dbvault/dbvault/backend/internal/jobs"
	"github.com/dbvault/dbvault/backend/internal/notifications"
	"github.com/dbvault/dbvault/backend/internal/storage"
)

// Backup is a backup record as exposed by the API.
type Backup struct {
	ID                   string          `json:"id"`
	OrganizationID       string          `json:"organization_id"`
	DatabaseID           string          `json:"database_id"`
	DatabaseName         string          `json:"database_name"`
	Engine               string          `json:"engine"`
	ScheduleID           *string         `json:"schedule_id"`
	ScheduleName         *string         `json:"schedule_name"`
	StorageDestinationID string          `json:"storage_destination_id"`
	StorageName          string          `json:"storage_name"`
	StorageType          string          `json:"storage_type"`
	JobID                *string         `json:"job_id"`
	Trigger              string          `json:"trigger"`
	Status               string          `json:"status"`
	StorageKey           *string         `json:"storage_key"`
	Format               string          `json:"format"`
	Compression          string          `json:"compression"`
	Encrypted            bool            `json:"encrypted"`
	EncryptionKeyID      *string         `json:"encryption_key_id"`
	SizeBytes            *int64          `json:"size_bytes"`
	RawSizeBytes         *int64          `json:"raw_size_bytes"`
	Checksum             *string         `json:"checksum_sha256"`
	PGVersion            *string         `json:"pg_version"`
	PGDumpVersion        *string         `json:"pg_dump_version"`
	TableCount           *int            `json:"table_count"`
	Error                *string         `json:"error"`
	StartedAt            *time.Time      `json:"started_at"`
	CompletedAt          *time.Time      `json:"completed_at"`
	DurationMs           *int64          `json:"duration_ms"`
	VerificationStatus   string          `json:"verification_status"`
	Verification         json.RawMessage `json:"verification"`
	VerifiedAt           *time.Time      `json:"verified_at"`
	DeletedAt            *time.Time      `json:"deleted_at"`
	DeletedReason        *string         `json:"deleted_reason"`
	CreatedBy            *string         `json:"created_by"`
	CreatedAt            time.Time       `json:"created_at"`
}

const selectBackup = `SELECT b.id, b.organization_id, b.database_id, d.name, d.engine, b.schedule_id, s.name, b.storage_destination_id, sd.name, sd.type,
	b.job_id, b.trigger, b.status, b.storage_key, b.format, b.compression, b.encrypted, b.encryption_key_id, b.size_bytes, b.raw_size_bytes,
	b.checksum_sha256, b.pg_version, b.pg_dump_version, b.table_count, b.error, b.started_at, b.completed_at, b.duration_ms,
	b.verification_status, b.verification, b.verified_at, b.deleted_at, b.deleted_reason, b.created_by, b.created_at
	FROM backups b
	JOIN databases d ON d.id = b.database_id
	JOIN storage_destinations sd ON sd.id = b.storage_destination_id
	LEFT JOIN backup_schedules s ON s.id = b.schedule_id`

func scanBackup(row pgx.Row) (Backup, error) {
	var b Backup
	err := row.Scan(&b.ID, &b.OrganizationID, &b.DatabaseID, &b.DatabaseName, &b.Engine, &b.ScheduleID, &b.ScheduleName, &b.StorageDestinationID,
		&b.StorageName, &b.StorageType, &b.JobID, &b.Trigger, &b.Status, &b.StorageKey, &b.Format, &b.Compression, &b.Encrypted,
		&b.EncryptionKeyID, &b.SizeBytes, &b.RawSizeBytes, &b.Checksum, &b.PGVersion, &b.PGDumpVersion, &b.TableCount, &b.Error,
		&b.StartedAt, &b.CompletedAt, &b.DurationMs, &b.VerificationStatus, &b.Verification, &b.VerifiedAt, &b.DeletedAt,
		&b.DeletedReason, &b.CreatedBy, &b.CreatedAt)
	return b, err
}

type Service struct {
	Pool         *pgxpool.Pool
	Queue        *jobs.Queue
	Databases    *database.Service
	Destinations *storage.DestinationService
	Keys         *encryption.KeyStore
	Notify       *notifications.Service
	Engine       *Engine
	AppURL       string
	WorkDir      string
}

// ListFilter narrows List.
type ListFilter struct {
	DatabaseID string
	ScheduleID string
	Status     string
	Trigger    string
	Before     *time.Time
	Limit      int
}

func (s *Service) List(ctx context.Context, orgID string, f ListFilter) ([]Backup, error) {
	if f.Limit <= 0 || f.Limit > 200 {
		f.Limit = 50
	}
	rows, err := s.Pool.Query(ctx, selectBackup+` WHERE b.organization_id = $1
		AND ($2 = '' OR b.database_id::text = $2)
		AND ($3 = '' OR b.schedule_id::text = $3)
		AND (($4 = '' AND b.status <> 'deleted') OR b.status = $4)
		AND ($5 = '' OR b.trigger = $5)
		AND ($6::timestamptz IS NULL OR b.created_at < $6)
		ORDER BY b.created_at DESC LIMIT $7`, orgID, f.DatabaseID, f.ScheduleID, f.Status, f.Trigger, f.Before, f.Limit)
	if err != nil {
		return nil, err
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Backup, error) { return scanBackup(r) })
	if out == nil {
		out = []Backup{}
	}
	return out, err
}

func (s *Service) Get(ctx context.Context, orgID, id string) (Backup, error) {
	b, err := scanBackup(s.Pool.QueryRow(ctx, selectBackup+` WHERE b.id = $1 AND b.organization_id = $2`, id, orgID))
	if db.IsNotFound(err) {
		return b, apperr.NotFound("Backup")
	}
	return b, err
}

// CreateInput is a manual backup request.
type CreateInput struct {
	DatabaseID           string `json:"database_id"`
	StorageDestinationID string `json:"storage_destination_id"`
	Compression          string `json:"compression"`
	Encrypted            *bool  `json:"encrypted"`
}

// Queued is returned when a backup is accepted.
type Queued struct {
	BackupID string `json:"backup_id"`
	JobID    string `json:"job_id"`
	Status   string `json:"status"`
}

// ErrAlreadyRunning is returned when the database already has a backup in progress.
func alreadyRunning(backupID string) error {
	e := apperr.Conflict("A backup of this database is already queued or running.")
	e.Fields = map[string]string{"backup_id": backupID}
	return e
}

// CreateManual queues an on-demand backup. It returns immediately with the
// job id; the worker performs the backup.
func (s *Service) CreateManual(ctx context.Context, orgID, userID string, in CreateInput) (Queued, error) {
	dbRec, err := s.Databases.Get(ctx, orgID, in.DatabaseID)
	if err != nil {
		return Queued{}, err
	}
	if in.Compression == "" {
		in.Compression = compress.Zstd
	}
	if !compress.Valid(in.Compression) {
		return Queued{}, apperr.Validation(map[string]string{"compression": "Must be one of: zstd, gzip, none."})
	}
	encrypted := in.Encrypted == nil || *in.Encrypted

	destID := in.StorageDestinationID
	if destID == "" {
		// Prefer the destination used by the database's schedule, then the org default.
		_ = s.Pool.QueryRow(ctx, `SELECT storage_destination_id FROM backup_schedules WHERE database_id = $1 ORDER BY enabled DESC, created_at LIMIT 1`, dbRec.ID).Scan(&destID)
	}
	if destID == "" {
		d, err := s.Destinations.Default(ctx, orgID)
		if err != nil {
			return Queued{}, err
		}
		destID = d.ID
	}
	if _, err := s.Destinations.Get(ctx, orgID, destID); err != nil {
		return Queued{}, err
	}

	var q Queued
	err = db.WithTx(ctx, s.Pool, func(tx pgx.Tx) error {
		var err error
		q, err = s.createInTx(ctx, tx, createArgs{orgID: orgID, databaseID: dbRec.ID, destinationID: destID, compression: in.Compression,
			encrypted: encrypted, trigger: "manual", userID: userID})
		return err
	})
	if err != nil {
		return q, err
	}
	_ = s.Queue.Push(ctx, q.JobID)
	return q, nil
}

type createArgs struct {
	orgID, databaseID, destinationID, scheduleID string
	compression                                  string
	encrypted                                    bool
	trigger                                      string
	userID                                       string
}

// createInTx inserts the backup record and its job. The per-database row
// lock serialises concurrent requests so two backups of the same database
// are never queued at once.
func (s *Service) createInTx(ctx context.Context, tx pgx.Tx, a createArgs) (Queued, error) {
	if _, err := tx.Exec(ctx, `SELECT 1 FROM databases WHERE id = $1 FOR UPDATE`, a.databaseID); err != nil {
		return Queued{}, err
	}
	var active string
	err := tx.QueryRow(ctx, `SELECT id FROM backups WHERE database_id = $1 AND status IN ('queued', 'running') LIMIT 1`, a.databaseID).Scan(&active)
	if err == nil {
		return Queued{}, alreadyRunning(active)
	}
	if !db.IsNotFound(err) {
		return Queued{}, err
	}
	var keyID *string
	if a.encrypted {
		k, err := s.Keys.Active(ctx, a.orgID)
		if err != nil {
			return Queued{}, err
		}
		keyID = &k.ID
	}
	var backupID string
	err = tx.QueryRow(ctx, `INSERT INTO backups (organization_id, database_id, schedule_id, storage_destination_id, trigger, compression, encrypted, encryption_key_id, created_by)
		VALUES ($1, $2, NULLIF($3, '')::uuid, $4, $5, $6, $7, $8, NULLIF($9, '')::uuid) RETURNING id`,
		a.orgID, a.databaseID, a.scheduleID, a.destinationID, a.trigger, a.compression, a.encrypted, keyID, a.userID).Scan(&backupID)
	if err != nil {
		return Queued{}, err
	}
	j, err := s.Queue.Create(ctx, tx, jobs.Spec{OrganizationID: a.orgID, Type: jobs.TypeBackup, Payload: map[string]string{"backup_id": backupID}, CreatedBy: a.userID})
	if err != nil {
		return Queued{}, err
	}
	if _, err := tx.Exec(ctx, `UPDATE backups SET job_id = $2 WHERE id = $1`, backupID, j.ID); err != nil {
		return Queued{}, err
	}
	if err := audit.Record(ctx, tx, audit.Entry{OrgID: a.orgID, Action: audit.BackupStarted, ResourceType: "backup", ResourceID: backupID,
		Metadata: map[string]any{"database_id": a.databaseID, "trigger": a.trigger, "job_id": j.ID}}); err != nil {
		return Queued{}, err
	}
	return Queued{BackupID: backupID, JobID: j.ID, Status: jobs.StatusQueued}, nil
}

// ScheduledArgs describes a due schedule.
type ScheduledArgs struct {
	OrgID, DatabaseID, DestinationID, ScheduleID, Compression string
	Encrypted                                                 bool
}

// CreateScheduled queues a backup for a due schedule inside the
// scheduler's transaction. It returns ok=false if a backup of the database
// is still in progress (the run is skipped rather than stacked).
func (s *Service) CreateScheduled(ctx context.Context, tx pgx.Tx, a ScheduledArgs) (Queued, bool, error) {
	q, err := s.createInTx(ctx, tx, createArgs{orgID: a.OrgID, databaseID: a.DatabaseID, destinationID: a.DestinationID, scheduleID: a.ScheduleID,
		compression: a.Compression, encrypted: a.Encrypted, trigger: "scheduled"})
	if e, ok := apperr.As(err); ok && e.Code == "conflict" {
		return Queued{}, false, nil
	}
	return q, err == nil, err
}

// activeVerification returns the id of a queued/running verification job for a backup.
func (s *Service) activeJobFor(ctx context.Context, backupID string) (string, string, bool) {
	var id, typ string
	err := s.Pool.QueryRow(ctx, `SELECT id, type FROM jobs WHERE status IN ('queued', 'running') AND type IN ('verification', 'restore')
		AND (payload->>'backup_id' = $1 OR id IN (SELECT job_id FROM restore_jobs WHERE backup_id = $1::uuid))
		LIMIT 1`, backupID).Scan(&id, &typ)
	return id, typ, err == nil
}

// RequestVerification queues a restore test for a completed backup.
func (s *Service) RequestVerification(ctx context.Context, orgID, userID, id string) (Queued, error) {
	b, err := s.Get(ctx, orgID, id)
	if err != nil {
		return Queued{}, err
	}
	if b.Status != "completed" {
		return Queued{}, apperr.Conflict("Only completed backups can be verified.")
	}
	if jobID, typ, busy := s.activeJobFor(ctx, id); busy && typ == jobs.TypeVerification {
		return Queued{BackupID: id, JobID: jobID, Status: jobs.StatusQueued}, nil
	}
	j, err := s.Queue.Enqueue(ctx, jobs.Spec{OrganizationID: orgID, Type: jobs.TypeVerification, Payload: map[string]string{"backup_id": id}, CreatedBy: userID})
	if err != nil {
		return Queued{}, err
	}
	audit.MustRecord(ctx, s.Pool, audit.Entry{OrgID: orgID, Action: audit.BackupVerifyRequested, ResourceType: "backup", ResourceID: id, Metadata: map[string]any{"job_id": j.ID}})
	return Queued{BackupID: id, JobID: j.ID, Status: j.Status}, nil
}

// Delete removes a backup's artifact from storage, then marks it deleted.
// The record is kept for history.
func (s *Service) Delete(ctx context.Context, orgID, id, reason string) error {
	b, err := s.Get(ctx, orgID, id)
	if err != nil {
		return err
	}
	switch b.Status {
	case "queued", "running":
		return apperr.Conflict("This backup is still in progress. Cancel its job first.")
	case "deleted":
		return nil
	}
	if _, typ, busy := s.activeJobFor(ctx, id); busy {
		return apperr.Conflict("A " + typ + " job is using this backup. Wait for it to finish.")
	}
	if b.StorageKey != nil && b.Status == "completed" {
		dest, err := s.Destinations.GetIncludingDeleted(ctx, orgID, b.StorageDestinationID)
		if err != nil {
			return err
		}
		st, err := s.Destinations.Open(ctx, dest)
		if err != nil {
			return apperr.Unprocessable("storage_error", "Could not open the storage destination: "+err.Error())
		}
		if err := st.Delete(ctx, *b.StorageKey); err != nil {
			return apperr.Unprocessable("storage_error", "Could not delete the backup from storage: "+err.Error())
		}
	}
	_, err = s.Pool.Exec(ctx, `UPDATE backups SET status = 'deleted', deleted_at = now(), deleted_reason = $2 WHERE id = $1`, id, reason)
	if err != nil {
		return err
	}
	action := audit.BackupDeleted
	if reason != "manual" {
		action = audit.BackupExpired
	}
	audit.MustRecord(ctx, s.Pool, audit.Entry{OrgID: orgID, Action: action, ResourceType: "backup", ResourceID: id,
		Metadata: map[string]any{"database": b.DatabaseName, "reason": reason, "key": deref(b.StorageKey)}})
	return nil
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// Stats powers the dashboard overview.
type Stats struct {
	Databases            int        `json:"databases"`
	ProtectedDatabases   int        `json:"protected_databases"`
	BackupsToday         int        `json:"backups_today"`
	StorageUsedBytes     int64      `json:"storage_used_bytes"`
	FailedBackups7d      int        `json:"failed_backups_7d"`
	LastSuccessfulBackup *time.Time `json:"last_successful_backup"`
	VerifiedBackups30d   int        `json:"verified_backups_30d"`
	RunningJobs          int        `json:"running_jobs"`
	Daily                []DayStat  `json:"daily"`
}

type DayStat struct {
	Date      string `json:"date"`
	Completed int    `json:"completed"`
	Failed    int    `json:"failed"`
	Bytes     int64  `json:"bytes"`
}

func (s *Service) Stats(ctx context.Context, orgID string) (Stats, error) {
	var st Stats
	err := s.Pool.QueryRow(ctx, `SELECT
		(SELECT count(*) FROM databases WHERE organization_id = $1 AND deleted_at IS NULL),
		(SELECT count(DISTINCT s.database_id) FROM backup_schedules s JOIN databases d ON d.id = s.database_id
		  WHERE s.organization_id = $1 AND s.enabled AND d.deleted_at IS NULL),
		(SELECT count(*) FROM backups WHERE organization_id = $1 AND status = 'completed' AND completed_at >= date_trunc('day', now())),
		COALESCE((SELECT sum(size_bytes) FROM backups WHERE organization_id = $1 AND status = 'completed'), 0)::bigint,
		(SELECT count(*) FROM backups WHERE organization_id = $1 AND status = 'failed' AND created_at > now() - interval '7 days'),
		(SELECT max(completed_at) FROM backups WHERE organization_id = $1 AND status = 'completed'),
		(SELECT count(*) FROM backups WHERE organization_id = $1 AND verification_status = 'passed' AND verified_at > now() - interval '30 days'),
		(SELECT count(*) FROM jobs WHERE organization_id = $1 AND status IN ('queued', 'running') AND type <> 'notification')`, orgID).
		Scan(&st.Databases, &st.ProtectedDatabases, &st.BackupsToday, &st.StorageUsedBytes, &st.FailedBackups7d,
			&st.LastSuccessfulBackup, &st.VerifiedBackups30d, &st.RunningJobs)
	if err != nil {
		return st, err
	}
	rows, err := s.Pool.Query(ctx, `SELECT to_char(day, 'YYYY-MM-DD'),
		count(b.id) FILTER (WHERE b.status = 'completed'),
		count(b.id) FILTER (WHERE b.status = 'failed'),
		COALESCE(sum(b.size_bytes) FILTER (WHERE b.status = 'completed'), 0)::bigint
		FROM generate_series(date_trunc('day', now()) - interval '13 days', date_trunc('day', now()), interval '1 day') day
		LEFT JOIN backups b ON b.organization_id = $1 AND b.created_at >= day AND b.created_at < day + interval '1 day'
		GROUP BY day ORDER BY day`, orgID)
	if err != nil {
		return st, err
	}
	st.Daily, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (DayStat, error) {
		var d DayStat
		err := r.Scan(&d.Date, &d.Completed, &d.Failed, &d.Bytes)
		return d, err
	})
	return st, err
}
