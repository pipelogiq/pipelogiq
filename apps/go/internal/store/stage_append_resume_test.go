package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/jmoiron/sqlx"
	_ "modernc.org/sqlite"

	"pipelogiq/internal/types"
)

func TestAppendStages_HappyPath(t *testing.T) {
	st, db := setupStageOpsTestStore(t)
	pipelineID := insertPipelineRow(t, db, "append-happy", types.PipelineStatusRunning, false)

	response, err := st.AppendStages(context.Background(), pipelineID, types.AppendStagesRequest{
		Stages: []types.StageCreate{
			{
				Name:         "agent.tool.1",
				StageHandler: "AgentToolHandler",
				Input:        `{"x":1}`,
				Options: &types.StageOptions{
					MaxRetries: ptrInt(2),
				},
			},
			{
				Name:            "agent.tool.2",
				StageHandler:    "AgentToolHandler",
				Input:           `{"x":2}`,
				RunNextIfFailed: true,
			},
		},
	}, "application:101")
	if err != nil {
		t.Fatalf("AppendStages() error = %v", err)
	}

	if len(response.Stages) != 2 {
		t.Fatalf("len(response.Stages) = %d, want 2", len(response.Stages))
	}
	if response.Stages[0].ID <= 0 || response.Stages[1].ID <= 0 {
		t.Fatalf("stage IDs must be assigned, got %#v", response.Stages)
	}
	if response.Stages[0].Status != types.StageStatusNotStarted || response.Stages[1].Status != types.StageStatusNotStarted {
		t.Fatalf("unexpected statuses: %#v", response.Stages)
	}
	if response.Stages[0].NextStageID == nil || *response.Stages[0].NextStageID != response.Stages[1].ID {
		t.Fatalf("unexpected nextStageId for first stage: %#v", response.Stages[0].NextStageID)
	}

	var stageCount int
	if err := db.Get(&stageCount, `SELECT COUNT(*) FROM stage WHERE pipeline_id = $1`, pipelineID); err != nil {
		t.Fatalf("count stages: %v", err)
	}
	if stageCount != 2 {
		t.Fatalf("stage count = %d, want 2", stageCount)
	}
}

func TestAppendStages_NotFound(t *testing.T) {
	st, _ := setupStageOpsTestStore(t)

	_, err := st.AppendStages(context.Background(), 999_999, types.AppendStagesRequest{
		Stages: []types.StageCreate{{Name: "s", StageHandler: "h"}},
	}, "application:101")
	if !IsPipelineNotFoundError(err) {
		t.Fatalf("expected pipeline not found error, got %v", err)
	}
}

func TestAppendStages_ConflictOnTerminalPipeline(t *testing.T) {
	st, db := setupStageOpsTestStore(t)
	pipelineID := insertPipelineRow(t, db, "append-terminal", types.PipelineStatusCompleted, true)

	_, err := st.AppendStages(context.Background(), pipelineID, types.AppendStagesRequest{
		Stages: []types.StageCreate{{Name: "s", StageHandler: "h"}},
	}, "application:101")
	if !IsPipelineAppendNotAllowedError(err) {
		t.Fatalf("expected append not allowed error, got %v", err)
	}
}

func TestAppendStages_ConcurrentWithPipelineFinalization(t *testing.T) {
	st, db := setupStageOpsTestStore(t)
	pipelineID := insertPipelineRow(t, db, "append-concurrent", types.PipelineStatusRunning, false)

	start := make(chan struct{})
	var appendErr error
	var finalizeErr error
	var wg sync.WaitGroup

	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		_, appendErr = st.AppendStages(context.Background(), pipelineID, types.AppendStagesRequest{
			Stages: []types.StageCreate{{Name: "append-stage", StageHandler: "h"}},
		}, "application:101")
	}()
	go func() {
		defer wg.Done()
		<-start
		_, finalizeErr = db.Exec(`
			UPDATE pipeline
			SET status = $1, is_completed = true, finished_at = $2
			WHERE id = $3
		`, types.PipelineStatusCompleted, time.Now().UTC(), pipelineID)
	}()

	close(start)
	wg.Wait()

	if finalizeErr != nil {
		t.Fatalf("finalize update error = %v", finalizeErr)
	}

	if appendErr != nil && !IsPipelineAppendNotAllowedError(appendErr) {
		t.Fatalf("appendErr = %v, want nil or ErrPipelineAppendNotAllowed", appendErr)
	}

	var stageCount int
	if err := db.Get(&stageCount, `SELECT COUNT(*) FROM stage WHERE pipeline_id = $1`, pipelineID); err != nil {
		t.Fatalf("count stages: %v", err)
	}
	if stageCount > 1 {
		t.Fatalf("stage count = %d, expected at most one appended stage", stageCount)
	}
}

func TestResumeStageApproval_HappyPathApprovedAndRejected(t *testing.T) {
	st, db := setupStageOpsTestStore(t)

	pipelineApproved := insertPipelineRow(t, db, "resume-approved", types.PipelineStatusRunning, false)
	stageApproved := insertStageRow(t, db, pipelineApproved, "approve-me", types.StageStatusWaitingApproval)

	if err := st.ResumeStageApproval(context.Background(), stageApproved, types.ResumeStageRequest{
		Approved: true,
	}, "application:101"); err != nil {
		t.Fatalf("ResumeStageApproval(approved=true) error = %v", err)
	}

	var statusApproved string
	if err := db.Get(&statusApproved, `SELECT status FROM stage WHERE id = $1`, stageApproved); err != nil {
		t.Fatalf("load approved stage status: %v", err)
	}
	if statusApproved != types.StageStatusCompleted {
		t.Fatalf("approved stage status = %s, want %s", statusApproved, types.StageStatusCompleted)
	}

	pipelineRejected := insertPipelineRow(t, db, "resume-rejected", types.PipelineStatusRunning, false)
	stageRejected := insertStageRow(t, db, pipelineRejected, "reject-me", types.StageStatusWaitingApproval)

	reason := "User rejected action"
	if err := st.ResumeStageApproval(context.Background(), stageRejected, types.ResumeStageRequest{
		Approved:        false,
		RejectionReason: &reason,
	}, "application:101"); err != nil {
		t.Fatalf("ResumeStageApproval(approved=false) error = %v", err)
	}

	var statusRejected string
	if err := db.Get(&statusRejected, `SELECT status FROM stage WHERE id = $1`, stageRejected); err != nil {
		t.Fatalf("load rejected stage status: %v", err)
	}
	if statusRejected != types.StageStatusFailed {
		t.Fatalf("rejected stage status = %s, want %s", statusRejected, types.StageStatusFailed)
	}
}

func TestResumeStageApproval_NotFound(t *testing.T) {
	st, _ := setupStageOpsTestStore(t)

	err := st.ResumeStageApproval(context.Background(), 999_999, types.ResumeStageRequest{
		Approved: true,
	}, "application:101")
	if !IsStageNotFoundError(err) {
		t.Fatalf("expected stage not found error, got %v", err)
	}
}

func TestResumeStageApproval_ConflictWhenNotWaiting(t *testing.T) {
	st, db := setupStageOpsTestStore(t)
	pipelineID := insertPipelineRow(t, db, "resume-conflict", types.PipelineStatusRunning, false)
	stageID := insertStageRow(t, db, pipelineID, "not-waiting", types.StageStatusPending)

	err := st.ResumeStageApproval(context.Background(), stageID, types.ResumeStageRequest{
		Approved: true,
	}, "application:101")
	if !IsStageNotWaitingApprovalError(err) {
		t.Fatalf("expected stage not waiting approval error, got %v", err)
	}
}

func TestResumeStageApproval_IdempotentSameDecision(t *testing.T) {
	st, db := setupStageOpsTestStore(t)
	pipelineID := insertPipelineRow(t, db, "resume-idempotent", types.PipelineStatusRunning, false)
	stageID := insertStageRow(t, db, pipelineID, "wait", types.StageStatusWaitingApproval)

	req := types.ResumeStageRequest{Approved: true}
	if err := st.ResumeStageApproval(context.Background(), stageID, req, "application:101"); err != nil {
		t.Fatalf("first resume error = %v", err)
	}
	if err := st.ResumeStageApproval(context.Background(), stageID, req, "application:101"); err != nil {
		t.Fatalf("second identical resume should be idempotent, got %v", err)
	}
}

func TestResumeStageApproval_ConflictOnDifferentDecision(t *testing.T) {
	st, db := setupStageOpsTestStore(t)
	pipelineID := insertPipelineRow(t, db, "resume-conflicting", types.PipelineStatusRunning, false)
	stageID := insertStageRow(t, db, pipelineID, "wait", types.StageStatusWaitingApproval)

	if err := st.ResumeStageApproval(context.Background(), stageID, types.ResumeStageRequest{
		Approved: true,
	}, "application:101"); err != nil {
		t.Fatalf("first resume error = %v", err)
	}

	reason := "conflicting decision"
	err := st.ResumeStageApproval(context.Background(), stageID, types.ResumeStageRequest{
		Approved:        false,
		RejectionReason: &reason,
	}, "application:101")
	if !IsStageResumeConflictError(err) {
		t.Fatalf("expected stage resume conflict error, got %v", err)
	}
}

func TestUpdateStageResult_FailedStageWithRunNextIfFailedContinuation_KeepsPipelineRunnable(t *testing.T) {
	st, db := setupStageOpsTestStore(t)
	pipelineID := insertPipelineRow(t, db, "failed-with-continuation", types.PipelineStatusRunning, false)
	failedStageID := insertStageRow(t, db, pipelineID, "agent-think", types.StageStatusPending)
	responderStageID := insertStageRow(t, db, pipelineID, "agent-responder", types.StageStatusNotStarted)
	insertStageRunNextIfFailedOption(t, db, responderStageID, true)

	pipeline, err := st.UpdateStageResult(context.Background(), types.StageResultMessage{
		StageID:   failedStageID,
		Result:    "Tool loop detected for 'saveBudgetResult' — responder appended.",
		IsSuccess: false,
		ErrorCode: "TOOL_LOOP",
	})
	if err != nil {
		t.Fatalf("UpdateStageResult() error = %v", err)
	}

	if pipeline == nil {
		t.Fatal("expected pipeline snapshot, got nil")
	}
	if pipeline.Status != types.PipelineStatusPending {
		t.Fatalf("pipeline status = %q, want %q", pipeline.Status, types.PipelineStatusPending)
	}

	next, err := st.GetStageToExecute(context.Background())
	if err != nil {
		t.Fatalf("GetStageToExecute() error = %v", err)
	}
	if next == nil {
		t.Fatal("expected continuation stage to become runnable")
	}
	if next.StageID != responderStageID {
		t.Fatalf("next stage id = %d, want %d", next.StageID, responderStageID)
	}
}

func TestUpdateStageResult_ToolLoopDoesNotUseAutomaticStageRetry(t *testing.T) {
	st, db := setupStageOpsTestStore(t)
	pipelineID := insertPipelineRow(t, db, "tool-loop-no-auto-retry", types.PipelineStatusRunning, false)
	failedStageID := insertStageRow(t, db, pipelineID, "agent-think", types.StageStatusPending)
	insertStageRetryOptions(t, db, failedStageID, 30, 120)

	pipeline, err := st.UpdateStageResult(context.Background(), types.StageResultMessage{
		StageID:   failedStageID,
		Result:    "Tool loop detected for 'saveBudgetResult' — responder appended.",
		IsSuccess: false,
		ErrorCode: "TOOL_LOOP",
	})
	if err != nil {
		t.Fatalf("UpdateStageResult() error = %v", err)
	}

	if pipeline == nil {
		t.Fatal("expected pipeline snapshot, got nil")
	}
	if pipeline.Status != types.PipelineStatusFailed {
		t.Fatalf("pipeline status = %q, want %q", pipeline.Status, types.PipelineStatusFailed)
	}

	var stageStatus string
	if err := db.Get(&stageStatus, `SELECT status FROM stage WHERE id = $1`, failedStageID); err != nil {
		t.Fatalf("load failed stage status: %v", err)
	}
	if stageStatus != types.StageStatusFailed {
		t.Fatalf("stage status = %q, want %q", stageStatus, types.StageStatusFailed)
	}
}

func TestUpdateStageResult_LlmInvalidRequestDoesNotUseAutomaticStageRetry(t *testing.T) {
	st, db := setupStageOpsTestStore(t)
	pipelineID := insertPipelineRow(t, db, "llm-invalid-request-no-auto-retry", types.PipelineStatusRunning, false)
	failedStageID := insertStageRow(t, db, pipelineID, "agent-think", types.StageStatusPending)
	insertStageRetryOptions(t, db, failedStageID, 30, 120)

	pipeline, err := st.UpdateStageResult(context.Background(), types.StageResultMessage{
		StageID:   failedStageID,
		Result:    "OpenAI invalid request",
		IsSuccess: false,
		ErrorCode: "LLM_INVALID_REQUEST",
	})
	if err != nil {
		t.Fatalf("UpdateStageResult() error = %v", err)
	}

	if pipeline == nil {
		t.Fatal("expected pipeline snapshot, got nil")
	}
	if pipeline.Status != types.PipelineStatusFailed {
		t.Fatalf("pipeline status = %q, want %q", pipeline.Status, types.PipelineStatusFailed)
	}

	var stageStatus string
	if err := db.Get(&stageStatus, `SELECT status FROM stage WHERE id = $1`, failedStageID); err != nil {
		t.Fatalf("load failed stage status: %v", err)
	}
	if stageStatus != types.StageStatusFailed {
		t.Fatalf("stage status = %q, want %q", stageStatus, types.StageStatusFailed)
	}
}

func TestUpdateStageResult_AppendedStages_AreInsertedAndMappedInContext(t *testing.T) {
	st, db := setupStageOpsTestStore(t)
	pipelineID := insertPipelineRow(t, db, "stage-result-append", types.PipelineStatusRunning, false)
	currentStageID := insertStageRow(t, db, pipelineID, "agent:orchestrator", types.StageStatusPending)

	pipeline, err := st.UpdateStageResult(context.Background(), types.StageResultMessage{
		StageID:   currentStageID,
		Result:    "ReAct mode: think stage appended. Loop begins.",
		IsSuccess: true,
		AppendedStages: []types.AppendedStage{
			{
				StageName:        "agent:think",
				StageHandlerName: "AgentThinkHandler",
				Options: &types.StageOptions{
					RetryInterval: ptrInt(120),
					MaxRetries:    ptrInt(30),
				},
			},
			{
				StageName:        "agent:responder",
				StageHandlerName: "AgentResponderHandler",
				Options: &types.StageOptions{
					RunNextIfFailed: ptrBool(true),
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("UpdateStageResult() error = %v", err)
	}

	if pipeline == nil {
		t.Fatal("expected pipeline snapshot, got nil")
	}
	if pipeline.Status != types.PipelineStatusPending {
		t.Fatalf("pipeline status = %q, want %q", pipeline.Status, types.PipelineStatusPending)
	}

	var stageCount int
	if err := db.Get(&stageCount, `SELECT COUNT(*) FROM stage WHERE pipeline_id = $1`, pipelineID); err != nil {
		t.Fatalf("count appended stages: %v", err)
	}
	if stageCount != 3 {
		t.Fatalf("stage count = %d, want 3", stageCount)
	}

	next, err := st.GetStageToExecute(context.Background())
	if err != nil {
		t.Fatalf("GetStageToExecute() error = %v", err)
	}
	if next == nil {
		t.Fatal("expected appended think stage to become runnable")
	}
	if next.StageHandlerName != "AgentThinkHandler" {
		t.Fatalf("next stage handler = %q, want %q", next.StageHandlerName, "AgentThinkHandler")
	}

	var rawStageMap string
	if err := db.Get(&rawStageMap, `
		SELECT value
		FROM pipeline_context_item
		WHERE pipeline_id = $1 AND key = $2
	`, pipelineID, appendedStageIDsContextKey); err != nil {
		t.Fatalf("load appended stage id map: %v", err)
	}

	var stageMap map[string]int
	if err := json.Unmarshal([]byte(rawStageMap), &stageMap); err != nil {
		t.Fatalf("unmarshal appended stage id map: %v", err)
	}
	if _, ok := stageMap["agent:think"]; !ok {
		t.Fatalf("expected stage map to contain agent:think, got %#v", stageMap)
	}
	if _, ok := stageMap["agent:responder"]; !ok {
		t.Fatalf("expected stage map to contain agent:responder, got %#v", stageMap)
	}
}

func TestGetStageToExecute_AllowsRunNextIfFailedStageAfterEarlierFailure(t *testing.T) {
	st, db := setupStageOpsTestStore(t)
	pipelineID := insertPipelineRow(t, db, "append-followup-after-failure", types.PipelineStatusRunning, false)
	insertStageRow(t, db, pipelineID, "failed-step", types.StageStatusFailed)
	followupStageID := insertStageRow(t, db, pipelineID, "responder", types.StageStatusNotStarted)
	insertStageRunNextIfFailedOption(t, db, followupStageID, true)

	next, err := st.GetStageToExecute(context.Background())
	if err != nil {
		t.Fatalf("GetStageToExecute() error = %v", err)
	}
	if next == nil {
		t.Fatal("expected follow-up stage, got nil")
	}
	if next.StageID != followupStageID {
		t.Fatalf("next stage id = %d, want %d", next.StageID, followupStageID)
	}
}

func TestPausePipeline_StopsSchedulingUntilResume(t *testing.T) {
	st, db := setupStageOpsTestStore(t)
	ctx := context.Background()

	pipelineID := insertPipelineRow(t, db, "pause-resume", types.PipelineStatusRunning, false)
	currentStageID := insertStageRow(t, db, pipelineID, "current", types.StageStatusPending)
	nextStageID := insertStageRow(t, db, pipelineID, "next", types.StageStatusNotStarted)

	if _, err := st.UpdateStageStatus(ctx, types.SetStageStatusMessage{
		StageID: currentStageID,
		Status:  types.StageStatusRunning,
	}); err != nil {
		t.Fatalf("UpdateStageStatus(running) error = %v", err)
	}

	if err := st.PausePipeline(ctx, pipelineID); err != nil {
		t.Fatalf("PausePipeline() error = %v", err)
	}

	pipeline, err := st.UpdateStageResult(ctx, types.StageResultMessage{
		StageID:   currentStageID,
		Result:    "done",
		IsSuccess: true,
	})
	if err != nil {
		t.Fatalf("UpdateStageResult() error = %v", err)
	}

	if pipeline.Status != types.PipelineStatusPaused {
		t.Fatalf("pipeline status after paused stage result = %q, want %q", pipeline.Status, types.PipelineStatusPaused)
	}

	next, err := st.GetStageToExecute(ctx)
	if err != nil {
		t.Fatalf("GetStageToExecute() while paused error = %v", err)
	}
	if next != nil {
		t.Fatalf("expected no stage while paused, got stage %d", next.StageID)
	}

	if err := st.ResumePipeline(ctx, pipelineID); err != nil {
		t.Fatalf("ResumePipeline() error = %v", err)
	}

	next, err = st.GetStageToExecute(ctx)
	if err != nil {
		t.Fatalf("GetStageToExecute() after resume error = %v", err)
	}
	if next == nil {
		t.Fatal("expected next stage after resume")
	}
	if next.StageID != nextStageID {
		t.Fatalf("next stage id = %d, want %d", next.StageID, nextStageID)
	}
}

func TestResumeStageApproval_ConcurrentRequests(t *testing.T) {
	st, db := setupStageOpsTestStore(t)
	pipelineID := insertPipelineRow(t, db, "resume-concurrent", types.PipelineStatusRunning, false)
	stageID := insertStageRow(t, db, pipelineID, "wait", types.StageStatusWaitingApproval)

	reason := "denied"
	var errApproved error
	var errRejected error
	var wg sync.WaitGroup
	start := make(chan struct{})

	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		errApproved = st.ResumeStageApproval(context.Background(), stageID, types.ResumeStageRequest{
			Approved: true,
		}, "application:101")
	}()
	go func() {
		defer wg.Done()
		<-start
		errRejected = st.ResumeStageApproval(context.Background(), stageID, types.ResumeStageRequest{
			Approved:        false,
			RejectionReason: &reason,
		}, "application:101")
	}()

	close(start)
	wg.Wait()

	approvedOK := errApproved == nil
	rejectedOK := errRejected == nil

	if approvedOK == rejectedOK {
		t.Fatalf("expected exactly one successful resume, got approvedErr=%v rejectedErr=%v", errApproved, errRejected)
	}

	if errApproved != nil && !errors.Is(errApproved, ErrStageResumeConflict) && !errors.Is(errApproved, ErrStageNotWaitingApproval) {
		t.Fatalf("unexpected approved error: %v", errApproved)
	}
	if errRejected != nil && !errors.Is(errRejected, ErrStageResumeConflict) && !errors.Is(errRejected, ErrStageNotWaitingApproval) {
		t.Fatalf("unexpected rejected error: %v", errRejected)
	}
}

func setupStageOpsTestStore(t *testing.T) (*Store, *sqlx.DB) {
	t.Helper()

	dsn := fmt.Sprintf(
		"file:stage_ops_test_%d?mode=memory&cache=shared&_pragma=foreign_keys(ON)&_pragma=busy_timeout(5000)",
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
		next_retry_at TIMESTAMP NULL,
		approval_decision BOOLEAN NULL,
		approval_rejection_reason TEXT NULL,
		approval_resumed_at TIMESTAMP NULL,
		approval_resumed_by TEXT NULL
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
		key TEXT NOT NULL,
		value TEXT NOT NULL,
		value_type TEXT NULL,
		pipeline_id INTEGER NOT NULL
	);
	CREATE TABLE api_key (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		application_id INTEGER NOT NULL,
		key TEXT NOT NULL,
		created_at TIMESTAMP NULL,
		disabled_at TIMESTAMP NULL,
		expires_at TIMESTAMP NULL,
		last_used TIMESTAMP NULL
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

func insertPipelineRow(t *testing.T, db *sqlx.DB, name, status string, completed bool) int {
	t.Helper()
	var id int
	if err := db.QueryRow(`
		INSERT INTO pipeline (name, status, created_at, is_completed)
		VALUES ($1, $2, $3, $4)
		RETURNING id
	`, name, status, time.Now().UTC(), completed).Scan(&id); err != nil {
		t.Fatalf("insert pipeline: %v", err)
	}
	return id
}

func insertStageRow(t *testing.T, db *sqlx.DB, pipelineID int, name, status string) int {
	t.Helper()
	var id int
	if err := db.QueryRow(`
		INSERT INTO stage
			(name, stage_handler_name, status, pipeline_id, created_at, is_skipped, is_event, retry_attempt)
		VALUES
			($1, $2, $3, $4, $5, false, false, 0)
		RETURNING id
	`, name, "handler", status, pipelineID, time.Now().UTC()).Scan(&id); err != nil {
		t.Fatalf("insert stage: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO stage_io (stage_id, input) VALUES ($1, $2)`, id, "{}"); err != nil {
		t.Fatalf("insert stage io: %v", err)
	}
	return id
}

func ptrInt(v int) *int {
	return &v
}

func ptrBool(v bool) *bool {
	return &v
}

func insertStageRunNextIfFailedOption(t *testing.T, db *sqlx.DB, stageID int, enabled bool) {
	t.Helper()
	if _, err := db.Exec(`
		INSERT INTO stage_options (stage_id, run_next_if_failed)
		VALUES ($1, $2)
	`, stageID, enabled); err != nil {
		t.Fatalf("insert stage option: %v", err)
	}
}

func insertStageRetryOptions(t *testing.T, db *sqlx.DB, stageID int, maxRetries int, retryIntervalSeconds int) {
	t.Helper()
	if _, err := db.Exec(`
		INSERT INTO stage_options (stage_id, max_retries, retry_interval)
		VALUES ($1, $2, $3)
	`, stageID, maxRetries, retryIntervalSeconds); err != nil {
		t.Fatalf("insert retry stage option: %v", err)
	}
}
