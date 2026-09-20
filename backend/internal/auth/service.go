package auth

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dbvault/dbvault/backend/internal/apperr"
	"github.com/dbvault/dbvault/backend/internal/audit"
	"github.com/dbvault/dbvault/backend/internal/db"
	"github.com/dbvault/dbvault/backend/internal/reqctx"
)

const (
	SessionTTL       = 14 * 24 * time.Hour
	resetTokenTTL    = time.Hour
	sessionTouchStep = 5 * time.Minute
)

// Mailer sends transactional email (password resets, invitations).
type Mailer interface {
	Configured() bool
	Send(ctx context.Context, to, subject, textBody string) error
}

// OrgCreator creates a user's first organization inside the registration
// transaction (implemented by the organizations package).
type OrgCreator func(ctx context.Context, tx pgx.Tx, userID, userName string) (string, error)

type Service struct {
	Pool              *pgxpool.Pool
	Hasher            *Hasher
	Mailer            Mailer
	AppURL            string
	AllowRegistration bool
	CreateOrg         OrgCreator
}

type User struct {
	ID          string     `json:"id"`
	Email       string     `json:"email"`
	Name        string     `json:"name"`
	LastLoginAt *time.Time `json:"last_login_at"`
	CreatedAt   time.Time  `json:"created_at"`
}

// SessionResult is returned after a successful login or registration.
type SessionResult struct {
	User      User
	Token     string
	SessionID string
	CSRF      string
	ExpiresAt time.Time
}

func normalizeEmail(e string) string { return strings.ToLower(strings.TrimSpace(e)) }

// invited reports whether token is a live invitation addressed to email.
// Someone holding one has to be able to create the account it was sent to,
// otherwise closed registration makes invitations impossible to accept.
func (s *Service) invited(ctx context.Context, email, token string) bool {
	if token == "" || !strings.HasPrefix(token, InviteTokenPrefix) {
		return false
	}
	var n int
	err := s.Pool.QueryRow(ctx, `SELECT count(*) FROM organization_invitations
		WHERE token_hash = $1 AND lower(email) = $2 AND accepted_at IS NULL AND expires_at > now()`,
		s.Hasher.Hash(token), normalizeEmail(email)).Scan(&n)
	return err == nil && n > 0
}

// Register creates a user, their personal organization and a session.
// inviteToken may be empty; it only matters when registration is closed.
func (s *Service) Register(ctx context.Context, name, email, password, inviteToken string) (SessionResult, string, error) {
	if !s.AllowRegistration {
		var n int
		if err := s.Pool.QueryRow(ctx, `SELECT count(*) FROM users`).Scan(&n); err != nil {
			return SessionResult{}, "", err
		}
		// The very first account can always be created so a fresh install
		// is usable; after that, new users must be invited.
		if n > 0 && !s.invited(ctx, email, inviteToken) {
			return SessionResult{}, "", apperr.Forbidden("Registration is disabled on this DBVault instance. Ask an administrator for an invitation.")
		}
	}
	hash, err := HashPassword(password)
	if err != nil {
		return SessionResult{}, "", err
	}
	email = normalizeEmail(email)
	name = strings.TrimSpace(name)

	var user User
	var orgID string
	err = db.WithTx(ctx, s.Pool, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx, `INSERT INTO users (email, name, password_hash) VALUES ($1, $2, $3)
			RETURNING id, email, name, last_login_at, created_at`, email, name, hash).
			Scan(&user.ID, &user.Email, &user.Name, &user.LastLoginAt, &user.CreatedAt)
		if db.IsUniqueViolation(err) {
			return apperr.Validation(map[string]string{"email": "An account with this email already exists."})
		}
		if err != nil {
			return err
		}
		actx := reqctx.WithPrincipal(ctx, reqctx.Principal{UserID: user.ID, Email: user.Email, Name: user.Name})
		orgID, err = s.CreateOrg(actx, tx, user.ID, user.Name)
		if err != nil {
			return err
		}
		return audit.Record(actx, tx, audit.Entry{OrgID: orgID, Action: audit.UserRegistered, ResourceType: "user", ResourceID: user.ID})
	})
	if err != nil {
		return SessionResult{}, "", err
	}
	res, err := s.createSession(ctx, user)
	return res, orgID, err
}

// ErrInvalidCredentials is deliberately generic.
var ErrInvalidCredentials = apperr.Unauthorized("Invalid email or password.")

// Authenticate verifies credentials without creating a session.
func (s *Service) Authenticate(ctx context.Context, email, password string) (User, error) {
	email = normalizeEmail(email)
	var user User
	var hash string
	err := s.Pool.QueryRow(ctx, `SELECT id, email, name, last_login_at, created_at, password_hash FROM users WHERE email = $1`, email).
		Scan(&user.ID, &user.Email, &user.Name, &user.LastLoginAt, &user.CreatedAt, &hash)
	if db.IsNotFound(err) {
		_, _ = VerifyPassword(password, dummyHash) // equalise timing
		audit.MustRecord(ctx, s.Pool, audit.Entry{Action: audit.UserLoginFailed, ActorEmail: email, Metadata: map[string]any{"reason": "unknown_email"}})
		return User{}, ErrInvalidCredentials
	}
	if err != nil {
		return User{}, err
	}
	ok, err := VerifyPassword(password, hash)
	if err != nil {
		return User{}, err
	}
	if !ok {
		audit.MustRecord(ctx, s.Pool, audit.Entry{Action: audit.UserLoginFailed, ActorEmail: email, ResourceType: "user", ResourceID: user.ID, Metadata: map[string]any{"reason": "bad_password"}})
		return User{}, ErrInvalidCredentials
	}
	_, _ = s.Pool.Exec(ctx, `UPDATE users SET last_login_at = now() WHERE id = $1`, user.ID)
	return user, nil
}

// Login verifies credentials and creates a browser session.
func (s *Service) Login(ctx context.Context, email, password string) (SessionResult, error) {
	user, err := s.Authenticate(ctx, email, password)
	if err != nil {
		return SessionResult{}, err
	}
	res, err := s.createSession(ctx, user)
	if err != nil {
		return res, err
	}
	actx := reqctx.WithPrincipal(ctx, reqctx.Principal{UserID: user.ID, Email: user.Email, Name: user.Name, SessionID: res.SessionID})
	audit.MustRecord(actx, s.Pool, audit.Entry{Action: audit.UserLoggedIn, ResourceType: "user", ResourceID: user.ID})
	return res, nil
}

func (s *Service) createSession(ctx context.Context, user User) (SessionResult, error) {
	token, hash, err := s.Hasher.NewToken(SessionTokenPrefix)
	if err != nil {
		return SessionResult{}, err
	}
	meta := reqctx.MetaFrom(ctx)
	expires := time.Now().Add(SessionTTL)
	var sessionID string
	err = s.Pool.QueryRow(ctx, `INSERT INTO sessions (user_id, token_hash, ip_address, user_agent, expires_at)
		VALUES ($1, $2, $3, $4, $5) RETURNING id`, user.ID, hash, meta.IP, truncate(meta.UserAgent, 512), expires).Scan(&sessionID)
	if err != nil {
		return SessionResult{}, err
	}
	return SessionResult{User: user, Token: token, SessionID: sessionID, CSRF: s.Hasher.CSRFToken(sessionID), ExpiresAt: expires}, nil
}

// ResolveSession returns the principal for a session token.
func (s *Service) ResolveSession(ctx context.Context, token string) (reqctx.Principal, error) {
	if !strings.HasPrefix(token, SessionTokenPrefix) {
		return reqctx.Principal{}, errInvalidToken
	}
	var p reqctx.Principal
	var lastSeen time.Time
	err := s.Pool.QueryRow(ctx, `SELECT s.id, s.last_seen_at, u.id, u.email, u.name FROM sessions s
		JOIN users u ON u.id = s.user_id WHERE s.token_hash = $1 AND s.expires_at > now()`, s.Hasher.Hash(token)).
		Scan(&p.SessionID, &lastSeen, &p.UserID, &p.Email, &p.Name)
	if db.IsNotFound(err) {
		return p, errInvalidToken
	}
	if err != nil {
		return p, err
	}
	if time.Since(lastSeen) > sessionTouchStep {
		// Sliding expiry, written at most every few minutes per session.
		_, _ = s.Pool.Exec(ctx, `UPDATE sessions SET last_seen_at = now(), expires_at = now() + $2::interval WHERE id = $1`,
			p.SessionID, "336 hours")
	}
	return p, nil
}

// ResolveAPIToken returns the principal for an API token.
func (s *Service) ResolveAPIToken(ctx context.Context, token string) (reqctx.Principal, error) {
	if !strings.HasPrefix(token, APITokenPrefix) {
		return reqctx.Principal{}, errInvalidToken
	}
	var p reqctx.Principal
	var lastUsed *time.Time
	err := s.Pool.QueryRow(ctx, `SELECT t.id, t.last_used_at, u.id, u.email, u.name FROM api_tokens t
		JOIN users u ON u.id = t.user_id WHERE t.token_hash = $1 AND (t.expires_at IS NULL OR t.expires_at > now())`, s.Hasher.Hash(token)).
		Scan(&p.TokenID, &lastUsed, &p.UserID, &p.Email, &p.Name)
	if db.IsNotFound(err) {
		return p, errInvalidToken
	}
	if err != nil {
		return p, err
	}
	if lastUsed == nil || time.Since(*lastUsed) > sessionTouchStep {
		_, _ = s.Pool.Exec(ctx, `UPDATE api_tokens SET last_used_at = now() WHERE id = $1`, p.TokenID)
	}
	return p, nil
}

var errInvalidToken = errors.New("invalid or expired token")

// Logout deletes the current session.
func (s *Service) Logout(ctx context.Context, sessionID string) error {
	_, err := s.Pool.Exec(ctx, `DELETE FROM sessions WHERE id = $1`, sessionID)
	return err
}

// GetUser returns a user by id.
func (s *Service) GetUser(ctx context.Context, id string) (User, error) {
	var u User
	err := s.Pool.QueryRow(ctx, `SELECT id, email, name, last_login_at, created_at FROM users WHERE id = $1`, id).
		Scan(&u.ID, &u.Email, &u.Name, &u.LastLoginAt, &u.CreatedAt)
	return u, err
}

// UpdateProfile changes the display name.
func (s *Service) UpdateProfile(ctx context.Context, userID, name string) (User, error) {
	var u User
	err := s.Pool.QueryRow(ctx, `UPDATE users SET name = $2 WHERE id = $1 RETURNING id, email, name, last_login_at, created_at`, userID, strings.TrimSpace(name)).
		Scan(&u.ID, &u.Email, &u.Name, &u.LastLoginAt, &u.CreatedAt)
	return u, err
}

// ChangePassword verifies the current password, sets a new one and revokes
// every other session.
func (s *Service) ChangePassword(ctx context.Context, p reqctx.Principal, current, next string) error {
	var hash string
	if err := s.Pool.QueryRow(ctx, `SELECT password_hash FROM users WHERE id = $1`, p.UserID).Scan(&hash); err != nil {
		return err
	}
	ok, err := VerifyPassword(current, hash)
	if err != nil {
		return err
	}
	if !ok {
		return apperr.Validation(map[string]string{"current_password": "Current password is incorrect."})
	}
	newHash, err := HashPassword(next)
	if err != nil {
		return err
	}
	return db.WithTx(ctx, s.Pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `UPDATE users SET password_hash = $2 WHERE id = $1`, p.UserID, newHash); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM sessions WHERE user_id = $1 AND id::text <> $2`, p.UserID, p.SessionID); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Entry{Action: audit.UserPasswordChanged, ResourceType: "user", ResourceID: p.UserID})
	})
}

// RequestPasswordReset emails a single-use reset link. It never reveals
// whether the email exists.
func (s *Service) RequestPasswordReset(ctx context.Context, email string) error {
	email = normalizeEmail(email)
	var userID string
	err := s.Pool.QueryRow(ctx, `SELECT id FROM users WHERE email = $1`, email).Scan(&userID)
	if db.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}
	token, hash, err := s.Hasher.NewToken(ResetTokenPrefix)
	if err != nil {
		return err
	}
	if _, err := s.Pool.Exec(ctx, `INSERT INTO password_reset_tokens (user_id, token_hash, expires_at) VALUES ($1, $2, $3)`,
		userID, hash, time.Now().Add(resetTokenTTL)); err != nil {
		return err
	}
	if s.Mailer == nil || !s.Mailer.Configured() {
		// Without SMTP the link cannot be delivered; operators can still
		// reset passwords via the database. Never log the token itself.
		return nil
	}
	link := s.AppURL + "/reset-password?token=" + token
	body := "Someone requested a password reset for your DBVault account.\n\n" +
		"Reset your password (valid for 1 hour):\n" + link + "\n\n" +
		"If you did not request this, you can ignore this email."
	// Send asynchronously so response time doesn't reveal whether the
	// account exists.
	go func() {
		sendCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := s.Mailer.Send(sendCtx, email, "Reset your DBVault password", body); err != nil {
			slog.Error("failed to send password reset email", "error", err.Error())
		}
	}()
	return nil
}

// ResetPassword consumes a reset token and sets a new password, revoking all sessions.
func (s *Service) ResetPassword(ctx context.Context, token, password string) error {
	if !strings.HasPrefix(token, ResetTokenPrefix) {
		return apperr.BadRequest("This reset link is invalid or has expired.")
	}
	hash, err := HashPassword(password)
	if err != nil {
		return err
	}
	return db.WithTx(ctx, s.Pool, func(tx pgx.Tx) error {
		var userID string
		err := tx.QueryRow(ctx, `UPDATE password_reset_tokens SET used_at = now()
			WHERE token_hash = $1 AND used_at IS NULL AND expires_at > now() RETURNING user_id`, s.Hasher.Hash(token)).Scan(&userID)
		if db.IsNotFound(err) {
			return apperr.BadRequest("This reset link is invalid or has expired.")
		}
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE users SET password_hash = $2 WHERE id = $1`, userID, hash); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM sessions WHERE user_id = $1`, userID); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Entry{Action: audit.UserPasswordReset, ResourceType: "user", ResourceID: userID})
	})
}

// Session is an active browser session as listed in security settings.
type Session struct {
	ID         string    `json:"id"`
	IPAddress  *string   `json:"ip_address"`
	UserAgent  *string   `json:"user_agent"`
	CreatedAt  time.Time `json:"created_at"`
	LastSeenAt time.Time `json:"last_seen_at"`
	ExpiresAt  time.Time `json:"expires_at"`
	Current    bool      `json:"current"`
}

func (s *Service) ListSessions(ctx context.Context, p reqctx.Principal) ([]Session, error) {
	rows, err := s.Pool.Query(ctx, `SELECT id, ip_address, user_agent, created_at, last_seen_at, expires_at FROM sessions
		WHERE user_id = $1 AND expires_at > now() ORDER BY last_seen_at DESC`, p.UserID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Session, error) {
		var x Session
		err := r.Scan(&x.ID, &x.IPAddress, &x.UserAgent, &x.CreatedAt, &x.LastSeenAt, &x.ExpiresAt)
		x.Current = x.ID == p.SessionID
		return x, err
	})
}

func (s *Service) RevokeSession(ctx context.Context, userID, sessionID string) error {
	tag, err := s.Pool.Exec(ctx, `DELETE FROM sessions WHERE id = $1 AND user_id = $2`, sessionID, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return apperr.NotFound("Session")
	}
	return nil
}

// APIToken is a CLI/automation token (the secret is only shown once).
type APIToken struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	Prefix     string     `json:"prefix"`
	LastUsedAt *time.Time `json:"last_used_at"`
	ExpiresAt  *time.Time `json:"expires_at"`
	CreatedAt  time.Time  `json:"created_at"`
}

// CreateAPIToken issues a token for userID. The plaintext is returned once.
func (s *Service) CreateAPIToken(ctx context.Context, userID, name string, ttl time.Duration) (APIToken, string, error) {
	token, hash, err := s.Hasher.NewToken(APITokenPrefix)
	if err != nil {
		return APIToken{}, "", err
	}
	var expires *time.Time
	if ttl > 0 {
		t := time.Now().Add(ttl)
		expires = &t
	}
	var t APIToken
	err = s.Pool.QueryRow(ctx, `INSERT INTO api_tokens (user_id, name, token_prefix, token_hash, expires_at) VALUES ($1, $2, $3, $4, $5)
		RETURNING id, name, token_prefix, last_used_at, expires_at, created_at`, userID, name, token[:12], hash, expires).
		Scan(&t.ID, &t.Name, &t.Prefix, &t.LastUsedAt, &t.ExpiresAt, &t.CreatedAt)
	if err != nil {
		return t, "", err
	}
	audit.MustRecord(ctx, s.Pool, audit.Entry{Action: audit.APITokenCreated, ResourceType: "api_token", ResourceID: t.ID, Metadata: map[string]any{"name": name}})
	return t, token, nil
}

func (s *Service) ListAPITokens(ctx context.Context, userID string) ([]APIToken, error) {
	rows, err := s.Pool.Query(ctx, `SELECT id, name, token_prefix, last_used_at, expires_at, created_at FROM api_tokens
		WHERE user_id = $1 ORDER BY created_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (APIToken, error) {
		var t APIToken
		err := r.Scan(&t.ID, &t.Name, &t.Prefix, &t.LastUsedAt, &t.ExpiresAt, &t.CreatedAt)
		return t, err
	})
}

func (s *Service) RevokeAPIToken(ctx context.Context, userID, id string) error {
	tag, err := s.Pool.Exec(ctx, `DELETE FROM api_tokens WHERE id = $1 AND user_id = $2`, id, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return apperr.NotFound("API token")
	}
	audit.MustRecord(ctx, s.Pool, audit.Entry{Action: audit.APITokenRevoked, ResourceType: "api_token", ResourceID: id})
	return nil
}

// PurgeExpired removes expired sessions and reset tokens (run by the scheduler).
func PurgeExpired(ctx context.Context, pool *pgxpool.Pool) error {
	if _, err := pool.Exec(ctx, `DELETE FROM sessions WHERE expires_at < now()`); err != nil {
		return err
	}
	_, err := pool.Exec(ctx, `DELETE FROM password_reset_tokens WHERE expires_at < now() - interval '1 day'`)
	return err
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}
