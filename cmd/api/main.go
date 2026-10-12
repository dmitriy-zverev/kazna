// Command api runs the HTTP API server.
package main

import (
	"context"
	"errors"
	"log/slog"
	nethttp "net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/dmitriy-zverev/kazna/internal/applications"
	"github.com/dmitriy-zverev/kazna/internal/config"
	"github.com/dmitriy-zverev/kazna/internal/database"
	httpserver "github.com/dmitriy-zverev/kazna/internal/http"
	"github.com/dmitriy-zverev/kazna/internal/logging"
	"github.com/dmitriy-zverev/kazna/internal/metrics"
	"github.com/dmitriy-zverev/kazna/internal/payments"
	"github.com/dmitriy-zverev/kazna/internal/providers"
	"github.com/dmitriy-zverev/kazna/internal/providers/yookassa"
	"github.com/dmitriy-zverev/kazna/internal/security"
	"github.com/dmitriy-zverev/kazna/internal/version"
)

// unfinishedInterval is how often the unfinished-payments gauge is refreshed.
const unfinishedInterval = 15 * time.Second

// publishUnfinished keeps kazna_unfinished_payments current. A gauge cannot be
// derived from an increment, so it is polled rather than counted.
func publishUnfinished(ctx context.Context, logger *slog.Logger, svc *payments.Service, registry *metrics.Metrics) {
	ticker := time.NewTicker(unfinishedInterval)
	defer ticker.Stop()

	for {
		if count, err := svc.UnfinishedCount(ctx); err == nil {
			registry.SetUnfinished(count)
		} else if ctx.Err() == nil {
			logger.Warn("api: cannot read unfinished payments", slog.Any("error", err))
		}

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func main() {
	if err := run(); err != nil {
		slog.Error("api: fatal", slog.Any("error", err))
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	logger := logging.Setup(cfg.LogLevel)
	logger.Info("api: starting",
		slog.String("version", version.String()),
		slog.String("env", cfg.AppEnv),
		slog.String("addr", cfg.HTTPAddr),
	)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := database.RunMigrations(cfg.DatabaseURL); err != nil {
		return err
	}
	logger.Info("api: migrations applied")

	pool, err := database.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	cipher, err := security.NewCipher(cfg.MasterEncryptionKey, cfg.MasterEncryptionPreviousKeys)
	if err != nil {
		return err
	}

	registry := metrics.New()
	apps := applications.NewService(pool, cipher, applications.Options{Production: cfg.IsProduction()})
	if !cfg.AdminEnabled() {
		logger.Warn("api: admin API is disabled because ADMIN_TOKEN is not set")
	}

	// One provider client per account, built from the decrypted credentials.
	providerClients := func(shopID, secretKey string) providers.Client {
		return yookassa.NewClient(cfg.YooKassaAPIURL, shopID, secretKey)
	}
	paymentService := payments.NewService(pool, apps, providerClients, payments.Options{
		Production:                cfg.IsProduction(),
		ProviderIdempotencyWindow: cfg.ProviderIdempotencyWindow,
		ProviderTimeout:           10 * time.Second,
		PollBaseInterval:          cfg.PaymentPollInterval,
		Recorder:                  registry,
	})

	go publishUnfinished(ctx, logger, paymentService, registry)

	srv := &nethttp.Server{
		Addr: cfg.HTTPAddr,
		Handler: httpserver.NewRouter(httpserver.Deps{
			Logger:       logger,
			Config:       cfg,
			Health:       database.NewChecker(pool),
			Applications: apps,
			Payments:     paymentService,
			Metrics:      registry,
		}),
		ReadHeaderTimeout: 10 * time.Second,
	}

	serveErr := make(chan error, 1)
	go func() {
		logger.Info("api: listening", slog.String("addr", cfg.HTTPAddr))
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, nethttp.ErrServerClosed) {
			serveErr <- err
		}
	}()

	select {
	case err := <-serveErr:
		return err
	case <-ctx.Done():
		logger.Info("api: shutting down")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}
