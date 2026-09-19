// Command worker executes backup, restore, verification, cleanup and
// notification jobs.
package main

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/google/uuid"

	"github.com/dbvault/dbvault/backend/internal/app"
	"github.com/dbvault/dbvault/backend/internal/config"
	"github.com/dbvault/dbvault/backend/internal/jobs"
	"github.com/dbvault/dbvault/backend/internal/logging"
	"github.com/dbvault/dbvault/backend/internal/server"
	"github.com/dbvault/dbvault/backend/internal/worker"
)

func main() {
	boot := logging.New("info", "worker")
	cfg, err := config.Load()
	if err != nil {
		server.Fatal(boot, "invalid configuration", err)
	}
	log := logging.New(cfg.LogLevel, "worker")
	slog.SetDefault(log)

	ctx, stop := server.SignalContext()
	defer stop()

	a, err := app.New(ctx, cfg, log, app.RoleWorker)
	if err != nil {
		server.Fatal(log, "startup failed", err)
	}
	defer a.Close()

	if err := os.MkdirAll(cfg.WorkDir, 0o700); err != nil {
		server.Fatal(log, "create WORK_DIR", err)
	}
	cleanWorkDir(cfg.WorkDir, log)
	a.ConfigureSandbox(ctx)

	caps := map[string]any{"verify_mode": cfg.VerifyMode, "verify_available": false, "verify_detail": a.Restores.SandboxUnavailableReason}
	if _, v, err := a.Tools.Version(ctx, "pg_dump"); err == nil {
		caps["pg_dump_version"] = v
	} else {
		log.Error("pg_dump not found: backups will fail until PostgreSQL client tools are installed", "error", err.Error())
		caps["pg_dump_version"] = nil
	}
	if a.Restores.Sandbox != nil {
		if err := a.Restores.Sandbox.Check(ctx); err != nil {
			caps["verify_detail"] = err.Error()
		} else {
			caps["verify_available"] = true
			caps["verify_detail"] = "Restore tests run in a " + a.Restores.Sandbox.Name() + " sandbox"
		}
	}

	hostname, _ := os.Hostname()
	w := &worker.Worker{
		ID:           hostname + "-" + uuid.NewString()[:8],
		Pool:         a.Pool,
		Queue:        a.Queue,
		Redis:        a.Redis,
		Concurrency:  cfg.WorkerConcurrency,
		GracePeriod:  cfg.ShutdownGracePeriod,
		Log:          log,
		Capabilities: caps,
		Handlers: map[string]worker.Handler{
			jobs.TypeBackup:       a.Backups.ExecuteBackup,
			jobs.TypeCleanup:      a.Backups.ExecuteCleanup,
			jobs.TypeRestore:      a.Restores.ExecuteRestore,
			jobs.TypeVerification: a.Restores.ExecuteVerification,
			jobs.TypeNotification: a.Notifications.Deliver,
		},
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(rw http.ResponseWriter, _ *http.Request) {
		writeJSON(rw, http.StatusOK, map[string]any{"status": "ok", "version": app.Version})
	})
	mux.HandleFunc("GET /ready", func(rw http.ResponseWriter, r *http.Request) {
		checks := a.Ready(r.Context())
		status := http.StatusOK
		for _, v := range checks {
			if v != "ok" {
				status = http.StatusServiceUnavailable
			}
		}
		writeJSON(rw, status, map[string]any{"checks": checks})
	})
	mux.HandleFunc("GET /status", func(rw http.ResponseWriter, _ *http.Request) {
		writeJSON(rw, http.StatusOK, w.Status())
	})
	health := &http.Server{Addr: cfg.WorkerHealthAddr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = server.Serve(ctx, health, log) }()

	log.Info("DBVault worker starting", "version", app.Version, "worker_id", w.ID, "verify_mode", cfg.VerifyMode)
	w.Run(ctx)
	log.Info("DBVault worker stopped")
}

// cleanWorkDir removes temp files left behind by a previous crash.
func cleanWorkDir(dir string, log *slog.Logger) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if err := os.RemoveAll(dir + "/" + e.Name()); err == nil {
			log.Debug("removed stale work file", "name", e.Name())
		}
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
