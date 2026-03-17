package store

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/jmoiron/sqlx"
	_ "modernc.org/sqlite"

	"pipelogiq/internal/types"
)

func TestMarkActiveTooLongFailsRunningStage(t *testing.T) {
	st, db := setupStageTimeoutTestStore(t)

	pipelineID := insertTimeoutPipeline(t, db, "running-pipeline", types.PipelineStatusRunning)
	stageID := insertTimeoutStage(t, db, pipelineID, "running-stage", types.StageStatusRunning, time.Now().UTC().Add(-10*time.Minute))

	affected, err := st.MarkActiveTooLong(context.Background(), 5*time.Minute)
	if err != nil {
		t.Fatalf("MarkActiveTooLong() error = %v", err)
	}
	if affected != 1 {
		t.Fatalf("MarkActiveTooLong() affected = %d, want 1", affected)
	}

	var stageStatus string
	var stageFinishedAt *time.Time
	var stageOutput string
	if err := db.QueryRow(`
		SELECT s.status, s.finished_at, COALESCE(io.output, '')
		FROM stage s
		LEFT JOIN stage_io io ON io.stage_id = s.id
		WHERE s.id = $1
	`, stageID).Scan(&stageStatus, &stageFinishedAt, &stageOutput); err != nil {
		t.Fatalf("query stage: %v", err)
	}
	if stageStatus != types.StageStatusFailed {
		t.Fatalf("stage status = %q, want %q", stageStatus, types.StageStatusFailed)
	}
	if stageFinishedAt == nil {
		t.Fatal("expected finished_at to be set")
	}
	if !strings.Contains(stageOutput, "running for too long") {
		t.Fatalf("stage output = %q, want running timeout message", stageOutput)
	}

	var pipelineStatus string
	var pipelineCompleted bool
	if err := db.QueryRow(`
		SELECT status, is_completed FROM pipeline WHERE id = $1
	`, pipelineID).Scan(&pipelineStatus, &pipelineCompleted); err != nil {
		t.Fatalf("query pipeline: %v", err)
	}
	if pipelineStatus != types.PipelineStatusFailed {
		t.Fatalf("pipeline status = %q, want %q", pipelineStatus, types.PipelineStatusFailed)
	}
	if !pipelineCompleted {
		t.Fatal("expected pipeline to be completed after timeout")
	}
}

func TestMarkActiveTooLongHonorsStageSpecificTimeout(t *testing.T) {
	st, db := setupStageTimeoutTestStore(t)

	pipelineID := insertTimeoutPipeline(t, db, "pending-pipeline", types.PipelineStatusRunning)
	stageID := insertTimeoutStage(t, db, pipelineID, "pending-stage", types.StageStatusPending, time.Now().UTC().Add(-2*time.Minute))
	if _, err := db.Exec(`
		INSERT INTO stage_options (stage_id, time_out) VALUES ($1, $2)
	`, stageID, 60); err != nil {
		t.Fatalf("insert stage options: %v", err)
	}

	affected, err := st.MarkActiveTooLong(context.Background(), 5*time.Minute)
	if err != nil {
		t.Fatalf("MarkActiveTooLong() error = %v", err)
	}
	if affected != 1 {
		t.Fatalf("MarkActiveTooLong() affected = %d, want 1", affected)
	}

	var stageStatus string
	var stageOutput string
	if err := db.QueryRow(`
		SELECT s.status, COALESCE(io.output, '')
		FROM stage s
		LEFT JOIN stage_io io ON io.stage_id = s.id
		WHERE s.id = $1
	`, stageID).Scan(&stageStatus, &stageOutput); err != nil {
		t.Fatalf("query stage: %v", err)
	}
	if stageStatus != types.StageStatusFailed {
		t.Fatalf("stage status = %q, want %q", stageStatus, types.StageStatusFailed)
	}
	if !strings.Contains(stageOutput, "pending for too long") {
		t.Fatalf("stage output = %q, want pending timeout message", stageOutput)
	}
}

func TestMarkActiveTooLongLeavesRecentStageUntouched(t *testing.T) {
	st, db := setupStageTimeoutTestStore(t)

	pipelineID := insertTimeoutPipeline(t, db, "recent-pipeline", types.PipelineStatusRunning)
	stageID := insertTimeoutStage(t, db, pipelineID, "recent-stage", types.StageStatusRunning, time.Now().UTC().Add(-30*time.Second))

	affected, err := st.MarkActiveTooLong(context.Background(), 5*time.Minute)
	if err != nil {
		t.Fatalf("MarkActiveTooLong() error = %v", err)
	}
	if affected != 0 {
		t.Fatalf("MarkActiveTooLong() affected = %d, want 0", affected)
	}

	var stageStatus string
	if err := db.QueryRow(`SELECT status FROM stage WHERE id = $1`, stageID).Scan(&stageStatus); err != nil {
		t.Fatalf("query stage: %v", err)
	}
	if stageStatus != types.StageStatusRunning {
		t.Fatalf("stage status = %q, want %q", stageStatus, types.StageStatusRunning)
	}
}

func setupStageTimeoutTestStore(t *testing.T) (*Store, *sqlx.DB) {
	t.Helper()

	dsn := fmt.Sprintf(
		"file:stage_timeout_test_%d?mode=memory&cache=shared&_pragma=foreign_keys(ON)&_pragma=busy_timeout(5000)",
		time.Now().UnixNano(),
	)
	db, err := sqlx.Open("sqlite", dsn)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}

	schema := `
	CREATE TABLE pipeline (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL,
		status TEXT,
		created_at TIMESTAMP NOT NULL,
		finished_at TIMESTAMP NULL,
		is_completed BOOLEAN NOT NULL DEFAULT 0,
		application_id INTEGER NULL,
		trace_id TEXT NULL
	);
	CREATE TABLE stage (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL,
		stage_handler_name TEXT NULL,
		description TEXT NULL,
		status TEXT NULL,
		pipeline_id INTEGER NOT NULL,
		created_at TIMESTAMP NOT NULL,
		finished_at TIMESTAMP NULL,
		started_at TIMESTAMP NULL,
		is_skipped BOOLEAN NULL,
		is_event BOOLEAN NULL,
		span_id TEXT NULL,
		retry_attempt INTEGER NOT NULL DEFAULT 0,
		next_retry_at TIMESTAMP NULL
	);
	CREATE TABLE stage_io (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		input TEXT NULL,
		output TEXT NULL,
		stage_id INTEGER NOT NULL
	);
	CREATE TABLE stage_options (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		stage_id INTEGER NOT NULL,
		time_out INTEGER NULL
	);
	CREATE TABLE stage_log (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		log TEXT NULL,
		log_level TEXT NULL,
		created_at TIMESTAMP NULL,
		stage_id INTEGER NOT NULL
	);
	`

	if _, err = db.Exec(schema); err != nil {
		_ = db.Close()
		t.Fatalf("create schema: %v", err)
	}

	t.Cleanup(func() {
		_ = db.Close()
	})

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return New(db, logger), db
}

func insertTimeoutPipeline(t *testing.T, db *sqlx.DB, name, status string) int {
	t.Helper()

	var id int
	if err := db.QueryRow(`
		INSERT INTO pipeline (name, status, created_at, is_completed)
		VALUES ($1, $2, $3, false)
		RETURNING id
	`, name, status, time.Now().UTC()).Scan(&id); err != nil {
		t.Fatalf("insert pipeline: %v", err)
	}
	return id
}

func insertTimeoutStage(t *testing.T, db *sqlx.DB, pipelineID int, name, status string, startedAt time.Time) int {
	t.Helper()

	var id int
	if err := db.QueryRow(`
		INSERT INTO stage (
			name,
			stage_handler_name,
			status,
			pipeline_id,
			created_at,
			started_at,
			is_skipped,
			is_event,
			retry_attempt
		)
		VALUES ($1, $2, $3, $4, $5, $6, false, false, 0)
		RETURNING id
	`, name, "handler", status, pipelineID, startedAt.Add(-time.Minute), startedAt).Scan(&id); err != nil {
		t.Fatalf("insert stage: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO stage_io (stage_id, input) VALUES ($1, $2)`, id, "{}"); err != nil {
		t.Fatalf("insert stage io: %v", err)
	}
	return id
}
