package worker

import (
	"io"
	"log/slog"
	"testing"
	"time"

	"pipelogiq/internal/config"
)

func TestNormalizeWorkerConfigUsesSafeDefaultsForInvalidDurations(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	cfg := normalizeWorkerConfig(config.WorkerConfig{
		PollInterval:       0,
		StageActiveTimeout: 0,
	}, logger)

	if cfg.PollInterval != defaultWorkerPollInterval {
		t.Fatalf("PollInterval = %v, want %v", cfg.PollInterval, defaultWorkerPollInterval)
	}
	if cfg.StageActiveTimeout != defaultStageActiveTimeout {
		t.Fatalf("StageActiveTimeout = %v, want %v", cfg.StageActiveTimeout, defaultStageActiveTimeout)
	}
}

func TestNormalizeWorkerConfigClampsTinyPollInterval(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	cfg := normalizeWorkerConfig(config.WorkerConfig{
		PollInterval:       10 * time.Millisecond,
		StageActiveTimeout: 5 * time.Minute,
	}, logger)

	if cfg.PollInterval != minWorkerPollInterval {
		t.Fatalf("PollInterval = %v, want %v", cfg.PollInterval, minWorkerPollInterval)
	}
}

func TestStageTimeoutWatchIntervalHasMinimum(t *testing.T) {
	if got := stageTimeoutWatchInterval(500 * time.Millisecond); got != minStageTimeoutWatcherInterval {
		t.Fatalf("stageTimeoutWatchInterval() = %v, want %v", got, minStageTimeoutWatcherInterval)
	}
}
