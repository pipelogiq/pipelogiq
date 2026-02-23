package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"pipelogiq/internal/alerts"
	"pipelogiq/internal/config"
	"pipelogiq/internal/db"
	"pipelogiq/internal/logger"
	"pipelogiq/internal/mq"
	observabilityrepo "pipelogiq/internal/observability/repo"
	"pipelogiq/internal/store"
	"pipelogiq/internal/telemetry"
	"pipelogiq/internal/worker"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	cfg, err := config.LoadWorker()
	if err != nil {
		slog.Error("config error", "err", err)
		os.Exit(1)
	}

	logg := logger.New(cfg.LogLevel)
	otelShutdown, err := telemetry.Init(ctx, "pipeline-worker", logg)
	if err != nil {
		logg.Error("opentelemetry init failed", "err", err)
		os.Exit(1)
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := otelShutdown(shutdownCtx); err != nil {
			logg.Error("opentelemetry shutdown failed", "err", err)
		}
	}()

	dbConn, err := db.Connect(ctx, cfg.DatabaseURL, logg)
	if err != nil {
		logg.Error("db connection failed", "err", err)
		os.Exit(1)
	}
	defer dbConn.Close()

	mqClient := mq.NewClient(cfg.RabbitURL, logg)
	defer mqClient.Close()

	store := store.New(dbConn, logg)
	alertsNotifier := alerts.New(observabilityrepo.NewSQLRepository(store.DB()), logg)
	store.SetAlertSink(alertsNotifier)
	w := worker.New(cfg, store, mqClient, logg)

	if err := w.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		logg.Error("worker exited", "err", err)
		os.Exit(1)
	}
}
