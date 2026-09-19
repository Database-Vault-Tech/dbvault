// Command api serves the DBVault REST API.
package main

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/dbvault/dbvault/backend/internal/api"
	"github.com/dbvault/dbvault/backend/internal/app"
	"github.com/dbvault/dbvault/backend/internal/config"
	"github.com/dbvault/dbvault/backend/internal/logging"
	"github.com/dbvault/dbvault/backend/internal/server"
)

func main() {
	boot := logging.New("info", "api")
	cfg, err := config.Load()
	if err != nil {
		server.Fatal(boot, "invalid configuration", err)
	}
	log := logging.New(cfg.LogLevel, "api")
	slog.SetDefault(log)

	ctx, stop := server.SignalContext()
	defer stop()

	a, err := app.New(ctx, cfg, log, app.RoleAPI)
	if err != nil {
		server.Fatal(log, "startup failed", err)
	}
	defer a.Close()

	srv := &http.Server{
		Addr:              cfg.APIAddr,
		Handler:           api.NewRouter(a),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       60 * time.Second,
		// No WriteTimeout: backup downloads stream for as long as they need.
		IdleTimeout:    120 * time.Second,
		MaxHeaderBytes: 1 << 20,
	}
	log.Info("DBVault API starting", "version", app.Version, "app_url", cfg.AppURL)
	if err := server.Serve(ctx, srv, log); err != nil {
		server.Fatal(log, "server error", err)
	}
	log.Info("DBVault API stopped")
}
