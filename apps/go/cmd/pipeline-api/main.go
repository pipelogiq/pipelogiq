package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"pipelogiq/internal/api"
	"pipelogiq/internal/config"
	"pipelogiq/internal/db"
	"pipelogiq/internal/logger"
	"pipelogiq/internal/mq"
	"pipelogiq/internal/store"
	"pipelogiq/internal/telemetry"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	cfg, err := config.LoadAPI()
	if err != nil {
		slog.Error("config error", "err", err)
		os.Exit(1)
	}

	logg := logger.New(cfg.LogLevel)
	otelShutdown, err := telemetry.Init(ctx, "pipeline-api", logg)
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

	st := store.New(dbConn, logg)

	// Internal API (JWT-protected, for web dashboard)
	internalServer := api.NewServer(cfg, st, mqClient, logg)

	// External API (API-key auth, for SDK clients and workers)
	externalServer := api.NewExternalServer(cfg, st, mqClient, logg)

	errCh := make(chan error, 2)
	go func() {
		if err := internalServer.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
			errCh <- err
		}
	}()
	go func() {
		if err := externalServer.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
			errCh <- err
		}
	}()

	select {
	case <-ctx.Done():
		logg.Info("shutting down")
	case err := <-errCh:
		logg.Error("server exited with error", "err", err)
		os.Exit(1)
	}
}
