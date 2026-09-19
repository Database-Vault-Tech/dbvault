package scheduler

import (
	"context"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dbvault/dbvault/backend/internal/auth"
	"github.com/dbvault/dbvault/backend/internal/backups"
	"github.com/dbvault/dbvault/backend/internal/db"
	"github.com/dbvault/dbvault/backend/internal/jobs"
)

const leaderLockID = 7243002

// Runner turns due schedules into backup jobs and performs housekeeping.
//
// Duplicate protection is layered:
//  1. only one scheduler replica is leader (PostgreSQL advisory lock);
//  2. due schedules are claimed with SELECT ... FOR UPDATE SKIP LOCKED and
//     next_run_at is advanced in the same transaction as the job insert;
//  3. a new backup is never queued while one for the same database is
//     still queued or running.
type Runner struct {
	Pool    *pgxpool.Pool
	Queue   *jobs.Queue
	Backups *backups.Service
	Log     *slog.Logger

	leader   atomic.Bool
	lastTick atomic.Int64
}

// Status is exposed on the scheduler health endpoint.
type Status struct {
	Leader   bool      `json:"leader"`
	LastTick time.Time `json:"last_tick"`
}

func (r *Runner) Status() Status {
	return Status{Leader: r.leader.Load(), LastTick: time.Unix(r.lastTick.Load(), 0).UTC()}
}

// Run blocks until ctx is cancelled.
func (r *Runner) Run(ctx context.Context) {
	for ctx.Err() == nil {
		conn, err := r.Pool.Acquire(ctx)
		if err != nil {
			r.sleep(ctx, 5*time.Second)
			continue
		}
		var got bool
		if err := conn.QueryRow(ctx, `SELECT pg_try_advisory_lock($1)`, leaderLockID).Scan(&got); err != nil || !got {
			conn.Release()
			r.lastTick.Store(time.Now().Unix())
			r.sleep(ctx, 10*time.Second)
			continue
		}
		r.Log.Info("scheduler acquired leadership")
		r.leader.Store(true)
		r.lead(ctx, conn)
		r.leader.Store(false)
		// Releasing the connection releases the session-level lock; destroy
		// it rather than returning a lock-holding connection to the pool.
		_ = conn.Conn().Close(context.Background())
		conn.Release()
	}
}

func (r *Runner) sleep(ctx context.Context, d time.Duration) {
	select {
	case <-ctx.Done():
	case <-time.After(d):
	}
}

// lead runs the scheduling loops while holding the leader lock.
func (r *Runner) lead(ctx context.Context, lockConn *pgxpool.Conn) {
	due := time.NewTicker(10 * time.Second)
	sweep := time.NewTicker(5 * time.Second)
	reap := time.NewTicker(30 * time.Second)
	hourly := time.NewTicker(time.Hour)
	defer due.Stop()
	defer sweep.Stop()
	defer reap.Stop()
	defer hourly.Stop()

	r.tick(ctx)
	r.housekeeping(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-due.C:
			// Confirm we still hold the lock (the connection may have died).
			if err := lockConn.Ping(ctx); err != nil {
				r.Log.Warn("lost scheduler leadership", "error", err.Error())
				return
			}
			r.tick(ctx)
		case <-sweep.C:
			if n, err := r.Queue.SweepUndispatched(ctx, time.Minute); err != nil {
				r.Log.Warn("sweep undispatched jobs", "error", err.Error())
			} else if n > 0 {
				r.Log.Info("dispatched queued jobs", "count", n)
			}
		case <-reap.C:
			if ids, err := r.Queue.ReapDead(ctx, 90*time.Second); err != nil {
				r.Log.Warn("reap dead jobs", "error", err.Error())
			} else if len(ids) > 0 {
				r.Log.Warn("marked jobs from unresponsive workers as failed", "count", len(ids), "job_ids", ids)
			}
		case <-hourly.C:
			r.housekeeping(ctx)
		}
	}
}

// tick queues backups for every due schedule.
func (r *Runner) tick(ctx context.Context) {
	r.lastTick.Store(time.Now().Unix())
	var pushed []string
	err := db.WithTx(ctx, r.Pool, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT s.id, s.organization_id, s.database_id, s.storage_destination_id, s.compression, s.encryption,
			s.cron_expression, s.timezone, d.name
			FROM backup_schedules s JOIN databases d ON d.id = s.database_id
			WHERE s.enabled AND s.next_run_at <= now() AND d.deleted_at IS NULL
			ORDER BY s.next_run_at LIMIT 50
			FOR UPDATE OF s SKIP LOCKED`)
		if err != nil {
			return err
		}
		type due struct {
			id, org, dbID, dest, compression, cron, tz, dbName string
			encrypted                                          bool
		}
		var list []due
		for rows.Next() {
			var d due
			if err := rows.Scan(&d.id, &d.org, &d.dbID, &d.dest, &d.compression, &d.encrypted, &d.cron, &d.tz, &d.dbName); err != nil {
				rows.Close()
				return err
			}
			list = append(list, d)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		for _, d := range list {
			next, err := nextRun(d.cron, d.tz)
			if err != nil {
				r.Log.Error("invalid schedule; disabling", "schedule_id", d.id, "error", err.Error())
				if _, err := tx.Exec(ctx, `UPDATE backup_schedules SET enabled = false WHERE id = $1`, d.id); err != nil {
					return err
				}
				continue
			}
			q, ok, err := r.Backups.CreateScheduled(ctx, tx, backups.ScheduledArgs{OrgID: d.org, DatabaseID: d.dbID, DestinationID: d.dest,
				ScheduleID: d.id, Compression: d.compression, Encrypted: d.encrypted})
			if err != nil {
				return err
			}
			if ok {
				pushed = append(pushed, q.JobID)
				r.Log.Info("scheduled backup queued", "schedule_id", d.id, "database_id", d.dbID, "backup_id", q.BackupID, "job_id", q.JobID)
			} else {
				r.Log.Warn("skipping scheduled run: previous backup still in progress", "schedule_id", d.id, "database", d.dbName)
			}
			if _, err := tx.Exec(ctx, `UPDATE backup_schedules SET last_run_at = now(), next_run_at = $2 WHERE id = $1`, d.id, next); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		r.Log.Error("scheduler tick failed", "error", err.Error())
		return
	}
	if err := r.Queue.Push(ctx, pushed...); err != nil {
		r.Log.Warn("push scheduled jobs (sweeper will retry)", "error", err.Error())
	}
}

// housekeeping applies retention for every schedule and purges expired
// auth artifacts and old job logs.
func (r *Runner) housekeeping(ctx context.Context) {
	rows, err := r.Pool.Query(ctx, `SELECT id, organization_id FROM backup_schedules
		WHERE retention_daily + retention_weekly + retention_monthly > 0`)
	if err == nil {
		type sc struct{ id, org string }
		list, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (sc, error) {
			var s sc
			err := row.Scan(&s.id, &s.org)
			return s, err
		})
		if err == nil {
			for _, s := range list {
				if err := r.Backups.EnqueueCleanup(ctx, s.org, s.id); err != nil {
					r.Log.Warn("enqueue cleanup", "schedule_id", s.id, "error", err.Error())
				}
			}
		}
	}
	if err := auth.PurgeExpired(ctx, r.Pool); err != nil {
		r.Log.Warn("purge expired sessions", "error", err.Error())
	}
	if _, err := r.Pool.Exec(ctx, `DELETE FROM jobs WHERE type = 'notification' AND status IN ('completed', 'cancelled') AND completed_at < now() - interval '30 days'`); err != nil {
		r.Log.Warn("purge old notification jobs", "error", err.Error())
	}
}
