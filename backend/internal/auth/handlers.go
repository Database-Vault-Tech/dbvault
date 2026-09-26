package auth

import (
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/dbvault/dbvault/backend/internal/apperr"
	"github.com/dbvault/dbvault/backend/internal/audit"
	"github.com/dbvault/dbvault/backend/internal/httpx"
	"github.com/dbvault/dbvault/backend/internal/ratelimit"
	"github.com/dbvault/dbvault/backend/internal/reqctx"
	"github.com/dbvault/dbvault/backend/internal/validate"
)

// Handlers exposes authentication over HTTP.
type Handlers struct {
	Svc          *Service
	Limiter      *ratelimit.Limiter
	CookieSecure bool
}

// PublicRoutes are reachable without a session (rate limited by the caller).
func (h *Handlers) PublicRoutes(r chi.Router) {
	r.Post("/register", h.register)
	r.Post("/login", h.login)
	r.Post("/login/2fa", h.loginTwoFactor)
	r.Post("/token", h.token)
	r.Post("/password/forgot", h.forgotPassword)
	r.Post("/password/reset", h.resetPassword)
}

// PrivateRoutes require an authenticated principal.
func (h *Handlers) PrivateRoutes(r chi.Router) {
	r.Post("/logout", h.logout)
	r.Patch("/me", h.updateProfile)
	r.Post("/password/change", h.changePassword)
	r.Get("/sessions", h.listSessions)
	r.Delete("/sessions/{id}", h.revokeSession)
	r.Get("/tokens", h.listTokens)
	r.Post("/tokens", h.createToken)
	r.Delete("/tokens/{id}", h.revokeToken)
	r.Get("/2fa", h.twoFactorStatus)
	r.Post("/2fa/setup", h.twoFactorSetup)
	r.Post("/2fa/enable", h.twoFactorEnable)
	r.Post("/2fa/disable", h.twoFactorDisable)
	r.Post("/2fa/recovery-codes", h.regenerateRecoveryCodes)
}

// perEmailLimit slows targeted credential guessing even across many IPs.
func (h *Handlers) perEmailLimit(r *http.Request, email string) error {
	ok, _ := h.Limiter.Allow(r.Context(), "login-email:"+normalizeEmail(email), 20, time.Hour)
	if !ok {
		return apperr.RateLimited()
	}
	return nil
}

type registerReq struct {
	Name     string `json:"name"`
	Email    string `json:"email"`
	Password string `json:"password"`
	// Set when arriving from an invitation link, so the account can be
	// created even though open registration is disabled.
	InviteToken string `json:"invite_token"`
}

func (h *Handlers) register(w http.ResponseWriter, r *http.Request) {
	var req registerReq
	if err := httpx.Decode(w, r, &req); err != nil {
		httpx.Error(w, r, err)
		return
	}
	v := validate.New()
	v.Required("name", req.Name)
	v.MaxLen("name", req.Name, 100)
	v.Email("email", normalizeEmail(req.Email))
	if msg := ValidatePasswordStrength(req.Password); msg != "" {
		v.Check(false, "password", msg)
	}
	if err := v.Err(); err != nil {
		httpx.Error(w, r, err)
		return
	}
	res, orgID, err := h.Svc.Register(r.Context(), req.Name, req.Email, req.Password, strings.TrimSpace(req.InviteToken))
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	SetSessionCookies(w, res, h.CookieSecure)
	httpx.JSON(w, http.StatusCreated, map[string]any{"user": res.User, "organization_id": orgID, "csrf_token": res.CSRF})
}

type loginReq struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func (h *Handlers) login(w http.ResponseWriter, r *http.Request) {
	var req loginReq
	if err := httpx.Decode(w, r, &req); err != nil {
		httpx.Error(w, r, err)
		return
	}
	if req.Email == "" || req.Password == "" {
		httpx.Error(w, r, ErrInvalidCredentials)
		return
	}
	if err := h.perEmailLimit(r, req.Email); err != nil {
		httpx.Error(w, r, err)
		return
	}
	res, challenge, err := h.Svc.Login(r.Context(), req.Email, req.Password)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	if challenge != nil {
		// No session yet: the browser must come back with a code.
		httpx.JSON(w, http.StatusOK, map[string]any{"mfa_required": true, "mfa_token": challenge.Token, "expires_at": challenge.ExpiresAt})
		return
	}
	SetSessionCookies(w, res, h.CookieSecure)
	httpx.JSON(w, http.StatusOK, map[string]any{"user": res.User, "csrf_token": res.CSRF})
}

func (h *Handlers) loginTwoFactor(w http.ResponseWriter, r *http.Request) {
	var req struct {
		MFAToken string `json:"mfa_token"`
		Code     string `json:"code"`
	}
	if err := httpx.Decode(w, r, &req); err != nil {
		httpx.Error(w, r, err)
		return
	}
	res, err := h.Svc.CompleteMFALogin(r.Context(), req.MFAToken, req.Code)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	SetSessionCookies(w, res, h.CookieSecure)
	httpx.JSON(w, http.StatusOK, map[string]any{"user": res.User, "csrf_token": res.CSRF})
}

type tokenReq struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	// Code is the authenticator or recovery code, required when the
	// account has two-factor authentication enabled.
	Code string `json:"code"`
	Name string `json:"name"`
}

// token exchanges credentials for an API token (used by `dbvault init`).
func (h *Handlers) token(w http.ResponseWriter, r *http.Request) {
	var req tokenReq
	if err := httpx.Decode(w, r, &req); err != nil {
		httpx.Error(w, r, err)
		return
	}
	if req.Email == "" || req.Password == "" {
		httpx.Error(w, r, ErrInvalidCredentials)
		return
	}
	if err := h.perEmailLimit(r, req.Email); err != nil {
		httpx.Error(w, r, err)
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = "CLI"
	}
	if len(name) > 100 {
		name = name[:100]
	}
	t, secret, user, err := h.Svc.ExchangeForAPIToken(r.Context(), req.Email, req.Password, req.Code, name)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, map[string]any{"token": secret, "api_token": t, "user": user})
}

func (h *Handlers) logout(w http.ResponseWriter, r *http.Request) {
	p, _ := reqctx.PrincipalFrom(r.Context())
	if p.SessionID != "" {
		if err := h.Svc.Logout(r.Context(), p.SessionID); err != nil {
			httpx.Error(w, r, err)
			return
		}
		audit.MustRecord(r.Context(), h.Svc.Pool, audit.Entry{Action: audit.UserLoggedOut, ResourceType: "user", ResourceID: p.UserID})
	}
	ClearSessionCookies(w, h.CookieSecure)
	httpx.NoContent(w)
}

func (h *Handlers) updateProfile(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
	}
	if err := httpx.Decode(w, r, &req); err != nil {
		httpx.Error(w, r, err)
		return
	}
	v := validate.New()
	v.Required("name", req.Name)
	v.MaxLen("name", req.Name, 100)
	if err := v.Err(); err != nil {
		httpx.Error(w, r, err)
		return
	}
	p, _ := reqctx.PrincipalFrom(r.Context())
	u, err := h.Svc.UpdateProfile(r.Context(), p.UserID, req.Name)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, u)
}

func (h *Handlers) changePassword(w http.ResponseWriter, r *http.Request) {
	var req struct {
		CurrentPassword string `json:"current_password"`
		NewPassword     string `json:"new_password"`
	}
	if err := httpx.Decode(w, r, &req); err != nil {
		httpx.Error(w, r, err)
		return
	}
	v := validate.New()
	v.Required("current_password", req.CurrentPassword)
	if msg := ValidatePasswordStrength(req.NewPassword); msg != "" {
		v.Check(false, "new_password", msg)
	}
	if err := v.Err(); err != nil {
		httpx.Error(w, r, err)
		return
	}
	p, _ := reqctx.PrincipalFrom(r.Context())
	if err := h.Svc.ChangePassword(r.Context(), p, req.CurrentPassword, req.NewPassword); err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.NoContent(w)
}

func (h *Handlers) forgotPassword(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email string `json:"email"`
	}
	if err := httpx.Decode(w, r, &req); err != nil {
		httpx.Error(w, r, err)
		return
	}
	if err := h.Svc.RequestPasswordReset(r.Context(), req.Email); err != nil {
		httpx.Error(w, r, err)
		return
	}
	// Always the same response, whether or not the account exists.
	httpx.JSON(w, http.StatusAccepted, map[string]any{
		"message":       "If an account exists for that email, a reset link has been sent.",
		"email_enabled": h.Svc.Mailer != nil && h.Svc.Mailer.Configured(),
	})
}

func (h *Handlers) resetPassword(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Token    string `json:"token"`
		Password string `json:"password"`
	}
	if err := httpx.Decode(w, r, &req); err != nil {
		httpx.Error(w, r, err)
		return
	}
	if msg := ValidatePasswordStrength(req.Password); msg != "" {
		httpx.Error(w, r, apperr.Validation(map[string]string{"password": msg}))
		return
	}
	if err := h.Svc.ResetPassword(r.Context(), req.Token, req.Password); err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.NoContent(w)
}

func (h *Handlers) listSessions(w http.ResponseWriter, r *http.Request) {
	p, _ := reqctx.PrincipalFrom(r.Context())
	sessions, err := h.Svc.ListSessions(r.Context(), p)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, sessions)
}

func (h *Handlers) revokeSession(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if !httpx.IsUUID(id) {
		httpx.Error(w, r, apperr.NotFound("Session"))
		return
	}
	p, _ := reqctx.PrincipalFrom(r.Context())
	if err := h.Svc.RevokeSession(r.Context(), p.UserID, id); err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.NoContent(w)
}

func (h *Handlers) listTokens(w http.ResponseWriter, r *http.Request) {
	p, _ := reqctx.PrincipalFrom(r.Context())
	tokens, err := h.Svc.ListAPITokens(r.Context(), p.UserID)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, tokens)
}

func (h *Handlers) createToken(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name          string `json:"name"`
		ExpiresInDays int    `json:"expires_in_days"`
	}
	if err := httpx.Decode(w, r, &req); err != nil {
		httpx.Error(w, r, err)
		return
	}
	v := validate.New()
	v.Required("name", req.Name)
	v.MaxLen("name", req.Name, 100)
	v.Range("expires_in_days", req.ExpiresInDays, 0, 3650)
	if err := v.Err(); err != nil {
		httpx.Error(w, r, err)
		return
	}
	p, _ := reqctx.PrincipalFrom(r.Context())
	t, secret, err := h.Svc.CreateAPIToken(r.Context(), p.UserID, strings.TrimSpace(req.Name), time.Duration(req.ExpiresInDays)*24*time.Hour)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, map[string]any{"token": secret, "api_token": t})
}

func (h *Handlers) revokeToken(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if !httpx.IsUUID(id) {
		httpx.Error(w, r, apperr.NotFound("API token"))
		return
	}
	p, _ := reqctx.PrincipalFrom(r.Context())
	if err := h.Svc.RevokeAPIToken(r.Context(), p.UserID, id); err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.NoContent(w)
}

func (h *Handlers) twoFactorStatus(w http.ResponseWriter, r *http.Request) {
	p, _ := reqctx.PrincipalFrom(r.Context())
	st, err := h.Svc.GetTwoFactorStatus(r.Context(), p.UserID)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, st)
}

// twoFactorReq is shared by the 2FA endpoints; each uses the fields it needs.
type twoFactorReq struct {
	Password string `json:"password"`
	Code     string `json:"code"`
}

// decodeTwoFactor decodes the body and requires the named fields.
func decodeTwoFactor(w http.ResponseWriter, r *http.Request, needPassword, needCode bool) (twoFactorReq, bool) {
	var req twoFactorReq
	if err := httpx.Decode(w, r, &req); err != nil {
		httpx.Error(w, r, err)
		return req, false
	}
	v := validate.New()
	if needPassword {
		v.Required("password", req.Password)
	}
	if needCode {
		v.Required("code", req.Code)
		v.MaxLen("code", req.Code, 64)
	}
	if err := v.Err(); err != nil {
		httpx.Error(w, r, err)
		return req, false
	}
	return req, true
}

// sessionOnly keeps 2FA management out of reach of API tokens: a leaked CLI
// token must not be able to turn off the second factor.
func sessionOnly(w http.ResponseWriter, r *http.Request) (reqctx.Principal, bool) {
	p, _ := reqctx.PrincipalFrom(r.Context())
	if p.SessionID == "" {
		httpx.Error(w, r, apperr.Forbidden("Two-factor authentication can only be managed from a signed-in browser."))
		return p, false
	}
	return p, true
}

func (h *Handlers) twoFactorSetup(w http.ResponseWriter, r *http.Request) {
	p, ok := sessionOnly(w, r)
	if !ok {
		return
	}
	req, ok := decodeTwoFactor(w, r, true, false)
	if !ok {
		return
	}
	setup, err := h.Svc.BeginTOTPSetup(r.Context(), p, req.Password)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, setup)
}

func (h *Handlers) twoFactorEnable(w http.ResponseWriter, r *http.Request) {
	p, ok := sessionOnly(w, r)
	if !ok {
		return
	}
	req, ok := decodeTwoFactor(w, r, false, true)
	if !ok {
		return
	}
	codes, err := h.Svc.EnableTOTP(r.Context(), p, req.Code)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"recovery_codes": codes})
}

func (h *Handlers) twoFactorDisable(w http.ResponseWriter, r *http.Request) {
	p, ok := sessionOnly(w, r)
	if !ok {
		return
	}
	req, ok := decodeTwoFactor(w, r, true, true)
	if !ok {
		return
	}
	if err := h.Svc.DisableTOTP(r.Context(), p, req.Password, req.Code); err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.NoContent(w)
}

func (h *Handlers) regenerateRecoveryCodes(w http.ResponseWriter, r *http.Request) {
	p, ok := sessionOnly(w, r)
	if !ok {
		return
	}
	req, ok := decodeTwoFactor(w, r, true, true)
	if !ok {
		return
	}
	codes, err := h.Svc.RegenerateRecoveryCodes(r.Context(), p, req.Password, req.Code)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"recovery_codes": codes})
}
