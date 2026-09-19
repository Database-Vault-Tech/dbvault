package logging

import (
	"bytes"
	"strings"
	"testing"
)

func TestRedactsSecrets(t *testing.T) {
	var buf bytes.Buffer
	l := NewWithWriter(&buf, "info", "test")
	l.Info("connecting", "password", "hunter2", "db_password", "hunter3", "s3_secret_key", "abc", "Authorization", "Bearer x",
		"api_token", "dbv_123", "host", "db.internal")
	out := buf.String()
	for _, secret := range []string{"hunter2", "hunter3", "abc\"", "Bearer x", "dbv_123"} {
		if strings.Contains(out, secret) {
			t.Errorf("log output leaked %q: %s", secret, out)
		}
	}
	if !strings.Contains(out, "db.internal") || !strings.Contains(out, `"service":"test"`) {
		t.Errorf("non-sensitive fields missing: %s", out)
	}
}
