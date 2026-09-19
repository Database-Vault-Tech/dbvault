// Package app wires DBVault's services together. The API, worker and
// scheduler binaries all build the same App so behaviour is identical.
package app

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/dbvault/dbvault/backend/internal/auth"
	"github.com/dbvault/dbvault/backend/internal/backups"
	"github.com/dbvault/dbvault/backend/internal/config"
	"github.com/dbvault/dbvault/backend/internal/database"
	"github.com/dbvault/dbvault/backend/internal/db"
	"github.com/dbvault/dbvault/backend/internal/encryption"
	"github.com/dbvault/dbvault/backend/internal/engine"
	"github.com/dbvault/dbvault/backend/internal/engine/mysql"
	"github.com/dbvault/dbvault/backend/internal/engine/postgres"
	"github.com/dbvault/dbvault/backend/internal/jobs"
	"github.com/dbvault/dbvault/backend/internal/notifications"
	"github.com/dbvault/dbvault/backend/internal/organizations"
	"github.com/dbvault/dbvault/backend/internal/pgtools"
	"github.com/dbvault/dbvault/backend/internal/ratelimit"
	"github.com/dbvault/dbvault/backend/internal/restore"
	"github.com/dbvault/dbvault/backend/internal/scheduler"
	"github.com/dbvault/dbvault/backend/internal/storage"
)

// Version is set at build time with -ldflags "-X .../internal/app.Version=v1.2.3".
var Version = "dev"

type App struct {
	Config        *config.Config
	Log           *slog.Logger
	Pool          *pgxpool.Pool
	Redis         *redis.Client
	Sealer        *encryption.Sealer
	Keys          *encryption.KeyStore
	Queue         *jobs.Queue
	Limiter       *ratelimit.Limiter
	Mailer        *notifications.SMTPMailer
	Auth          *auth.Service
	Organizations *organizations.Service
	Databases     *database.Service
	Destinations  *storage.DestinationService
	Notifications *notifications.Service
	Backups       *backups.Service
	Restores      *restore.Service
	Schedules     *scheduler.Service
	Tools         pgtools.Tools
	Postgres      *postgres.Driver
	MySQL         *mysql.Driver
	MariaDB       *mysql.Driver
	Drivers       *engine.Registry
}

// Role selects startup behaviour.
type Role int

const (
	RoleAPI Role = iota
	RoleWorker
	RoleScheduler
)

// New connects to PostgreSQL and Redis and builds every service. The API
// applies migrations; the worker and scheduler wait for them.
func New(ctx context.Context, cfg *config.Config, log *slog.Logger, role Role) (*App, error) {
	pool, err := db.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		return nil, err
	}
	if role == RoleAPI && cfg.MigrateOnStart {
		if err := db.Migrate(ctx, pool); err != nil {
			pool.Close()
			return nil, fmt.Errorf("migrate: %w", err)
		}
	} else if err := db.WaitForMigrations(ctx, pool, 3*time.Minute); err != nil {
		pool.Close()
		return nil, err
	}

	ropts, err := redis.ParseURL(cfg.RedisURL)
	if err != nil {
		pool.Close()
		return nil, fmt.Errorf("parse REDIS_URL: %w", err)
	}
	rdb := redis.NewClient(ropts)
	if err := waitRedis(ctx, rdb, log); err != nil {
		pool.Close()
		return nil, err
	}

	sealer, err := encryption.NewSealer(cfg.EncryptionKey)
	if err != nil {
		pool.Close()
		return nil, err
	}

	a := &App{Config: cfg, Log: log, Pool: pool, Redis: rdb, Sealer: sealer}
	a.Keys = encryption.NewKeyStore(pool, sealer)
	a.Queue = jobs.NewQueue(pool, rdb)
	a.Limiter = ratelimit.New(rdb)
	a.Mailer = notifications.NewSMTPMailer(cfg.SMTP)
	a.Tools = pgtools.Tools{BinDir: cfg.PgBinDir}
	a.Postgres = postgres.New(a.Tools, cfg.WorkDir)
	a.MySQL = mysql.NewMySQL(cfg.MySQLBinDir, cfg.WorkDir)
	a.MariaDB = mysql.NewMariaDB(cfg.MySQLBinDir, cfg.WorkDir)
	a.Drivers = engine.NewRegistry(a.Postgres, a.MySQL, a.MariaDB)
	hasher := auth.NewHasher(cfg.AuthSecret)

	a.Organizations = &organizations.Service{Pool: pool, Keys: a.Keys, Hasher: hasher, Mailer: a.Mailer, AppURL: cfg.AppURL}
	a.Auth = &auth.Service{Pool: pool, Hasher: hasher, Mailer: a.Mailer, AppURL: cfg.AppURL, AllowRegistration: cfg.AllowRegistration,
		CreateOrg: a.Organizations.CreatePersonal}
	a.Databases = &database.Service{Pool: pool, Sealer: sealer, WorkDir: cfg.WorkDir, Drivers: a.Drivers}
	a.Destinations = &storage.DestinationService{Pool: pool, Sealer: sealer, Options: storage.Options{LocalRoot: cfg.LocalStorageRoot}, Builtin: cfg.S3}
	a.Notifications = &notifications.Service{Pool: pool, Sealer: sealer, Queue: a.Queue, AppURL: cfg.AppURL, EmailConfigured: a.Mailer.Configured(),
		Senders: map[string]notifications.Sender{
			"email":   &notifications.EmailSender{Mailer: a.Mailer},
			"webhook": notifications.NewWebhookSender(cfg.AllowPrivateNetworkTargets),
		}}
	a.Backups = &backups.Service{Pool: pool, Queue: a.Queue, Databases: a.Databases, Destinations: a.Destinations, Keys: a.Keys,
		Notify: a.Notifications, AppURL: cfg.AppURL, WorkDir: cfg.WorkDir,
		Engine: &backups.Engine{Drivers: a.Drivers, VerifyUpload: cfg.VerifyUploadedData}}
	a.Restores = &restore.Service{Pool: pool, Queue: a.Queue, Backups: a.Backups, Databases: a.Databases, Destinations: a.Destinations,
		Keys: a.Keys, Notify: a.Notifications, Drivers: a.Drivers, WorkDir: cfg.WorkDir, AppURL: cfg.AppURL}
	a.Schedules = &scheduler.Service{Pool: pool, Backups: a.Backups}
	return a, nil
}

// ConfigureSandbox sets up restore testing according to VERIFY_MODE.
func (a *App) ConfigureSandbox(ctx context.Context) {
	switch a.Config.VerifyMode {
	case config.VerifyModeDocker:
		sb, err := restore.NewDockerSandbox(a.Config.VerifyDockerHost, a.Config.VerifyDockerNetwork, a.Config.VerifyDockerImage)
		if err != nil {
			a.Restores.SandboxUnavailableReason = err.Error()
			return
		}
		if err := sb.Check(ctx, a.Postgres); err != nil {
			a.Log.Warn("docker restore sandbox unavailable at startup (will retry per verification)", "error", err.Error())
		} else if n, err := sb.RemoveStale(ctx, 2*time.Hour); err == nil && n > 0 {
			a.Log.Info("removed stale verification containers", "count", n)
		}
		a.Restores.Sandbox = sb
	case config.VerifyModeServer:
		sandboxes := restore.ServerSandboxes{}
		for _, s := range []struct {
			url string
			drv engine.Driver
		}{{a.Config.VerifyPostgresURL, a.Postgres}, {a.Config.VerifyMySQLURL, a.MySQL}, {a.Config.VerifyMariaDBURL, a.MariaDB}} {
			if s.url == "" {
				continue
			}
			sb, err := restore.NewServerSandbox(s.url, s.drv)
			if err != nil {
				a.Log.Error("invalid verification server URL", "engine", s.drv.Name(), "error", err.Error())
				continue
			}
			sandboxes[s.drv.Name()] = sb
		}
		if len(sandboxes) == 0 {
			a.Restores.SandboxUnavailableReason = "no valid verification server URL is configured"
			return
		}
		a.Restores.Sandbox = sandboxes
	default:
		a.Restores.SandboxUnavailableReason = "VERIFY_MODE is disabled"
	}
}

func waitRedis(ctx context.Context, rdb *redis.Client, log *slog.Logger) error {
	deadline := time.Now().Add(60 * time.Second)
	for {
		err := rdb.Ping(ctx).Err()
		if err == nil {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("connect to redis: %w", err)
		}
		log.Warn("waiting for redis", "error", err.Error())
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
}

// Close releases connections.
func (a *App) Close() {
	_ = a.Redis.Close()
	a.Pool.Close()
}

// Ready checks dependencies for readiness probes.
func (a *App) Ready(ctx context.Context) map[string]string {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	out := map[string]string{"database": "ok", "redis": "ok"}
	if err := a.Pool.Ping(ctx); err != nil {
		out["database"] = "unavailable"
	}
	if err := a.Redis.Ping(ctx).Err(); err != nil {
		out["redis"] = "unavailable"
	}
	return out
}
