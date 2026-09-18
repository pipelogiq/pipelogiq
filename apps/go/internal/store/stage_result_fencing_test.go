package store

import (
	"context"
	"testing"
	"time"

	"pipelogiq/internal/types"
)

// Queue acceptance and database consumption are separate events. A result can
// outlive its lease in the queue without its execution having been recovered.
func TestReliabilityCurrentResultSurvivesLeaseExpiryBeforeConsumption(t *testing.T) {
	t.Parallel()
	for _, initialStatus := range []string{types.StageStatusPending, types.StageStatusRunning} {
		for _, outcome := range []string{"success", "failure", "approval"} {
			t.Run(initialStatus+"/"+outcome, func(t *testing.T) {
				t.Parallel()
				st, db := setupSchedulingStore(t)
				ctx := context.Background()
				pipelineID := insertPipeline(t, db, "queued-result")
				stageID := insertStage(t, db, pipelineID, "claim", "claim-handler")
				execution := reliabilitySchedule(t, st)
				queuedResult := reliabilitySuccessResult(execution, "queued-before-expiry")
				wantStatus := types.StageStatusCompleted
				switch outcome {
				case "failure":
					queuedResult.IsSuccess = false
					wantStatus = types.StageStatusFailed
				case "approval":
					queuedResult.IsSuccess = false
					queuedResult.IsWaitingForApproval = true
					wantStatus = types.StageStatusWaitingApproval
				}
				if _, err := db.Exec(
					`UPDATE stage SET status=$1, lease_owner='worker-a', lease_expires_at=$2 WHERE id=$3`,
					initialStatus, time.Now().UTC().Add(-time.Minute), stageID,
				); err != nil {
					t.Fatalf("expire lease before result consumption: %v", err)
				}

				response, err := st.UpdateStageResult(ctx, queuedResult)
				if err != nil {
					t.Fatalf("consume queued result: %v", err)
				}
				if got := reliabilityOnlyStage(t, response).Status; got != wantStatus {
					t.Fatalf("result status = %q, want %q", got, wantStatus)
				}
				if got := reliabilityStageOutput(t, db, stageID); got != queuedResult.Result {
					t.Fatalf("output = %q, want %q", got, queuedResult.Result)
				}
				// Result consumption wins first, so subsequent lease recovery
				// cannot resurrect this execution and run its handler again.
				if recovered, err := st.RecoverExpiredStageLeases(ctx); err != nil || recovered != 0 {
					t.Fatalf("recovery after result = %d, %v; want 0, nil", recovered, err)
				}
				queuedResult.Result = "duplicate-must-not-overwrite"
				if _, err := st.UpdateStageResult(ctx, queuedResult); err != nil {
					t.Fatalf("consume duplicate: %v", err)
				}
				if got := reliabilityStageOutput(t, db, stageID); got != "queued-before-expiry" {
					t.Fatalf("duplicate changed output to %q", got)
				}
			})
		}
	}
}

func TestReliabilityExpiredResultRequiresBothCurrentExecutionFences(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name   string
		change func(*types.StageResultMessage)
	}{
		{"wrong execution", func(result *types.StageResultMessage) { result.ExecutionID = "superseded" }},
		{"wrong attempt", func(result *types.StageResultMessage) { result.Attempt++ }},
		{"legacy no execution", func(result *types.StageResultMessage) { result.ExecutionID = "" }},
		{"legacy no attempt", func(result *types.StageResultMessage) { result.Attempt = 0 }},
		{"legacy negative attempt", func(result *types.StageResultMessage) { result.Attempt = -1 }},
		{"legacy no fences", func(result *types.StageResultMessage) {
			result.ExecutionID = ""
			result.Attempt = 0
		}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			st, db := setupSchedulingStore(t)
			ctx := context.Background()
			pipelineID := insertPipeline(t, db, "expired-fences")
			stageID := insertStage(t, db, pipelineID, "claim", "claim-handler")
			execution := reliabilitySchedule(t, st)
			if _, err := db.Exec(
				`UPDATE stage SET status=$1, lease_owner='worker-a', lease_expires_at=$2 WHERE id=$3`,
				types.StageStatusRunning, time.Now().UTC().Add(-time.Minute), stageID,
			); err != nil {
				t.Fatalf("expire lease: %v", err)
			}
			stale := reliabilitySuccessResult(execution, "must-not-write")
			testCase.change(&stale)
			response, err := st.UpdateStageResult(ctx, stale)
			if err != nil {
				t.Fatalf("consume stale result: %v", err)
			}
			if got := reliabilityOnlyStage(t, response).Status; got != types.StageStatusRunning {
				t.Fatalf("stale result changed status to %q", got)
			}
			if got := reliabilityStageOutput(t, db, stageID); got != "" {
				t.Fatalf("stale result wrote output %q", got)
			}
			if _, err := st.UpdateStageResult(ctx, reliabilitySuccessResult(execution, "current")); err != nil {
				t.Fatalf("consume current result: %v", err)
			}
			if got := reliabilityStageOutput(t, db, stageID); got != "current" {
				t.Fatalf("current result output = %q", got)
			}
		})
	}
}
