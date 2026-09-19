package notifications

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestWebhookDeliveryIsSigned(t *testing.T) {
	var gotSig, gotTS, gotEvent string
	var body []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotSig, gotTS, gotEvent = r.Header.Get("X-DBVault-Signature"), r.Header.Get("X-DBVault-Timestamp"), r.Header.Get("X-DBVault-Event")
		body, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	s := NewWebhookSender(true)
	ch := ResolvedChannel{Channel: Channel{Name: "ops"}, Secrets: Secrets{URL: srv.URL, SigningSecret: "whsec_test"}}
	ev := Event{Type: EventBackupFailed, Title: "Backup of production failed", Message: "Connection timeout", OccurredAt: time.Now()}
	status, err := s.Send(context.Background(), ch, ev)
	if err != nil || status != http.StatusNoContent {
		t.Fatalf("send: %d %v", status, err)
	}
	if gotEvent != EventBackupFailed {
		t.Errorf("event header %q", gotEvent)
	}
	m := hmac.New(sha256.New, []byte("whsec_test"))
	m.Write([]byte(gotTS + "."))
	m.Write(body)
	if gotSig != "sha256="+hex.EncodeToString(m.Sum(nil)) {
		t.Fatal("signature does not verify")
	}
	var decoded Event
	if err := json.Unmarshal(body, &decoded); err != nil || decoded.Title != ev.Title {
		t.Fatalf("body: %s", body)
	}
}

func TestWebhookNon2xxIsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	s := NewWebhookSender(true)
	status, err := s.Send(context.Background(), ResolvedChannel{Secrets: Secrets{URL: srv.URL}}, Event{Type: "test"})
	if err == nil || status != 500 {
		t.Fatalf("expected failure, got %d %v", status, err)
	}
}

func TestWebhookDoesNotFollowRedirects(t *testing.T) {
	hit := false
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hit = true }))
	defer target.Close()
	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusFound)
	}))
	defer redirector.Close()
	s := NewWebhookSender(true)
	_, err := s.Send(context.Background(), ResolvedChannel{Secrets: Secrets{URL: redirector.URL}}, Event{Type: "test"})
	if err == nil || hit {
		t.Fatal("redirects must not be followed")
	}
}

func TestWebhookBlocksPrivateNetworks(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()
	s := NewWebhookSender(false) // hosted mode
	_, err := s.Send(context.Background(), ResolvedChannel{Secrets: Secrets{URL: srv.URL}}, Event{Type: "test"})
	if err == nil || !strings.Contains(err.Error(), "not a public address") {
		t.Fatalf("loopback target must be blocked, got %v", err)
	}
}

func TestEmailMessageHeadersCannotBeInjected(t *testing.T) {
	msg, err := buildMessage("DBVault <alerts@example.com>", "ops@example.com", "Backup failed\r\nBcc: attacker@evil.test", "body")
	if err != nil {
		t.Fatal(err)
	}
	headers := strings.SplitN(string(msg), "\r\n\r\n", 2)[0]
	for _, line := range strings.Split(headers, "\r\n") {
		if strings.HasPrefix(strings.ToLower(line), "bcc:") {
			t.Fatalf("header injection: %q", headers)
		}
	}
}

func TestInputValidation(t *testing.T) {
	url := "ftp://example.com"
	in := Input{Name: "hook", Type: "webhook", URL: &url, Events: []string{EventBackupFailed}}
	if in.Validate(true) == nil {
		t.Error("non-http webhook must be rejected")
	}
	in = Input{Name: "mail", Type: "email", Recipients: []string{"ops@example.com"}, Events: []string{"backup.exploded"}}
	if in.Validate(true) == nil {
		t.Error("unknown event must be rejected")
	}
	in = Input{Name: "mail", Type: "email", Recipients: []string{"Ops@Example.com "}, Events: []string{EventBackupFailed}}
	if err := in.Validate(true); err != nil || in.Recipients[0] != "ops@example.com" {
		t.Errorf("valid email channel rejected: %v", err)
	}
}
