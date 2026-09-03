package store

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jmoiron/sqlx"

	"pipelogiq/internal/types"
)

func TestReliabilityTransientErrorsRespectRetryCountAndDelay(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name           string
		errorCode      string
		backoff        string
		expectedDelays []time.Duration
	}{
		{
			name:           "timeout",
			errorCode:      types.ErrorCodeTimeout,
			backoff:        "fixed",
			expectedDelays: []time.Duration{time.Second, time.Second},
		},
		{
			name:           "upstream error",
			errorCode:      types.ErrorCodeUpstreamError,
			backoff:        "fixed",
			expectedDelays: []time.Duration{time.Second, time.Second},
		},
		{
			name:           "rate limit exponential delay",
			errorCode:      types.ErrorCodeRateLimitExceeded,
			backoff:        "exponential",
			expectedDelays: []time.Duration{time.Second, 2 * time.Second},
		},
	}

	for _, testCase := range testCases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			st, db := setupSchedulingStore(t)
			ctx := context.Background()
			pipelineID := insertPipeline(t, db, "retry-"+testCase.errorCode)
			stageID := insertStage(t, db, pipelineID, "claim", "claim-handler")
			reliabilityInsertStageOptions(
				t,
				db,
				stageID,
				2,
				1,
				testCase.errorCode,
				testCase.backoff,
				10,
			)

			execution := reliabilitySchedule(t, st)
			for retryIndex, expectedDelay := range testCase.expectedDelays {
				before := time.Now().UTC()
				response, err := st.UpdateStageResult(ctx, reliabilityFailureResult(
					execution,
					testCase.errorCode,
					nil,
				))
				if err != nil {
					t.Fatalf("UpdateStageResult retry %d: %v", retryIndex+1, err)
				}

				stage := reliabilityOnlyStage(t, response)
				if stage.Status != types.StageStatusRetryScheduled {
					t.Fatalf("retry %d status = %q, want %q", retryIndex+1, stage.Status, types.StageStatusRetryScheduled)
				}
				if stage.RetryAttempt != retryIndex+1 {
					t.Fatalf("retryAttempt = %d, want %d", stage.RetryAttempt, retryIndex+1)
				}
				if stage.LastErrorCode != testCase.errorCode {
					t.Fatalf("lastErrorCode = %q, want %q", stage.LastErrorCode, testCase.errorCode)
				}
				if stage.FailureDisposition != types.RetryDispositionRetryable {
					t.Fatalf("failureDisposition = %q, want %q", stage.FailureDisposition, types.RetryDispositionRetryable)
				}
				reliabilityAssertDelay(t, before, stage.NextRetryAt, expectedDelay)

				reliabilityMakeRetryDue(t, db, stageID)
				execution = reliabilitySchedule(t, st)
				if execution.Attempt != retryIndex+2 {
					t.Fatalf("execution attempt = %d, want %d", execution.Attempt, retryIndex+2)
				}
			}

			response, err := st.UpdateStageResult(ctx, reliabilityFailureResult(
				execution,
				testCase.errorCode,
				nil,
			))
			if err != nil {
				t.Fatalf("UpdateStageResult after retries exhausted: %v", err)
			}
			stage := reliabilityOnlyStage(t, response)
			if stage.Status != types.StageStatusFailed {
				t.Fatalf("final stage status = %q, want %q", stage.Status, types.StageStatusFailed)
			}
			if stage.RetryAttempt != len(testCase.expectedDelays) {
				t.Fatalf("final retryAttempt = %d, want %d", stage.RetryAttempt, len(testCase.expectedDelays))
			}
			if stage.Attempt != len(testCase.expectedDelays)+1 {
				t.Fatalf("final execution attempt = %d, want %d", stage.Attempt, len(testCase.expectedDelays)+1)
			}
			if stage.FailureDisposition != types.RetryDispositionTerminal || !stage.IsTerminal {
				t.Fatalf("final failure metadata = disposition %q terminal %t", stage.FailureDisposition, stage.IsTerminal)
			}
			if response.Status != types.PipelineStatusFailed || !response.IsTerminal {
				t.Fatalf("pipeline final state = %q terminal=%t", response.Status, response.IsTerminal)
			}
		})
	}
}

func TestReliabilityTerminalErrorsAreNotRetriedAndBlockNextStage(t *testing.T) {
	t.Parallel()

	retryable := true
	notRetryable := false
	testCases := []struct {
		name      string
		errorCode string
		retryable *bool
	}{
		{name: "business rejection", errorCode: types.ErrorCodeBusinessRejected, retryable: &retryable},
		{name: "validation error", errorCode: types.ErrorCodeValidationError},
		{name: "invalid state", errorCode: types.ErrorCodeInvalidState},
		{name: "missing required data", errorCode: types.ErrorCodeMissingRequiredData},
		{name: "unlisted error is filtered", errorCode: "PERMANENT_OTHER"},
		{name: "explicit terminal overrides transient code", errorCode: types.ErrorCodeTimeout, retryable: &notRetryable},
	}

	for _, testCase := range testCases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			st, db := setupSchedulingStore(t)
			ctx := context.Background()
			pipelineID := insertPipeline(t, db, "terminal-"+testCase.errorCode)
			firstStageID := insertStage(t, db, pipelineID, "validate", "validation-handler")
			secondStageID := insertStage(t, db, pipelineID, "publish", "publish-handler")
			reliabilityInsertStageOptions(
				t,
				db,
				firstStageID,
				3,
				1,
				strings.Join([]string{
					types.ErrorCodeTimeout,
					types.ErrorCodeUpstreamError,
					types.ErrorCodeRateLimitExceeded,
					types.ErrorCodeBusinessRejected,
					types.ErrorCodeValidationError,
					types.ErrorCodeInvalidState,
					types.ErrorCodeMissingRequiredData,
				}, ","),
				"fixed",
				0,
			)

			execution := reliabilitySchedule(t, st)
			response, err := st.UpdateStageResult(ctx, reliabilityFailureResult(
				execution,
				testCase.errorCode,
				testCase.retryable,
			))
			if err != nil {
				t.Fatalf("UpdateStageResult: %v", err)
			}

			if len(response.Stages) != 2 {
				t.Fatalf("stage count = %d, want 2", len(response.Stages))
			}
			if response.Stages[0].Status != types.StageStatusFailed ||
				response.Stages[0].RetryAttempt != 0 ||
				response.Stages[0].FailureDisposition != types.RetryDispositionTerminal {
				t.Fatalf("first stage = %+v", response.Stages[0])
			}
			if response.Stages[1].ID != secondStageID ||
				response.Stages[1].Status != types.StageStatusNotStarted {
				t.Fatalf("next stage = %+v", response.Stages[1])
			}
			if response.Status != types.PipelineStatusFailed || !response.IsTerminal {
				t.Fatalf("pipeline = status %q terminal=%t", response.Status, response.IsTerminal)
			}

			next, err := st.GetStageToExecute(ctx)
			if err != nil {
				t.Fatalf("GetStageToExecute after terminal failure: %v", err)
			}
			if next != nil {
				t.Fatalf("terminal failure scheduled next stage: %+v", next)
			}
		})
	}
}

func TestReliabilityDuplicateAndStaleStageResultsAreNoOps(t *testing.T) {
	t.Parallel()

	t.Run("duplicate final result", func(t *testing.T) {
		st, db := setupSchedulingStore(t)
		ctx := context.Background()
		pipelineID := insertPipeline(t, db, "duplicate-result")
		stageID := insertStage(t, db, pipelineID, "claim", "claim-handler")
		execution := reliabilitySchedule(t, st)

		first := reliabilitySuccessResult(execution, "confirmed")
		if _, err := st.UpdateStageResult(ctx, first); err != nil {
			t.Fatalf("first UpdateStageResult: %v", err)
		}
		logCountBefore := reliabilityStageLogCount(t, db, stageID)

		duplicate := first
		duplicate.Result = "must-not-overwrite"
		response, err := st.UpdateStageResult(ctx, duplicate)
		if err != nil {
			t.Fatalf("duplicate UpdateStageResult: %v", err)
		}
		if got := reliabilityStageOutput(t, db, stageID); got != "confirmed" {
			t.Fatalf("duplicate overwrote output: %q", got)
		}
		if got := reliabilityStageLogCount(t, db, stageID); got != logCountBefore {
			t.Fatalf("duplicate added logs: before=%d after=%d", logCountBefore, got)
		}
		if response.Status != types.PipelineStatusCompleted || !response.IsTerminal {
			t.Fatalf("duplicate response status = %q terminal=%t", response.Status, response.IsTerminal)
		}
	})

	t.Run("stale execution and attempt", func(t *testing.T) {
		st, db := setupSchedulingStore(t)
		ctx := context.Background()
		pipelineID := insertPipeline(t, db, "stale-result")
		stageID := insertStage(t, db, pipelineID, "claim", "claim-handler")

		oldExecution := reliabilitySchedule(t, st)
		if _, err := db.Exec(
			`UPDATE stage SET started_at=$1 WHERE id=$2`,
			time.Now().UTC().Add(-2*time.Minute),
			stageID,
		); err != nil {
			t.Fatalf("age undispatched stage: %v", err)
		}
		recovered, err := st.RecoverUndispatchedStages(ctx, time.Minute)
		if err != nil {
			t.Fatalf("RecoverUndispatchedStages: %v", err)
		}
		if recovered != 1 {
			t.Fatalf("recovered = %d, want 1", recovered)
		}

		currentExecution := reliabilitySchedule(t, st)
		if currentExecution.ExecutionID == oldExecution.ExecutionID {
			t.Fatal("re-dispatch reused execution id")
		}
		if currentExecution.Attempt != oldExecution.Attempt+1 {
			t.Fatalf("new attempt = %d, want %d", currentExecution.Attempt, oldExecution.Attempt+1)
		}

		if _, err = st.UpdateStageResult(ctx, reliabilitySuccessResult(oldExecution, "stale-token")); err != nil {
			t.Fatalf("stale-token UpdateStageResult: %v", err)
		}
		wrongAttempt := reliabilitySuccessResult(currentExecution, "stale-attempt")
		wrongAttempt.Attempt = oldExecution.Attempt
		if _, err = st.UpdateStageResult(ctx, wrongAttempt); err != nil {
			t.Fatalf("stale-attempt UpdateStageResult: %v", err)
		}

		var status string
		if err = db.Get(&status, `SELECT status FROM stage WHERE id=$1`, stageID); err != nil {
			t.Fatalf("load stage status: %v", err)
		}
		if status != types.StageStatusPending {
			t.Fatalf("stale result changed status to %q", status)
		}
		if got := reliabilityStageOutput(t, db, stageID); got != "" {
			t.Fatalf("stale result wrote output %q", got)
		}

		if _, err = st.UpdateStageResult(ctx, reliabilitySuccessResult(currentExecution, "current")); err != nil {
			t.Fatalf("current UpdateStageResult: %v", err)
		}
		if got := reliabilityStageOutput(t, db, stageID); got != "current" {
			t.Fatalf("current output = %q", got)
		}
	})
}

func TestReliabilityContextPersistsAcrossRetryAndNextStageAndIsRedacted(t *testing.T) {
	t.Parallel()

	st, db := setupSchedulingStore(t)
	ctx := context.Background()
	pipelineID := insertPipeline(t, db, "context-and-redaction")
	firstStageID := insertStage(t, db, pipelineID, "claim", "claim-handler")
	secondStageID := insertStage(t, db, pipelineID, "publish", "publish-handler")
	reliabilityInsertStageOptions(
		t,
		db,
		firstStageID,
		1,
		1,
		types.ErrorCodeTimeout,
		"fixed",
		0,
	)

	const secret = "secret-claim-token"
	if _, err := db.Exec(`
		INSERT INTO pipeline_context_item (pipeline_id, key, value, value_type, is_sensitive)
		VALUES
			($1, 'tenantId', 'tenant-1', 'string', false),
			($1, 'externalToken', $2, 'string', true)
	`, pipelineID, secret); err != nil {
		t.Fatalf("insert context: %v", err)
	}
	if _, err := db.Exec(`
		UPDATE stage_io SET input=$1 WHERE stage_id=$2
	`, `{"token":"`+secret+`"}`, firstStageID); err != nil {
		t.Fatalf("update stage input: %v", err)
	}

	firstExecution := reliabilitySchedule(t, st)
	if got := reliabilityContextValue(firstExecution.ContextItems, "externalToken"); got != secret {
		t.Fatalf("execution context secret = %q", got)
	}

	failure := reliabilityFailureResult(firstExecution, types.ErrorCodeTimeout, nil)
	failure.Result = "upstream outcome unknown for " + secret
	failure.ContextItems = []types.ContextItem{
		{Key: "claimPhase", Value: "outcome_unknown", ValueType: "string"},
		{Key: "externalToken", Value: secret, ValueType: "string", IsSensitive: true},
	}
	failure.Logs = []types.StageLogMessage{{
		Message:  "request token=" + secret,
		LogLevel: "ERROR",
		Created:  time.Now().UTC(),
	}}

	retryResponse, err := st.UpdateStageResult(ctx, failure)
	if err != nil {
		t.Fatalf("UpdateStageResult timeout: %v", err)
	}
	retryStage := retryResponse.Stages[0]
	if retryStage.Attempt != 1 ||
		retryStage.RetryAttempt != 1 ||
		retryStage.LastErrorCode != types.ErrorCodeTimeout ||
		retryStage.FailureDisposition != types.RetryDispositionRetryable ||
		retryStage.IsTerminal {
		t.Fatalf("retry status metadata = %+v", retryStage)
	}

	logs, err := st.GetStageLogs(ctx, pipelineID, &firstStageID)
	if err != nil {
		t.Fatalf("GetStageLogs: %v", err)
	}
	for _, logEntry := range logs {
		if strings.Contains(logEntry.Message, secret) {
			t.Fatalf("sensitive value persisted in log: %q", logEntry.Message)
		}
	}
	if !reliabilityLogsContain(logs, types.RedactedContextValue) {
		t.Fatalf("redacted marker missing from logs: %+v", logs)
	}

	publicRetry := types.RedactPipelineResponse(retryResponse)
	if got := reliabilityContextValue(publicRetry.PipelineContext, "externalToken"); got != types.RedactedContextValue {
		t.Fatalf("public context secret = %q", got)
	}
	if publicRetry.Stages[0].Input == nil || strings.Contains(*publicRetry.Stages[0].Input, secret) {
		t.Fatalf("public input was not redacted: %v", publicRetry.Stages[0].Input)
	}
	if publicRetry.Stages[0].Output == nil || strings.Contains(*publicRetry.Stages[0].Output, secret) {
		t.Fatalf("public output was not redacted: %v", publicRetry.Stages[0].Output)
	}

	reliabilityMakeRetryDue(t, db, firstStageID)
	secondExecution := reliabilitySchedule(t, st)
	if got := reliabilityContextValue(secondExecution.ContextItems, "claimPhase"); got != "outcome_unknown" {
		t.Fatalf("retry context claimPhase = %q", got)
	}
	if got := reliabilityContextValue(secondExecution.ContextItems, "externalToken"); got != secret {
		t.Fatalf("retry execution lost raw sensitive context: %q", got)
	}

	success := reliabilitySuccessResult(secondExecution, "confirmed")
	success.ContextItems = []types.ContextItem{{
		Key: "claimPhase", Value: "confirmed", ValueType: "string",
	}}
	if _, err = st.UpdateStageResult(ctx, success); err != nil {
		t.Fatalf("UpdateStageResult success: %v", err)
	}

	nextExecution := reliabilitySchedule(t, st)
	if nextExecution.StageID != secondStageID {
		t.Fatalf("scheduled stage = %d, want %d", nextExecution.StageID, secondStageID)
	}
	if got := reliabilityContextValue(nextExecution.ContextItems, "claimPhase"); got != "confirmed" {
		t.Fatalf("next-stage context claimPhase = %q", got)
	}
	if got := reliabilityContextValue(nextExecution.ContextItems, "externalToken"); got != secret {
		t.Fatalf("next-stage execution lost sensitive context: %q", got)
	}
}

func TestReliabilityCancellationFencesPendingAndRunningResults(t *testing.T) {
	t.Parallel()

	for _, activeStatus := range []string{types.StageStatusPending, types.StageStatusRunning} {
		activeStatus := activeStatus
		t.Run(activeStatus, func(t *testing.T) {
			t.Parallel()

			st, db := setupSchedulingStore(t)
			ctx := context.Background()
			const applicationID = 42
			pipelineID := insertPipeline(t, db, "cancel-"+activeStatus)
			if _, err := db.Exec(
				`UPDATE pipeline SET application_id=$1 WHERE id=$2`,
				applicationID,
				pipelineID,
			); err != nil {
				t.Fatalf("set application: %v", err)
			}
			stageID := insertStage(t, db, pipelineID, "cancel-me", "handler")
			execution := reliabilitySchedule(t, st)
			if activeStatus == types.StageStatusRunning {
				if _, err := db.Exec(`
					UPDATE stage
					SET status=$1, lease_owner='worker-a', lease_expires_at=$2
					WHERE id=$3
				`, types.StageStatusRunning, time.Now().UTC().Add(time.Minute), stageID); err != nil {
					t.Fatalf("mark running: %v", err)
				}
			}

			cancelled, err := st.CancelPipelineForApplication(ctx, pipelineID, applicationID)
			if err != nil {
				t.Fatalf("CancelPipelineForApplication: %v", err)
			}
			if cancelled.Status != types.PipelineStatusCancelled || !cancelled.IsTerminal {
				t.Fatalf("cancelled pipeline = %q terminal=%t", cancelled.Status, cancelled.IsTerminal)
			}
			if len(cancelled.Stages) != 1 || cancelled.Stages[0].Status != types.StageStatusCancelled {
				t.Fatalf("cancelled stages = %+v", cancelled.Stages)
			}

			var executionID, leaseOwner *string
			var leaseExpiry *time.Time
			if err = db.QueryRow(`
				SELECT execution_id, lease_owner, lease_expires_at
				FROM stage WHERE id=$1
			`, stageID).Scan(&executionID, &leaseOwner, &leaseExpiry); err != nil {
				t.Fatalf("load execution metadata: %v", err)
			}
			if executionID != nil || leaseOwner != nil || leaseExpiry != nil {
				t.Fatalf("cancel left execution metadata: execution=%v owner=%v expiry=%v", executionID, leaseOwner, leaseExpiry)
			}

			late := reliabilitySuccessResult(execution, "late-success-must-not-win")
			lateResponse, err := st.UpdateStageResult(ctx, late)
			if err != nil {
				t.Fatalf("late UpdateStageResult: %v", err)
			}
			if lateResponse.Status != types.PipelineStatusCancelled ||
				lateResponse.Stages[0].Status != types.StageStatusCancelled {
				t.Fatalf("late result changed cancelled state: %+v", lateResponse)
			}
			if got := reliabilityStageOutput(t, db, stageID); got != "" {
				t.Fatalf("late result wrote output %q", got)
			}

			repeated, err := st.CancelPipelineForApplication(ctx, pipelineID, applicationID)
			if err != nil {
				t.Fatalf("idempotent repeated cancellation: %v", err)
			}
			if repeated.Status != types.PipelineStatusCancelled {
				t.Fatalf("repeated cancellation status = %q", repeated.Status)
			}
		})
	}
}

func TestReliabilityStageLeaseFencesWorkersAndCanRecoverAfterExpiry(t *testing.T) {
	t.Parallel()

	st, db := setupSchedulingStore(t)
	ctx := context.Background()
	reliabilityCreateWorkerClientTable(t, db)

	const applicationID = 77
	reliabilityInsertWorker(t, db, "worker-a", applicationID, "token-a")
	reliabilityInsertWorker(t, db, "worker-b", applicationID, "token-b")

	pipelineID := insertPipeline(t, db, "lease")
	if _, err := db.Exec(
		`UPDATE pipeline SET application_id=$1 WHERE id=$2`,
		applicationID,
		pipelineID,
	); err != nil {
		t.Fatalf("set application: %v", err)
	}
	stageID := insertStage(t, db, pipelineID, "claim", "claim-handler")
	firstExecution := reliabilitySchedule(t, st)

	firstLease, err := st.AcquireStageLease(
		ctx,
		stageID,
		types.StageLeaseRequest{ExecutionID: firstExecution.ExecutionID, WorkerID: "worker-a"},
		"token-a",
		time.Minute,
	)
	if err != nil {
		t.Fatalf("AcquireStageLease worker-a: %v", err)
	}
	if !firstLease.Acquired || firstLease.Attempt != firstExecution.Attempt {
		t.Fatalf("worker-a lease = %+v", firstLease)
	}

	secondLease, err := st.AcquireStageLease(
		ctx,
		stageID,
		types.StageLeaseRequest{ExecutionID: firstExecution.ExecutionID, WorkerID: "worker-b"},
		"token-b",
		time.Minute,
	)
	if err != nil {
		t.Fatalf("AcquireStageLease worker-b: %v", err)
	}
	if secondLease.Acquired || secondLease.Reason != "lease_held" {
		t.Fatalf("worker-b concurrently acquired active lease: %+v", secondLease)
	}

	if _, err = db.Exec(
		`UPDATE stage SET lease_expires_at=$1 WHERE id=$2`,
		time.Now().UTC().Add(-time.Minute),
		stageID,
	); err != nil {
		t.Fatalf("expire lease: %v", err)
	}

	expiredLease, err := st.AcquireStageLease(
		ctx,
		stageID,
		types.StageLeaseRequest{ExecutionID: firstExecution.ExecutionID, WorkerID: "worker-b"},
		"token-b",
		time.Minute,
	)
	if err != nil {
		t.Fatalf("AcquireStageLease after expiry: %v", err)
	}
	if expiredLease.Acquired || expiredLease.Reason != "lease_expired" {
		t.Fatalf("expired execution was reacquired before recovery: %+v", expiredLease)
	}

	if _, err = st.UpdateStageResult(ctx, reliabilitySuccessResult(firstExecution, "expired-result")); err != nil {
		t.Fatalf("result after lease expiry: %v", err)
	}
	var expiredStatus string
	if err = db.Get(&expiredStatus, `SELECT status FROM stage WHERE id=$1`, stageID); err != nil {
		t.Fatalf("load expired stage status: %v", err)
	}
	if expiredStatus != types.StageStatusRunning || reliabilityStageOutput(t, db, stageID) != "" {
		t.Fatalf("expired result was accepted: status=%q", expiredStatus)
	}

	recovered, err := st.RecoverExpiredStageLeases(ctx)
	if err != nil {
		t.Fatalf("RecoverExpiredStageLeases: %v", err)
	}
	if recovered != 1 {
		t.Fatalf("expired leases recovered = %d, want 1", recovered)
	}

	secondExecution := reliabilitySchedule(t, st)
	if secondExecution.ExecutionID == firstExecution.ExecutionID {
		t.Fatal("expired execution id was reused")
	}
	if secondExecution.Attempt != firstExecution.Attempt+1 {
		t.Fatalf("post-expiry attempt = %d, want %d", secondExecution.Attempt, firstExecution.Attempt+1)
	}

	secondLease, err = st.AcquireStageLease(
		ctx,
		stageID,
		types.StageLeaseRequest{ExecutionID: secondExecution.ExecutionID, WorkerID: "worker-b"},
		"token-b",
		time.Minute,
	)
	if err != nil {
		t.Fatalf("AcquireStageLease worker-b after expiry: %v", err)
	}
	if !secondLease.Acquired {
		t.Fatalf("worker-b did not acquire recovered execution: %+v", secondLease)
	}

	if _, err = st.UpdateStageResult(ctx, reliabilitySuccessResult(firstExecution, "stale")); err != nil {
		t.Fatalf("late expired result: %v", err)
	}
	var status string
	if err = db.Get(&status, `SELECT status FROM stage WHERE id=$1`, stageID); err != nil {
		t.Fatalf("load stage status: %v", err)
	}
	if status != types.StageStatusRunning {
		t.Fatalf("late expired result changed status to %q", status)
	}

	if _, err = st.UpdateStageResult(ctx, reliabilitySuccessResult(secondExecution, "confirmed")); err != nil {
		t.Fatalf("current leased result: %v", err)
	}
}

func TestReliabilityUndispatchedRecoveryUsesNewExecutionAndKeepsConfirmedDispatch(t *testing.T) {
	t.Parallel()

	st, db := setupSchedulingStore(t)
	ctx := context.Background()
	pipelineID := insertPipeline(t, db, "undispatched")
	stageID := insertStage(t, db, pipelineID, "claim", "claim-handler")

	firstExecution := reliabilitySchedule(t, st)
	if _, err := db.Exec(
		`UPDATE stage SET started_at=$1 WHERE id=$2`,
		time.Now().UTC().Add(-2*time.Minute),
		stageID,
	); err != nil {
		t.Fatalf("age first dispatch: %v", err)
	}
	recovered, err := st.RecoverUndispatchedStages(ctx, time.Minute)
	if err != nil {
		t.Fatalf("RecoverUndispatchedStages: %v", err)
	}
	if recovered != 1 {
		t.Fatalf("undispatched recovered = %d, want 1", recovered)
	}

	secondExecution := reliabilitySchedule(t, st)
	if secondExecution.ExecutionID == firstExecution.ExecutionID {
		t.Fatal("undispatched recovery reused execution id")
	}
	if secondExecution.Attempt != 2 {
		t.Fatalf("second attempt = %d, want 2", secondExecution.Attempt)
	}
	marked, err := st.MarkStageDispatched(ctx, stageID, secondExecution.ExecutionID)
	if err != nil {
		t.Fatalf("MarkStageDispatched: %v", err)
	}
	if !marked {
		t.Fatal("current execution was not marked dispatched")
	}

	if _, err = db.Exec(
		`UPDATE stage SET started_at=$1 WHERE id=$2`,
		time.Now().UTC().Add(-2*time.Minute),
		stageID,
	); err != nil {
		t.Fatalf("age confirmed dispatch: %v", err)
	}
	recovered, err = st.RecoverUndispatchedStages(ctx, time.Minute)
	if err != nil {
		t.Fatalf("RecoverUndispatchedStages confirmed: %v", err)
	}
	if recovered != 0 {
		t.Fatalf("confirmed dispatch recovered = %d, want 0", recovered)
	}
}

func TestReliabilityLegacyResultAndRowsRemainReadable(t *testing.T) {
	t.Parallel()

	st, db := setupSchedulingStore(t)
	ctx := context.Background()
	pipelineID := insertPipeline(t, db, "legacy-row")
	stageID := insertStage(t, db, pipelineID, "legacy", "legacy-handler")

	before, err := st.GetPipelineWithStages(ctx, pipelineID)
	if err != nil {
		t.Fatalf("GetPipelineWithStages legacy row: %v", err)
	}
	stage := reliabilityOnlyStage(t, before)
	if stage.Attempt != 0 ||
		stage.RetryAttempt != 0 ||
		stage.LastErrorCode != "" ||
		stage.FailureDisposition != "" {
		t.Fatalf("legacy row metadata = %+v", stage)
	}

	execution := reliabilitySchedule(t, st)
	legacyResult := types.StageResultMessage{
		PipelineID: execution.PipelineID,
		StageID:    stageID,
		Result:     "legacy-success",
		IsSuccess:  true,
	}
	after, err := st.UpdateStageResult(ctx, legacyResult)
	if err != nil {
		t.Fatalf("legacy UpdateStageResult: %v", err)
	}
	if after.Status != types.PipelineStatusCompleted || !after.IsTerminal {
		t.Fatalf("legacy result pipeline = %q terminal=%t", after.Status, after.IsTerminal)
	}
	if got := reliabilityStageOutput(t, db, stageID); got != "legacy-success" {
		t.Fatalf("legacy result output = %q", got)
	}
}

func TestReliabilityClaimOutcomeUnknownQueriesStatusInsteadOfPostingAgain(t *testing.T) {
	t.Parallel()

	var externalMu sync.Mutex
	postCount := 0
	getCount := 0
	accepted := false
	external := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		externalMu.Lock()
		defer externalMu.Unlock()

		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/claims":
			postCount++
			if r.Header.Get("Idempotency-Key") != "claim-correlation-1" {
				http.Error(w, "missing stable idempotency key", http.StatusBadRequest)
				return
			}
			accepted = true
			externalMu.Unlock()
			time.Sleep(100 * time.Millisecond)
			externalMu.Lock()
			w.WriteHeader(http.StatusAccepted)
		case r.Method == http.MethodGet && r.URL.Path == "/claims/claim-correlation-1":
			getCount++
			if !accepted {
				http.Error(w, "not found", http.StatusNotFound)
				return
			}
			_, _ = io.WriteString(w, "Confirmed")
		default:
			http.NotFound(w, r)
		}
	}))
	defer external.Close()

	st, db := setupSchedulingStore(t)
	ctx := context.Background()
	pipelineID := insertPipeline(t, db, "ensure-claim")
	if _, err := db.Exec(
		`UPDATE pipeline SET idempotency_key=$1 WHERE id=$2`,
		"claim-correlation-1",
		pipelineID,
	); err != nil {
		t.Fatalf("set idempotency key: %v", err)
	}
	stageID := insertStage(t, db, pipelineID, "ensure-claim", "claim-handler")
	reliabilityInsertStageOptions(
		t,
		db,
		stageID,
		2,
		1,
		strings.Join([]string{
			types.ErrorCodeTimeout,
			types.ErrorCodeUpstreamError,
			types.ErrorCodeRateLimitExceeded,
		}, ","),
		"fixed",
		0,
	)

	journal := &reliabilityClaimJournal{}
	firstExecution := reliabilitySchedule(t, st)
	firstResult := reliabilityExecuteEnsureClaim(
		t,
		external.URL,
		firstExecution,
		journal,
		15*time.Millisecond,
	)
	if firstResult.IsSuccess || firstResult.ErrorCode != types.ErrorCodeTimeout {
		t.Fatalf("first result = %+v", firstResult)
	}
	firstResponse, err := st.UpdateStageResult(ctx, firstResult)
	if err != nil {
		t.Fatalf("persist outcome unknown: %v", err)
	}
	if firstResponse.Stages[0].Status != types.StageStatusRetryScheduled {
		t.Fatalf("first stage status = %q", firstResponse.Stages[0].Status)
	}

	reliabilityMakeRetryDue(t, db, stageID)
	secondExecution := reliabilitySchedule(t, st)
	secondResult := reliabilityExecuteEnsureClaim(
		t,
		external.URL,
		secondExecution,
		journal,
		time.Second,
	)
	if !secondResult.IsSuccess {
		t.Fatalf("reconciliation result = %+v", secondResult)
	}
	finalResponse, err := st.UpdateStageResult(ctx, secondResult)
	if err != nil {
		t.Fatalf("persist confirmed claim: %v", err)
	}
	if finalResponse.Status != types.PipelineStatusCompleted || !finalResponse.IsTerminal {
		t.Fatalf("claim pipeline = %q terminal=%t", finalResponse.Status, finalResponse.IsTerminal)
	}

	externalMu.Lock()
	posts := postCount
	gets := getCount
	externalMu.Unlock()
	if posts != 1 {
		t.Fatalf("claim POST count = %d, want 1", posts)
	}
	if gets != 1 {
		t.Fatalf("claim status GET count = %d, want 1", gets)
	}
}

func TestReliabilityExecutionMetadataIsStableAcrossRetry(t *testing.T) {
	st, db := setupSchedulingStore(t)
	ctx := context.Background()
	pipelineID := insertPipeline(t, db, "stable-metadata")
	stageID := insertStage(t, db, pipelineID, "claim", "claim-handler")
	traceID := "0123456789abcdef0123456789abcdef"
	spanID := "0123456789abcdef"
	idempotencyKey := "stable-correlation-key"
	if _, err := db.Exec(`
		UPDATE pipeline
		SET trace_id=$1, idempotency_key=$2
		WHERE id=$3
	`, traceID, idempotencyKey, pipelineID); err != nil {
		t.Fatalf("set pipeline metadata: %v", err)
	}
	if _, err := db.Exec(`UPDATE stage SET span_id=$1 WHERE id=$2`, spanID, stageID); err != nil {
		t.Fatalf("set stage span: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO pipeline_context_item (key, value, value_type, is_sensitive, pipeline_id)
		VALUES ('tracestate', '"vendor=value"', 'System.String', false, $1)
	`, pipelineID); err != nil {
		t.Fatalf("insert tracestate: %v", err)
	}
	reliabilityInsertStageOptions(
		t,
		db,
		stageID,
		1,
		1,
		types.ErrorCodeTimeout,
		"fixed",
		0,
	)

	first := reliabilitySchedule(t, st)
	if first.PipelineID == nil || *first.PipelineID != pipelineID ||
		first.StageID != stageID ||
		first.IdempotencyKey != idempotencyKey ||
		first.Traceparent != "00-"+traceID+"-"+spanID+"-01" ||
		first.Tracestate != "vendor=value" {
		t.Fatalf("first execution metadata = %+v", first)
	}

	retryable := true
	if _, err := st.UpdateStageResult(
		ctx,
		reliabilityFailureResult(first, types.ErrorCodeTimeout, &retryable),
	); err != nil {
		t.Fatalf("schedule retry: %v", err)
	}
	reliabilityMakeRetryDue(t, db, stageID)
	second := reliabilitySchedule(t, st)

	if second.ExecutionID == first.ExecutionID || second.Attempt != first.Attempt+1 {
		t.Fatalf("execution identity did not advance: first=%+v second=%+v", first, second)
	}
	if second.PipelineID == nil || *second.PipelineID != pipelineID ||
		second.StageID != stageID ||
		second.IdempotencyKey != first.IdempotencyKey ||
		second.Traceparent != first.Traceparent ||
		second.Tracestate != first.Tracestate {
		t.Fatalf("stable metadata changed: first=%+v second=%+v", first, second)
	}
}

func TestReliabilityBackoffJitterRespectsCapAndLargeAttempts(t *testing.T) {
	const capDelay = 15 * time.Second
	for sample := 0; sample < 50; sample++ {
		delay := computeStageOptionsBackoff(10, 15, "exponential", true, 4)
		if delay <= 0 || delay > capDelay {
			t.Fatalf("jittered capped delay = %s", delay)
		}
	}

	if delay := computeStageOptionsBackoff(1, 60, "exponential", false, 10_000); delay != time.Minute {
		t.Fatalf("large-attempt delay = %s, want %s", delay, time.Minute)
	}
}

type reliabilityClaimJournal struct {
	mu             sync.Mutex
	outcomeUnknown bool
	confirmed      bool
}

func reliabilityExecuteEnsureClaim(
	t *testing.T,
	baseURL string,
	execution *types.StageNextMessage,
	journal *reliabilityClaimJournal,
	timeout time.Duration,
) types.StageResultMessage {
	t.Helper()

	result := types.StageResultMessage{
		PipelineID:  execution.PipelineID,
		StageID:     execution.StageID,
		ExecutionID: execution.ExecutionID,
		Attempt:     execution.Attempt,
	}

	journal.mu.Lock()
	confirmed := journal.confirmed
	outcomeUnknown := journal.outcomeUnknown
	journal.mu.Unlock()

	if confirmed {
		result.IsSuccess = true
		result.Result = "already-confirmed"
		return result
	}

	client := &http.Client{Timeout: timeout}
	if outcomeUnknown {
		response, err := client.Get(baseURL + "/claims/" + execution.IdempotencyKey)
		if err != nil {
			result.ErrorCode = types.ErrorCodeUpstreamError
			result.Result = err.Error()
			return result
		}
		defer response.Body.Close()
		body, err := io.ReadAll(response.Body)
		if err != nil {
			result.ErrorCode = types.ErrorCodeUpstreamError
			result.Result = err.Error()
			return result
		}
		if response.StatusCode != http.StatusOK || string(body) != "Confirmed" {
			result.ErrorCode = types.ErrorCodeUpstreamError
			result.Result = fmt.Sprintf("status=%d body=%s", response.StatusCode, body)
			return result
		}

		journal.mu.Lock()
		journal.confirmed = true
		journal.outcomeUnknown = false
		journal.mu.Unlock()
		result.IsSuccess = true
		result.Result = "confirmed-by-status"
		result.ContextItems = []types.ContextItem{{
			Key: "claimPhase", Value: "confirmed", ValueType: "string",
		}}
		return result
	}

	request, err := http.NewRequest(http.MethodPost, baseURL+"/claims", nil)
	if err != nil {
		t.Fatalf("new claim request: %v", err)
	}
	request.Header.Set("Idempotency-Key", execution.IdempotencyKey)
	response, err := client.Do(request)
	if response != nil {
		_ = response.Body.Close()
	}
	if err == nil {
		t.Fatal("first claim POST unexpectedly returned a known outcome")
	}

	journal.mu.Lock()
	journal.outcomeUnknown = true
	journal.mu.Unlock()
	result.ErrorCode = types.ErrorCodeTimeout
	result.Result = "outcome unknown"
	result.ContextItems = []types.ContextItem{{
		Key: "claimPhase", Value: "outcome_unknown", ValueType: "string",
	}}
	return result
}

func reliabilityInsertStageOptions(
	t *testing.T,
	db *sqlx.DB,
	stageID int,
	maxRetries int,
	retryInterval int,
	retryCodes string,
	backoff string,
	maxRetryInterval int,
) {
	t.Helper()
	if _, err := db.Exec(`
		INSERT INTO stage_options (
			stage_id,
			max_retries,
			retry_interval,
			retry_on_error_codes,
			retry_backoff,
			max_retry_interval,
			retry_jitter
		)
		VALUES ($1, $2, $3, $4, $5, $6, false)
	`, stageID, maxRetries, retryInterval, retryCodes, backoff, maxRetryInterval); err != nil {
		t.Fatalf("insert stage options: %v", err)
	}
}

func reliabilitySchedule(t *testing.T, st *Store) *types.StageNextMessage {
	t.Helper()
	message, err := st.GetStageToExecute(context.Background())
	if err != nil {
		t.Fatalf("GetStageToExecute: %v", err)
	}
	if message == nil {
		t.Fatal("GetStageToExecute returned nil")
	}
	if strings.TrimSpace(message.ExecutionID) == "" || message.Attempt <= 0 {
		t.Fatalf("execution metadata = id %q attempt %d", message.ExecutionID, message.Attempt)
	}
	return message
}

func reliabilityFailureResult(
	execution *types.StageNextMessage,
	errorCode string,
	retryable *bool,
) types.StageResultMessage {
	return types.StageResultMessage{
		PipelineID:  execution.PipelineID,
		StageID:     execution.StageID,
		ExecutionID: execution.ExecutionID,
		Attempt:     execution.Attempt,
		Result:      "failure-" + errorCode,
		ErrorCode:   errorCode,
		Retryable:   retryable,
	}
}

func reliabilitySuccessResult(
	execution *types.StageNextMessage,
	result string,
) types.StageResultMessage {
	return types.StageResultMessage{
		PipelineID:  execution.PipelineID,
		StageID:     execution.StageID,
		ExecutionID: execution.ExecutionID,
		Attempt:     execution.Attempt,
		Result:      result,
		IsSuccess:   true,
	}
}

func reliabilityOnlyStage(t *testing.T, response *types.PipelineResponse) types.StageResponse {
	t.Helper()
	if response == nil || len(response.Stages) != 1 {
		t.Fatalf("pipeline stages = %+v, want exactly one", response)
	}
	return response.Stages[0]
}

func reliabilityAssertDelay(
	t *testing.T,
	start time.Time,
	nextRetryAt *time.Time,
	expected time.Duration,
) {
	t.Helper()
	if nextRetryAt == nil {
		t.Fatal("nextRetryAt is nil")
	}
	actual := nextRetryAt.Sub(start)
	const tolerance = 400 * time.Millisecond
	if actual < expected-tolerance || actual > expected+tolerance {
		t.Fatalf("retry delay = %s, want %s ± %s", actual, expected, tolerance)
	}
}

func reliabilityMakeRetryDue(t *testing.T, db *sqlx.DB, stageID int) {
	t.Helper()
	if _, err := db.Exec(
		`UPDATE stage SET next_retry_at=$1 WHERE id=$2`,
		time.Now().UTC().Add(-time.Second),
		stageID,
	); err != nil {
		t.Fatalf("make retry due: %v", err)
	}
}

func reliabilityStageOutput(t *testing.T, db *sqlx.DB, stageID int) string {
	t.Helper()
	var output *string
	if err := db.Get(&output, `SELECT output FROM stage_io WHERE stage_id=$1`, stageID); err != nil {
		t.Fatalf("load stage output: %v", err)
	}
	if output == nil {
		return ""
	}
	return *output
}

func reliabilityStageLogCount(t *testing.T, db *sqlx.DB, stageID int) int {
	t.Helper()
	var count int
	if err := db.Get(&count, `SELECT COUNT(*) FROM stage_log WHERE stage_id=$1`, stageID); err != nil {
		t.Fatalf("count stage logs: %v", err)
	}
	return count
}

func reliabilityContextValue(items []types.ContextItem, key string) string {
	for _, item := range items {
		if item.Key == key {
			return item.Value
		}
	}
	return ""
}

func reliabilityLogsContain(logs []types.StageLog, expected string) bool {
	for _, logEntry := range logs {
		if strings.Contains(logEntry.Message, expected) {
			return true
		}
	}
	return false
}

func reliabilityCreateWorkerClientTable(t *testing.T, db *sqlx.DB) {
	t.Helper()
	if _, err := db.Exec(`
		CREATE TABLE worker_client (
			id TEXT PRIMARY KEY,
			application_id INTEGER NOT NULL,
			session_token TEXT NOT NULL,
			session_expires_at TIMESTAMP NOT NULL
		)
	`); err != nil {
		t.Fatalf("create worker_client: %v", err)
	}
}

func reliabilityInsertWorker(
	t *testing.T,
	db *sqlx.DB,
	workerID string,
	applicationID int,
	sessionToken string,
) {
	t.Helper()
	if _, err := db.Exec(`
		INSERT INTO worker_client (id, application_id, session_token, session_expires_at)
		VALUES ($1, $2, $3, $4)
	`, workerID, applicationID, sessionToken, time.Now().UTC().Add(time.Hour)); err != nil {
		t.Fatalf("insert worker %s: %v", workerID, err)
	}
}
