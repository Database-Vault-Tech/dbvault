package jobs

import (
	"context"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/dbvault/dbvault/backend/internal/db"
)

func testQueue(t *testing.T) *Queue {
	t.Helper()
	dbURL, redisURL := os.Getenv("DBVAULT_TEST_DATABASE_URL"), os.Getenv("DBVAULT_TEST_REDIS_URL")
	if dbURL == "" || redisURL == "" {
		t.Skip("DBVAULT_TEST_DATABASE_URL and DBVAULT_TEST_REDIS_URL not set")
	}
	ctx := context.Background()
	pool, err := db.Connect(ctx, dbURL)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	opts, _ := redis.ParseURL(redisURL)
	rdb := redis.NewClient(opts)
	t.Cleanup(func() { rdb.Close(); pool.Close() })
	return NewQueue(pool, rdb)
}

func TestClaimIsExclusive(t *testing.T) {
	q := testQueue(t)
	ctx := context.Background()
	j, err := q.Create(ctx, q.pool, Spec{Type: TypeCleanup, Payload: map[string]string{"schedule_id": "x"}})
	if err != nil {
		t.Fatal(err)
	}
	var wins atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if _, ok, err := q.Claim(ctx, j.ID, "worker-"+string(rune('a'+i))); err == nil && ok {
				wins.Add(1)
			}
		}(i)
	}
	wg.Wait()
	if wins.Load() != 1 {
		t.Fatalf("job claimed %d times, want exactly once", wins.Load())
	}
}

func TestFailRetriesWithBackoffThenFails(t *testing.T) {
	q := testQueue(t)
	ctx := context.Background()
	j, _ := q.Create(ctx, q.pool, Spec{Type: TypeNotification, Payload: map[string]string{}, MaxAttempts: 2})
	if _, ok, _ := q.Claim(ctx, j.ID, "w"); !ok {
		t.Fatal("claim failed")
	}
	retrying, err := q.Fail(ctx, j.ID, context.DeadlineExceeded)
	if err != nil || !retrying {
		t.Fatalf("first failure should retry: %v %v", retrying, err)
	}
	got, _ := q.GetAny(ctx, j.ID)
	if got.Status != StatusQueued || !got.RunAfter.After(time.Now()) {
		t.Fatalf("retry not scheduled in the future: %+v", got)
	}
	if _, ok, _ := q.Claim(ctx, j.ID, "w"); ok {
		t.Fatal("job must not be claimable before its backoff expires")
	}
	_, _ = q.pool.Exec(ctx, `UPDATE jobs SET run_after = now() WHERE id = $1`, j.ID)
	if _, ok, _ := q.Claim(ctx, j.ID, "w"); !ok {
		t.Fatal("job should be claimable after backoff")
	}
	retrying, _ = q.Fail(ctx, j.ID, context.DeadlineExceeded)
	got, _ = q.GetAny(ctx, j.ID)
	if retrying || got.Status != StatusFailed {
		t.Fatalf("second failure should be final: %+v", got)
	}
}

func TestReapDeadWorkers(t *testing.T) {
	q := testQueue(t)
	ctx := context.Background()
	j, _ := q.Create(ctx, q.pool, Spec{Type: TypeCleanup, Payload: map[string]string{}})
	_, _, _ = q.Claim(ctx, j.ID, "crashed-worker")
	_, _ = q.pool.Exec(ctx, `UPDATE jobs SET heartbeat_at = now() - interval '10 minutes' WHERE id = $1`, j.ID)
	ids, err := q.ReapDead(ctx, 90*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, id := range ids {
		found = found || id == j.ID
	}
	got, _ := q.GetAny(ctx, j.ID)
	if !found || got.Status != StatusFailed || got.Error == nil {
		t.Fatalf("stale job not reaped: %+v", got)
	}
}

func TestSweepDispatchesLostJobs(t *testing.T) {
	q := testQueue(t)
	ctx := context.Background()
	_ = q.rdb.Del(ctx, QueueKey).Err()
	j, _ := q.Create(ctx, q.pool, Spec{Type: TypeCleanup, Payload: map[string]string{}}) // never pushed
	n, err := q.SweepUndispatched(ctx, time.Minute)
	if err != nil || n < 1 {
		t.Fatalf("sweep: %d %v", n, err)
	}
	// The dispatch is recorded durably (a worker may already have popped the
	// id from Redis, so the database is the reliable place to check).
	var pushed *time.Time
	if err := q.pool.QueryRow(ctx, `SELECT pushed_at FROM jobs WHERE id = $1`, j.ID).Scan(&pushed); err != nil || pushed == nil {
		t.Fatalf("undispatched job was not pushed: %v", err)
	}
	// A freshly pushed job is not pushed again immediately.
	if n2, _ := q.SweepUndispatched(ctx, time.Minute); n2 != 0 {
		t.Fatalf("recently pushed jobs re-pushed: %d", n2)
	}
	_, _ = q.pool.Exec(ctx, `UPDATE jobs SET status = 'cancelled' WHERE id = $1`, j.ID)
}

func TestCancelQueuedJob(t *testing.T) {
	q := testQueue(t)
	ctx := context.Background()
	var orgID string
	if err := q.pool.QueryRow(ctx, `INSERT INTO organizations (name, slug) VALUES ('t', 't-'||gen_random_uuid()) RETURNING id`).Scan(&orgID); err != nil {
		t.Fatal(err)
	}
	j, _ := q.Create(ctx, q.pool, Spec{OrganizationID: orgID, Type: TypeCleanup, Payload: map[string]string{}})
	got, err := q.RequestCancel(ctx, orgID, j.ID)
	if err != nil || got.Status != StatusCancelled {
		t.Fatalf("cancel: %+v %v", got, err)
	}
	if _, ok, _ := q.Claim(ctx, j.ID, "w"); ok {
		t.Fatal("cancelled job must not be claimable")
	}
	if _, err := q.RequestCancel(ctx, orgID, j.ID); err != ErrNotCancellable {
		t.Fatalf("cancelling a finished job: %v", err)
	}
}
