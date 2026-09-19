// Package config loads DBVault configuration from the environment.
//
// Every setting is documented in .env.example at the repository root.
package config

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Verification sandbox modes.
const (
	VerifyModeDocker   = "docker"
	VerifyModeServer   = "server"
	VerifyModeDisabled = "disabled"
)

type Config struct {
	Env      string
	LogLevel string

	// HTTP
	APIAddr           string
	AppURL            string
	CookieSecure      bool
	TrustProxyHeaders bool
	AllowRegistration bool

	// Infrastructure
	DatabaseURL    string
	RedisURL       string
	MigrateOnStart bool

	// Secrets
	AuthSecret    []byte
	EncryptionKey []byte

	// Built-in S3-compatible storage (MinIO in docker compose). Optional.
	S3 S3Defaults

	// Filesystem storage root. Local storage destinations are confined to it.
	LocalStorageRoot string

	// Worker
	WorkerConcurrency   int
	WorkerHealthAddr    string
	WorkDir             string
	PgBinDir            string
	VerifyUploadedData  bool
	ShutdownGracePeriod time.Duration

	// Restore verification sandbox
	VerifyMode          string
	VerifyPostgresURL   string
	VerifyDockerHost    string
	VerifyDockerNetwork string
	VerifyDockerImage   string

	// Scheduler
	SchedulerHealthAddr string

	// Notifications
	SMTP                       SMTPConfig
	AllowPrivateNetworkTargets bool

	// Rate limiting
	RateLimitAuthPerMinute int
	RateLimitAPIPerMinute  int
}

type S3Defaults struct {
	Endpoint       string
	PublicEndpoint string
	Region         string
	Bucket         string
	AccessKey      string
	SecretKey      string
	ForcePathStyle bool
}

// Configured reports whether a built-in S3-compatible store is available.
func (s S3Defaults) Configured() bool {
	return s.Endpoint != "" && s.Bucket != "" && s.AccessKey != "" && s.SecretKey != ""
}

type SMTPConfig struct {
	Host     string
	Port     int
	Username string
	Password string
	From     string
	TLSMode  string // starttls | tls | none
}

func (s SMTPConfig) Configured() bool { return s.Host != "" && s.From != "" }

// Load reads configuration from the environment and validates it.
func Load() (*Config, error) {
	c := &Config{
		Env:                        env("DBVAULT_ENV", "production"),
		LogLevel:                   env("LOG_LEVEL", "info"),
		APIAddr:                    env("API_ADDR", ":8080"),
		AppURL:                     strings.TrimRight(env("APP_URL", "http://localhost:3000"), "/"),
		TrustProxyHeaders:          envBool("TRUST_PROXY_HEADERS", false),
		AllowRegistration:          envBool("ALLOW_REGISTRATION", true),
		DatabaseURL:                env("DATABASE_URL", ""),
		RedisURL:                   env("REDIS_URL", "redis://localhost:6379/0"),
		MigrateOnStart:             envBool("MIGRATE_ON_START", true),
		LocalStorageRoot:           env("LOCAL_STORAGE_ROOT", "/var/lib/dbvault/backups"),
		WorkerConcurrency:          envInt("WORKER_CONCURRENCY", 2),
		WorkerHealthAddr:           env("WORKER_HEALTH_ADDR", ":8081"),
		WorkDir:                    env("WORK_DIR", filepath.Join(os.TempDir(), "dbvault")),
		PgBinDir:                   env("PG_BIN_DIR", ""),
		VerifyUploadedData:         envBool("VERIFY_UPLOADED_DATA", true),
		ShutdownGracePeriod:        envDuration("WORKER_SHUTDOWN_GRACE_PERIOD", 5*time.Minute),
		VerifyMode:                 env("VERIFY_MODE", VerifyModeDisabled),
		VerifyPostgresURL:          env("VERIFY_POSTGRES_URL", ""),
		VerifyDockerHost:           env("VERIFY_DOCKER_HOST", "unix:///var/run/docker.sock"),
		VerifyDockerNetwork:        env("VERIFY_DOCKER_NETWORK", ""),
		VerifyDockerImage:          env("VERIFY_DOCKER_IMAGE", "postgres:{major}-alpine"),
		SchedulerHealthAddr:        env("SCHEDULER_HEALTH_ADDR", ":8082"),
		AllowPrivateNetworkTargets: envBool("ALLOW_PRIVATE_NETWORK_TARGETS", true),
		RateLimitAuthPerMinute:     envInt("RATE_LIMIT_AUTH_PER_MINUTE", 10),
		RateLimitAPIPerMinute:      envInt("RATE_LIMIT_API_PER_MINUTE", 600),
		S3: S3Defaults{
			Endpoint:       env("S3_ENDPOINT", ""),
			PublicEndpoint: env("S3_PUBLIC_ENDPOINT", ""),
			Region:         env("S3_REGION", "us-east-1"),
			Bucket:         env("S3_BUCKET", ""),
			AccessKey:      env("S3_ACCESS_KEY", ""),
			SecretKey:      env("S3_SECRET_KEY", ""),
			ForcePathStyle: envBool("S3_FORCE_PATH_STYLE", true),
		},
		SMTP: SMTPConfig{
			Host:     env("SMTP_HOST", ""),
			Port:     envInt("SMTP_PORT", 587),
			Username: env("SMTP_USERNAME", ""),
			Password: env("SMTP_PASSWORD", ""),
			From:     env("SMTP_FROM", ""),
			TLSMode:  env("SMTP_TLS", "starttls"),
		},
	}
	c.CookieSecure = envBool("COOKIE_SECURE", strings.HasPrefix(c.AppURL, "https://"))

	if c.DatabaseURL == "" {
		return nil, errors.New("DATABASE_URL is required")
	}
	if _, err := url.Parse(c.AppURL); err != nil {
		return nil, fmt.Errorf("APP_URL is invalid: %w", err)
	}
	if err := c.loadSecrets(); err != nil {
		return nil, err
	}
	switch c.VerifyMode {
	case VerifyModeDocker, VerifyModeDisabled:
	case VerifyModeServer:
		if c.VerifyPostgresURL == "" {
			return nil, errors.New("VERIFY_POSTGRES_URL is required when VERIFY_MODE=server")
		}
	default:
		return nil, fmt.Errorf("VERIFY_MODE must be one of docker, server, disabled (got %q)", c.VerifyMode)
	}
	switch c.SMTP.TLSMode {
	case "starttls", "tls", "none":
	default:
		return nil, fmt.Errorf("SMTP_TLS must be one of starttls, tls, none (got %q)", c.SMTP.TLSMode)
	}
	if c.WorkerConcurrency < 1 {
		c.WorkerConcurrency = 1
	}
	return c, nil
}

func (c *Config) IsDevelopment() bool { return c.Env == "development" }

// loadSecrets resolves AUTH_SECRET and ENCRYPTION_KEY.
//
// Secrets come from the environment first. When they are absent and
// DBVAULT_SECRETS_FILE is set, they are read from that file; if the file does
// not exist and AUTO_GENERATE_SECRETS=true, strong random secrets are
// generated once and persisted with 0600 permissions. This lets
// `docker compose up` work without shipping any default secrets.
func (c *Config) loadSecrets() error {
	authSecret := os.Getenv("AUTH_SECRET")
	encKey := os.Getenv("ENCRYPTION_KEY")

	if (authSecret == "" || encKey == "") && os.Getenv("DBVAULT_SECRETS_FILE") != "" {
		fileVals, err := loadOrCreateSecretsFile(os.Getenv("DBVAULT_SECRETS_FILE"), envBool("AUTO_GENERATE_SECRETS", false))
		if err != nil {
			return err
		}
		if authSecret == "" {
			authSecret = fileVals["AUTH_SECRET"]
		}
		if encKey == "" {
			encKey = fileVals["ENCRYPTION_KEY"]
		}
	}

	if len(authSecret) < 32 {
		return errors.New("AUTH_SECRET must be set and at least 32 characters (generate with: openssl rand -base64 48)")
	}
	c.AuthSecret = []byte(authSecret)

	if encKey == "" {
		return errors.New("ENCRYPTION_KEY must be set (generate with: openssl rand -base64 32)")
	}
	key, err := base64.StdEncoding.DecodeString(encKey)
	if err != nil || len(key) != 32 {
		return errors.New("ENCRYPTION_KEY must be 32 random bytes, base64-encoded (generate with: openssl rand -base64 32)")
	}
	c.EncryptionKey = key
	return nil
}

func loadOrCreateSecretsFile(path string, autoGenerate bool) (map[string]string, error) {
	vals, err := readSecretsFile(path)
	if err == nil {
		return vals, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("read secrets file: %w", err)
	}
	if !autoGenerate {
		return nil, fmt.Errorf("secrets file %s does not exist and AUTO_GENERATE_SECRETS is not enabled", path)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create secrets dir: %w", err)
	}
	auth := make([]byte, 48)
	enc := make([]byte, 32)
	if _, err := rand.Read(auth); err != nil {
		return nil, err
	}
	if _, err := rand.Read(enc); err != nil {
		return nil, err
	}
	content := "# Generated by DBVault on first start. BACK THIS FILE UP.\n" +
		"# Losing ENCRYPTION_KEY makes every encrypted backup unrecoverable.\n" +
		"AUTH_SECRET=" + base64.StdEncoding.EncodeToString(auth) + "\n" +
		"ENCRYPTION_KEY=" + base64.StdEncoding.EncodeToString(enc) + "\n"

	// Write to a uniquely named temp file, then hard-link it into place: the
	// link fails if another service (api, worker, scheduler all start
	// together) won the race, in which case we read the winner's file.
	// A random suffix is required because every container runs as PID 1.
	suffix := make([]byte, 8)
	if _, err := rand.Read(suffix); err != nil {
		return nil, err
	}
	tmp := path + ".tmp-" + hex.EncodeToString(suffix)
	if err := os.WriteFile(tmp, []byte(content), 0o600); err != nil {
		return nil, fmt.Errorf("write secrets file: %w", err)
	}
	if err := os.Link(tmp, path); err != nil && !errors.Is(err, os.ErrExist) {
		_ = os.Remove(tmp)
		return nil, fmt.Errorf("persist secrets file: %w", err)
	}
	_ = os.Remove(tmp)
	return readSecretsFile(path)
}

func readSecretsFile(path string) (map[string]string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	vals := map[string]string{}
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if ok {
			vals[strings.TrimSpace(k)] = strings.TrimSpace(v)
		}
	}
	return vals, nil
}

func env(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return def
}

func envBool(key string, def bool) bool {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return def
	}
	return b
}

func envInt(key string, def int) int {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	i, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return i
}

func envDuration(key string, def time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return def
	}
	return d
}
