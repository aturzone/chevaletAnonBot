// Command bot is the Go entrypoint for ChevaletAnonBot — the in-progress port
// of the Python bot (which lives on the `python` branch).
//
// This is the foundation build: it loads configuration and runs the health
// endpoint. The Telegram dispatcher, database layer and background jobs are
// added in subsequent milestones on the `go` branch (see MIGRATION.md).
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/aturzone/chevaletAnonBot/internal/config"
	"github.com/aturzone/chevaletAnonBot/internal/health"
)

func main() {
	cfg, err := config.Load()
	slog.SetDefault(newLogger(os.Getenv("LOG_LEVEL")))
	if err != nil {
		slog.Error("failed to load configuration", "err", err)
		os.Exit(1)
	}

	slog.Info("starting ChevaletAnonBot (Go foundation build)",
		"bot_id", cfg.BotID,
		"db_host", cfg.DBHost,
		"db_name", cfg.DBName,
		"health_port", cfg.HealthPort,
		"send_gm_gn", cfg.SendGMGN,
		"admins", len(cfg.Admins),
	)

	hl, err := health.Listen(cfg.HealthPort)
	if err != nil {
		slog.Error("failed to start health listener", "err", err)
		os.Exit(1)
	}
	defer hl.Close()
	slog.Info("health endpoint listening", "port", cfg.HealthPort)

	// TODO(go-migration): subsequent milestones on the `go` branch add:
	//   - internal/db   : pgx pool + identical schema + DBHandler methods
	//   - internal/bot  : gotgbot dispatcher, prep middleware, all handlers
	//   - internal/jobs : AI responder, GM/GN greetings, set_commands, db check
	// Until then the foundation runs the health endpoint and waits for a signal.

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()
	slog.Info("shutting down")
}

// newLogger maps the Python LOG_LEVEL names onto slog levels.
func newLogger(level string) *slog.Logger {
	var lvl slog.Level
	switch strings.ToUpper(strings.TrimSpace(level)) {
	case "DEBUG":
		lvl = slog.LevelDebug
	case "WARNING", "WARN":
		lvl = slog.LevelWarn
	case "ERROR", "CRITICAL":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: lvl}))
}
