package scheduler

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dbvault/dbvault/backend/internal/apperr"
	"github.com/dbvault/dbvault/backend/internal/audit"
	"github.com/dbvault/dbvault/backend/internal/auth"
	"github.com/dbvault/dbvault/backend/internal/backups"
	"github.com/dbvault/dbvault/backend/internal/compress"
	"github.com/dbvault/dbvault/backend/internal/db"
	"github.com/dbvault/dbvault/backend/internal/httpx"
	"github.com/dbvault/dbvault/backend/internal/reqctx"
	"github.com/dbvault/dbvault/backend/internal/retention"
	"github.com/dbvault/dbvault/backend/internal/validate"
)

type Schedule struct {
	ID                   string           `json:"id"`
	DatabaseID           string           `json:"database_id"`
	DatabaseName         string           `json:"database_name"`
	StorageDestinationID string           `json:"storage_destination_id"`
	StorageName          string           `json:"storage_name"`
	StorageType          string           `json:"storage_type"`
	Name                 string           `json:"name"`
	Preset               string           `json:"preset"`
	CronExpression       string           `json:"cron_expression"`
	Timezone             string           `json:"timezone"`
	Description          string           `json:"description"`
	Enabled              bool             `json:"enabled"`
	Compression          string           `json:"compression"`
	Encryption           bool             `json:"encryption"`
	Retention            retention.Policy `json:"retention"`
	VerifyAfterBackup    bool             `json:"verify_after_backup"`
	NextRunAt            *time.Time       `json:"next_run_at"`
	LastRunAt            *time.Time       `json:"last_run_at"`
	LastBackupStatus     *string          `json:"last_backup_status"`
	BackupCount          int              `json:"backup_count"`
	CreatedAt            time.Time        `json:"created_at"`
	UpdatedAt            time.Time        `json:"updated_at"`
}

type Service struct {
	Pool    *pgxpool.Pool
	Backups *backups.Service
}

const selectSchedule = `SELECT s.id, s.database_id, d.name, s.storage_destination_id, sd.name, sd.type, s.name, s.preset, s.cron_expression, s.timezone,
	s.enabled, s.compression, s.encryption, s.retention_daily, s.retention_weekly, s.retention_monthly, s.verify_after_backup,
	s.next_run_at, s.last_run_at,
	(SELECT status FROM backups b WHERE b.schedule_id = s.id ORDER BY created_at DESC LIMIT 1),
	(SELECT count(*) FROM backups b WHERE b.schedule_id = s.id AND b.status = 'completed'),
	s.created_at, s.updated_at
	FROM backup_schedules s
	JOIN databases d ON d.id = s.database_id
	JOIN storage_destinations sd ON sd.id = s.storage_destination_id`

func scanSchedule(row pgx.Row) (Schedule, error) {
	var s Schedule
	err := row.Scan(&s.ID, &s.DatabaseID, &s.DatabaseName, &s.StorageDestinationID, &s.StorageName, &s.StorageType, &s.Name, &s.Preset,
		&s.CronExpression, &s.Timezone, &s.Enabled, &s.Compression, &s.Encryption, &s.Retention.Daily, &s.Retention.Weekly,
		&s.Retention.Monthly, &s.VerifyAfterBackup, &s.NextRunAt, &s.LastRunAt, &s.LastBackupStatus, &s.BackupCount, &s.CreatedAt, &s.UpdatedAt)
	s.Description = Describe(s.Preset, s.CronExpression, s.Timezone)
	return s, err
}

func (s *Service) List(ctx context.Context, orgID, databaseID string) ([]Schedule, error) {
	rows, err := s.Pool.Query(ctx, selectSchedule+` WHERE s.organization_id = $1 AND d.deleted_at IS NULL AND ($2 = '' OR s.database_id::text = $2)
		ORDER BY lower(d.name), s.created_at`, orgID, databaseID)
	if err != nil {
		return nil, err
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Schedule, error) { return scanSchedule(r) })
	if out == nil {
		out = []Schedule{}
	}
	return out, err
}

func (s *Service) Get(ctx context.Context, orgID, id string) (Schedule, error) {
	sc, err := scanSchedule(s.Pool.QueryRow(ctx, selectSchedule+` WHERE s.id = $1 AND s.organization_id = $2`, id, orgID))
	if db.IsNotFound(err) {
		return sc, apperr.NotFound("Schedule")
	}
	return sc, err
}

// Input is the create/update payload.
type Input struct {
	DatabaseID           string           `json:"database_id"`
	StorageDestinationID string           `json:"storage_destination_id"`
	Name                 string           `json:"name"`
	Preset               string           `json:"preset"`
	CronExpression       string           `json:"cron_expression"`
	Timezone             string           `json:"timezone"`
	Enabled              *bool            `json:"enabled"`
	Compression          string           `json:"compression"`
	Encryption           *bool            `json:"encryption"`
	Retention            retention.Policy `json:"retention"`
	VerifyAfterBackup    bool             `json:"verify_after_backup"`
}

func (in *Input) normalize() error {
	in.Name = strings.TrimSpace(in.Name)
	if in.Timezone == "" {
		in.Timezone = "UTC"
	}
	if in.Compression == "" {
		in.Compression = compress.Zstd
	}
	v := validate.New()
	v.Check(httpx.IsUUID(in.DatabaseID), "database_id", "Select a database.")
	v.Check(httpx.IsUUID(in.StorageDestinationID), "storage_destination_id", "Select a storage destination.")
	v.MaxLen("name", in.Name, 80)
	v.OneOf("preset", in.Preset, "hourly", "every_6_hours", "daily", "weekly", "custom")
	v.OneOf("compression", in.Compression, compress.Zstd, compress.Gzip, compress.None)
	v.Range("retention.daily", in.Retention.Daily, 0, 3650)
	v.Range("retention.weekly", in.Retention.Weekly, 0, 520)
	v.Range("retention.monthly", in.Retention.Monthly, 0, 240)
	if !v.Has("preset") {
		expr, err := Resolve(in.Preset, in.CronExpression)
		if err == nil {
			err = Validate(expr, in.Timezone)
		}
		if err != nil {
			field := "cron_expression"
			if strings.Contains(err.Error(), "timezone") {
				field = "timezone"
			}
			v.Check(false, field, strings.ToUpper(err.Error()[:1])+err.Error()[1:]+".")
		}
		in.CronExpression = expr
	}
	if in.Name == "" {
		in.Name = Describe(in.Preset, in.CronExpression, in.Timezone)
	}
	return v.Err()
}

func nextRun(expr, tz string) (time.Time, error) {
	sched, loc, err := Parse(expr, tz)
	if err != nil {
		return time.Time{}, err
	}
	return NextRuns(sched, loc, time.Now(), 1)[0], nil
}

func (s *Service) checkRefs(ctx context.Context, orgID string, in Input) error {
	var ok bool
	if err := s.Pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM databases WHERE id = $1 AND organization_id = $2 AND deleted_at IS NULL)`, in.DatabaseID, orgID).Scan(&ok); err != nil {
		return err
	}
	if !ok {
		return apperr.Validation(map[string]string{"database_id": "Unknown database."})
	}
	if err := s.Pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM storage_destinations WHERE id = $1 AND organization_id = $2 AND deleted_at IS NULL)`, in.StorageDestinationID, orgID).Scan(&ok); err != nil {
		return err
	}
	if !ok {
		return apperr.Validation(map[string]string{"storage_destination_id": "Unknown storage destination."})
	}
	return nil
}

func (s *Service) Create(ctx context.Context, orgID, userID string, in Input) (Schedule, error) {
	if err := s.checkRefs(ctx, orgID, in); err != nil {
		return Schedule{}, err
	}
	next, err := nextRun(in.CronExpression, in.Timezone)
	if err != nil {
		return Schedule{}, err
	}
	enabled := in.Enabled == nil || *in.Enabled
	encryption := in.Encryption == nil || *in.Encryption
	var id string
	err = db.WithTx(ctx, s.Pool, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx, `INSERT INTO backup_schedules (organization_id, database_id, storage_destination_id, name, preset, cron_expression, timezone,
			enabled, compression, encryption, retention_daily, retention_weekly, retention_monthly, verify_after_backup, next_run_at, created_by)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16) RETURNING id`,
			orgID, in.DatabaseID, in.StorageDestinationID, in.Name, in.Preset, in.CronExpression, in.Timezone, enabled, in.Compression, encryption,
			in.Retention.Daily, in.Retention.Weekly, in.Retention.Monthly, in.VerifyAfterBackup, next, userID).Scan(&id)
		if err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Entry{OrgID: orgID, Action: audit.ScheduleCreated, ResourceType: "schedule", ResourceID: id,
			Metadata: map[string]any{"name": in.Name, "cron": in.CronExpression, "timezone": in.Timezone, "retention": in.Retention}})
	})
	if err != nil {
		return Schedule{}, err
	}
	return s.Get(ctx, orgID, id)
}

func (s *Service) Update(ctx context.Context, orgID, id string, in Input) (Schedule, error) {
	if _, err := s.Get(ctx, orgID, id); err != nil {
		return Schedule{}, err
	}
	if err := s.checkRefs(ctx, orgID, in); err != nil {
		return Schedule{}, err
	}
	next, err := nextRun(in.CronExpression, in.Timezone)
	if err != nil {
		return Schedule{}, err
	}
	enabled := in.Enabled == nil || *in.Enabled
	encryption := in.Encryption == nil || *in.Encryption
	_, err = s.Pool.Exec(ctx, `UPDATE backup_schedules SET database_id = $3, storage_destination_id = $4, name = $5, preset = $6, cron_expression = $7,
		timezone = $8, enabled = $9, compression = $10, encryption = $11, retention_daily = $12, retention_weekly = $13, retention_monthly = $14,
		verify_after_backup = $15, next_run_at = $16 WHERE id = $1 AND organization_id = $2`,
		id, orgID, in.DatabaseID, in.StorageDestinationID, in.Name, in.Preset, in.CronExpression, in.Timezone, enabled, in.Compression, encryption,
		in.Retention.Daily, in.Retention.Weekly, in.Retention.Monthly, in.VerifyAfterBackup, next)
	if err != nil {
		return Schedule{}, err
	}
	audit.MustRecord(ctx, s.Pool, audit.Entry{OrgID: orgID, Action: audit.ScheduleUpdated, ResourceType: "schedule", ResourceID: id,
		Metadata: map[string]any{"name": in.Name, "cron": in.CronExpression, "enabled": enabled, "retention": in.Retention}})
	return s.Get(ctx, orgID, id)
}

func (s *Service) Delete(ctx context.Context, orgID, id string) error {
	sc, err := s.Get(ctx, orgID, id)
	if err != nil {
		return err
	}
	return db.WithTx(ctx, s.Pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `DELETE FROM backup_schedules WHERE id = $1`, id); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Entry{OrgID: orgID, Action: audit.ScheduleDeleted, ResourceType: "schedule", ResourceID: id, Metadata: map[string]any{"name": sc.Name}})
	})
}

// RetentionPreview shows what the policy would keep and delete right now.
func (s *Service) RetentionPreview(ctx context.Context, orgID, id string) (map[string]any, error) {
	sc, err := s.Get(ctx, orgID, id)
	if err != nil {
		return nil, err
	}
	items, err := s.Backups.RetentionItems(ctx, id)
	if err != nil {
		return nil, err
	}
	loc, err := time.LoadLocation(sc.Timezone)
	if err != nil {
		loc = time.UTC
	}
	decisions := retention.Plan(items, sc.Retention, time.Now(), loc)
	keep, del := 0, 0
	for _, d := range decisions {
		if d.Keep {
			keep++
		} else {
			del++
		}
	}
	return map[string]any{"policy": sc.Retention, "enabled": sc.Retention.Enabled(), "summary": sc.Retention.String(),
		"keep": keep, "delete": del, "decisions": decisions}, nil
}

// Routes

func (s *Service) Routes(r chi.Router) {
	r.Get("/schedules", s.handleList)
	r.Post("/schedules", s.handleCreate)
	r.Get("/schedules/preview", s.handlePreview)
	r.Get("/schedules/{id}", s.handleGet)
	r.Patch("/schedules/{id}", s.handleUpdate)
	r.Delete("/schedules/{id}", s.handleDelete)
	r.Post("/schedules/{id}/run", s.handleRun)
	r.Get("/schedules/{id}/retention", s.handleRetention)
}

func idParam(w http.ResponseWriter, r *http.Request) (string, bool) {
	id := chi.URLParam(r, "id")
	if !httpx.IsUUID(id) {
		httpx.Error(w, r, apperr.NotFound("Schedule"))
		return "", false
	}
	return id, true
}

func (s *Service) handleList(w http.ResponseWriter, r *http.Request) {
	m, _ := reqctx.MembershipFrom(r.Context())
	dbID := r.URL.Query().Get("database_id")
	if dbID != "" && !httpx.IsUUID(dbID) {
		httpx.Error(w, r, apperr.BadRequest("Invalid database_id."))
		return
	}
	list, err := s.List(r.Context(), m.OrgID, dbID)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, list)
}

func (s *Service) handleGet(w http.ResponseWriter, r *http.Request) {
	m, _ := reqctx.MembershipFrom(r.Context())
	id, ok := idParam(w, r)
	if !ok {
		return
	}
	sc, err := s.Get(r.Context(), m.OrgID, id)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	sched, loc, err := Parse(sc.CronExpression, sc.Timezone)
	out := map[string]any{"schedule": sc}
	if err == nil {
		out["next_runs"] = NextRuns(sched, loc, time.Now(), 5)
	}
	httpx.JSON(w, http.StatusOK, out)
}

func (s *Service) handlePreview(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	preset := q.Get("preset")
	if preset == "" {
		preset = "custom"
	}
	tz := q.Get("timezone")
	if tz == "" {
		tz = "UTC"
	}
	expr, err := Resolve(preset, q.Get("cron"))
	if err == nil {
		err = Validate(expr, tz)
	}
	if err != nil {
		httpx.JSON(w, http.StatusOK, map[string]any{"valid": false, "error": err.Error()})
		return
	}
	sched, loc, _ := Parse(expr, tz)
	httpx.JSON(w, http.StatusOK, map[string]any{"valid": true, "cron_expression": expr, "description": Describe(preset, expr, tz),
		"next_runs": NextRuns(sched, loc, time.Now(), 5)})
}

func (s *Service) handleCreate(w http.ResponseWriter, r *http.Request) {
	m, err := auth.Require(r.Context(), auth.RoleAdmin)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	var in Input
	if err := httpx.Decode(w, r, &in); err != nil {
		httpx.Error(w, r, err)
		return
	}
	if err := in.normalize(); err != nil {
		httpx.Error(w, r, err)
		return
	}
	p, _ := reqctx.PrincipalFrom(r.Context())
	sc, err := s.Create(r.Context(), m.OrgID, p.UserID, in)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, sc)
}

func (s *Service) handleUpdate(w http.ResponseWriter, r *http.Request) {
	m, err := auth.Require(r.Context(), auth.RoleAdmin)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	id, ok := idParam(w, r)
	if !ok {
		return
	}
	var in Input
	if err := httpx.Decode(w, r, &in); err != nil {
		httpx.Error(w, r, err)
		return
	}
	if err := in.normalize(); err != nil {
		httpx.Error(w, r, err)
		return
	}
	sc, err := s.Update(r.Context(), m.OrgID, id, in)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, sc)
}

func (s *Service) handleDelete(w http.ResponseWriter, r *http.Request) {
	m, err := auth.Require(r.Context(), auth.RoleAdmin)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	id, ok := idParam(w, r)
	if !ok {
		return
	}
	if err := s.Delete(r.Context(), m.OrgID, id); err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.NoContent(w)
}

// handleRun starts an immediate backup with the schedule's settings. It is
// recorded as a manual backup, so retention never deletes it.
func (s *Service) handleRun(w http.ResponseWriter, r *http.Request) {
	m, err := auth.Require(r.Context(), auth.RoleMember)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	id, ok := idParam(w, r)
	if !ok {
		return
	}
	sc, err := s.Get(r.Context(), m.OrgID, id)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	p, _ := reqctx.PrincipalFrom(r.Context())
	enc := sc.Encryption
	q, err := s.Backups.CreateManual(r.Context(), m.OrgID, p.UserID, backups.CreateInput{
		DatabaseID: sc.DatabaseID, StorageDestinationID: sc.StorageDestinationID, Compression: sc.Compression, Encrypted: &enc})
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusAccepted, q)
}

func (s *Service) handleRetention(w http.ResponseWriter, r *http.Request) {
	m, _ := reqctx.MembershipFrom(r.Context())
	id, ok := idParam(w, r)
	if !ok {
		return
	}
	out, err := s.RetentionPreview(r.Context(), m.OrgID, id)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}
