package auth

import (
	"context"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/dbvault/dbvault/backend/internal/apperr"
	"github.com/dbvault/dbvault/backend/internal/audit"
	"github.com/dbvault/dbvault/backend/internal/db"
	"github.com/dbvault/dbvault/backend/internal/reqctx"
)

const (
	totpIssuer = "DBVault"
	// mfaChallengeTTL is how long a user has to enter their code after the
	// password check succeeds.
	mfaChallengeTTL = 5 * time.Minute
	// mfaMaxAttempts caps code guesses per password check. Together with the
	// per-email login limit this bounds guessing to ~100 codes an hour.
	mfaMaxAttempts = 5
)

// ErrMFARequired is returned when a password check succeeded but a second
// factor is needed (the CLI prompts for a code when it sees this).
var ErrMFARequired = apperr.New(http.StatusUnauthorized, "mfa_required", "Enter the 6-digit code from your authenticator app, or a recovery code.")

var (
	errInvalidCode      = apperr.Validation(map[string]string{"code": "That code is invalid or has already been used."})
	errChallengeExpired = apperr.New(http.StatusUnauthorized, "mfa_expired", "This sign-in has expired. Enter your password again.")
)

// TwoFactorStatus is shown in security settings.
type TwoFactorStatus struct {
	Enabled                bool       `json:"enabled"`
	EnabledAt              *time.Time `json:"enabled_at"`
	RecoveryCodesRemaining int        `json:"recovery_codes_remaining"`
}

// TOTPSetup is returned when enrollment starts. The secret is shown once so
// it can be typed into apps that can't scan the QR code.
type TOTPSetup struct {
	Secret string `json:"secret"`
	URI    string `json:"otpauth_uri"`
}

func totpAAD(userID string) string { return "user:" + userID + ":totp" }

func (s *Service) twoFactorEnabled(ctx context.Context, userID string) (bool, error) {
	var enabled bool
	err := s.Pool.QueryRow(ctx, `SELECT totp_enabled_at IS NOT NULL FROM users WHERE id = $1`, userID).Scan(&enabled)
	return enabled, err
}

// checkPassword re-verifies the signed-in user's password before a
// sensitive change, so a hijacked session alone cannot make it.
func (s *Service) checkPassword(ctx context.Context, userID, password string) error {
	var hash string
	if err := s.Pool.QueryRow(ctx, `SELECT password_hash FROM users WHERE id = $1`, userID).Scan(&hash); err != nil {
		return err
	}
	ok, err := VerifyPassword(password, hash)
	if err != nil {
		return err
	}
	if !ok {
		return apperr.Validation(map[string]string{"password": "Password is incorrect."})
	}
	return nil
}

// GetTwoFactorStatus reports whether 2FA is on and how many recovery codes are left.
func (s *Service) GetTwoFactorStatus(ctx context.Context, userID string) (TwoFactorStatus, error) {
	var st TwoFactorStatus
	err := s.Pool.QueryRow(ctx, `SELECT u.totp_enabled_at,
			(SELECT count(*) FROM user_recovery_codes c WHERE c.user_id = u.id AND c.used_at IS NULL)
		FROM users u WHERE u.id = $1`, userID).Scan(&st.EnabledAt, &st.RecoveryCodesRemaining)
	st.Enabled = st.EnabledAt != nil
	return st, err
}

// BeginTOTPSetup generates a new secret for p after re-checking their
// password. It has no effect on sign-in until EnableTOTP confirms a code.
func (s *Service) BeginTOTPSetup(ctx context.Context, p reqctx.Principal, password string) (TOTPSetup, error) {
	if err := s.checkPassword(ctx, p.UserID, password); err != nil {
		return TOTPSetup{}, err
	}
	secret, err := NewTOTPSecret()
	if err != nil {
		return TOTPSetup{}, err
	}
	sealed, err := s.Sealer.SealString(secret, totpAAD(p.UserID))
	if err != nil {
		return TOTPSetup{}, err
	}
	tag, err := s.Pool.Exec(ctx, `UPDATE users SET totp_secret_encrypted = $2, totp_last_step = NULL
		WHERE id = $1 AND totp_enabled_at IS NULL`, p.UserID, sealed)
	if err != nil {
		return TOTPSetup{}, err
	}
	if tag.RowsAffected() == 0 {
		return TOTPSetup{}, apperr.Conflict("Two-factor authentication is already enabled. Turn it off first to set up a new authenticator.")
	}
	return TOTPSetup{Secret: secret, URI: TOTPURI(totpIssuer, p.Email, secret)}, nil
}

// EnableTOTP turns on 2FA once the user proves their authenticator produces
// valid codes. It returns fresh recovery codes (shown once) and signs out
// every other session.
func (s *Service) EnableTOTP(ctx context.Context, p reqctx.Principal, code string) ([]string, error) {
	var sealed *string
	var enabledAt *time.Time
	if err := s.Pool.QueryRow(ctx, `SELECT totp_secret_encrypted, totp_enabled_at FROM users WHERE id = $1`, p.UserID).
		Scan(&sealed, &enabledAt); err != nil {
		return nil, err
	}
	if enabledAt != nil {
		return nil, apperr.Conflict("Two-factor authentication is already enabled.")
	}
	if sealed == nil {
		return nil, apperr.BadRequest("Start two-factor setup first.")
	}
	secret, err := s.Sealer.Open(*sealed, totpAAD(p.UserID))
	if err != nil {
		return nil, err
	}
	step, ok := matchTOTP(string(secret), normalizeCode(code), time.Now())
	if !ok {
		return nil, apperr.Validation(map[string]string{"code": "That code doesn't match. Check your device's clock and try the newest code."})
	}
	codes, err := NewRecoveryCodes()
	if err != nil {
		return nil, err
	}
	err = db.WithTx(ctx, s.Pool, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `UPDATE users SET totp_enabled_at = now(), totp_last_step = $2
			WHERE id = $1 AND totp_enabled_at IS NULL AND totp_secret_encrypted = $3`, p.UserID, step, *sealed)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			// Setup was restarted (new secret) or finished in another tab.
			return apperr.Conflict("Two-factor setup changed in another window. Start again.")
		}
		if err := s.replaceRecoveryCodes(ctx, tx, p.UserID, codes); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM sessions WHERE user_id = $1 AND id::text <> $2`, p.UserID, p.SessionID); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Entry{Action: audit.UserTwoFactorEnabled, ResourceType: "user", ResourceID: p.UserID})
	})
	if err != nil {
		return nil, err
	}
	return codes, nil
}

// DisableTOTP turns 2FA off. It needs both the password and a current code
// (or recovery code), so neither a stolen session nor a stolen password is
// enough to remove the second factor.
func (s *Service) DisableTOTP(ctx context.Context, p reqctx.Principal, password, code string) error {
	if err := s.checkPassword(ctx, p.UserID, password); err != nil {
		return err
	}
	if _, err := s.verifySecondFactor(ctx, p.UserID, code); err != nil {
		return err
	}
	return db.WithTx(ctx, s.Pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `UPDATE users SET totp_secret_encrypted = NULL, totp_enabled_at = NULL, totp_last_step = NULL
			WHERE id = $1`, p.UserID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM user_recovery_codes WHERE user_id = $1`, p.UserID); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Entry{Action: audit.UserTwoFactorDisabled, ResourceType: "user", ResourceID: p.UserID})
	})
}

// RegenerateRecoveryCodes replaces every recovery code (used or not).
func (s *Service) RegenerateRecoveryCodes(ctx context.Context, p reqctx.Principal, password, code string) ([]string, error) {
	if err := s.checkPassword(ctx, p.UserID, password); err != nil {
		return nil, err
	}
	if _, err := s.verifySecondFactor(ctx, p.UserID, code); err != nil {
		return nil, err
	}
	codes, err := NewRecoveryCodes()
	if err != nil {
		return nil, err
	}
	err = db.WithTx(ctx, s.Pool, func(tx pgx.Tx) error {
		if err := s.replaceRecoveryCodes(ctx, tx, p.UserID, codes); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Entry{Action: audit.UserRecoveryCodesReset, ResourceType: "user", ResourceID: p.UserID})
	})
	return codes, err
}

func (s *Service) replaceRecoveryCodes(ctx context.Context, tx pgx.Tx, userID string, codes []string) error {
	if _, err := tx.Exec(ctx, `DELETE FROM user_recovery_codes WHERE user_id = $1`, userID); err != nil {
		return err
	}
	for _, c := range codes {
		if _, err := tx.Exec(ctx, `INSERT INTO user_recovery_codes (user_id, code_hash) VALUES ($1, $2)`,
			userID, s.Hasher.recoveryCodeHash(userID, normalizeCode(c))); err != nil {
			return err
		}
	}
	return nil
}

// verifySecondFactor checks an authenticator code or consumes a recovery
// code for a user with 2FA enabled. Each authenticator code works once;
// each recovery code works once. Failures are audited.
func (s *Service) verifySecondFactor(ctx context.Context, userID, code string) (method string, err error) {
	code = normalizeCode(code)
	if code == "" {
		return "", apperr.Validation(map[string]string{"code": "Enter a code."})
	}
	if isTOTPCode(code) {
		var sealed *string
		if err := s.Pool.QueryRow(ctx, `SELECT totp_secret_encrypted FROM users WHERE id = $1 AND totp_enabled_at IS NOT NULL`, userID).
			Scan(&sealed); err != nil && !db.IsNotFound(err) {
			return "", err
		}
		if sealed != nil {
			secret, err := s.Sealer.Open(*sealed, totpAAD(userID))
			if err != nil {
				return "", err
			}
			if step, ok := matchTOTP(string(secret), code, time.Now()); ok {
				// Only a step newer than the last accepted one counts, so an
				// observed code can't be replayed.
				tag, err := s.Pool.Exec(ctx, `UPDATE users SET totp_last_step = $2
					WHERE id = $1 AND (totp_last_step IS NULL OR totp_last_step < $2)`, userID, step)
				if err != nil {
					return "", err
				}
				if tag.RowsAffected() == 1 {
					return "totp", nil
				}
			}
		}
	} else {
		tag, err := s.Pool.Exec(ctx, `UPDATE user_recovery_codes SET used_at = now()
			WHERE user_id = $1 AND code_hash = $2 AND used_at IS NULL`, userID, s.Hasher.recoveryCodeHash(userID, code))
		if err != nil {
			return "", err
		}
		if tag.RowsAffected() == 1 {
			audit.MustRecord(ctx, s.Pool, audit.Entry{Action: audit.UserRecoveryCodeUsed, ResourceType: "user", ResourceID: userID})
			return "recovery_code", nil
		}
	}
	audit.MustRecord(ctx, s.Pool, audit.Entry{Action: audit.UserTwoFactorFailed, ResourceType: "user", ResourceID: userID})
	return "", errInvalidCode
}

// startMFAChallenge records a successful password check that still needs a
// second factor and returns the token the browser presents with the code.
func (s *Service) startMFAChallenge(ctx context.Context, userID string) (string, time.Time, error) {
	token, hash, err := s.Hasher.NewToken(MFATokenPrefix)
	if err != nil {
		return "", time.Time{}, err
	}
	expires := time.Now().Add(mfaChallengeTTL)
	if _, err := s.Pool.Exec(ctx, `INSERT INTO mfa_challenges (user_id, token_hash, expires_at) VALUES ($1, $2, $3)`,
		userID, hash, expires); err != nil {
		return "", time.Time{}, err
	}
	return token, expires, nil
}

// CompleteMFALogin exchanges a pending challenge and a valid code for a session.
func (s *Service) CompleteMFALogin(ctx context.Context, token, code string) (SessionResult, error) {
	if len(token) <= len(MFATokenPrefix) || token[:len(MFATokenPrefix)] != MFATokenPrefix {
		return SessionResult{}, errChallengeExpired
	}
	hash := s.Hasher.Hash(token)
	var userID string
	err := s.Pool.QueryRow(ctx, `UPDATE mfa_challenges SET attempts = attempts + 1
		WHERE token_hash = $1 AND expires_at > now() AND attempts < $2 RETURNING user_id`, hash, mfaMaxAttempts).Scan(&userID)
	if db.IsNotFound(err) {
		return SessionResult{}, errChallengeExpired
	}
	if err != nil {
		return SessionResult{}, err
	}
	user, err := s.GetUser(ctx, userID)
	if err != nil {
		return SessionResult{}, err
	}
	actx := reqctx.WithPrincipal(ctx, reqctx.Principal{UserID: user.ID, Email: user.Email, Name: user.Name})
	method, err := s.verifySecondFactor(actx, userID, code)
	if err != nil {
		return SessionResult{}, err
	}
	// Single use: a second request with the same token must fail.
	tag, err := s.Pool.Exec(ctx, `DELETE FROM mfa_challenges WHERE token_hash = $1`, hash)
	if err != nil {
		return SessionResult{}, err
	}
	if tag.RowsAffected() == 0 {
		return SessionResult{}, errChallengeExpired
	}
	return s.finishLogin(ctx, user, map[string]any{"second_factor": method})
}

// requireSecondFactor is used by non-interactive sign-ins (the CLI's token
// exchange), which send the code with the password in one request.
func (s *Service) requireSecondFactor(ctx context.Context, userID, code string) (string, error) {
	enabled, err := s.twoFactorEnabled(ctx, userID)
	if err != nil || !enabled {
		return "", err
	}
	if normalizeCode(code) == "" {
		return "", ErrMFARequired
	}
	return s.verifySecondFactor(ctx, userID, code)
}
