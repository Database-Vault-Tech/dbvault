package notifications

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"fmt"
	"mime"
	"mime/quotedprintable"
	"net"
	"net/smtp"
	"strconv"
	"strings"
	"time"

	"github.com/dbvault/dbvault/backend/internal/config"
)

// SMTPMailer sends plain-text email over SMTP (STARTTLS, implicit TLS, or
// plaintext for local tools such as Mailpit).
type SMTPMailer struct {
	cfg config.SMTPConfig
}

func NewSMTPMailer(cfg config.SMTPConfig) *SMTPMailer { return &SMTPMailer{cfg: cfg} }

func (m *SMTPMailer) Configured() bool { return m != nil && m.cfg.Configured() }

// Send delivers one message. Header values are sanitised so user-controlled
// text can't inject additional headers.
func (m *SMTPMailer) Send(ctx context.Context, to, subject, body string) error {
	if !m.Configured() {
		return errors.New("SMTP is not configured (set SMTP_HOST and SMTP_FROM)")
	}
	if strings.ContainsAny(to, "\r\n") {
		return errors.New("invalid recipient")
	}
	msg, err := buildMessage(m.cfg.From, to, subject, body)
	if err != nil {
		return err
	}

	addr := net.JoinHostPort(m.cfg.Host, strconv.Itoa(m.cfg.Port))
	deadline, ok := ctx.Deadline()
	if !ok {
		deadline = time.Now().Add(30 * time.Second)
	}
	dialer := &net.Dialer{Deadline: deadline}
	tlsCfg := &tls.Config{ServerName: m.cfg.Host, MinVersion: tls.VersionTLS12}

	var conn net.Conn
	if m.cfg.TLSMode == "tls" {
		conn, err = tls.DialWithDialer(dialer, "tcp", addr, tlsCfg)
	} else {
		conn, err = dialer.DialContext(ctx, "tcp", addr)
	}
	if err != nil {
		return fmt.Errorf("connect to SMTP server: %w", err)
	}
	_ = conn.SetDeadline(deadline)
	c, err := smtp.NewClient(conn, m.cfg.Host)
	if err != nil {
		conn.Close()
		return fmt.Errorf("SMTP handshake: %w", err)
	}
	defer c.Close()

	if m.cfg.TLSMode == "starttls" {
		if ok, _ := c.Extension("STARTTLS"); !ok {
			return errors.New("SMTP server does not support STARTTLS (set SMTP_TLS=tls or none)")
		}
		if err := c.StartTLS(tlsCfg); err != nil {
			return fmt.Errorf("SMTP STARTTLS: %w", err)
		}
	}
	if m.cfg.Username != "" {
		if err := c.Auth(smtp.PlainAuth("", m.cfg.Username, m.cfg.Password, m.cfg.Host)); err != nil {
			return fmt.Errorf("SMTP authentication failed: %w", err)
		}
	}
	if err := c.Mail(addressOnly(m.cfg.From)); err != nil {
		return fmt.Errorf("SMTP MAIL FROM: %w", err)
	}
	if err := c.Rcpt(to); err != nil {
		return fmt.Errorf("SMTP RCPT TO: %w", err)
	}
	w, err := c.Data()
	if err != nil {
		return fmt.Errorf("SMTP DATA: %w", err)
	}
	if _, err := w.Write(msg); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("SMTP send: %w", err)
	}
	return c.Quit()
}

func addressOnly(from string) string {
	if i := strings.LastIndex(from, "<"); i >= 0 {
		return strings.TrimSuffix(from[i+1:], ">")
	}
	return from
}

func sanitizeHeader(s string) string {
	return strings.NewReplacer("\r", " ", "\n", " ").Replace(s)
}

func buildMessage(from, to, subject, body string) ([]byte, error) {
	var buf bytes.Buffer
	id := make([]byte, 12)
	_, _ = rand.Read(id)
	domain := "dbvault.local"
	if at := strings.LastIndex(addressOnly(from), "@"); at >= 0 {
		domain = addressOnly(from)[at+1:]
	}
	headers := [][2]string{
		{"From", sanitizeHeader(from)},
		{"To", sanitizeHeader(to)},
		{"Subject", mime.QEncoding.Encode("utf-8", sanitizeHeader(subject))},
		{"Date", time.Now().Format(time.RFC1123Z)},
		{"Message-ID", "<" + hex.EncodeToString(id) + "@" + domain + ">"},
		{"MIME-Version", "1.0"},
		{"Content-Type", "text/plain; charset=utf-8"},
		{"Content-Transfer-Encoding", "quoted-printable"},
		{"X-Mailer", "DBVault"},
	}
	for _, h := range headers {
		buf.WriteString(h[0] + ": " + h[1] + "\r\n")
	}
	buf.WriteString("\r\n")
	qp := quotedprintable.NewWriter(&buf)
	if _, err := qp.Write([]byte(strings.ReplaceAll(body, "\n", "\r\n"))); err != nil {
		return nil, err
	}
	if err := qp.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
