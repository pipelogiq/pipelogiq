package store

import (
	"context"
	"testing"
	"time"

	"pipelogiq/internal/types"
)

func TestGetPipelineStagesIncludesRetryAndFailureHistory(t *testing.T) {
	st, db := setupSchedulingStore(t)
	ctx := context.Background()

	pipelineID := insertPipeline(t, db, "stage-metadata")
	stageID := insertStage(t, db, pipelineID, "retryable-stage", "handler")
	nextRetryAt := time.Now().UTC().Add(10 * time.Minute).Truncate(time.Second)
	lastFailedAt := time.Now().UTC().Add(-2 * time.Minute).Truncate(time.Second)

	if _, err := db.Exec(`
		UPDATE stage
		SET status = $1, next_retry_at = $2
		WHERE id = $3
	`, types.StageStatusRetryScheduled, nextRetryAt, stageID); err != nil {
		t.Fatalf("update stage retry metadata: %v", err)
	}

	if _, err := db.Exec(`
		INSERT INTO stage_log (log, log_level, created_at, stage_id)
		VALUES ($1, 'INFO', $2, $3)
	`, "Stage 'retryable-stage' (id=1) status changed: Running -> Failed [pipeline=1, source=result_consumer]", lastFailedAt, stageID); err != nil {
		t.Fatalf("insert failure history: %v", err)
	}

	stages, err := st.GetPipelineStages(ctx, pipelineID)
	if err != nil {
		t.Fatalf("GetPipelineStages: %v", err)
	}
	if len(stages) != 1 {
		t.Fatalf("len(stages) = %d, want 1", len(stages))
	}

	stage := stages[0]
	if stage.NextRetryAt == nil {
		t.Fatal("NextRetryAt = nil, want timestamp")
	}
	if !stage.NextRetryAt.UTC().Equal(nextRetryAt) {
		t.Fatalf("NextRetryAt = %s, want %s", stage.NextRetryAt.UTC(), nextRetryAt)
	}
	if !stage.HasFailureHistory {
		t.Fatal("HasFailureHistory = false, want true")
	}
	if stage.FailureCount != 1 {
		t.Fatalf("FailureCount = %d, want 1", stage.FailureCount)
	}
	if stage.LastFailedAt == nil {
		t.Fatal("LastFailedAt = nil, want timestamp")
	}
	if !stage.LastFailedAt.UTC().Equal(lastFailedAt) {
		t.Fatalf("LastFailedAt = %s, want %s", stage.LastFailedAt.UTC(), lastFailedAt)
	}
}
