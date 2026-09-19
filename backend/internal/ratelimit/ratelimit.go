// Package ratelimit implements fixed-window rate limiting backed by Redis,
// so limits hold across multiple API replicas.
package ratelimit

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/dbvault/dbvault/backend/internal/apperr"
	"github.com/dbvault/dbvault/backend/internal/httpx"
	"github.com/dbvault/dbvault/backend/internal/reqctx"
)

type Limiter struct {
	rdb *redis.Client
}

func New(rdb *redis.Client) *Limiter { return &Limiter{rdb: rdb} }

var incrScript = redis.NewScript(`
local n = redis.call('INCR', KEYS[1])
if n == 1 then redis.call('PEXPIRE', KEYS[1], ARGV[1]) end
return {n, redis.call('PTTL', KEYS[1])}`)

// Allow counts one hit against key and reports whether it is within limit.
// If Redis is unavailable the request is allowed (and the failure logged):
// an outage should not lock every user out; argon2 cost still bounds
// credential guessing.
func (l *Limiter) Allow(ctx context.Context, key string, limit int, window time.Duration) (bool, time.Duration) {
	res, err := incrScript.Run(ctx, l.rdb, []string{"dbvault:rl:" + key}, window.Milliseconds()).Int64Slice()
	if err != nil || len(res) != 2 {
		slog.Warn("rate limiter unavailable", "error", errString(err))
		return true, 0
	}
	return res[0] <= int64(limit), time.Duration(res[1]) * time.Millisecond
}

// Middleware limits requests per client (user when authenticated, otherwise IP).
func (l *Limiter) Middleware(bucket string, limit int, window time.Duration) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := "ip:" + reqctx.MetaFrom(r.Context()).IP
			if p, ok := reqctx.PrincipalFrom(r.Context()); ok {
				id = "user:" + p.UserID
			}
			ok, retry := l.Allow(r.Context(), bucket+":"+id, limit, window)
			if !ok {
				w.Header().Set("Retry-After", strconv.Itoa(int(retry.Seconds())+1))
				httpx.Error(w, r, apperr.RateLimited())
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func errString(err error) string {
	if err == nil {
		return "unexpected response"
	}
	return err.Error()
}
