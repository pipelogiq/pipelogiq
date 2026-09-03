package store

import (
	"context"
	"database/sql"
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

const schedulingTestSchema = `
CREATE TABLE pipeline (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	name TEXT NOT NULL,
	status TEXT,
	created_at TIMESTAMP NOT NULL,
	finished_at TIMESTAMP NULL,
	is_completed BOOLEAN NOT NULL DEFAULT 0,
	application_id INTEGER NULL,
	trace_id TEXT NULL,
	idempotency_key TEXT NULL,
	request_hash TEXT NULL
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
	next_retry_at TIMESTAMP NULL,
	execution_id TEXT NULL,
	execution_attempt INTEGER NOT NULL DEFAULT 0,
	dispatched_at TIMESTAMP NULL,
	lease_owner TEXT NULL,
	lease_expires_at TIMESTAMP NULL,
	last_error_code TEXT NULL,
	failure_disposition TEXT NULL
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
	run_next_if_failed BOOLEAN NULL,
	retry_interval INTEGER NULL,
	time_out INTEGER NULL,
	max_retries INTEGER NULL,
	retry_on_error_codes TEXT NULL,
	retry_backoff TEXT NULL,
	max_retry_interval INTEGER NULL,
	retry_jitter BOOLEAN NULL,
	depends_on TEXT NULL,
	run_in_parallel_with TEXT NULL,
	fail_if_output_empty BOOLEAN NULL,
	notify_on_failure BOOLEAN NULL,
	run_as_user TEXT NULL
);
CREATE TABLE stage_log (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	log TEXT NULL,
	log_level TEXT NULL,
	created_at TIMESTAMP NULL,
	stage_id INTEGER NOT NULL
);
CREATE TABLE pipeline_context_item (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	pipeline_id INTEGER NOT NULL,
	key TEXT NOT NULL,
	value TEXT NULL,
	value_type TEXT NULL,
	is_sensitive BOOLEAN NOT NULL DEFAULT 0
);
`

func setupSchedulingStore(t *testing.T) (*Store, *sqlx.DB) {
	t.Helper()

	dsn := fmt.Sprintf(
		"file:scheduling_test_%d?mode=memory&cache=shared&_pragma=foreign_keys(ON)&_pragma=busy_timeout(5000)",
		time.Now().UnixNano(),
	)
	db, err := sqlx.Open("sqlite", dsn)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}

	if _, err = db.Exec(schedulingTestSchema); err != nil {
		_ = db.Close()
		t.Fatalf("create schema: %v", err)
	}

	t.Cleanup(func() { _ = db.Close() })

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return New(db, logger), db
}

func insertPipeline(t *testing.T, db *sqlx.DB, name string) int {
	t.Helper()
	var id int
	if err := db.QueryRow(`
		INSERT INTO pipeline (name, status, created_at, is_completed)
		VALUES ($1, $2, $3, false)
		RETURNING id
	`, name, types.PipelineStatusNotStarted, time.Now().UTC()).Scan(&id); err != nil {
		t.Fatalf("insert pipeline: %v", err)
	}
	return id
}

func insertStage(t *testing.T, db *sqlx.DB, pipelineID int, name, handler string) int {
	t.Helper()
	var id int
	if err := db.QueryRow(`
		INSERT INTO stage (name, stage_handler_name, status, pipeline_id, created_at, is_skipped, is_event, retry_attempt)
		VALUES ($1, $2, $3, $4, $5, false, false, 0)
		RETURNING id
	`, name, handler, types.StageStatusNotStarted, pipelineID, time.Now().UTC()).Scan(&id); err != nil {
		t.Fatalf("insert stage %q: %v", name, err)
	}
	if _, err := db.Exec(`INSERT INTO stage_io (stage_id, input) VALUES ($1, '{}')`, id); err != nil {
		t.Fatalf("insert stage_io: %v", err)
	}
	return id
}

func setDependsOn(t *testing.T, db *sqlx.DB, stageID int, dependsOn string) {
	t.Helper()
	if _, err := db.Exec(`
		INSERT INTO stage_options (stage_id, depends_on) VALUES ($1, $2)
	`, stageID, dependsOn); err != nil {
		t.Fatalf("insert depends_on: %v", err)
	}
}

func completeStage(t *testing.T, db *sqlx.DB, stageID int) {
	t.Helper()
	if _, err := db.Exec(`
		UPDATE stage SET status = $1, finished_at = CURRENT_TIMESTAMP WHERE id = $2
	`, types.StageStatusCompleted, stageID); err != nil {
		t.Fatalf("complete stage %d: %v", stageID, err)
	}
}

// TestSequentialScheduling verifies backward compatibility: stages without
// depends_on execute strictly in insertion order.
func TestSequentialScheduling(t *testing.T) {
	st, db := setupSchedulingStore(t)
	ctx := context.Background()

	pid := insertPipeline(t, db, "sequential")
	sA := insertStage(t, db, pid, "A", "handler")
	sB := insertStage(t, db, pid, "B", "handler")
	sC := insertStage(t, db, pid, "C", "handler")

	// First call should return stage A.
	msg, err := st.GetStageToExecute(ctx)
	if err != nil {
		t.Fatalf("GetStageToExecute: %v", err)
	}
	if msg == nil || msg.StageID != sA {
		t.Fatalf("expected stage A (id=%d), got %v", sA, msg)
	}

	// While A is Pending, no other stage should be returned.
	msg, err = st.GetStageToExecute(ctx)
	if err != nil {
		t.Fatalf("GetStageToExecute: %v", err)
	}
	if msg != nil {
		t.Fatalf("expected nil while A is Pending, got stage %d", msg.StageID)
	}

	// Complete A, B should be next.
	completeStage(t, db, sA)
	msg, err = st.GetStageToExecute(ctx)
	if err != nil {
		t.Fatalf("GetStageToExecute: %v", err)
	}
	if msg == nil || msg.StageID != sB {
		t.Fatalf("expected stage B (id=%d), got %v", sB, msg)
	}

	// Complete B, C should be next.
	completeStage(t, db, sB)
	msg, err = st.GetStageToExecute(ctx)
	if err != nil {
		t.Fatalf("GetStageToExecute: %v", err)
	}
	if msg == nil || msg.StageID != sC {
		t.Fatalf("expected stage C (id=%d), got %v", sC, msg)
	}
}

func TestGetStageToExecute_MarksPipelinePending(t *testing.T) {
	st, db := setupSchedulingStore(t)
	ctx := context.Background()

	pid := insertPipeline(t, db, "pending-pipeline")
	stageID := insertStage(t, db, pid, "A", "handler")

	msg, err := st.GetStageToExecute(ctx)
	if err != nil {
		t.Fatalf("GetStageToExecute: %v", err)
	}
	if msg == nil || msg.StageID != stageID {
		t.Fatalf("expected stage %d, got %#v", stageID, msg)
	}

	var pipelineStatus string
	if err := db.Get(&pipelineStatus, `SELECT status FROM pipeline WHERE id = $1`, pid); err != nil {
		t.Fatalf("load pipeline status: %v", err)
	}
	if pipelineStatus != types.PipelineStatusPending {
		t.Fatalf("pipeline status = %q, want %q", pipelineStatus, types.PipelineStatusPending)
	}

	var row struct {
		Status    string       `db:"status"`
		StartedAt sql.NullTime `db:"started_at"`
	}
	if err := db.Get(&row, `SELECT status, started_at FROM stage WHERE id = $1`, stageID); err != nil {
		t.Fatalf("load stage: %v", err)
	}
	if row.Status != types.StageStatusPending {
		t.Fatalf("stage status = %q, want %q", row.Status, types.StageStatusPending)
	}
	if !row.StartedAt.Valid {
		t.Fatal("expected started_at to be set when stage becomes pending")
	}
}

func TestUpdateStageStatus_RunningSetsStartedAtAndPipelineRunning(t *testing.T) {
	st, db := setupSchedulingStore(t)
	ctx := context.Background()

	pid := insertPipeline(t, db, "running-pipeline")
	stageID := insertStage(t, db, pid, "A", "handler")

	if _, err := st.GetStageToExecute(ctx); err != nil {
		t.Fatalf("GetStageToExecute: %v", err)
	}

	if _, err := st.UpdateStageStatus(ctx, types.SetStageStatusMessage{
		StageID: stageID,
		Status:  types.StageStatusRunning,
	}); err != nil {
		t.Fatalf("UpdateStageStatus: %v", err)
	}

	var pipelineStatus string
	if err := db.Get(&pipelineStatus, `SELECT status FROM pipeline WHERE id = $1`, pid); err != nil {
		t.Fatalf("load pipeline status: %v", err)
	}
	if pipelineStatus != types.PipelineStatusRunning {
		t.Fatalf("pipeline status = %q, want %q", pipelineStatus, types.PipelineStatusRunning)
	}

	var startedAt sql.NullTime
	if err := db.Get(&startedAt, `SELECT started_at FROM stage WHERE id = $1`, stageID); err != nil {
		t.Fatalf("load stage started_at: %v", err)
	}
	if !startedAt.Valid {
		t.Fatal("expected started_at to be set when stage becomes running")
	}
}

func TestRecoverOrphanedStages_LogsReasonForRunningReset(t *testing.T) {
	st, db := setupSchedulingStore(t)
	ctx := context.Background()

	pid := insertPipeline(t, db, "orphan-running")
	stageID := insertStage(t, db, pid, "agent:think", "AgentThinkHandler")

	startedAt := time.Now().UTC().Add(-6 * time.Minute)
	if _, err := db.Exec(`
		UPDATE stage
		SET status = $1, started_at = $2
		WHERE id = $3
	`, types.StageStatusRunning, startedAt, stageID); err != nil {
		t.Fatalf("mark stage running: %v", err)
	}

	recovered, err := st.RecoverOrphanedStages(ctx, 5*time.Minute)
	if err != nil {
		t.Fatalf("RecoverOrphanedStages: %v", err)
	}
	if recovered != 1 {
		t.Fatalf("recovered = %d, want 1", recovered)
	}

	var stageStatus string
	if err := db.Get(&stageStatus, `SELECT status FROM stage WHERE id = $1`, stageID); err != nil {
		t.Fatalf("load stage status: %v", err)
	}
	if stageStatus != types.StageStatusNotStarted {
		t.Fatalf("stage status = %q, want %q", stageStatus, types.StageStatusNotStarted)
	}

	var logs []string
	if err := db.Select(&logs, `
		SELECT log
		FROM stage_log
		WHERE stage_id = $1
		ORDER BY id
	`, stageID); err != nil {
		t.Fatalf("load stage logs: %v", err)
	}
	if len(logs) < 2 {
		t.Fatalf("expected at least 2 stage logs, got %d: %#v", len(logs), logs)
	}

	reasonLog := logs[len(logs)-1]
	if !strings.Contains(reasonLog, "Orphan recovery reset this stage") {
		t.Fatalf("reason log missing orphan recovery message: %q", reasonLog)
	}
	if !strings.Contains(reasonLog, "marked the stage Running") {
		t.Fatalf("reason log missing running explanation: %q", reasonLog)
	}
	if !strings.Contains(reasonLog, "threshold 5m0s") {
		t.Fatalf("reason log missing threshold: %q", reasonLog)
	}
}

func TestRecoverOrphanedStages_RespectsStageTimeoutBeforeReset(t *testing.T) {
	st, db := setupSchedulingStore(t)
	ctx := context.Background()

	pid := insertPipeline(t, db, "orphan-running-timeout-aware")
	stageID := insertStage(t, db, pid, "evaluate-budget-strategy", "VesselOps.EvaluateBudgetStrategy")

	if _, err := db.Exec(`
		INSERT INTO stage_options (stage_id, time_out)
		VALUES ($1, $2)
	`, stageID, 900); err != nil {
		t.Fatalf("insert stage timeout: %v", err)
	}

	startedAt := time.Now().UTC().Add(-6 * time.Minute)
	if _, err := db.Exec(`
		UPDATE stage
		SET status = $1, started_at = $2
		WHERE id = $3
	`, types.StageStatusRunning, startedAt, stageID); err != nil {
		t.Fatalf("mark stage running: %v", err)
	}

	recovered, err := st.RecoverOrphanedStages(ctx, 5*time.Minute)
	if err != nil {
		t.Fatalf("RecoverOrphanedStages: %v", err)
	}
	if recovered != 0 {
		t.Fatalf("recovered = %d, want 0", recovered)
	}

	var stageStatus string
	if err := db.Get(&stageStatus, `SELECT status FROM stage WHERE id = $1`, stageID); err != nil {
		t.Fatalf("load stage status: %v", err)
	}
	if stageStatus != types.StageStatusRunning {
		t.Fatalf("stage status = %q, want %q", stageStatus, types.StageStatusRunning)
	}
}

func TestRecoverOrphanedStages_DoesNotResetPending(t *testing.T) {
	st, db := setupSchedulingStore(t)
	ctx := context.Background()

	pid := insertPipeline(t, db, "pending-left-alone")
	stageID := insertStage(t, db, pid, "agent:think", "AgentThinkHandler")

	startedAt := time.Now().UTC().Add(-30 * time.Minute)
	if _, err := db.Exec(`
		UPDATE stage
		SET status = $1, started_at = $2
		WHERE id = $3
	`, types.StageStatusPending, startedAt, stageID); err != nil {
		t.Fatalf("mark stage pending: %v", err)
	}

	recovered, err := st.RecoverOrphanedStages(ctx, 5*time.Minute)
	if err != nil {
		t.Fatalf("RecoverOrphanedStages: %v", err)
	}
	if recovered != 0 {
		t.Fatalf("recovered = %d, want 0", recovered)
	}

	var stageStatus string
	if err := db.Get(&stageStatus, `SELECT status FROM stage WHERE id = $1`, stageID); err != nil {
		t.Fatalf("load stage status: %v", err)
	}
	if stageStatus != types.StageStatusPending {
		t.Fatalf("stage status = %q, want %q", stageStatus, types.StageStatusPending)
	}
}

// TestDependsOnParallelDispatch verifies that stages with explicit depends_on
// can run in parallel when their dependencies are satisfied independently.
//
//	Pipeline:  A → (B depends_on A, C depends_on A)
//	After A completes, both B and C should be dispatchable.
func TestDependsOnParallelDispatch(t *testing.T) {
	st, db := setupSchedulingStore(t)
	ctx := context.Background()

	pid := insertPipeline(t, db, "parallel-deps")
	sA := insertStage(t, db, pid, "A", "handler")
	sB := insertStage(t, db, pid, "B", "handler")
	sC := insertStage(t, db, pid, "C", "handler")

	setDependsOn(t, db, sB, "A")
	setDependsOn(t, db, sC, "A")

	// Only A is eligible initially.
	msg, err := st.GetStageToExecute(ctx)
	if err != nil {
		t.Fatalf("GetStageToExecute: %v", err)
	}
	if msg == nil || msg.StageID != sA {
		t.Fatalf("expected stage A (id=%d), got %v", sA, msg)
	}

	// Complete A. Now B and C should both be eligible.
	completeStage(t, db, sA)

	got := map[int]bool{}
	for i := 0; i < 2; i++ {
		msg, err = st.GetStageToExecute(ctx)
		if err != nil {
			t.Fatalf("GetStageToExecute[%d]: %v", i, err)
		}
		if msg == nil {
			t.Fatalf("expected a stage on call %d, got nil", i)
		}
		got[msg.StageID] = true
	}

	if !got[sB] || !got[sC] {
		t.Fatalf("expected both B(%d) and C(%d) dispatched, got %v", sB, sC, got)
	}

	// No more stages.
	msg, err = st.GetStageToExecute(ctx)
	if err != nil {
		t.Fatalf("GetStageToExecute: %v", err)
	}
	if msg != nil {
		t.Fatalf("expected nil, got stage %d", msg.StageID)
	}
}

// TestDependsOnDiamond verifies a diamond dependency pattern:
//
//	A → B (depends_on A)
//	A → C (depends_on A)
//	D (depends_on B,C) — waits for both B and C.
func TestDependsOnDiamond(t *testing.T) {
	st, db := setupSchedulingStore(t)
	ctx := context.Background()

	pid := insertPipeline(t, db, "diamond")
	sA := insertStage(t, db, pid, "A", "handler")
	sB := insertStage(t, db, pid, "B", "handler")
	sC := insertStage(t, db, pid, "C", "handler")
	sD := insertStage(t, db, pid, "D", "handler")

	setDependsOn(t, db, sB, "A")
	setDependsOn(t, db, sC, "A")
	setDependsOn(t, db, sD, "B,C")

	// Dispatch and complete A.
	msg, _ := st.GetStageToExecute(ctx)
	if msg == nil || msg.StageID != sA {
		t.Fatalf("expected A, got %v", msg)
	}
	completeStage(t, db, sA)

	// Dispatch B and C (parallel).
	msg1, _ := st.GetStageToExecute(ctx)
	msg2, _ := st.GetStageToExecute(ctx)
	if msg1 == nil || msg2 == nil {
		t.Fatalf("expected two stages, got %v and %v", msg1, msg2)
	}
	ids := map[int]bool{msg1.StageID: true, msg2.StageID: true}
	if !ids[sB] || !ids[sC] {
		t.Fatalf("expected B and C, got %v", ids)
	}

	// D should NOT be eligible yet (B and C are Pending, not Completed).
	msg, _ = st.GetStageToExecute(ctx)
	if msg != nil {
		t.Fatalf("expected nil (D blocked), got stage %d", msg.StageID)
	}

	// Complete B only — D still blocked by C.
	completeStage(t, db, sB)
	msg, _ = st.GetStageToExecute(ctx)
	if msg != nil {
		t.Fatalf("expected nil (D blocked by C), got stage %d", msg.StageID)
	}

	// Complete C — D should now be eligible.
	completeStage(t, db, sC)
	msg, _ = st.GetStageToExecute(ctx)
	if msg == nil || msg.StageID != sD {
		t.Fatalf("expected D (id=%d), got %v", sD, msg)
	}
}

// TestMixedSequentialAndDependsOn verifies that stages without depends_on
// stay sequential while stages with depends_on follow dependency rules.
func TestMixedSequentialAndDependsOn(t *testing.T) {
	st, db := setupSchedulingStore(t)
	ctx := context.Background()

	pid := insertPipeline(t, db, "mixed")
	sA := insertStage(t, db, pid, "A", "handler")
	sB := insertStage(t, db, pid, "B", "handler") // sequential — waits for A

	// B has no depends_on, so it uses sequential logic (all prior stages must complete).
	msg, _ := st.GetStageToExecute(ctx)
	if msg == nil || msg.StageID != sA {
		t.Fatalf("expected A, got %v", msg)
	}

	msg, _ = st.GetStageToExecute(ctx)
	if msg != nil {
		t.Fatalf("expected nil (B waits for A), got stage %d", msg.StageID)
	}

	completeStage(t, db, sA)
	msg, _ = st.GetStageToExecute(ctx)
	if msg == nil || msg.StageID != sB {
		t.Fatalf("expected B (id=%d), got %v", sB, msg)
	}
}

// TestCrossPipelineDispatch verifies that stages from different pipelines
// can be dispatched independently in the same batch.
func TestCrossPipelineDispatch(t *testing.T) {
	st, db := setupSchedulingStore(t)
	ctx := context.Background()

	pid1 := insertPipeline(t, db, "pipeline-1")
	pid2 := insertPipeline(t, db, "pipeline-2")
	s1 := insertStage(t, db, pid1, "A", "handler")
	s2 := insertStage(t, db, pid2, "A", "handler")

	got := map[int]bool{}
	for i := 0; i < 2; i++ {
		msg, err := st.GetStageToExecute(ctx)
		if err != nil {
			t.Fatalf("GetStageToExecute[%d]: %v", i, err)
		}
		if msg == nil {
			t.Fatalf("expected stage on call %d, got nil", i)
		}
		got[msg.StageID] = true
	}

	if !got[s1] || !got[s2] {
		t.Fatalf("expected stages from both pipelines (%d, %d), got %v", s1, s2, got)
	}
}

// TestEmptyPipelineReturnsNil verifies no crash when there are no stages.
func TestEmptyPipelineReturnsNil(t *testing.T) {
	st, _ := setupSchedulingStore(t)
	ctx := context.Background()

	msg, err := st.GetStageToExecute(ctx)
	if err != nil {
		t.Fatalf("GetStageToExecute: %v", err)
	}
	if msg != nil {
		t.Fatalf("expected nil, got %v", msg)
	}
}

// BenchmarkGetStageToExecute measures throughput of stage scheduling under load.
// Creates N pipelines with M stages each and dispatches them all.
func BenchmarkGetStageToExecute(b *testing.B) {
	for _, tc := range []struct {
		name      string
		pipelines int
		stages    int
	}{
		{"1x5", 1, 5},
		{"10x3", 10, 3},
		{"50x5", 50, 5},
		{"100x1", 100, 1},
	} {
		b.Run(tc.name, func(b *testing.B) {
			benchmarkScheduling(b, tc.pipelines, tc.stages)
		})
	}
}

func benchmarkScheduling(b *testing.B, numPipelines, stagesPerPipeline int) {
	b.Helper()

	dsn := fmt.Sprintf(
		"file:bench_%d?mode=memory&cache=shared&_pragma=foreign_keys(ON)&_pragma=busy_timeout(5000)",
		time.Now().UnixNano(),
	)
	db, err := sqlx.Open("sqlite", dsn)
	if err != nil {
		b.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()

	if _, err = db.Exec(schedulingTestSchema); err != nil {
		b.Fatalf("create schema: %v", err)
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	st := New(db, logger)
	ctx := context.Background()

	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		b.StopTimer()
		// Clean slate.
		db.Exec("DELETE FROM stage_log")
		db.Exec("DELETE FROM stage_io")
		db.Exec("DELETE FROM stage_options")
		db.Exec("DELETE FROM pipeline_context_item")
		db.Exec("DELETE FROM stage")
		db.Exec("DELETE FROM pipeline")

		type stageRef struct{ pipelineID, stageID int }
		var stages []stageRef

		for p := 0; p < numPipelines; p++ {
			var pid int
			db.QueryRow(`
				INSERT INTO pipeline (name, status, created_at, is_completed) VALUES ($1, $2, $3, false) RETURNING id
			`, fmt.Sprintf("p%d", p), types.PipelineStatusNotStarted, time.Now().UTC()).Scan(&pid)

			for s := 0; s < stagesPerPipeline; s++ {
				var sid int
				db.QueryRow(`
					INSERT INTO stage (name, stage_handler_name, status, pipeline_id, created_at, is_skipped, is_event, retry_attempt)
					VALUES ($1, $2, $3, $4, $5, false, false, 0) RETURNING id
				`, fmt.Sprintf("s%d", s), "handler", types.StageStatusNotStarted, pid, time.Now().UTC()).Scan(&sid)
				db.Exec(`INSERT INTO stage_io (stage_id, input) VALUES ($1, '{}')`, sid)
				stages = append(stages, stageRef{pid, sid})
			}
		}

		b.StartTimer()

		// Dispatch all stages sequentially (complete each before next).
		for i, ref := range stages {
			msg, err := st.GetStageToExecute(ctx)
			if err != nil {
				b.Fatalf("iteration %d: %v", i, err)
			}
			if msg == nil {
				b.Fatalf("iteration %d: expected stage, got nil (ref stage=%d, pipeline=%d)", i, ref.stageID, ref.pipelineID)
			}
			db.Exec(`UPDATE stage SET status = $1, finished_at = CURRENT_TIMESTAMP WHERE id = $2`,
				types.StageStatusCompleted, msg.StageID)
		}
	}
}

// BenchmarkParallelDependsOn measures dispatch throughput when stages use depends_on
// for parallel execution within a single pipeline.
func BenchmarkParallelDependsOn(b *testing.B) {
	dsn := fmt.Sprintf(
		"file:bench_parallel_%d?mode=memory&cache=shared&_pragma=foreign_keys(ON)&_pragma=busy_timeout(5000)",
		time.Now().UnixNano(),
	)
	db, err := sqlx.Open("sqlite", dsn)
	if err != nil {
		b.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()

	if _, err = db.Exec(schedulingTestSchema); err != nil {
		b.Fatalf("create schema: %v", err)
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	st := New(db, logger)
	ctx := context.Background()

	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		b.StopTimer()
		db.Exec("DELETE FROM stage_log")
		db.Exec("DELETE FROM stage_io")
		db.Exec("DELETE FROM stage_options")
		db.Exec("DELETE FROM pipeline_context_item")
		db.Exec("DELETE FROM stage")
		db.Exec("DELETE FROM pipeline")

		var pid int
		db.QueryRow(`
			INSERT INTO pipeline (name, status, created_at, is_completed) VALUES ('fan-out', $1, $2, false) RETURNING id
		`, types.PipelineStatusNotStarted, time.Now().UTC()).Scan(&pid)

		// Root stage.
		var rootID int
		db.QueryRow(`
			INSERT INTO stage (name, stage_handler_name, status, pipeline_id, created_at, is_skipped, is_event, retry_attempt)
			VALUES ('root', 'handler', $1, $2, $3, false, false, 0) RETURNING id
		`, types.StageStatusNotStarted, pid, time.Now().UTC()).Scan(&rootID)
		db.Exec(`INSERT INTO stage_io (stage_id, input) VALUES ($1, '{}')`, rootID)

		// 20 fan-out stages, all depend on root.
		fanIDs := make([]int, 20)
		for i := range fanIDs {
			var sid int
			db.QueryRow(`
				INSERT INTO stage (name, stage_handler_name, status, pipeline_id, created_at, is_skipped, is_event, retry_attempt)
				VALUES ($1, 'handler', $2, $3, $4, false, false, 0) RETURNING id
			`, fmt.Sprintf("fan-%d", i), types.StageStatusNotStarted, pid, time.Now().UTC()).Scan(&sid)
			db.Exec(`INSERT INTO stage_io (stage_id, input) VALUES ($1, '{}')`, sid)
			db.Exec(`INSERT INTO stage_options (stage_id, depends_on) VALUES ($1, 'root')`, sid)
			fanIDs[i] = sid
		}

		b.StartTimer()

		// Dispatch root.
		msg, _ := st.GetStageToExecute(ctx)
		if msg == nil {
			b.Fatal("expected root stage")
		}
		db.Exec(`UPDATE stage SET status = $1, finished_at = CURRENT_TIMESTAMP WHERE id = $2`,
			types.StageStatusCompleted, msg.StageID)

		// Dispatch all 20 fan-out stages.
		for i := 0; i < 20; i++ {
			msg, err := st.GetStageToExecute(ctx)
			if err != nil {
				b.Fatalf("fan dispatch %d: %v", i, err)
			}
			if msg == nil {
				b.Fatalf("fan dispatch %d: expected stage, got nil", i)
			}
		}
	}
}
