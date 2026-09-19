// Package worker executes background jobs pulled from the queue.
package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/dbvault/dbvault/backend/internal/jobs"
	"github.com/dbvault/dbvault/backend/internal/logging"
)

// Handler executes one job. It must honour ctx cancellation.
type Handler func(ctx context.Context, job jobs.Job, log *jobs.Logger) (any, error)

// PresenceKeyPrefix is where workers advertise themselves in Redis.
const PresenceKeyPrefix = "dbvault:workers:"

const (
	// presenceEvery is how often a worker advertises itself in Redis.
	presenceEvery = 10 * time.Second
	// jobHeartbeatEvery bounds how quickly a cancellation takes effect.
	jobHeartbeatEvery = 3 * time.Second
)

type Worker struct {
	ID          string
	Pool        *pgxpool.Pool
	Queue       *jobs.Queue
	Redis       *redis.Client
	Handlers    map[string]Handler
	Concurrency int
	GracePeriod time.Duration
	Log         *slog.Logger
	// Capabilities is advertised to the API (pg_dump version, verify mode...).
	Capabilities map[string]any

	startedAt time.Time
	active    atomic.Int32
	running   sync.Map // job id -> job type
}

// Presence is what a worker advertises in Redis.
type Presence struct {
	ID           string            `json:"id"`
	Hostname     string            `json:"hostname"`
	StartedAt    time.Time         `json:"started_at"`
	SeenAt       time.Time         `json:"seen_at"`
	Concurrency  int               `json:"concurrency"`
	ActiveJobs   int               `json:"active_jobs"`
	Running      map[string]string `json:"running"`
	Capabilities map[string]any    `json:"capabilities"`
}

func (w *Worker) presence() Presence {
	host, _ := os.Hostname()
	running := map[string]string{}
	w.running.Range(func(k, v any) bool {
		running[k.(string)] = v.(string)
		return true
	})
	return Presence{ID: w.ID, Hostname: host, StartedAt: w.startedAt, SeenAt: time.Now().UTC(), Concurrency: w.Concurrency,
		ActiveJobs: int(w.active.Load()), Running: running, Capabilities: w.Capabilities}
}

// Status returns the worker's current presence (for its health endpoint).
func (w *Worker) Status() Presence { return w.presence() }

func (w *Worker) advertise(ctx context.Context) {
	b, _ := json.Marshal(w.presence())
	if err := w.Redis.Set(ctx, PresenceKeyPrefix+w.ID, b, 3*presenceEvery).Err(); err != nil {
		w.Log.Warn("advertise worker presence", "error", err.Error())
	}
}

// Run processes jobs until ctx is cancelled, then waits up to GracePeriod
// for running jobs before cancelling them.
func (w *Worker) Run(ctx context.Context) {
	w.startedAt = time.Now().UTC()
	// hardCtx outlives ctx by the grace period so in-flight jobs can finish.
	hardCtx, hardCancel := context.WithCancel(context.Background())
	defer hardCancel()

	go func() {
		t := time.NewTicker(presenceEvery)
		defer t.Stop()
		w.advertise(hardCtx)
		for {
			select {
			case <-hardCtx.Done():
				return
			case <-t.C:
				w.advertise(hardCtx)
			}
		}
	}()

	var wg sync.WaitGroup
	for i := 0; i < w.Concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			w.loop(ctx, hardCtx)
		}()
	}
	w.Log.Info("worker started", "worker_id", w.ID, "concurrency", w.Concurrency)

	<-ctx.Done()
	w.Log.Info("worker shutting down; waiting for running jobs", "active", w.active.Load(), "grace_period", w.GracePeriod.String())
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(w.GracePeriod):
		w.Log.Warn("grace period expired; cancelling running jobs")
		hardCancel()
		<-done
	}
	_ = w.Redis.Del(context.Background(), PresenceKeyPrefix+w.ID).Err()
}

func (w *Worker) loop(ctx, hardCtx context.Context) {
	for ctx.Err() == nil {
		id, err := w.Queue.Pop(ctx, 5*time.Second)
		if err != nil {
			if ctx.Err() == nil {
				w.Log.Warn("queue pop failed", "error", err.Error())
				time.Sleep(2 * time.Second)
			}
			continue
		}
		if id == "" {
			continue
		}
		job, ok, err := w.Queue.Claim(hardCtx, id, w.ID)
		if err != nil {
			w.Log.Error("claim job", "job_id", id, "error", err.Error())
			continue
		}
		if !ok {
			continue // already claimed, cancelled or not due
		}
		w.execute(hardCtx, job)
	}
}

func (w *Worker) execute(hardCtx context.Context, job jobs.Job) {
	w.active.Add(1)
	w.running.Store(job.ID, job.Type)
	defer func() {
		w.active.Add(-1)
		w.running.Delete(job.ID)
	}()

	logger := w.Log.With("job_id", job.ID, "job_type", job.Type)
	if job.OrganizationID != nil {
		logger = logger.With("organization_id", *job.OrganizationID)
	}
	jl := jobs.NewLogger(w.Pool, job.ID, logger)
	ctx, cancel := context.WithCancel(logging.WithLogger(hardCtx, logger))
	defer cancel()

	var userCancelled atomic.Bool
	hbDone := make(chan struct{})
	go func() {
		defer close(hbDone)
		t := time.NewTicker(jobHeartbeatEvery)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				cancelReq, err := w.Queue.Heartbeat(ctx, job.ID, w.ID)
				if err != nil {
					logger.Warn("job heartbeat failed", "error", err.Error())
					continue
				}
				if cancelReq {
					userCancelled.Store(true)
					jl.Warnf("Cancellation requested; stopping")
					cancel()
					return
				}
			}
		}
	}()

	start := time.Now()
	handler, ok := w.Handlers[job.Type]
	var result any
	var err error
	if !ok {
		err = fmt.Errorf("no handler for job type %q", job.Type)
	} else {
		result, err = w.safeRun(ctx, handler, job, jl)
	}
	cancel()
	<-hbDone

	// Finalise with a context that survives shutdown.
	fctx, fcancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer fcancel()
	dur := time.Since(start)
	switch {
	case err == nil:
		// A job that finished is completed, even if a cancel raced with it.
		if cerr := w.Queue.Complete(fctx, job.ID, result); cerr != nil {
			logger.Error("record job completion", "error", cerr.Error())
		}
		logger.Info("job completed", "duration_ms", dur.Milliseconds(), "status", "completed")
	case userCancelled.Load():
		_ = w.Queue.MarkCancelled(fctx, job.ID)
		_ = jobs.SyncRelated(fctx, w.Pool, []string{job.ID}, jobs.StatusCancelled, "Cancelled by user")
		logger.Info("job cancelled", "duration_ms", dur.Milliseconds(), "status", "cancelled")
	default:
		if hardCtx.Err() != nil {
			err = errors.New("the worker shut down before this job finished")
		}
		retrying, ferr := w.Queue.Fail(fctx, job.ID, err)
		if ferr != nil {
			logger.Error("record job failure", "error", ferr.Error())
		}
		if !retrying {
			_ = jobs.SyncRelated(fctx, w.Pool, []string{job.ID}, jobs.StatusFailed, err.Error())
		}
		logger.Warn("job failed", "duration_ms", dur.Milliseconds(), "status", "failed", "retrying", retrying, "error", err.Error())
	}
}

// safeRun converts panics into job failures (stack logged server-side only).
func (w *Worker) safeRun(ctx context.Context, h Handler, job jobs.Job, jl *jobs.Logger) (result any, err error) {
	defer func() {
		if p := recover(); p != nil {
			jl.Slog().Error("job panicked", "panic", fmt.Sprint(p), "stack", string(debug.Stack()))
			jl.Errorf("Internal error while running this job (details are in the worker logs)")
			err = errors.New("internal error")
		}
	}()
	return h(ctx, job, jl)
}

// ListPresence returns every live worker (used by the API).
func ListPresence(ctx context.Context, rdb *redis.Client) ([]Presence, error) {
	var out []Presence
	iter := rdb.Scan(ctx, 0, PresenceKeyPrefix+"*", 100).Iterator()
	for iter.Next(ctx) {
		b, err := rdb.Get(ctx, iter.Val()).Bytes()
		if err != nil {
			continue
		}
		var p Presence
		if json.Unmarshal(b, &p) == nil {
			out = append(out, p)
		}
	}
	if out == nil {
		out = []Presence{}
	}
	return out, iter.Err()
}
