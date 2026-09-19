package notifications

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Sender delivers an event through one channel type. Adding Slack or
// Discord means implementing Sender and registering it in Senders.
type Sender interface {
	Send(ctx context.Context, ch ResolvedChannel, ev Event) (status int, err error)
}

// ResolvedChannel is a channel with its secrets decrypted (worker only).
type ResolvedChannel struct {
	Channel
	Secrets Secrets
}

// Secrets is the sealed part of a channel.
type Secrets struct {
	URL           string `json:"url,omitempty"`
	SigningSecret string `json:"signing_secret,omitempty"`
}

// EmailSender delivers events as plain-text email.
type EmailSender struct {
	Mailer *SMTPMailer
}

func (s *EmailSender) Send(ctx context.Context, ch ResolvedChannel, ev Event) (int, error) {
	if !s.Mailer.Configured() {
		return 0, errors.New("SMTP is not configured on this server (set SMTP_HOST and SMTP_FROM)")
	}
	subject := "[DBVault] " + ev.Title
	body := ev.Title + "\n\n" + ev.Message + "\n"
	if len(ev.Details) > 0 {
		body += "\n"
		for _, d := range ev.Details {
			body += fmt.Sprintf("%s: %s\n", d.Label, d.Value)
		}
	}
	if ev.URL != "" {
		body += "\nOpen in DBVault: " + ev.URL + "\n"
	}
	body += "\n--\nYou receive this because the \"" + ch.Name + "\" notification channel is subscribed to " + ev.Type + ".\n"
	var errs []string
	for _, to := range ch.Config.Recipients {
		if err := s.Mailer.Send(ctx, to, subject, body); err != nil {
			errs = append(errs, to+": "+err.Error())
		}
	}
	if len(errs) > 0 {
		return 0, errors.New(strings.Join(errs, "; "))
	}
	return 0, nil
}

// WebhookSender POSTs events as JSON, signed with HMAC-SHA256:
//
//	X-DBVault-Signature: sha256=hex(HMAC(secret, timestamp + "." + body))
//
// Receivers should recompute the signature and reject stale timestamps.
type WebhookSender struct {
	AllowPrivateNetworks bool
	client               *http.Client
}

func NewWebhookSender(allowPrivate bool) *WebhookSender {
	dialer := &net.Dialer{Timeout: 5 * time.Second}
	if !allowPrivate {
		dialer.Control = denyPrivate
	}
	return &WebhookSender{
		AllowPrivateNetworks: allowPrivate,
		client: &http.Client{
			Timeout: 10 * time.Second,
			Transport: &http.Transport{
				DialContext:           dialer.DialContext,
				TLSHandshakeTimeout:   5 * time.Second,
				ResponseHeaderTimeout: 10 * time.Second,
				Proxy:                 nil,
			},
			// Never follow redirects: they could point at internal services.
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
	}
}

// denyPrivate blocks connections to loopback, private, link-local and other
// non-public addresses (SSRF protection for hosted deployments).
func denyPrivate(_, address string, _ syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return err
	}
	ip := net.ParseIP(host)
	if ip == nil || ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsUnspecified() || ip.IsMulticast() || ip.IsInterfaceLocalMulticast() {
		return fmt.Errorf("webhook target %s is not a public address", host)
	}
	return nil
}

func (s *WebhookSender) Send(ctx context.Context, ch ResolvedChannel, ev Event) (int, error) {
	body, err := json.Marshal(ev)
	if err != nil {
		return 0, err
	}
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, ch.Secrets.URL, bytes.NewReader(body))
	if err != nil {
		return 0, fmt.Errorf("invalid webhook URL")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "DBVault-Webhook/1.0")
	req.Header.Set("X-DBVault-Event", ev.Type)
	req.Header.Set("X-DBVault-Delivery", ev.DeliveryID)
	req.Header.Set("X-DBVault-Timestamp", ts)
	if ch.Secrets.SigningSecret != "" {
		req.Header.Set("X-DBVault-Signature", "sha256="+Sign(ch.Secrets.SigningSecret, ts, body))
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("webhook request failed: %s", scrubURL(err.Error(), ch.Secrets.URL))
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return resp.StatusCode, fmt.Errorf("webhook returned HTTP %d", resp.StatusCode)
	}
	return resp.StatusCode, nil
}

// Sign computes the webhook signature.
func Sign(secret, timestamp string, body []byte) string {
	m := hmac.New(sha256.New, []byte(secret))
	m.Write([]byte(timestamp))
	m.Write([]byte("."))
	m.Write(body)
	return hex.EncodeToString(m.Sum(nil))
}

// scrubURL removes the (secret-bearing) webhook URL from error messages.
func scrubURL(msg, url string) string {
	if url == "" {
		return msg
	}
	return strings.ReplaceAll(msg, url, "[webhook url]")
}
