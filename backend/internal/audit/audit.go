// Package audit records security-relevant actions in an append-only log.
package audit

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dbvault/dbvault/backend/internal/db"
	"github.com/dbvault/dbvault/backend/internal/logging"
	"github.com/dbvault/dbvault/backend/internal/reqctx"
)

// Actions.
const (
	UserRegistered         = "user.registered"
	UserLoggedIn           = "user.logged_in"
	UserLoginFailed        = "user.login_failed"
	UserLoggedOut          = "user.logged_out"
	UserPasswordChanged    = "user.password_changed"
	UserPasswordReset      = "user.password_reset"
	UserTwoFactorEnabled   = "user.2fa_enabled"
	UserTwoFactorDisabled  = "user.2fa_disabled"
	UserTwoFactorFailed    = "user.2fa_failed"
	UserRecoveryCodeUsed   = "user.recovery_code_used"
	UserRecoveryCodesReset = "user.recovery_codes_regenerated"
	APITokenCreated        = "api_token.created"
	APITokenRevoked        = "api_token.revoked"
	OrgCreated             = "organization.created"
	OrgUpdated             = "organization.updated"
	MemberInvited          = "member.invited"
	MemberJoined           = "member.joined"
	MemberRoleChanged      = "member.role_changed"
	MemberRemoved          = "member.removed"
	InvitationRevoked      = "invitation.revoked"
	RecoveryKeyExported    = "encryption_key.exported"
	DatabaseCreated        = "database.created"
	DatabaseUpdated        = "database.updated"
	DatabaseDeleted        = "database.deleted"
	DatabaseTested         = "database.connection_tested"
	StorageCreated         = "storage.created"
	StorageUpdated         = "storage.updated"
	StorageDeleted         = "storage.deleted"
	ScheduleCreated        = "schedule.created"
	ScheduleUpdated        = "schedule.updated"
	ScheduleDeleted        = "schedule.deleted"
	BackupStarted          = "backup.started"
	BackupCompleted        = "backup.completed"
	BackupFailed           = "backup.failed"
	BackupDeleted          = "backup.deleted"
	BackupDownloaded       = "backup.downloaded"
	BackupVerifyRequested  = "backup.verification_requested"
	BackupVerified         = "backup.verified"
	BackupVerifyFailed     = "backup.verification_failed"
	BackupExpired          = "backup.expired"
	RestoreStarted         = "restore.started"
	RestoreCompleted       = "restore.completed"
	RestoreFailed          = "restore.failed"
	JobCancelled           = "job.cancelled"
	NotificationCreated    = "notification.created"
	NotificationUpdated    = "notification.updated"
	NotificationDeleted    = "notification.deleted"
	NotificationTestedSent = "notification.test_sent"
	AdminOrgViewed         = "admin.organization_viewed"
)

// Entry is one action to record.
type Entry struct {
	OrgID        string
	Action       string
	ResourceType string
	ResourceID   string
	Metadata     map[string]any
	// ActorEmail overrides the actor label (e.g. failed logins).
	ActorEmail string
}

// Record writes an audit entry using q (pass a transaction to make the
// entry atomic with the change it describes). The actor, IP and user agent
// come from the request context; entries without a principal are recorded
// as system actions. Sensitive metadata keys are always dropped.
func Record(ctx context.Context, q db.DB, e Entry) error {
	meta := map[string]any{}
	for k, v := range e.Metadata {
		if !logging.IsSensitiveKey(k) {
			meta[k] = v
		}
	}
	metaJSON, err := json.Marshal(meta)
	if err != nil {
		return err
	}
	actorType := "system"
	var actorID, actorEmail *string
	if p, ok := reqctx.PrincipalFrom(ctx); ok {
		actorType = p.ActorType()
		actorID, actorEmail = &p.UserID, &p.Email
	}
	if e.ActorEmail != "" {
		actorEmail = &e.ActorEmail
	}
	m := reqctx.MetaFrom(ctx)
	_, err = q.Exec(ctx, `INSERT INTO audit_logs (organization_id, actor_type, actor_id, actor_email, action, resource_type, resource_id, metadata, ip_address, user_agent)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
		nilIfEmpty(e.OrgID), actorType, actorID, actorEmail, e.Action, nilIfEmpty(e.ResourceType), nilIfEmpty(e.ResourceID), metaJSON,
		nilIfEmpty(m.IP), nilIfEmpty(truncate(m.UserAgent, 512)))
	return err
}

// MustRecord records an entry and logs (rather than propagates) failures,
// for call sites where the action already happened.
func MustRecord(ctx context.Context, q db.DB, e Entry) {
	if err := Record(ctx, q, e); err != nil {
		slog.Error("failed to write audit log", "action", e.Action, "error", err.Error())
	}
}

// Log is an audit entry as returned by the API.
type Log struct {
	ID           string          `json:"id"`
	ActorType    string          `json:"actor_type"`
	ActorID      *string         `json:"actor_id"`
	ActorEmail   *string         `json:"actor_email"`
	Action       string          `json:"action"`
	ResourceType *string         `json:"resource_type"`
	ResourceID   *string         `json:"resource_id"`
	Metadata     json.RawMessage `json:"metadata"`
	IPAddress    *string         `json:"ip_address"`
	UserAgent    *string         `json:"user_agent"`
	CreatedAt    time.Time       `json:"created_at"`
}

// Filter narrows List. Before is a cursor (created_at of the last row seen).
type Filter struct {
	Action       string
	ResourceType string
	ResourceID   string
	Before       *time.Time
	Limit        int
}

// List returns audit entries newest first.
func List(ctx context.Context, pool *pgxpool.Pool, orgID string, f Filter) ([]Log, error) {
	if f.Limit <= 0 || f.Limit > 200 {
		f.Limit = 50
	}
	rows, err := pool.Query(ctx, `SELECT id, actor_type, actor_id, actor_email, action, resource_type, resource_id, metadata, ip_address, user_agent, created_at
		FROM audit_logs WHERE organization_id = $1
		  AND ($2 = '' OR action LIKE $2 || '%')
		  AND ($3 = '' OR resource_type = $3)
		  AND ($4 = '' OR resource_id::text = $4)
		  AND ($5::timestamptz IS NULL OR created_at < $5)
		ORDER BY created_at DESC LIMIT $6`, orgID, f.Action, f.ResourceType, f.ResourceID, f.Before, f.Limit)
	if err != nil {
		return nil, err
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Log, error) {
		var l Log
		err := r.Scan(&l.ID, &l.ActorType, &l.ActorID, &l.ActorEmail, &l.Action, &l.ResourceType, &l.ResourceID, &l.Metadata, &l.IPAddress, &l.UserAgent, &l.CreatedAt)
		return l, err
	})
	if out == nil {
		out = []Log{}
	}
	return out, err
}

func nilIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}
