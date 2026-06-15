// Command bot is the Go entrypoint for ChevaletAnonBot — the in-progress port
// of the Python bot (which lives on the `python` branch). See MIGRATION.md.
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	_ "time/tzdata" // embed the IANA tz database so Asia/Tehran (GM/GN) resolves without OS tzdata

	"github.com/aturzone/chevaletAnonBot/internal/bot"
	"github.com/aturzone/chevaletAnonBot/internal/config"
	"github.com/aturzone/chevaletAnonBot/internal/db"
	"github.com/aturzone/chevaletAnonBot/internal/health"
	"github.com/aturzone/chevaletAnonBot/internal/texts"
)

func main() {
	cfg, err := config.Load()
	slog.SetDefault(newLogger(os.Getenv("LOG_LEVEL")))
	if err != nil {
		slog.Error("failed to load configuration", "err", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	slog.Info("starting ChevaletAnonBot (Go)",
		"bot_id", cfg.BotID,
		"db_host", cfg.DBHost,
		"db_name", cfg.DBName,
		"health_port", cfg.HealthPort,
		"admins", len(cfg.Admins),
	)

	database, err := db.Connect(ctx, cfg)
	if err != nil {
		slog.Error("failed to connect to database", "err", err)
		os.Exit(1)
	}
	defer database.Close()
	if err := database.MakeTables(ctx); err != nil {
		slog.Error("failed to ensure schema", "err", err)
		os.Exit(1)
	}
	slog.Info("database ready")

	txt := texts.New("Texts")

	b, err := bot.New(cfg, database, txt)
	if err != nil {
		slog.Error("failed to initialise bot", "err", err)
		os.Exit(1)
	}

	hl, err := health.Listen(cfg.HealthPort)
	if err != nil {
		slog.Error("failed to start health listener", "err", err)
		os.Exit(1)
	}
	defer hl.Close()
	slog.Info("health endpoint listening", "port", cfg.HealthPort)

	if err := b.Run(ctx); err != nil {
		slog.Error("bot stopped with error", "err", err)
		os.Exit(1)
	}
	slog.Info("shut down cleanly")
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
