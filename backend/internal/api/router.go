// Package api assembles the HTTP API.
package api

import (
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net"
	"net/http"
	"runtime/debug"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/dbvault/dbvault/backend/internal/admin"
	"github.com/dbvault/dbvault/backend/internal/app"
	"github.com/dbvault/dbvault/backend/internal/apperr"
	"github.com/dbvault/dbvault/backend/internal/audit"
	"github.com/dbvault/dbvault/backend/internal/auth"
	"github.com/dbvault/dbvault/backend/internal/httpx"
	"github.com/dbvault/dbvault/backend/internal/logging"
	"github.com/dbvault/dbvault/backend/internal/reqctx"
	"github.com/dbvault/dbvault/backend/internal/restore"
	"github.com/dbvault/dbvault/backend/internal/worker"
)

// NewRouter builds the complete HTTP handler.
func NewRouter(a *app.App) http.Handler {
	r := chi.NewRouter()
	r.Use(requestMeta(a.Config.TrustProxyHeaders))
	r.Use(requestLogger)
	r.Use(recoverer)
	r.Use(securityHeaders)

	health := func(w http.ResponseWriter, r *http.Request) {
		httpx.JSON(w, http.StatusOK, map[string]string{"status": "ok", "version": app.Version})
	}
	ready := func(w http.ResponseWriter, r *http.Request) {
		checks := a.Ready(r.Context())
		status := http.StatusOK
		for _, v := range checks {
			if v != "ok" {
				status = http.StatusServiceUnavailable
			}
		}
		httpx.JSON(w, status, map[string]any{"status": map[bool]string{true: "ready", false: "not_ready"}[status == http.StatusOK], "checks": checks})
	}
	r.Get("/health", health)
	r.Get("/ready", ready)

	adm := admin.New(a.Pool, a.Redis, a.Config.InstanceAdminEmails)
	authH := &auth.Handlers{Svc: a.Auth, Limiter: a.Limiter, CookieSecure: a.Config.CookieSecure}

	r.Route("/api", func(r chi.Router) {
		r.Use(a.Auth.Middleware)
		r.Use(a.Limiter.Middleware("api", a.Config.RateLimitAPIPerMinute, time.Minute))
		r.Use(a.Auth.CSRF)
		r.Get("/health", health)

		r.Route("/auth", func(r chi.Router) {
			r.Group(func(r chi.Router) {
				r.Use(a.Limiter.Middleware("auth", a.Config.RateLimitAuthPerMinute, time.Minute))
				authH.PublicRoutes(r)
			})
			r.Group(func(r chi.Router) {
				r.Use(auth.RequireAuth)
				authH.PrivateRoutes(r)
			})
		})

		r.Group(func(r chi.Router) {
			r.Use(auth.RequireAuth)
			r.Get("/me", meHandler(a, adm))
			r.Get("/system/status", systemStatusHandler(a))
			a.Organizations.UserRoutes(r)
			adm.Routes(r)

			r.Group(func(r chi.Router) {
				r.Use(a.Organizations.Middleware)
				a.Organizations.OrgRoutes(r)
				a.Databases.Routes(r)
				a.Destinations.Routes(r)
				a.Schedules.Routes(r)
				a.Backups.Routes(r)
				a.Restores.Routes(r)
				a.Notifications.Routes(r)
				a.Queue.Routes(r)
				r.Get("/audit-logs", auditLogsHandler(a))
			})
		})

		r.NotFound(func(w http.ResponseWriter, r *http.Request) {
			httpx.Error(w, r, apperr.NotFound("Endpoint"))
		})
		r.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) {
			httpx.Error(w, r, apperr.New(http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed."))
		})
	})
	return r
}

func meHandler(a *app.App, adm *admin.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, _ := reqctx.PrincipalFrom(r.Context())
		user, err := a.Auth.GetUser(r.Context(), p.UserID)
		if err != nil {
			httpx.Error(w, r, err)
			return
		}
		orgs, err := a.Organizations.ListForUser(r.Context(), p.UserID)
		if err != nil {
			httpx.Error(w, r, err)
			return
		}
		out := map[string]any{"user": user, "organizations": orgs, "auth_method": p.ActorType(), "is_instance_admin": adm.IsAdmin(p)}
		if p.SessionID != "" {
			// Lets the SPA recover its CSRF token if the cookie was lost.
			out["csrf_token"] = a.Auth.Hasher.CSRFToken(p.SessionID)
		}
		httpx.JSON(w, http.StatusOK, out)
	}
}

// systemStatusHandler reports worker availability and capabilities so the
// UI can warn when nothing will execute jobs, and be honest about whether
// restore testing is possible.
func systemStatusHandler(a *app.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		workers, err := worker.ListPresence(r.Context(), a.Redis)
		if err != nil {
			httpx.Error(w, r, err)
			return
		}
		verify := map[string]any{"available": false, "mode": "unknown", "message": restore.UnavailableMessage}
		for _, wk := range workers {
			if v, ok := wk.Capabilities["verify_available"].(bool); ok && v {
				verify = map[string]any{"available": true, "mode": wk.Capabilities["verify_mode"], "message": wk.Capabilities["verify_detail"]}
				break
			}
			if m, ok := wk.Capabilities["verify_mode"]; ok {
				verify = map[string]any{"available": false, "mode": m, "message": wk.Capabilities["verify_detail"]}
			}
		}
		httpx.JSON(w, http.StatusOK, map[string]any{
			"version":            app.Version,
			"workers":            workers,
			"workers_online":     len(workers),
			"verification":       verify,
			"email_configured":   a.Mailer.Configured(),
			"builtin_storage":    a.Config.S3.Configured(),
			"allow_registration": a.Config.AllowRegistration,
		})
	}
}

func auditLogsHandler(a *app.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		m, _ := reqctx.MembershipFrom(r.Context())
		q := r.URL.Query()
		f := audit.Filter{Action: q.Get("action"), ResourceType: q.Get("resource_type"), ResourceID: q.Get("resource_id"),
			Limit: httpx.QueryInt(r, "limit", 50, 1, 200)}
		if b := q.Get("before"); b != "" {
			t, err := time.Parse(time.RFC3339Nano, b)
			if err != nil {
				httpx.Error(w, r, apperr.BadRequest("before must be an RFC 3339 timestamp."))
				return
			}
			f.Before = &t
		}
		logs, err := audit.List(r.Context(), a.Pool, m.OrgID, f)
		if err != nil {
			httpx.Error(w, r, err)
			return
		}
		meta := map[string]any{"limit": f.Limit}
		if len(logs) == f.Limit {
			meta["next_before"] = logs[len(logs)-1].CreatedAt.Format(time.RFC3339Nano)
		}
		httpx.List(w, logs, meta)
	}
}

// requestMeta assigns a request id and resolves the client IP. Proxy
// headers are only trusted when TRUST_PROXY_HEADERS=true (i.e. the API is
// only reachable through the frontend or a reverse proxy).
func requestMeta(trustProxy bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := r.Header.Get("X-Request-ID")
			if id == "" || len(id) > 64 || strings.ContainsAny(id, "\r\n\"") {
				b := make([]byte, 8)
				_, _ = rand.Read(b)
				id = hex.EncodeToString(b)
			}
			w.Header().Set("X-Request-ID", id)
			ip, _, err := net.SplitHostPort(r.RemoteAddr)
			if err != nil {
				ip = r.RemoteAddr
			}
			if trustProxy {
				if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
					ip = strings.TrimSpace(strings.Split(xff, ",")[0])
				} else if xr := r.Header.Get("X-Real-IP"); xr != "" {
					ip = strings.TrimSpace(xr)
				}
			}
			ctx := reqctx.WithMeta(r.Context(), reqctx.Meta{RequestID: id, IP: ip, UserAgent: r.UserAgent()})
			ctx = logging.WithLogger(ctx, logging.FromContext(ctx).With("request_id", id))
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

type statusRecorder struct {
	http.ResponseWriter
	status int
	bytes  int64
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusRecorder) Write(b []byte) (int, error) {
	if s.status == 0 {
		s.status = http.StatusOK
	}
	n, err := s.ResponseWriter.Write(b)
	s.bytes += int64(n)
	return n, err
}

func (s *statusRecorder) Flush() {
	if f, ok := s.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// requestLogger logs one structured line per request. Only the path is
// logged (never query strings, bodies or headers) so secrets can't leak.
func requestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w}
		next.ServeHTTP(rec, r)
		if r.URL.Path == "/health" || r.URL.Path == "/ready" {
			return
		}
		l := logging.FromContext(r.Context())
		if p, ok := reqctx.PrincipalFrom(r.Context()); ok {
			l = l.With("user_id", p.UserID)
		}
		level := slogLevel(rec.status)
		l.Log(r.Context(), level, "http request", "method", r.Method, "path", r.URL.Path, "status", rec.status,
			"duration_ms", time.Since(start).Milliseconds(), "bytes", rec.bytes)
	})
}

func recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if p := recover(); p != nil {
				if p == http.ErrAbortHandler {
					panic(p)
				}
				logging.FromContext(r.Context()).Error("panic serving request", "panic", p, "stack", string(debug.Stack()))
				httpx.Error(w, r, apperr.Internal())
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
		if strings.HasPrefix(r.URL.Path, "/api/") {
			h.Set("Cache-Control", "no-store")
		}
		next.ServeHTTP(w, r)
	})
}

func slogLevel(status int) slog.Level {
	switch {
	case status >= 500:
		return slog.LevelError
	case status >= 400:
		return slog.LevelWarn
	default:
		return slog.LevelInfo
	}
}
