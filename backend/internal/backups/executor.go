package backups

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/dbvault/dbvault/backend/internal/audit"
	"github.com/dbvault/dbvault/backend/internal/jobs"
	"github.com/dbvault/dbvault/backend/internal/notifications"
	"github.com/dbvault/dbvault/backend/internal/retention"
)

type backupPayload struct {
	BackupID string `json:"backup_id"`
}

// ExecuteBackup runs a backup job (worker side).
func (s *Service) ExecuteBackup(ctx context.Context, job jobs.Job, log *jobs.Logger) (any, error) {
	var p backupPayload
	if err := json.Unmarshal(job.Payload, &p); err != nil {
		return nil, err
	}
	orgID := *job.OrganizationID
	b, err := s.Get(ctx, orgID, p.BackupID)
	if err != nil {
		return nil, err
	}
	if b.Status != "queued" && b.Status != "running" {
		return map[string]string{"skipped": "backup is " + b.Status}, nil
	}
	if _, err := s.Pool.Exec(ctx, `UPDATE backups SET status = 'running', started_at = now(), error = NULL WHERE id = $1`, b.ID); err != nil {
		return nil, err
	}
	log.Infof("Backup of %q started (%s trigger)", b.DatabaseName, b.Trigger)

	res, runErr := s.runBackup(ctx, job, b, log)
	if runErr != nil {
		if ctx.Err() != nil {
			log.Warnf("Backup cancelled")
			return nil, ctx.Err()
		}
		s.markFailed(context.WithoutCancel(ctx), b, runErr, log)
		return nil, runErr
	}

	_, err = s.Pool.Exec(ctx, `UPDATE backups SET status = 'completed', storage_key = $2, size_bytes = $3, raw_size_bytes = $4, checksum_sha256 = $5,
		pg_version = $6, pg_dump_version = $7, table_count = $8, completed_at = now(), duration_ms = $9, error = NULL WHERE id = $1`,
		b.ID, res.Key, res.SizeBytes, res.RawSizeBytes, res.Checksum, res.PGVersion, res.PGDumpVersion, res.TableCount, res.Duration.Milliseconds())
	if err != nil {
		return nil, err
	}
	_, _ = s.Pool.Exec(ctx, `UPDATE databases SET pg_version = $2 WHERE id = $1`, b.DatabaseID, res.PGVersion)
	log.Infof("Backup completed in %s", res.Duration.Round(time.Second))

	audit.MustRecord(ctx, s.Pool, audit.Entry{OrgID: orgID, Action: audit.BackupCompleted, ResourceType: "backup", ResourceID: b.ID,
		Metadata: map[string]any{"database": b.DatabaseName, "size_bytes": res.SizeBytes, "duration_ms": res.Duration.Milliseconds(), "checksum_sha256": res.Checksum}})
	s.Notify.Emit(ctx, orgID, notifications.Event{
		Type: notifications.EventBackupSucceeded, Severity: "success",
		Title:   "Backup of " + b.DatabaseName + " completed",
		Message: fmt.Sprintf("A %s backup of %s completed successfully.", b.Trigger, b.DatabaseName),
		Details: []notifications.Detail{{Label: "Size", Value: HumanBytes(res.SizeBytes)}, {Label: "Duration", Value: res.Duration.Round(time.Second).String()},
			{Label: "Storage", Value: b.StorageName}, {Label: "Checksum", Value: "sha256:" + res.Checksum}},
		Resource: map[string]any{"backup_id": b.ID, "database_id": b.DatabaseID, "database": b.DatabaseName},
		URL:      s.AppURL + "/backups/" + b.ID,
	})

	if b.ScheduleID != nil {
		s.afterScheduledBackup(ctx, orgID, *b.ScheduleID, b.ID, log)
	}
	return res, nil
}

func (s *Service) runBackup(ctx context.Context, job jobs.Job, b Backup, log *jobs.Logger) (Result, error) {
	target, err := s.Databases.Target(ctx, b.OrganizationID, b.DatabaseID)
	if err != nil {
		return Result{}, fmt.Errorf("load database credentials: %w", err)
	}
	dest, err := s.Destinations.GetIncludingDeleted(ctx, b.OrganizationID, b.StorageDestinationID)
	if err != nil {
		return Result{}, err
	}
	st, err := s.Destinations.Open(ctx, dest)
	if err != nil {
		return Result{}, &StorageError{Err: err}
	}
	var publicKey string
	if b.Encrypted {
		k, err := s.Keys.PublicKey(ctx, b.OrganizationID, *b.EncryptionKeyID)
		if err != nil {
			return Result{}, fmt.Errorf("load encryption key: %w", err)
		}
		publicKey = k
	}
	drv, err := s.Engine.Drivers.For(target)
	if err != nil {
		return Result{}, err
	}
	now := time.Now()
	key := ObjectKey(dest.Config.Prefix, b.DatabaseName, now, drv.FileExtension(), b.Compression, b.Encrypted, "")
	if exists, err := st.Exists(ctx, key); err == nil && exists {
		// Two backups in the same second: keep names predictable but unique.
		key = ObjectKey(dest.Config.Prefix, b.DatabaseName, now, drv.FileExtension(), b.Compression, b.Encrypted, b.ID[:8])
	}
	_, _ = s.Pool.Exec(ctx, `UPDATE backups SET storage_key = $2, format = $3 WHERE id = $1`, b.ID, key, drv.Format())
	log.Infof("Destination: %s (%s)", dest.Name, st.Describe())

	return s.Engine.Run(ctx, Request{
		Target:      target,
		Storage:     st,
		Key:         key,
		Compression: b.Compression,
		PublicKey:   publicKey,
		Log:         log,
		OnProgress: func(p Progress) {
			_ = s.Queue.UpdateProgress(ctx, job.ID, p)
		},
	})
}

func (s *Service) markFailed(ctx context.Context, b Backup, cause error, log *jobs.Logger) {
	msg := cause.Error()
	log.Errorf("Backup failed: %s", msg)
	_, _ = s.Pool.Exec(ctx, `UPDATE backups SET status = 'failed', error = $2, completed_at = now(),
		duration_ms = (extract(epoch FROM now() - COALESCE(started_at, now())) * 1000)::bigint WHERE id = $1`, b.ID, msg)
	audit.MustRecord(ctx, s.Pool, audit.Entry{OrgID: b.OrganizationID, Action: audit.BackupFailed, ResourceType: "backup", ResourceID: b.ID,
		Metadata: map[string]any{"database": b.DatabaseName, "error": msg}})
	details := []notifications.Detail{{Label: "Database", Value: b.DatabaseName}, {Label: "Reason", Value: msg}, {Label: "Storage", Value: b.StorageName}}
	s.Notify.Emit(ctx, b.OrganizationID, notifications.Event{
		Type: notifications.EventBackupFailed, Severity: "error",
		Title:    "Backup of " + b.DatabaseName + " failed",
		Message:  "The " + b.Trigger + " backup of " + b.DatabaseName + " failed: " + msg,
		Details:  details,
		Resource: map[string]any{"backup_id": b.ID, "database_id": b.DatabaseID, "database": b.DatabaseName},
		URL:      s.AppURL + "/backups/" + b.ID,
	})
	var se *StorageError
	if errors.As(cause, &se) {
		s.Notify.Emit(ctx, b.OrganizationID, notifications.Event{
			Type: notifications.EventStorageFailed, Severity: "error",
			Title:    "Storage failure on " + b.StorageName,
			Message:  "DBVault could not write a backup to " + b.StorageName + ": " + se.Err.Error(),
			Details:  details,
			Resource: map[string]any{"storage_destination_id": b.StorageDestinationID, "backup_id": b.ID},
			URL:      s.AppURL + "/storage",
		})
	}
}

// afterScheduledBackup enqueues retention cleanup and, when configured, an
// automatic restore test.
func (s *Service) afterScheduledBackup(ctx context.Context, orgID, scheduleID, backupID string, log *jobs.Logger) {
	var verify, retentionOn bool
	err := s.Pool.QueryRow(ctx, `SELECT verify_after_backup, (retention_daily + retention_weekly + retention_monthly) > 0
		FROM backup_schedules WHERE id = $1`, scheduleID).Scan(&verify, &retentionOn)
	if err != nil {
		return
	}
	if retentionOn {
		if err := s.EnqueueCleanup(ctx, orgID, scheduleID); err != nil {
			log.Warnf("Could not queue retention cleanup: %v", err)
		}
	}
	if verify {
		if _, err := s.RequestVerification(ctx, orgID, "", backupID); err != nil {
			log.Warnf("Could not queue automatic verification: %v", err)
		} else {
			log.Infof("Automatic restore verification queued")
		}
	}
}

// EnqueueCleanup queues a retention run for a schedule unless one is pending.
func (s *Service) EnqueueCleanup(ctx context.Context, orgID, scheduleID string) error {
	var pending bool
	if err := s.Pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM jobs WHERE type = 'cleanup' AND status IN ('queued', 'running')
		AND payload->>'schedule_id' = $1)`, scheduleID).Scan(&pending); err != nil {
		return err
	}
	if pending {
		return nil
	}
	_, err := s.Queue.Enqueue(ctx, jobs.Spec{OrganizationID: orgID, Type: jobs.TypeCleanup, Payload: map[string]string{"schedule_id": scheduleID}, MaxAttempts: 3})
	return err
}

// RetentionItems loads the backups a schedule's retention policy governs:
// completed, scheduled backups produced by that schedule. Manual backups
// are never deleted automatically.
func (s *Service) RetentionItems(ctx context.Context, scheduleID string) ([]retention.Item, error) {
	rows, err := s.Pool.Query(ctx, `SELECT id, created_at FROM backups WHERE schedule_id = $1 AND trigger = 'scheduled' AND status = 'completed'
		ORDER BY created_at DESC`, scheduleID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (retention.Item, error) {
		var it retention.Item
		err := r.Scan(&it.ID, &it.CreatedAt)
		return it, err
	})
}

// SchedulePolicy loads a schedule's retention policy and timezone.
func (s *Service) SchedulePolicy(ctx context.Context, scheduleID string) (retention.Policy, *time.Location, string, error) {
	var p retention.Policy
	var tz, orgID string
	err := s.Pool.QueryRow(ctx, `SELECT retention_daily, retention_weekly, retention_monthly, timezone, organization_id FROM backup_schedules WHERE id = $1`, scheduleID).
		Scan(&p.Daily, &p.Weekly, &p.Monthly, &tz, &orgID)
	if err != nil {
		return p, nil, "", err
	}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		loc = time.UTC
	}
	return p, loc, orgID, nil
}

// ExecuteCleanup applies a schedule's retention policy (worker side).
//
// Safety rules:
//   - the plan is recomputed here from the database, never trusted from a payload;
//   - a per-schedule advisory lock prevents concurrent cleanups;
//   - the newest backup and anything younger than an hour are always kept;
//   - each deletion re-checks that no restore or verification uses the backup;
//   - a policy of all zeros deletes nothing.
func (s *Service) ExecuteCleanup(ctx context.Context, job jobs.Job, log *jobs.Logger) (any, error) {
	var p struct {
		ScheduleID string `json:"schedule_id"`
	}
	if err := json.Unmarshal(job.Payload, &p); err != nil {
		return nil, err
	}
	policy, loc, orgID, err := s.SchedulePolicy(ctx, p.ScheduleID)
	if errors.Is(err, pgx.ErrNoRows) {
		log.Infof("Schedule no longer exists; nothing to clean up")
		return map[string]int{"deleted": 0}, nil
	}
	if err != nil {
		return nil, err
	}
	if !policy.Enabled() {
		log.Infof("Retention is disabled for this schedule; keeping all backups")
		return map[string]int{"deleted": 0}, nil
	}

	conn, err := s.Pool.Acquire(ctx)
	if err != nil {
		return nil, err
	}
	defer conn.Release()
	var locked bool
	if err := conn.QueryRow(ctx, `SELECT pg_try_advisory_lock(hashtextextended($1, 0))`, "retention:"+p.ScheduleID).Scan(&locked); err != nil {
		return nil, err
	}
	if !locked {
		log.Infof("Another cleanup for this schedule is running; skipping")
		return map[string]int{"deleted": 0}, nil
	}
	defer func() {
		_, _ = conn.Exec(context.WithoutCancel(ctx), `SELECT pg_advisory_unlock(hashtextextended($1, 0))`, "retention:"+p.ScheduleID)
	}()

	items, err := s.RetentionItems(ctx, p.ScheduleID)
	if err != nil {
		return nil, err
	}
	decisions := retention.Plan(items, policy, time.Now(), loc)
	var toDelete []retention.Decision
	for _, d := range decisions {
		if !d.Keep {
			toDelete = append(toDelete, d)
		}
	}
	log.Infof("Retention policy (%s): %d backups, keeping %d, deleting %d", policy, len(decisions), len(decisions)-len(toDelete), len(toDelete))
	if len(toDelete) == len(decisions) && len(decisions) > 0 {
		// Plan guarantees this can't happen; refuse loudly if it ever does.
		return nil, errors.New("refusing to delete every backup of a schedule")
	}

	deleted, failed := 0, 0
	for _, d := range toDelete {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if err := s.Delete(ctx, orgID, d.ID, "retention: "+policy.String()); err != nil {
			failed++
			log.Warnf("Could not delete backup %s from %s: %v", d.ID[:8], d.CreatedAt.Format(time.RFC3339), err)
			continue
		}
		deleted++
		log.Infof("Deleted expired backup %s from %s", d.ID[:8], d.CreatedAt.In(loc).Format("2006-01-02 15:04"))
	}
	if failed > 0 {
		return nil, fmt.Errorf("%d expired backups could not be deleted (will retry)", failed)
	}
	return map[string]int{"kept": len(decisions) - len(toDelete), "deleted": deleted}, nil
}
