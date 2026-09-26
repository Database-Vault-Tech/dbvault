package auth

import (
	"encoding/base32"
	"net/url"
	"strings"
	"testing"
	"time"
)

// RFC 6238 Appendix B test vectors (SHA-1, 8 digits, seed "12345678901234567890").
func TestHOTPMatchesRFC6238Vectors(t *testing.T) {
	key := []byte("12345678901234567890")
	cases := []struct {
		unix int64
		want string
	}{
		{59, "94287082"},
		{1111111109, "07081804"},
		{1111111111, "14050471"},
		{1234567890, "89005924"},
		{2000000000, "69279037"},
		{20000000000, "65353130"},
	}
	for _, c := range cases {
		if got := hotp(key, totpStep(time.Unix(c.unix, 0)), 8); got != c.want {
			t.Errorf("T=%d: got %s, want %s", c.unix, got, c.want)
		}
	}
}

func TestMatchTOTP(t *testing.T) {
	secret, err := NewTOTPSecret()
	if err != nil {
		t.Fatal(err)
	}
	key, _ := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(secret)
	now := time.Unix(1_700_000_000, 0)
	cur := totpStep(now)

	for _, off := range []int64{-1, 0, 1} {
		code := hotp(key, cur+off, totpDigits)
		step, ok := matchTOTP(secret, code, now)
		if !ok || step != cur+off {
			t.Errorf("offset %d: ok=%v step=%d", off, ok, step)
		}
	}
	for _, off := range []int64{-3, 2, 5} {
		if _, ok := matchTOTP(secret, hotp(key, cur+off, totpDigits), now); ok {
			t.Errorf("code %d steps away must be rejected", off)
		}
	}
	if _, ok := matchTOTP(secret, "12345", now); ok {
		t.Error("short code accepted")
	}
	if _, ok := matchTOTP("not base32!", "123456", now); ok {
		t.Error("invalid secret accepted")
	}
	// Lowercase secrets (as some apps display them) still verify.
	if _, ok := matchTOTP(strings.ToLower(secret), hotp(key, cur, totpDigits), now); !ok {
		t.Error("lowercase secret rejected")
	}
}

func TestTOTPURI(t *testing.T) {
	u, err := url.Parse(TOTPURI("DBVault", "ada@example.com", "JBSWY3DPEHPK3PXP"))
	if err != nil {
		t.Fatal(err)
	}
	if u.Scheme != "otpauth" || u.Host != "totp" {
		t.Fatalf("unexpected uri %s", u)
	}
	if u.Path != "/DBVault:ada@example.com" {
		t.Errorf("label = %q", u.Path)
	}
	q := u.Query()
	if q.Get("secret") != "JBSWY3DPEHPK3PXP" || q.Get("issuer") != "DBVault" || q.Get("digits") != "6" || q.Get("period") != "30" {
		t.Errorf("query = %v", q)
	}
}

func TestRecoveryCodes(t *testing.T) {
	codes, err := NewRecoveryCodes()
	if err != nil {
		t.Fatal(err)
	}
	if len(codes) != recoveryCodeCount {
		t.Fatalf("got %d codes", len(codes))
	}
	seen := map[string]bool{}
	for _, c := range codes {
		if len(c) != 11 || c[5] != '-' {
			t.Errorf("bad format %q", c)
		}
		if strings.ContainsAny(c, "01ilo") {
			t.Errorf("ambiguous characters in %q", c)
		}
		if seen[c] {
			t.Errorf("duplicate code %q", c)
		}
		seen[c] = true
		if isTOTPCode(normalizeCode(c)) {
			t.Errorf("recovery code %q mistaken for a TOTP code", c)
		}
	}
	if normalizeCode(" ABCDE-fghjk ") != "abcdefghjk" {
		t.Error("normalizeCode must strip dashes/spaces and lowercase")
	}
	if !isTOTPCode(normalizeCode("123 456")) || isTOTPCode("12345a") {
		t.Error("isTOTPCode is wrong")
	}

	h := NewHasher([]byte("0123456789abcdef0123456789abcdef"))
	if string(h.recoveryCodeHash("u1", "abcdefghjk")) == string(h.recoveryCodeHash("u2", "abcdefghjk")) {
		t.Error("recovery code hashes must be scoped to the user")
	}
}
