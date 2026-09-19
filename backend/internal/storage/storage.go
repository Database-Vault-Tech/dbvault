// Package storage abstracts where backup artifacts live.
//
// Every implementation streams: Upload consumes an io.Reader of unknown
// length and Download returns an io.ReadCloser, so backup size never
// determines memory usage.
package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
)

// Storage is implemented by every backup destination.
type Storage interface {
	// Upload stores everything read from r under key and returns the byte count.
	// On error, no partial object is left behind.
	Upload(ctx context.Context, key string, r io.Reader) (int64, error)
	Download(ctx context.Context, key string) (io.ReadCloser, error)
	Delete(ctx context.Context, key string) error
	Exists(ctx context.Context, key string) (bool, error)
	Stat(ctx context.Context, key string) (Object, error)
	List(ctx context.Context, prefix string) ([]Object, error)
	// Describe returns a short human-readable location, e.g. "s3://bucket".
	Describe() string
}

type Object struct {
	Key          string    `json:"key"`
	Size         int64     `json:"size"`
	LastModified time.Time `json:"last_modified"`
}

// ErrNotFound is returned when an object does not exist.
var ErrNotFound = errors.New("object not found")

// Destination types.
const (
	TypeLocal = "local"
	TypeS3    = "s3"
	TypeR2    = "r2"
	TypeMinIO = "minio"
)

// Config is the non-secret part of a destination (stored as JSON).
type Config struct {
	Bucket         string `json:"bucket,omitempty"`
	Region         string `json:"region,omitempty"`
	Endpoint       string `json:"endpoint,omitempty"`
	AccountID      string `json:"account_id,omitempty"`
	ForcePathStyle bool   `json:"force_path_style,omitempty"`
	Prefix         string `json:"prefix,omitempty"`
	// Path is a directory relative to LOCAL_STORAGE_ROOT (local type only).
	Path string `json:"path,omitempty"`
}

// Credentials is the secret part of a destination (stored sealed).
type Credentials struct {
	AccessKeyID     string `json:"access_key_id"`
	SecretAccessKey string `json:"secret_access_key"`
}

// Options carries server-level settings needed to build destinations.
type Options struct {
	LocalRoot string
}

// New builds a Storage for the given destination type.
func New(ctx context.Context, typ string, cfg Config, creds Credentials, opts Options) (Storage, error) {
	switch typ {
	case TypeLocal:
		return NewLocal(opts.LocalRoot, cfg.Path)
	case TypeS3, TypeR2, TypeMinIO:
		return NewS3(ctx, typ, cfg, creds)
	default:
		return nil, fmt.Errorf("unsupported storage type %q", typ)
	}
}

// JoinKey joins a destination prefix and a relative key with single slashes.
func JoinKey(prefix, key string) string {
	prefix = strings.Trim(prefix, "/")
	key = strings.TrimLeft(key, "/")
	if prefix == "" {
		return key
	}
	return prefix + "/" + key
}

// Probe performs a write/read/delete round trip to prove a destination works.
func Probe(ctx context.Context, s Storage, prefix string) error {
	key := JoinKey(prefix, fmt.Sprintf(".dbvault-probe-%d", time.Now().UnixNano()))
	payload := "dbvault connectivity check"
	if _, err := s.Upload(ctx, key, strings.NewReader(payload)); err != nil {
		return fmt.Errorf("write test object: %w", err)
	}
	defer func() { _ = s.Delete(context.WithoutCancel(ctx), key) }()
	rc, err := s.Download(ctx, key)
	if err != nil {
		return fmt.Errorf("read test object: %w", err)
	}
	defer rc.Close()
	b, err := io.ReadAll(io.LimitReader(rc, 1024))
	if err != nil {
		return fmt.Errorf("read test object: %w", err)
	}
	if string(b) != payload {
		return errors.New("read test object: content mismatch")
	}
	if err := s.Delete(ctx, key); err != nil {
		return fmt.Errorf("delete test object: %w", err)
	}
	return nil
}
