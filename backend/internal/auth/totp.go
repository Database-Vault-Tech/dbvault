package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// TOTP parameters (RFC 6238). These are the defaults every authenticator app
// assumes, so they are not configurable.
const (
	totpDigits = 6
	totpPeriod = 30
	// totpSkew accepts codes from one step either side of now, tolerating
	// clock drift and a code typed just as it rolls over.
	totpSkew = 1

	recoveryCodeCount = 10
)

var b32 = base32.StdEncoding.WithPadding(base32.NoPadding)

// NewTOTPSecret returns a random 160-bit secret, base32-encoded as
// authenticator apps expect.
func NewTOTPSecret() (string, error) {
	b := make([]byte, 20)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return b32.EncodeToString(b), nil
}

// TOTPURI is the otpauth:// URI encoded in the setup QR code.
func TOTPURI(issuer, account, secret string) string {
	label := url.PathEscape(issuer + ":" + account)
	q := url.Values{}
	q.Set("secret", secret)
	q.Set("issuer", issuer)
	q.Set("algorithm", "SHA1")
	q.Set("digits", fmt.Sprint(totpDigits))
	q.Set("period", fmt.Sprint(totpPeriod))
	return "otpauth://totp/" + label + "?" + q.Encode()
}

// totpStep is the RFC 6238 time step containing t.
func totpStep(t time.Time) int64 { return t.Unix() / totpPeriod }

// hotp computes an RFC 4226 code for counter.
func hotp(key []byte, counter int64, digits int) string {
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], uint64(counter))
	m := hmac.New(sha1.New, key)
	m.Write(msg[:])
	sum := m.Sum(nil)
	off := sum[len(sum)-1] & 0x0f
	bin := binary.BigEndian.Uint32(sum[off:off+4]) & 0x7fffffff
	mod := uint32(1)
	for range digits {
		mod *= 10
	}
	return fmt.Sprintf("%0*d", digits, bin%mod)
}

// matchTOTP returns the time step code is valid for at now (within the
// allowed skew), or ok=false.
func matchTOTP(secret, code string, now time.Time) (step int64, ok bool) {
	key, err := b32.DecodeString(strings.ToUpper(secret))
	if err != nil || len(code) != totpDigits {
		return 0, false
	}
	cur := totpStep(now)
	// Check every candidate step so timing doesn't reveal which one matched.
	for s := cur - totpSkew; s <= cur+totpSkew; s++ {
		if subtle.ConstantTimeCompare([]byte(hotp(key, s, totpDigits)), []byte(code)) == 1 && !ok {
			step, ok = s, true
		}
	}
	return step, ok
}

// normalizeCode strips spaces and dashes users type or paste around codes,
// and lowercases recovery codes.
func normalizeCode(code string) string {
	return strings.ToLower(strings.NewReplacer(" ", "", "-", "", "\t", "").Replace(strings.TrimSpace(code)))
}

// isTOTPCode reports whether a normalized code looks like an authenticator
// code (six digits) rather than a recovery code.
func isTOTPCode(code string) bool {
	if len(code) != totpDigits {
		return false
	}
	for _, c := range code {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// recoveryAlphabet avoids characters that are easy to confuse when written
// down (0/o, 1/l/i).
const recoveryAlphabet = "abcdefghjkmnpqrstuvwxyz23456789"

// NewRecoveryCodes returns fresh single-use recovery codes formatted as
// xxxxx-xxxxx (10 characters, ~49 bits each).
func NewRecoveryCodes() ([]string, error) {
	codes := make([]string, recoveryCodeCount)
	for i := range codes {
		b := make([]byte, 10)
		if _, err := rand.Read(b); err != nil {
			return nil, err
		}
		var sb strings.Builder
		for j, v := range b {
			if j == 5 {
				sb.WriteByte('-')
			}
			// 256 % 31 != 0, but the resulting bias (<1%) is irrelevant
			// for rate-limited, HMAC-stored, single-use codes.
			sb.WriteByte(recoveryAlphabet[int(v)%len(recoveryAlphabet)])
		}
		codes[i] = sb.String()
	}
	return codes, nil
}

// recoveryCodeHash is the stored digest of a normalized recovery code,
// scoped to the user so equal codes never collide across accounts.
func (h *Hasher) recoveryCodeHash(userID, normalized string) []byte {
	return h.Hash("recovery:" + userID + ":" + normalized)
}
