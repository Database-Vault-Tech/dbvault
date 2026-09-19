// Package organizations implements multi-tenant organizations, membership,
// roles and invitations. Every protected resource belongs to exactly one
// organization and every API query is scoped by it.
package organizations

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dbvault/dbvault/backend/internal/apperr"
	"github.com/dbvault/dbvault/backend/internal/audit"
	"github.com/dbvault/dbvault/backend/internal/auth"
	"github.com/dbvault/dbvault/backend/internal/db"
	"github.com/dbvault/dbvault/backend/internal/encryption"
	"github.com/dbvault/dbvault/backend/internal/httpx"
	"github.com/dbvault/dbvault/backend/internal/logging"
	"github.com/dbvault/dbvault/backend/internal/reqctx"
)

// OrgHeader selects the organization a request targets (id or slug).
const OrgHeader = "X-DBVault-Org"

type Service struct {
	Pool   *pgxpool.Pool
	Keys   *encryption.KeyStore
	Hasher *auth.Hasher
	Mailer auth.Mailer
	AppURL string
}

type Organization struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Slug      string    `json:"slug"`
	Role      string    `json:"role,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

var slugStrip = regexp.MustCompile(`[^a-z0-9]+`)

func slugify(name string) string {
	s := strings.Trim(slugStrip.ReplaceAllString(strings.ToLower(name), "-"), "-")
	if len(s) > 40 {
		s = strings.Trim(s[:40], "-")
	}
	if s == "" {
		s = "org"
	}
	b := make([]byte, 3)
	_, _ = rand.Read(b)
	return s + "-" + hex.EncodeToString(b)
}

// Create makes an organization with ownerID as owner and provisions its
// backup encryption key, all inside tx.
func (s *Service) Create(ctx context.Context, tx pgx.Tx, ownerID, name string) (Organization, error) {
	var o Organization
	err := tx.QueryRow(ctx, `INSERT INTO organizations (name, slug) VALUES ($1, $2) RETURNING id, name, slug, created_at`,
		name, slugify(name)).Scan(&o.ID, &o.Name, &o.Slug, &o.CreatedAt)
	if err != nil {
		return o, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO organization_members (organization_id, user_id, role) VALUES ($1, $2, 'owner')`, o.ID, ownerID); err != nil {
		return o, err
	}
	if err := s.Keys.CreateForOrg(ctx, tx, o.ID); err != nil {
		return o, err
	}
	o.Role = auth.RoleOwner
	return o, audit.Record(ctx, tx, audit.Entry{OrgID: o.ID, Action: audit.OrgCreated, ResourceType: "organization", ResourceID: o.ID, Metadata: map[string]any{"name": name}})
}

// CreatePersonal is the auth.OrgCreator used during registration.
func (s *Service) CreatePersonal(ctx context.Context, tx pgx.Tx, userID, userName string) (string, error) {
	first := strings.Fields(userName)
	name := "My Organization"
	if len(first) > 0 {
		name = first[0] + "'s Organization"
	}
	o, err := s.Create(ctx, tx, userID, name)
	return o.ID, err
}

// ListForUser returns every organization the user belongs to.
func (s *Service) ListForUser(ctx context.Context, userID string) ([]Organization, error) {
	rows, err := s.Pool.Query(ctx, `SELECT o.id, o.name, o.slug, m.role, o.created_at FROM organizations o
		JOIN organization_members m ON m.organization_id = o.id WHERE m.user_id = $1 ORDER BY m.created_at, o.name`, userID)
	if err != nil {
		return nil, err
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Organization, error) {
		var o Organization
		err := r.Scan(&o.ID, &o.Name, &o.Slug, &o.Role, &o.CreatedAt)
		return o, err
	})
	if out == nil {
		out = []Organization{}
	}
	return out, err
}

// Middleware resolves the target organization and the caller's role. It
// uses the X-DBVault-Org header, falling back to the caller's first
// organization. Non-members get 404 so organization ids can't be probed.
func (s *Service) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, ok := reqctx.PrincipalFrom(r.Context())
		if !ok {
			httpx.Error(w, r, apperr.Unauthorized("Please sign in to continue."))
			return
		}
		sel := strings.TrimSpace(r.Header.Get(OrgHeader))
		var m reqctx.Membership
		var err error
		if sel == "" {
			err = s.Pool.QueryRow(r.Context(), `SELECT o.id, o.name, o.slug, m.role FROM organization_members m
				JOIN organizations o ON o.id = m.organization_id WHERE m.user_id = $1 ORDER BY m.created_at LIMIT 1`, p.UserID).
				Scan(&m.OrgID, &m.OrgName, &m.OrgSlug, &m.Role)
		} else {
			err = s.Pool.QueryRow(r.Context(), `SELECT o.id, o.name, o.slug, m.role FROM organization_members m
				JOIN organizations o ON o.id = m.organization_id
				WHERE m.user_id = $1 AND (o.id::text = $2 OR o.slug = $2)`, p.UserID, sel).
				Scan(&m.OrgID, &m.OrgName, &m.OrgSlug, &m.Role)
		}
		if db.IsNotFound(err) {
			httpx.Error(w, r, apperr.NotFound("Organization"))
			return
		}
		if err != nil {
			httpx.Error(w, r, err)
			return
		}
		ctx := reqctx.WithMembership(r.Context(), m)
		ctx = logging.WithLogger(ctx, logging.FromContext(ctx).With("organization_id", m.OrgID))
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// Member is an organization member as listed in team settings.
type Member struct {
	UserID    string     `json:"user_id"`
	Email     string     `json:"email"`
	Name      string     `json:"name"`
	Role      string     `json:"role"`
	LastLogin *time.Time `json:"last_login_at"`
	JoinedAt  time.Time  `json:"joined_at"`
}

func (s *Service) Members(ctx context.Context, orgID string) ([]Member, error) {
	rows, err := s.Pool.Query(ctx, `SELECT u.id, u.email, u.name, m.role, u.last_login_at, m.created_at
		FROM organization_members m JOIN users u ON u.id = m.user_id WHERE m.organization_id = $1
		ORDER BY CASE m.role WHEN 'owner' THEN 0 WHEN 'admin' THEN 1 WHEN 'member' THEN 2 ELSE 3 END, u.name`, orgID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Member, error) {
		var m Member
		err := r.Scan(&m.UserID, &m.Email, &m.Name, &m.Role, &m.LastLogin, &m.JoinedAt)
		return m, err
	})
}

// ChangeRole updates a member's role. Only owners may grant or revoke
// owner/admin, and the last owner can never be demoted.
func (s *Service) ChangeRole(ctx context.Context, actor reqctx.Membership, userID, role string) error {
	return db.WithTx(ctx, s.Pool, func(tx pgx.Tx) error {
		var current string
		err := tx.QueryRow(ctx, `SELECT role FROM organization_members WHERE organization_id = $1 AND user_id = $2 FOR UPDATE`, actor.OrgID, userID).Scan(&current)
		if db.IsNotFound(err) {
			return apperr.NotFound("Member")
		}
		if err != nil {
			return err
		}
		if actor.Role != auth.RoleOwner && (current == auth.RoleOwner || current == auth.RoleAdmin || role == auth.RoleOwner || role == auth.RoleAdmin) {
			return apperr.Forbidden("Only owners can grant or change owner and admin roles.")
		}
		if current == auth.RoleOwner && role != auth.RoleOwner {
			if err := ensureAnotherOwner(ctx, tx, actor.OrgID, userID); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(ctx, `UPDATE organization_members SET role = $3 WHERE organization_id = $1 AND user_id = $2`, actor.OrgID, userID, role); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Entry{OrgID: actor.OrgID, Action: audit.MemberRoleChanged, ResourceType: "user", ResourceID: userID,
			Metadata: map[string]any{"from": current, "to": role}})
	})
}

// RemoveMember removes a member (or lets a member leave).
func (s *Service) RemoveMember(ctx context.Context, actor reqctx.Membership, actorUserID, userID string) error {
	return db.WithTx(ctx, s.Pool, func(tx pgx.Tx) error {
		var current string
		err := tx.QueryRow(ctx, `SELECT role FROM organization_members WHERE organization_id = $1 AND user_id = $2 FOR UPDATE`, actor.OrgID, userID).Scan(&current)
		if db.IsNotFound(err) {
			return apperr.NotFound("Member")
		}
		if err != nil {
			return err
		}
		self := actorUserID == userID
		if !self {
			if !auth.RoleAtLeast(actor.Role, auth.RoleAdmin) {
				return apperr.Forbidden("Only admins can remove members.")
			}
			if (current == auth.RoleOwner || current == auth.RoleAdmin) && actor.Role != auth.RoleOwner {
				return apperr.Forbidden("Only owners can remove owners and admins.")
			}
		}
		if current == auth.RoleOwner {
			if err := ensureAnotherOwner(ctx, tx, actor.OrgID, userID); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(ctx, `DELETE FROM organization_members WHERE organization_id = $1 AND user_id = $2`, actor.OrgID, userID); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Entry{OrgID: actor.OrgID, Action: audit.MemberRemoved, ResourceType: "user", ResourceID: userID, Metadata: map[string]any{"role": current}})
	})
}

func ensureAnotherOwner(ctx context.Context, tx pgx.Tx, orgID, exceptUserID string) error {
	var n int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM organization_members WHERE organization_id = $1 AND role = 'owner' AND user_id <> $2`, orgID, exceptUserID).Scan(&n); err != nil {
		return err
	}
	if n == 0 {
		return apperr.Conflict("An organization must always have at least one owner. Promote another member to owner first.")
	}
	return nil
}

// Invitation is a pending invite.
type Invitation struct {
	ID        string     `json:"id"`
	Email     string     `json:"email"`
	Role      string     `json:"role"`
	InvitedBy *string    `json:"invited_by"`
	ExpiresAt time.Time  `json:"expires_at"`
	Accepted  *time.Time `json:"accepted_at"`
	CreatedAt time.Time  `json:"created_at"`
	// URL is only populated when the invitation is created.
	URL string `json:"url,omitempty"`
}

const inviteTTL = 7 * 24 * time.Hour

// Invite creates an invitation and emails it when SMTP is configured. The
// accept URL is returned once so admins can share it directly.
func (s *Service) Invite(ctx context.Context, actor reqctx.Membership, inviter reqctx.Principal, email, role string) (Invitation, error) {
	if role == auth.RoleAdmin && actor.Role != auth.RoleOwner {
		return Invitation{}, apperr.Forbidden("Only owners can invite admins.")
	}
	var exists bool
	if err := s.Pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM organization_members m JOIN users u ON u.id = m.user_id
		WHERE m.organization_id = $1 AND u.email = $2)`, actor.OrgID, email).Scan(&exists); err != nil {
		return Invitation{}, err
	}
	if exists {
		return Invitation{}, apperr.Validation(map[string]string{"email": "This person is already a member."})
	}
	token, hash, err := s.Hasher.NewToken(auth.InviteTokenPrefix)
	if err != nil {
		return Invitation{}, err
	}
	var inv Invitation
	err = db.WithTx(ctx, s.Pool, func(tx pgx.Tx) error {
		// Replace any pending invitation for the same email.
		if _, err := tx.Exec(ctx, `DELETE FROM organization_invitations WHERE organization_id = $1 AND email = $2 AND accepted_at IS NULL`, actor.OrgID, email); err != nil {
			return err
		}
		err := tx.QueryRow(ctx, `INSERT INTO organization_invitations (organization_id, email, role, token_hash, invited_by, expires_at)
			VALUES ($1, $2, $3, $4, $5, $6) RETURNING id, email, role, invited_by, expires_at, accepted_at, created_at`,
			actor.OrgID, email, role, hash, inviter.UserID, time.Now().Add(inviteTTL)).
			Scan(&inv.ID, &inv.Email, &inv.Role, &inv.InvitedBy, &inv.ExpiresAt, &inv.Accepted, &inv.CreatedAt)
		if err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Entry{OrgID: actor.OrgID, Action: audit.MemberInvited, ResourceType: "invitation", ResourceID: inv.ID,
			Metadata: map[string]any{"email": email, "role": role}})
	})
	if err != nil {
		return inv, err
	}
	inv.URL = s.AppURL + "/invite?token=" + token
	if s.Mailer != nil && s.Mailer.Configured() {
		body := inviter.Name + " invited you to join " + actor.OrgName + " on DBVault as " + role + ".\n\n" +
			"Accept the invitation (valid for 7 days):\n" + inv.URL + "\n"
		go func() {
			sendCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if err := s.Mailer.Send(sendCtx, email, "You're invited to "+actor.OrgName+" on DBVault", body); err != nil {
				logging.FromContext(ctx).Error("failed to send invitation email", "error", err.Error())
			}
		}()
	}
	return inv, nil
}

func (s *Service) Invitations(ctx context.Context, orgID string) ([]Invitation, error) {
	rows, err := s.Pool.Query(ctx, `SELECT id, email, role, invited_by, expires_at, accepted_at, created_at FROM organization_invitations
		WHERE organization_id = $1 AND accepted_at IS NULL AND expires_at > now() ORDER BY created_at DESC`, orgID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Invitation, error) {
		var i Invitation
		err := r.Scan(&i.ID, &i.Email, &i.Role, &i.InvitedBy, &i.ExpiresAt, &i.Accepted, &i.CreatedAt)
		return i, err
	})
}

func (s *Service) RevokeInvitation(ctx context.Context, orgID, id string) error {
	tag, err := s.Pool.Exec(ctx, `DELETE FROM organization_invitations WHERE id = $1 AND organization_id = $2 AND accepted_at IS NULL`, id, orgID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return apperr.NotFound("Invitation")
	}
	audit.MustRecord(ctx, s.Pool, audit.Entry{OrgID: orgID, Action: audit.InvitationRevoked, ResourceType: "invitation", ResourceID: id})
	return nil
}

// AcceptInvitation adds the caller to the inviting organization. The
// invitation email must match the caller's account email.
func (s *Service) AcceptInvitation(ctx context.Context, p reqctx.Principal, token string) (Organization, error) {
	if !strings.HasPrefix(token, auth.InviteTokenPrefix) {
		return Organization{}, apperr.BadRequest("This invitation is invalid or has expired.")
	}
	var org Organization
	err := db.WithTx(ctx, s.Pool, func(tx pgx.Tx) error {
		var invID, email, role string
		err := tx.QueryRow(ctx, `SELECT i.id, i.email, i.role, o.id, o.name, o.slug, o.created_at FROM organization_invitations i
			JOIN organizations o ON o.id = i.organization_id
			WHERE i.token_hash = $1 AND i.accepted_at IS NULL AND i.expires_at > now() FOR UPDATE OF i`, s.Hasher.Hash(token)).
			Scan(&invID, &email, &role, &org.ID, &org.Name, &org.Slug, &org.CreatedAt)
		if db.IsNotFound(err) {
			return apperr.BadRequest("This invitation is invalid or has expired.")
		}
		if err != nil {
			return err
		}
		if !strings.EqualFold(email, p.Email) {
			return apperr.Forbidden("This invitation was sent to " + email + ". Sign in with that account to accept it.")
		}
		if _, err := tx.Exec(ctx, `INSERT INTO organization_members (organization_id, user_id, role) VALUES ($1, $2, $3)
			ON CONFLICT (organization_id, user_id) DO NOTHING`, org.ID, p.UserID, role); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE organization_invitations SET accepted_at = now() WHERE id = $1`, invID); err != nil {
			return err
		}
		org.Role = role
		return audit.Record(ctx, tx, audit.Entry{OrgID: org.ID, Action: audit.MemberJoined, ResourceType: "user", ResourceID: p.UserID, Metadata: map[string]any{"role": role}})
	})
	return org, err
}

// ExportRecoveryKey returns the organization's backup private key (age
// identity) after re-verifying the owner's password. With this key, backups
// can be decrypted with the standard `age` tool even if DBVault is gone.
func (s *Service) ExportRecoveryKey(ctx context.Context, m reqctx.Membership, p reqctx.Principal, password string) (map[string]string, error) {
	var hash string
	if err := s.Pool.QueryRow(ctx, `SELECT password_hash FROM users WHERE id = $1`, p.UserID).Scan(&hash); err != nil {
		return nil, err
	}
	ok, err := auth.VerifyPassword(password, hash)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, apperr.Validation(map[string]string{"password": "Password is incorrect."})
	}
	key, identity, err := s.Keys.ActiveIdentity(ctx, m.OrgID)
	if err != nil {
		return nil, err
	}
	audit.MustRecord(ctx, s.Pool, audit.Entry{OrgID: m.OrgID, Action: audit.RecoveryKeyExported, ResourceType: "encryption_key", ResourceID: key.ID})
	return map[string]string{"key_id": key.ID, "public_key": key.PublicKey, "identity": identity, "algorithm": "age-x25519"}, nil
}
