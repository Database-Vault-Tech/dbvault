package auth

import (
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/dbvault/dbvault/backend/internal/apperr"
	"github.com/dbvault/dbvault/backend/internal/httpx"
	"github.com/dbvault/dbvault/backend/internal/logging"
	"github.com/dbvault/dbvault/backend/internal/reqctx"
)

const (
	SessionCookie = "dbvault_session"
	CSRFCookie    = "dbvault_csrf"
	CSRFHeader    = "X-CSRF-Token"
)

// Middleware resolves the caller from an API token (Authorization:
// Bearer) or a session cookie. It does not reject anonymous requests.
func (s *Service) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		var p reqctx.Principal
		var err error
		found := false
		if h := r.Header.Get("Authorization"); h != "" {
			tok, ok := strings.CutPrefix(h, "Bearer ")
			if !ok {
				httpx.Error(w, r, apperr.Unauthorized("Authorization header must use the Bearer scheme."))
				return
			}
			p, err = s.ResolveAPIToken(ctx, strings.TrimSpace(tok))
			if err != nil {
				httpx.Error(w, r, apperr.Unauthorized("Invalid or expired API token."))
				return
			}
			found = true
		} else if c, cerr := r.Cookie(SessionCookie); cerr == nil && c.Value != "" {
			p, err = s.ResolveSession(ctx, c.Value)
			found = err == nil
		}
		if found {
			ctx = reqctx.WithPrincipal(ctx, p)
			ctx = logging.WithLogger(ctx, logging.FromContext(ctx).With("user_id", p.UserID))
		}
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// RequireAuth rejects anonymous requests.
func RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := reqctx.PrincipalFrom(r.Context()); !ok {
			httpx.Error(w, r, apperr.Unauthorized("Please sign in to continue."))
			return
		}
		next.ServeHTTP(w, r)
	})
}

// CSRF protects cookie-authenticated, state-changing requests:
//   - unsafe requests with a body must be application/json (HTML forms
//     cannot send JSON cross-site without a CORS preflight, which we never
//     grant);
//   - a present Origin header must match APP_URL;
//   - session-authenticated requests must echo the signed CSRF token.
//
// API-token requests are exempt from the token check because browsers never
// attach bearer tokens automatically.
func (s *Service) CSRF(next http.Handler) http.Handler {
	allowedOrigin := originOf(s.AppURL)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions {
			next.ServeHTTP(w, r)
			return
		}
		if r.ContentLength != 0 && !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
			httpx.Error(w, r, apperr.New(http.StatusUnsupportedMediaType, "unsupported_media_type", "Requests must use Content-Type: application/json."))
			return
		}
		if o := r.Header.Get("Origin"); o != "" && o != allowedOrigin {
			httpx.Error(w, r, apperr.Forbidden("Cross-origin request rejected."))
			return
		}
		if p, ok := reqctx.PrincipalFrom(r.Context()); ok && p.SessionID != "" {
			if !s.Hasher.ValidCSRF(p.SessionID, r.Header.Get(CSRFHeader)) {
				httpx.Error(w, r, apperr.Forbidden("Missing or invalid CSRF token. Refresh the page and try again."))
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func originOf(u string) string {
	p, err := url.Parse(u)
	if err != nil {
		return ""
	}
	return p.Scheme + "://" + p.Host
}

// SetSessionCookies writes the session and CSRF cookies.
func SetSessionCookies(w http.ResponseWriter, res SessionResult, secure bool) {
	maxAge := int(time.Until(res.ExpiresAt).Seconds())
	http.SetCookie(w, &http.Cookie{
		Name: SessionCookie, Value: res.Token, Path: "/", MaxAge: maxAge,
		HttpOnly: true, Secure: secure, SameSite: http.SameSiteLaxMode,
	})
	// Readable by the frontend so it can echo it in the X-CSRF-Token header.
	http.SetCookie(w, &http.Cookie{
		Name: CSRFCookie, Value: res.CSRF, Path: "/", MaxAge: maxAge,
		HttpOnly: false, Secure: secure, SameSite: http.SameSiteLaxMode,
	})
}

// ClearSessionCookies expires the session and CSRF cookies.
func ClearSessionCookies(w http.ResponseWriter, secure bool) {
	for _, name := range []string{SessionCookie, CSRFCookie} {
		http.SetCookie(w, &http.Cookie{Name: name, Value: "", Path: "/", MaxAge: -1, HttpOnly: name == SessionCookie, Secure: secure, SameSite: http.SameSiteLaxMode})
	}
}
