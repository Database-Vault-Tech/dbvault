package auth

import (
	"strings"
	"testing"
)

func TestPasswordHashing(t *testing.T) {
	h, err := HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(h, "$argon2id$v=19$") || strings.Contains(h, "correct horse") {
		t.Fatalf("unexpected hash format %q", h)
	}
	ok, err := VerifyPassword("correct horse battery staple", h)
	if err != nil || !ok {
		t.Fatal("correct password rejected")
	}
	ok, _ = VerifyPassword("wrong password", h)
	if ok {
		t.Fatal("wrong password accepted")
	}
	h2, _ := HashPassword("correct horse battery staple")
	if h == h2 {
		t.Fatal("hashes must be salted")
	}
	if _, err := VerifyPassword("x", "$bcrypt$nope"); err == nil {
		t.Fatal("unknown formats must error")
	}
}

func TestPasswordStrength(t *testing.T) {
	if ValidatePasswordStrength("short") == "" {
		t.Error("short password accepted")
	}
	if ValidatePasswordStrength("password12") == "" {
		t.Error("guessable password accepted")
	}
	if ValidatePasswordStrength("tangerine-orbit-42") != "" {
		t.Error("reasonable password rejected")
	}
}

func TestTokens(t *testing.T) {
	h := NewHasher([]byte("0123456789abcdef0123456789abcdef"))
	tok, hash, err := h.NewToken(APITokenPrefix)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(tok, "dbv_") || len(tok) < 40 {
		t.Fatalf("unexpected token %q", tok)
	}
	if string(h.Hash(tok)) != string(hash) {
		t.Fatal("hash must be deterministic")
	}
	other := NewHasher([]byte("another-secret-another-secret-xx"))
	if string(other.Hash(tok)) == string(hash) {
		t.Fatal("hash must depend on AUTH_SECRET")
	}
	tok2, _, _ := h.NewToken(APITokenPrefix)
	if tok == tok2 {
		t.Fatal("tokens must be random")
	}
}

func TestCSRF(t *testing.T) {
	h := NewHasher([]byte("0123456789abcdef0123456789abcdef"))
	tok := h.CSRFToken("session-a")
	if !h.ValidCSRF("session-a", tok) {
		t.Fatal("valid token rejected")
	}
	if h.ValidCSRF("session-b", tok) {
		t.Fatal("token from another session accepted")
	}
	if h.ValidCSRF("session-a", "") || h.ValidCSRF("session-a", tok+"x") {
		t.Fatal("invalid token accepted")
	}
}

func TestRoles(t *testing.T) {
	cases := []struct {
		role, min string
		ok        bool
	}{
		{RoleOwner, RoleAdmin, true},
		{RoleAdmin, RoleAdmin, true},
		{RoleMember, RoleAdmin, false},
		{RoleViewer, RoleMember, false},
		{RoleMember, RoleViewer, true},
		{"", RoleViewer, false},
		{RoleOwner, "superuser", false},
	}
	for _, c := range cases {
		if got := RoleAtLeast(c.role, c.min); got != c.ok {
			t.Errorf("RoleAtLeast(%q,%q) = %v", c.role, c.min, got)
		}
	}
	if ValidRole("root") || !ValidRole(RoleViewer) {
		t.Error("ValidRole is wrong")
	}
}
