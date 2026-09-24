// Package admin is the read-only, installation-wide view for the people who
// run DBVault (INSTANCE_ADMIN_EMAILS). It deliberately exposes no secrets
// and no actions: no credentials, storage config, backup downloads or
// restores, and no writes of any kind. Opening an organization is recorded
// in that organization's own audit log so its members can see it happened.
package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/dbvault/dbvault/backend/internal/apperr"
	"github.com/dbvault/dbvault/backend/internal/audit"
	"github.com/dbvault/dbvault/backend/internal/db"
	"github.com/dbvault/dbvault/backend/internal/httpx"
	"github.com/dbvault/dbvault/backend/internal/reqctx"
	"github.com/dbvault/dbvault/backend/internal/worker"
)

// viewAuditWindow collapses repeated views of one organization by one admin
// (page refreshes, refetches) into a single audit entry.
const viewAuditWindow = 30 * time.Minute

type Service struct {
	Pool   *pgxpool.Pool
	Redis  *redis.Client
	emails map[string]bool
}

func New(pool *pgxpool.Pool, rdb *redis.Client, emails []string) *Service {
	s := &Service{Pool: pool, Redis: rdb, emails: map[string]bool{}}
	for _, e := range emails {
		s.emails[strings.ToLower(strings.TrimSpace(e))] = true
	}
	return s
}

// IsAdmin reports whether p may use the admin area. Only browser sessions
// qualify: a leaked API token must never grant installation-wide access.
func (s *Service) IsAdmin(p reqctx.Principal) bool {
	return p.SessionID != "" && s.emails[strings.ToLower(p.Email)]
}

// Routes mounts the admin API. Everyone else gets 404 so the area can't be
// discovered, and only GET is ever accepted.
func (s *Service) Routes(r chi.Router) {
	r.Route("/admin", func(r chi.Router) {
		r.Use(s.require)
		r.Get("/overview", s.handleOverview)
		r.Get("/organizations", s.handleOrganizations)
		r.Get("/organizations/{id}", s.handleOrganization)
		r.Get("/users", s.handleUsers)
	})
}

func (s *Service) require(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, ok := reqctx.PrincipalFrom(r.Context())
		if !ok || !s.IsAdmin(p) || r.Method != http.MethodGet {
			httpx.Error(w, r, apperr.NotFound("Endpoint"))
			return
		}
		next.ServeHTTP(w, r)
	})
}

type Overview struct {
	Organizations   int               `json:"organizations"`
	Users           int               `json:"users"`
	Databases       int               `json:"databases"`
	Backups         int               `json:"backups"`
	StorageBytes    int64             `json:"storage_bytes"`
	FailedBackups7d int               `json:"failed_backups_7d"`
	ActiveJobs      int               `json:"active_jobs"`
	Workers         []worker.Presence `json:"workers"`
}

func (s *Service) handleOverview(w http.ResponseWriter, r *http.Request) {
	var o Overview
	err := s.Pool.QueryRow(r.Context(), `SELECT
		(SELECT count(*) FROM organizations),
		(SELECT count(*) FROM users),
		(SELECT count(*) FROM databases WHERE deleted_at IS NULL),
		(SELECT count(*) FROM backups WHERE deleted_at IS NULL AND status = 'completed'),
		(SELECT coalesce(sum(size_bytes), 0) FROM backups WHERE deleted_at IS NULL AND status = 'completed'),
		(SELECT count(*) FROM backups WHERE status = 'failed' AND created_at > now() - interval '7 days'),
		(SELECT count(*) FROM jobs WHERE status IN ('queued', 'running'))`).
		Scan(&o.Organizations, &o.Users, &o.Databases, &o.Backups, &o.StorageBytes, &o.FailedBackups7d, &o.ActiveJobs)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	if o.Workers, err = worker.ListPresence(r.Context(), s.Redis); err != nil {
		httpx.Error(w, r, err)
		return
	}
	if o.Workers == nil {
		o.Workers = []worker.Presence{}
	}
	httpx.JSON(w, http.StatusOK, o)
}

type OrgSummary struct {
	ID              string     `json:"id"`
	Name            string     `json:"name"`
	Slug            string     `json:"slug"`
	OwnerEmail      *string    `json:"owner_email"`
	Members         int        `json:"members"`
	Databases       int        `json:"databases"`
	Backups         int        `json:"backups"`
	StorageBytes    int64      `json:"storage_bytes"`
	LastBackupAt    *time.Time `json:"last_backup_at"`
	FailedBackups7d int        `json:"failed_backups_7d"`
	CreatedAt       time.Time  `json:"created_at"`
}

const orgSummaryCols = `o.id, o.name, o.slug,
	(SELECT u.email::text FROM organization_members m JOIN users u ON u.id = m.user_id
	  WHERE m.organization_id = o.id AND m.role = 'owner' ORDER BY m.created_at LIMIT 1),
	(SELECT count(*) FROM organization_members m WHERE m.organization_id = o.id),
	(SELECT count(*) FROM databases d WHERE d.organization_id = o.id AND d.deleted_at IS NULL),
	(SELECT count(*) FROM backups b WHERE b.organization_id = o.id AND b.deleted_at IS NULL AND b.status = 'completed'),
	(SELECT coalesce(sum(b.size_bytes), 0) FROM backups b WHERE b.organization_id = o.id AND b.deleted_at IS NULL AND b.status = 'completed'),
	(SELECT max(b.completed_at) FROM backups b WHERE b.organization_id = o.id AND b.status = 'completed'),
	(SELECT count(*) FROM backups b WHERE b.organization_id = o.id AND b.status = 'failed' AND b.created_at > now() - interval '7 days'),
	o.created_at`

func scanOrgSummary(row pgx.Row) (OrgSummary, error) {
	var o OrgSummary
	err := row.Scan(&o.ID, &o.Name, &o.Slug, &o.OwnerEmail, &o.Members, &o.Databases, &o.Backups, &o.StorageBytes,
		&o.LastBackupAt, &o.FailedBackups7d, &o.CreatedAt)
	return o, err
}

func (s *Service) handleOrganizations(w http.ResponseWriter, r *http.Request) {
	rows, err := s.Pool.Query(r.Context(), `SELECT `+orgSummaryCols+` FROM organizations o ORDER BY o.created_at DESC`)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (OrgSummary, error) { return scanOrgSummary(r) })
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	if out == nil {
		out = []OrgSummary{}
	}
	httpx.JSON(w, http.StatusOK, out)
}

type Member struct {
	UserID      string     `json:"user_id"`
	Name        string     `json:"name"`
	Email       string     `json:"email"`
	Role        string     `json:"role"`
	LastLoginAt *time.Time `json:"last_login_at"`
	JoinedAt    time.Time  `json:"joined_at"`
}

// Database leaves out username, password, TLS settings and certificates.
type Database struct {
	ID               string     `json:"id"`
	Name             string     `json:"name"`
	Engine           string     `json:"engine"`
	Host             string     `json:"host"`
	Port             int        `json:"port"`
	Database         string     `json:"database"`
	Version          *string    `json:"version"`
	SizeBytes        *int64     `json:"size_bytes"`
	LastTestOK       *bool      `json:"last_test_ok"`
	LastBackupAt     *time.Time `json:"last_backup_at"`
	LastBackupStatus *string    `json:"last_backup_status"`
	CreatedAt        time.Time  `json:"created_at"`
}

// Storage leaves out config (endpoints, buckets, paths) and credentials.
type Storage struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	Type       string    `json:"type"`
	IsDefault  bool      `json:"is_default"`
	LastTestOK *bool     `json:"last_test_ok"`
	UsedBytes  int64     `json:"used_bytes"`
	CreatedAt  time.Time `json:"created_at"`
}

type Schedule struct {
	ID           string     `json:"id"`
	Name         string     `json:"name"`
	DatabaseName string     `json:"database_name"`
	Cron         string     `json:"cron_expression"`
	Timezone     string     `json:"timezone"`
	Enabled      bool       `json:"enabled"`
	NextRunAt    *time.Time `json:"next_run_at"`
	LastRunAt    *time.Time `json:"last_run_at"`
}

type Backup struct {
	ID                 string     `json:"id"`
	DatabaseName       string     `json:"database_name"`
	Status             string     `json:"status"`
	Trigger            string     `json:"trigger"`
	SizeBytes          *int64     `json:"size_bytes"`
	VerificationStatus string     `json:"verification_status"`
	Error              *string    `json:"error"`
	DurationMS         *int64     `json:"duration_ms"`
	CreatedAt          time.Time  `json:"created_at"`
	CompletedAt        *time.Time `json:"completed_at"`
}

type OrgDetail struct {
	Organization OrgSummary `json:"organization"`
	Members   []Member    `json:"members"`
	Databases []Database  `json:"databases"`
	Storage   []Storage   `json:"storage"`
	Schedules []Schedule  `json:"schedules"`
	Backups   []Backup    `json:"backups"`
	AuditLogs []audit.Log `json:"audit_logs"`
}

func (s *Service) handleOrganization(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if !httpx.IsUUID(id) {
		httpx.Error(w, r, apperr.NotFound("Organization"))
		return
	}
	ctx := r.Context()
	sum, err := scanOrgSummary(s.Pool.QueryRow(ctx, `SELECT `+orgSummaryCols+` FROM organizations o WHERE o.id = $1`, id))
	if db.IsNotFound(err) {
		httpx.Error(w, r, apperr.NotFound("Organization"))
		return
	}
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	d := OrgDetail{Organization: sum}
	if err := s.loadDetail(ctx, id, &d); err != nil {
		httpx.Error(w, r, err)
		return
	}
	s.recordView(ctx, sum)
	httpx.JSON(w, http.StatusOK, d)
}

func (s *Service) loadDetail(ctx context.Context, orgID string, d *OrgDetail) error {
	var err error
	if d.Members, err = collect(ctx, s.Pool, func(r pgx.CollectableRow) (Member, error) {
		var m Member
		return m, r.Scan(&m.UserID, &m.Name, &m.Email, &m.Role, &m.LastLoginAt, &m.JoinedAt)
	}, `SELECT u.id, u.name, u.email::text, m.role, u.last_login_at, m.created_at FROM organization_members m
		JOIN users u ON u.id = m.user_id WHERE m.organization_id = $1
		ORDER BY array_position(ARRAY['owner','admin','member','viewer'], m.role), u.name`, orgID); err != nil {
		return err
	}
	if d.Databases, err = collect(ctx, s.Pool, func(r pgx.CollectableRow) (Database, error) {
		var x Database
		return x, r.Scan(&x.ID, &x.Name, &x.Engine, &x.Host, &x.Port, &x.Database, &x.Version, &x.SizeBytes, &x.LastTestOK,
			&x.LastBackupAt, &x.LastBackupStatus, &x.CreatedAt)
	}, `SELECT d.id, d.name, d.engine, d.host, d.port, d.database_name, d.pg_version, d.size_bytes, d.last_test_ok,
		lb.created_at, lb.status, d.created_at
		FROM databases d
		LEFT JOIN LATERAL (SELECT b.created_at, b.status FROM backups b WHERE b.database_id = d.id AND b.status <> 'deleted'
			ORDER BY b.created_at DESC LIMIT 1) lb ON true
		WHERE d.organization_id = $1 AND d.deleted_at IS NULL ORDER BY d.name`, orgID); err != nil {
		return err
	}
	if d.Storage, err = collect(ctx, s.Pool, func(r pgx.CollectableRow) (Storage, error) {
		var x Storage
		return x, r.Scan(&x.ID, &x.Name, &x.Type, &x.IsDefault, &x.LastTestOK, &x.UsedBytes, &x.CreatedAt)
	}, `SELECT sd.id, sd.name, sd.type, sd.is_default, sd.last_test_ok,
		(SELECT coalesce(sum(b.size_bytes), 0) FROM backups b WHERE b.storage_destination_id = sd.id AND b.deleted_at IS NULL AND b.status = 'completed'),
		sd.created_at
		FROM storage_destinations sd WHERE sd.organization_id = $1 AND sd.deleted_at IS NULL ORDER BY sd.name`, orgID); err != nil {
		return err
	}
	if d.Schedules, err = collect(ctx, s.Pool, func(r pgx.CollectableRow) (Schedule, error) {
		var x Schedule
		return x, r.Scan(&x.ID, &x.Name, &x.DatabaseName, &x.Cron, &x.Timezone, &x.Enabled, &x.NextRunAt, &x.LastRunAt)
	}, `SELECT s.id, s.name, d.name, s.cron_expression, s.timezone, s.enabled, s.next_run_at, s.last_run_at
		FROM backup_schedules s JOIN databases d ON d.id = s.database_id
		WHERE s.organization_id = $1 AND d.deleted_at IS NULL ORDER BY d.name, s.name`, orgID); err != nil {
		return err
	}
	if d.Backups, err = collect(ctx, s.Pool, func(r pgx.CollectableRow) (Backup, error) {
		var x Backup
		return x, r.Scan(&x.ID, &x.DatabaseName, &x.Status, &x.Trigger, &x.SizeBytes, &x.VerificationStatus, &x.Error,
			&x.DurationMS, &x.CreatedAt, &x.CompletedAt)
	}, `SELECT b.id, d.name, b.status, b.trigger, b.size_bytes, b.verification_status, b.error, b.duration_ms, b.created_at, b.completed_at
		FROM backups b JOIN databases d ON d.id = b.database_id
		WHERE b.organization_id = $1 AND b.status <> 'deleted' ORDER BY b.created_at DESC LIMIT 50`, orgID); err != nil {
		return err
	}
	d.AuditLogs, err = audit.List(ctx, s.Pool, orgID, audit.Filter{Limit: 50})
	return err
}

// recordView writes admin.organization_viewed to the organization's audit
// log, at most once per admin and organization per viewAuditWindow.
func (s *Service) recordView(ctx context.Context, o OrgSummary) {
	p, _ := reqctx.PrincipalFrom(ctx)
	fresh, err := s.Redis.SetNX(ctx, "admin-view:"+p.UserID+":"+o.ID, 1, viewAuditWindow).Result()
	if err == nil && !fresh {
		return
	}
	audit.MustRecord(ctx, s.Pool, audit.Entry{OrgID: o.ID, Action: audit.AdminOrgViewed, ResourceType: "organization", ResourceID: o.ID,
		Metadata: map[string]any{"name": o.Name}})
}

type UserOrg struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Role string `json:"role"`
}

type User struct {
	ID              string     `json:"id"`
	Name            string     `json:"name"`
	Email           string     `json:"email"`
	IsInstanceAdmin bool       `json:"is_instance_admin"`
	Organizations   []UserOrg  `json:"organizations"`
	LastLoginAt     *time.Time `json:"last_login_at"`
	CreatedAt       time.Time  `json:"created_at"`
}

func (s *Service) handleUsers(w http.ResponseWriter, r *http.Request) {
	out, err := collect(r.Context(), s.Pool, func(r pgx.CollectableRow) (User, error) {
		var u User
		var orgs []byte
		if err := r.Scan(&u.ID, &u.Name, &u.Email, &u.LastLoginAt, &u.CreatedAt, &orgs); err != nil {
			return u, err
		}
		u.IsInstanceAdmin = s.emails[strings.ToLower(u.Email)]
		return u, json.Unmarshal(orgs, &u.Organizations)
	}, `SELECT u.id, u.name, u.email::text, u.last_login_at, u.created_at,
		coalesce(json_agg(json_build_object('id', o.id, 'name', o.name, 'role', m.role) ORDER BY o.name) FILTER (WHERE o.id IS NOT NULL), '[]')
		FROM users u
		LEFT JOIN organization_members m ON m.user_id = u.id
		LEFT JOIN organizations o ON o.id = m.organization_id
		GROUP BY u.id ORDER BY u.created_at DESC`)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

// collect runs query and maps every row, returning an empty (not nil) slice.
func collect[T any](ctx context.Context, pool *pgxpool.Pool, fn pgx.RowToFunc[T], query string, args ...any) ([]T, error) {
	rows, err := pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	out, err := pgx.CollectRows(rows, fn)
	if out == nil {
		out = []T{}
	}
	return out, err
}
