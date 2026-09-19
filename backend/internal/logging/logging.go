// Package logging configures structured JSON logging with secret redaction.
package logging

import (
	"context"
	"io"
	"log/slog"
	"os"
	"strings"
)

type ctxKey struct{}

// sensitiveKeys are attribute-name fragments whose values are never logged.
var sensitiveKeys = []string{"password", "secret", "token", "authorization", "cookie", "encryption_key", "access_key", "private_key", "api_key", "credential"}

// New returns a JSON logger that writes to stdout and redacts sensitive attributes.
func New(level, service string) *slog.Logger {
	return NewWithWriter(os.Stdout, level, service)
}

func NewWithWriter(w io.Writer, level, service string) *slog.Logger {
	var lvl slog.Level
	switch strings.ToLower(level) {
	case "debug":
		lvl = slog.LevelDebug
	case "warn", "warning":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}
	h := slog.NewJSONHandler(w, &slog.HandlerOptions{
		Level:       lvl,
		ReplaceAttr: redact,
	})
	return slog.New(h).With("service", service)
}

func redact(_ []string, a slog.Attr) slog.Attr {
	if IsSensitiveKey(a.Key) {
		return slog.String(a.Key, "[REDACTED]")
	}
	return a
}

// IsSensitiveKey reports whether values stored under key must never be logged.
func IsSensitiveKey(key string) bool {
	k := strings.ToLower(key)
	for _, s := range sensitiveKeys {
		if strings.Contains(k, s) {
			return true
		}
	}
	return false
}

// WithLogger stores a request- or job-scoped logger in ctx.
func WithLogger(ctx context.Context, l *slog.Logger) context.Context {
	return context.WithValue(ctx, ctxKey{}, l)
}

// FromContext returns the scoped logger, or the default logger.
func FromContext(ctx context.Context) *slog.Logger {
	if l, ok := ctx.Value(ctxKey{}).(*slog.Logger); ok {
		return l
	}
	return slog.Default()
}
