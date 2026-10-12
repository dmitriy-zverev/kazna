// Command worker runs background jobs (reconciliation, retries, event delivery).
//
// Stage 1 only establishes the process skeleton: it loads configuration,
// verifies database connectivity and waits for shutdown. Job loops are added in
// Stage 4 (recovery/reconciliation) and Stage 5 (event delivery).
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/dmitriy-zverev/kazna/internal/config"
	"github.com/dmitriy-zverev/kazna/internal/database"
	"github.com/dmitriy-zverev/kazna/internal/logging"
	"github.com/dmitriy-zverev/kazna/internal/version"
)

func main() {
	if err := run(); err != nil {
		slog.Error("worker: fatal", slog.Any("error", err))
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	logger := logging.Setup(cfg.LogLevel)
	logger.Info("worker: starting",
		slog.String("version", version.String()),
		slog.String("env", cfg.AppEnv),
		slog.Int("concurrency", cfg.WorkerConcurrency),
		slog.Duration("poll_interval", cfg.PaymentPollInterval),
	)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := database.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	logger.Info("worker: ready (no jobs registered yet)")
	<-ctx.Done()
	logger.Info("worker: shutting down")
	return nil
}
