// Command scheduler turns due backup schedules into jobs and performs
// housekeeping (dispatch sweeps, dead-worker recovery, retention runs).
package main

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/dbvault/dbvault/backend/internal/app"
	"github.com/dbvault/dbvault/backend/internal/config"
	"github.com/dbvault/dbvault/backend/internal/logging"
	"github.com/dbvault/dbvault/backend/internal/scheduler"
	"github.com/dbvault/dbvault/backend/internal/server"
)

func main() {
	boot := logging.New("info", "scheduler")
	cfg, err := config.Load()
	if err != nil {
		server.Fatal(boot, "invalid configuration", err)
	}
	log := logging.New(cfg.LogLevel, "scheduler")
	slog.SetDefault(log)

	ctx, stop := server.SignalContext()
	defer stop()

	a, err := app.New(ctx, cfg, log, app.RoleScheduler)
	if err != nil {
		server.Fatal(log, "startup failed", err)
	}
	defer a.Close()

	runner := &scheduler.Runner{Pool: a.Pool, Queue: a.Queue, Backups: a.Backups, Log: log}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "version": app.Version})
	})
	mux.HandleFunc("GET /ready", func(w http.ResponseWriter, r *http.Request) {
		checks := a.Ready(r.Context())
		status := http.StatusOK
		for _, v := range checks {
			if v != "ok" {
				status = http.StatusServiceUnavailable
			}
		}
		writeJSON(w, status, map[string]any{"checks": checks, "scheduler": runner.Status()})
	})
	health := &http.Server{Addr: cfg.SchedulerHealthAddr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = server.Serve(ctx, health, log) }()

	log.Info("DBVault scheduler starting", "version", app.Version)
	runner.Run(ctx)
	log.Info("DBVault scheduler stopped")
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
