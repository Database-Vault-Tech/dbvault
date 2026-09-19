package validate

import "testing"

func TestIsHost(t *testing.T) {
	good := []string{"localhost", "db.example.com", "10.0.0.5", "::1", "[::1]", "my_db-host.internal", "a"}
	bad := []string{"", "host:5432", "http://x", "a b", "/var/run/postgresql", "x/y", "-bad.example"}
	for _, h := range good {
		if !IsHost(h) {
			t.Errorf("%q should be a valid host", h)
		}
	}
	for _, h := range bad {
		if IsHost(h) {
			t.Errorf("%q should be rejected", h)
		}
	}
}

func TestIdentifiers(t *testing.T) {
	if !IsPGIdentifier("shop_restored") || !IsPGIdentifier("app-2026") {
		t.Error("valid identifiers rejected")
	}
	for _, s := range []string{"", "1abc", "drop table;", "a\"b", "x y", "db=evil"} {
		if IsPGIdentifier(s) {
			t.Errorf("%q should be rejected", s)
		}
	}
	if !IsResourceName("production db-1") || IsResourceName(" leading") || IsResourceName("<script>") {
		t.Error("IsResourceName is wrong")
	}
}

func TestValidatorCollectsFirstError(t *testing.T) {
	v := New()
	v.Required("name", "")
	v.MaxLen("name", "", 3)
	v.Email("email", "not-an-email")
	v.OneOf("mode", "x", "a", "b")
	v.Range("port", 70000, 1, 65535)
	err := v.Err()
	if err == nil {
		t.Fatal("expected error")
	}
	if !v.Has("name") || !v.Has("email") || !v.Has("mode") || !v.Has("port") {
		t.Fatalf("missing field errors: %v", err)
	}
	if New().Err() != nil {
		t.Fatal("empty validator should pass")
	}
}
