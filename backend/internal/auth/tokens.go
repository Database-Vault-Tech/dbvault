package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
)

// Token prefixes make leaked tokens easy to identify and scan for.
const (
	SessionTokenPrefix = "dbvs_"
	APITokenPrefix     = "dbv_"
	ResetTokenPrefix   = "dbvr_"
	InviteTokenPrefix  = "dbvi_"
	MFATokenPrefix     = "dbvm_"
)

// Hasher derives lookup hashes for bearer secrets. Only HMAC-SHA256 digests
// (keyed with AUTH_SECRET) are stored, so a database leak alone neither
// reveals nor lets anyone verify tokens.
type Hasher struct {
	secret []byte
}

func NewHasher(secret []byte) *Hasher { return &Hasher{secret: secret} }

// NewToken returns a fresh random token (256 bits) with prefix and its hash.
func (h *Hasher) NewToken(prefix string) (string, []byte, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", nil, err
	}
	tok := prefix + base64.RawURLEncoding.EncodeToString(b)
	return tok, h.Hash(tok), nil
}

// Hash returns the lookup hash of a token.
func (h *Hasher) Hash(token string) []byte {
	m := hmac.New(sha256.New, h.secret)
	m.Write([]byte("token:"))
	m.Write([]byte(token))
	return m.Sum(nil)
}

// CSRFToken derives the CSRF token bound to a session (signed double-submit
// cookie pattern): it can't be forged without AUTH_SECRET and is useless
// with any other session.
func (h *Hasher) CSRFToken(sessionID string) string {
	m := hmac.New(sha256.New, h.secret)
	m.Write([]byte("csrf:"))
	m.Write([]byte(sessionID))
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}

// ValidCSRF compares a presented CSRF token in constant time.
func (h *Hasher) ValidCSRF(sessionID, presented string) bool {
	want := h.CSRFToken(sessionID)
	return presented != "" && subtle.ConstantTimeCompare([]byte(want), []byte(presented)) == 1
}
