// Package jobs implements DBVault's background job system.
//
// PostgreSQL is the source of truth for every job (status, attempts, logs,
// progress). Redis is only the dispatch channel: job IDs are pushed onto a
// list and workers block on it. Workers claim a job with an atomic
// conditional UPDATE, so a job can never run twice even if its ID is pushed
// more than once, and a sweeper re-pushes queued jobs whose message was
// lost, so a job can never be stranded.
package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/dbvault/dbvault/backend/internal/db"
)

// Job types.
const (
	TypeBackup       = "backup"
	TypeRestore      = "restore"
	TypeVerification = "verification"
	TypeCleanup      = "cleanup"
	TypeNotification = "notification"
)

// Job statuses.
const (
	StatusQueued    = "queued"
	StatusRunning   = "running"
	StatusCompleted = "completed"
	StatusFailed    = "failed"
	StatusCancelled = "cancelled"
)

// QueueKey is the Redis list workers consume.
const QueueKey = "dbvault:queue:jobs"

type Job struct {
	ID              string          `json:"id"`
	OrganizationID  *string         `json:"organization_id"`
	Type            string          `json:"type"`
	Status          string          `json:"status"`
	Payload         json.RawMessage `json:"payload"`
	Result          json.RawMessage `json:"result"`
	Progress        json.RawMessage `json:"progress"`
	Error           *string         `json:"error"`
	Attempts        int             `json:"attempts"`
	MaxAttempts     int             `json:"max_attempts"`
	CancelRequested bool            `json:"cancel_requested"`
	WorkerID        *string         `json:"worker_id"`
	HeartbeatAt     *time.Time      `json:"heartbeat_at"`
	RunAfter        time.Time       `json:"run_after"`
	StartedAt       *time.Time      `json:"started_at"`
	CompletedAt     *time.Time      `json:"completed_at"`
	CreatedBy       *string         `json:"created_by"`
	CreatedAt       time.Time       `json:"created_at"`
	UpdatedAt       time.Time       `json:"updated_at"`
}

const jobColumns = `id, organization_id, type, status, payload, result, progress, error, attempts, max_attempts,
	cancel_requested, worker_id, heartbeat_at, run_after, started_at, completed_at, created_by, created_at, updated_at`

func scanJob(row pgx.Row) (Job, error) {
	var j Job
	err := row.Scan(&j.ID, &j.OrganizationID, &j.Type, &j.Status, &j.Payload, &j.Result, &j.Progress, &j.Error,
		&j.Attempts, &j.MaxAttempts, &j.CancelRequested, &j.WorkerID, &j.HeartbeatAt, &j.RunAfter, &j.StartedAt,
		&j.CompletedAt, &j.CreatedBy, &j.CreatedAt, &j.UpdatedAt)
	return j, err
}

// Spec describes a job to create.
type Spec struct {
	OrganizationID string // empty for system jobs
	Type           string
	Payload        any
	MaxAttempts    int
	RunAfter       time.Time
	CreatedBy      string
}

type Queue struct {
	pool *pgxpool.Pool
	rdb  *redis.Client
}

func NewQueue(pool *pgxpool.Pool, rdb *redis.Client) *Queue {
	return &Queue{pool: pool, rdb: rdb}
}

func nullable(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// Create inserts a queued job using q (which may be a transaction). The job
// is not dispatched until Push is called, normally right after commit; if
// that push is lost, the sweeper dispatches it anyway.
func (qu *Queue) Create(ctx context.Context, q db.DB, spec Spec) (Job, error) {
	payload, err := json.Marshal(spec.Payload)
	if err != nil {
		return Job{}, err
	}
	if spec.MaxAttempts < 1 {
		spec.MaxAttempts = 1
	}
	runAfter := spec.RunAfter
	if runAfter.IsZero() {
		runAfter = time.Now()
	}
	return scanJob(q.QueryRow(ctx, `INSERT INTO jobs (organization_id, type, payload, max_attempts, run_after, created_by)
		VALUES ($1, $2, $3, $4, $5, $6) RETURNING `+jobColumns,
		nullable(spec.OrganizationID), spec.Type, payload, spec.MaxAttempts, runAfter, nullable(spec.CreatedBy)))
}

// Push dispatches job IDs to workers. Errors are non-fatal for callers:
// the sweeper will dispatch any job whose push failed.
func (qu *Queue) Push(ctx context.Context, ids ...string) error {
	if len(ids) == 0 {
		return nil
	}
	vals := make([]any, len(ids))
	for i, id := range ids {
		vals[i] = id
	}
	if err := qu.rdb.LPush(ctx, QueueKey, vals...).Err(); err != nil {
		return fmt.Errorf("push jobs to redis: %w", err)
	}
	_, err := qu.pool.Exec(ctx, `UPDATE jobs SET pushed_at = now() WHERE id = ANY($1)`, ids)
	return err
}

// Enqueue creates and immediately dispatches a job outside a transaction.
func (qu *Queue) Enqueue(ctx context.Context, spec Spec) (Job, error) {
	j, err := qu.Create(ctx, qu.pool, spec)
	if err != nil {
		return j, err
	}
	if !j.RunAfter.After(time.Now()) {
		_ = qu.Push(ctx, j.ID)
	}
	return j, nil
}

// Pop blocks up to timeout for the next job ID. It returns "" on timeout.
func (qu *Queue) Pop(ctx context.Context, timeout time.Duration) (string, error) {
	res, err := qu.rdb.BRPop(ctx, timeout, QueueKey).Result()
	if errors.Is(err, redis.Nil) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return res[1], nil
}

// Claim atomically transitions a queued job to running. ok is false if the
// job was already claimed, cancelled, or is not yet due.
func (qu *Queue) Claim(ctx context.Context, id, workerID string) (Job, bool, error) {
	j, err := scanJob(qu.pool.QueryRow(ctx, `UPDATE jobs
		SET status = 'running', worker_id = $2, started_at = now(), heartbeat_at = now(), attempts = attempts + 1, error = NULL
		WHERE id = $1 AND status = 'queued' AND run_after <= now() AND NOT cancel_requested
		RETURNING `+jobColumns, id, workerID))
	if db.IsNotFound(err) {
		return Job{}, false, nil
	}
	if err != nil {
		return Job{}, false, err
	}
	return j, true, nil
}

// Heartbeat records liveness and reports whether cancellation was requested.
func (qu *Queue) Heartbeat(ctx context.Context, id, workerID string) (bool, error) {
	var cancel bool
	err := qu.pool.QueryRow(ctx, `UPDATE jobs SET heartbeat_at = now() WHERE id = $1 AND worker_id = $2 AND status = 'running'
		RETURNING cancel_requested`, id, workerID).Scan(&cancel)
	if db.IsNotFound(err) {
		// The job was finalised elsewhere (e.g. reaped); stop working on it.
		return true, nil
	}
	return cancel, err
}

// UpdateProgress stores a progress snapshot shown in the UI and CLI.
func (qu *Queue) UpdateProgress(ctx context.Context, id string, progress any) error {
	b, err := json.Marshal(progress)
	if err != nil {
		return err
	}
	_, err = qu.pool.Exec(ctx, `UPDATE jobs SET progress = $2 WHERE id = $1`, id, b)
	return err
}

// Complete marks a job successful.
func (qu *Queue) Complete(ctx context.Context, id string, result any) error {
	b, err := json.Marshal(result)
	if err != nil {
		return err
	}
	_, err = qu.pool.Exec(ctx, `UPDATE jobs SET status = 'completed', result = $2, completed_at = now() WHERE id = $1`, id, b)
	return err
}

// Fail records a failure. When attempts remain, the job is re-queued with
// exponential backoff and retrying is true.
func (qu *Queue) Fail(ctx context.Context, id string, cause error) (retrying bool, err error) {
	msg := cause.Error()
	var attempts, max int
	err = qu.pool.QueryRow(ctx, `SELECT attempts, max_attempts FROM jobs WHERE id = $1`, id).Scan(&attempts, &max)
	if err != nil {
		return false, err
	}
	if attempts < max {
		backoff := time.Duration(1<<min(attempts, 8)) * 30 * time.Second
		_, err = qu.pool.Exec(ctx, `UPDATE jobs SET status = 'queued', error = $2, worker_id = NULL, pushed_at = NULL,
			run_after = now() + $3::interval WHERE id = $1`, id, msg, fmt.Sprintf("%d seconds", int(backoff.Seconds())))
		return true, err
	}
	_, err = qu.pool.Exec(ctx, `UPDATE jobs SET status = 'failed', error = $2, completed_at = now() WHERE id = $1`, id, msg)
	return false, err
}

// MarkCancelled finalises a job that stopped because cancellation was requested.
func (qu *Queue) MarkCancelled(ctx context.Context, id string) error {
	_, err := qu.pool.Exec(ctx, `UPDATE jobs SET status = 'cancelled', error = 'Cancelled by user', completed_at = now() WHERE id = $1`, id)
	return err
}

// ErrNotCancellable is returned when a job has already finished.
var ErrNotCancellable = errors.New("job has already finished")

// RequestCancel cancels a queued job immediately, or flags a running job so
// its worker stops at the next heartbeat.
func (qu *Queue) RequestCancel(ctx context.Context, orgID, id string) (Job, error) {
	var out Job
	err := db.WithTx(ctx, qu.pool, func(tx pgx.Tx) error {
		j, err := scanJob(tx.QueryRow(ctx, `SELECT `+jobColumns+` FROM jobs WHERE id = $1 AND organization_id = $2 FOR UPDATE`, id, orgID))
		if err != nil {
			return err
		}
		switch j.Status {
		case StatusQueued:
			if _, err := tx.Exec(ctx, `UPDATE jobs SET status = 'cancelled', cancel_requested = true, error = 'Cancelled by user', completed_at = now() WHERE id = $1`, id); err != nil {
				return err
			}
			if err := SyncRelated(ctx, tx, []string{id}, StatusCancelled, "Cancelled by user"); err != nil {
				return err
			}
			j.Status = StatusCancelled
		case StatusRunning:
			if _, err := tx.Exec(ctx, `UPDATE jobs SET cancel_requested = true WHERE id = $1`, id); err != nil {
				return err
			}
			j.CancelRequested = true
		default:
			return ErrNotCancellable
		}
		out = j
		return nil
	})
	return out, err
}

// Get returns a job scoped to an organization.
func (qu *Queue) Get(ctx context.Context, orgID, id string) (Job, error) {
	return scanJob(qu.pool.QueryRow(ctx, `SELECT `+jobColumns+` FROM jobs WHERE id = $1 AND organization_id = $2`, id, orgID))
}

// GetAny returns a job regardless of organization (worker use only).
func (qu *Queue) GetAny(ctx context.Context, id string) (Job, error) {
	return scanJob(qu.pool.QueryRow(ctx, `SELECT `+jobColumns+` FROM jobs WHERE id = $1`, id))
}

// ListFilter narrows List.
type ListFilter struct {
	Type   string
	Status string
	Limit  int
}

// List returns recent jobs for an organization.
func (qu *Queue) List(ctx context.Context, orgID string, f ListFilter) ([]Job, error) {
	if f.Limit <= 0 || f.Limit > 200 {
		f.Limit = 50
	}
	rows, err := qu.pool.Query(ctx, `SELECT `+jobColumns+` FROM jobs WHERE organization_id = $1
		AND ($2 = '' OR type = $2) AND ($3 = '' OR status = $3) ORDER BY created_at DESC LIMIT $4`,
		orgID, f.Type, f.Status, f.Limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Job{}
	for rows.Next() {
		j, err := scanJob(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, j)
	}
	return out, rows.Err()
}

// SweepUndispatched re-pushes due queued jobs that were never pushed or
// whose push is older than staleAfter (e.g. Redis restarted). Duplicated IDs
// in Redis are harmless because Claim is atomic.
func (qu *Queue) SweepUndispatched(ctx context.Context, staleAfter time.Duration) (int, error) {
	rows, err := qu.pool.Query(ctx, `SELECT id FROM jobs WHERE status = 'queued' AND run_after <= now() AND NOT cancel_requested
		AND (pushed_at IS NULL OR pushed_at < now() - $1::interval) ORDER BY run_after LIMIT 500`,
		fmt.Sprintf("%d seconds", int(staleAfter.Seconds())))
	if err != nil {
		return 0, err
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return 0, err
	}
	return len(ids), qu.Push(ctx, ids...)
}

// ReapDead fails running jobs whose worker stopped heartbeating (crashed or
// was killed) and syncs the related backup/restore records.
func (qu *Queue) ReapDead(ctx context.Context, timeout time.Duration) ([]string, error) {
	var ids []string
	err := db.WithTx(ctx, qu.pool, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `UPDATE jobs SET status = 'failed', completed_at = now(),
			error = 'The worker stopped responding while running this job (it may have crashed or been restarted).'
			WHERE status = 'running' AND heartbeat_at < now() - $1::interval RETURNING id`,
			fmt.Sprintf("%d seconds", int(timeout.Seconds())))
		if err != nil {
			return err
		}
		ids, err = pgx.CollectRows(rows, pgx.RowTo[string])
		if err != nil {
			return err
		}
		return SyncRelated(ctx, tx, ids, StatusFailed, "The worker stopped responding while running this job.")
	})
	return ids, err
}

// SyncRelated propagates a terminal job status to the backup/restore rows
// that track it, so the UI never shows a job stuck in "running".
func SyncRelated(ctx context.Context, q db.DB, jobIDs []string, status, msg string) error {
	if len(jobIDs) == 0 {
		return nil
	}
	if _, err := q.Exec(ctx, `UPDATE backups SET status = $2, error = $3, completed_at = now()
		WHERE job_id = ANY($1) AND status IN ('queued', 'running')`, jobIDs, status, msg); err != nil {
		return err
	}
	if _, err := q.Exec(ctx, `UPDATE restore_jobs SET status = $2, error = $3, completed_at = now()
		WHERE job_id = ANY($1) AND status IN ('queued', 'running', 'verifying')`, jobIDs, status, msg); err != nil {
		return err
	}
	verifyStatus := "failed"
	if status == StatusCancelled {
		verifyStatus = "none"
	}
	_, err := q.Exec(ctx, `UPDATE backups SET verification_status = $2
		WHERE verification_status = 'running' AND id IN (
			SELECT (payload->>'backup_id')::uuid FROM jobs WHERE id = ANY($1) AND type = 'verification')`, jobIDs, verifyStatus)
	return err
}
