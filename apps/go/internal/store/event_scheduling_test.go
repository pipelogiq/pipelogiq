package store

import (
	"context"
	"fmt"
	"testing"
	"time"

	"pipelogiq/internal/types"
)

func TestMixedEventsExecuteInOrderAndRetryBeforeSuccessors(t *testing.T) {
	for eventIndex := 0; eventIndex < 3; eventIndex++ {
		t.Run(fmt.Sprintf("event-position-%d", eventIndex), func(t *testing.T) {
			st, db := setupSchedulingStore(t)
			ctx := context.Background()
			pipelineID := insertPipeline(t, db, "mixed-event")
			ids := make([]int, 3)
			for index := range ids {
				ids[index] = insertStage(t, db, pipelineID, fmt.Sprintf("step-%d", index), "mixed-handler")
			}
			if _, err := db.Exec(`UPDATE stage SET is_event=true WHERE id=$1`, ids[eventIndex]); err != nil {
				t.Fatal(err)
			}
			reliabilityInsertStageOptions(t, db, ids[eventIndex], 1, 1, types.ErrorCodeUpstreamError, "fixed", 1)
			for index, stageID := range ids {
				execution := reliabilitySchedule(t, st)
				if execution.StageID != stageID {
					t.Fatalf("stage order: got %d, want %d", execution.StageID, stageID)
				}
				if next, err := st.GetStageToExecute(ctx); err != nil || next != nil {
					t.Fatalf("successor passed unfinished stage: %+v, %v", next, err)
				}
				if index == eventIndex {
					if _, err := st.UpdateStageResult(ctx, reliabilityFailureResult(execution, types.ErrorCodeUpstreamError, nil)); err != nil {
						t.Fatal(err)
					}
					if next, err := st.GetStageToExecute(ctx); err != nil || next != nil {
						t.Fatalf("successor passed retrying event: %+v, %v", next, err)
					}
					reliabilityMakeRetryDue(t, db, stageID)
					retry := reliabilitySchedule(t, st)
					if retry.StageID != stageID || retry.ExecutionID == execution.ExecutionID || retry.Attempt != 2 {
						t.Fatalf("invalid event retry: %+v", retry)
					}
					execution = retry
				}
				pipeline, err := st.UpdateStageResult(ctx, reliabilitySuccessResult(execution, "done"))
				if err != nil {
					t.Fatal(err)
				}
				if pipeline.IsTerminal != (index == len(ids)-1) {
					t.Fatalf("terminal state after step %d: %+v", index, pipeline)
				}
				if index == len(ids)-1 && pipeline.Status != types.PipelineStatusCompleted {
					t.Fatalf("mixed pipeline not completed: %+v", pipeline)
				}
			}
		})
	}
}

func TestParallelMixedEventCompletionWaitsForEveryBranch(t *testing.T) {
	st, db := setupSchedulingStore(t)
	ctx := context.Background()
	pid := insertPipeline(t, db, "parallel-event")
	root := insertStage(t, db, pid, "root", "handler")
	event := insertStage(t, db, pid, "event-branch", "handler")
	last := insertStage(t, db, pid, "last-created-branch", "handler")
	if _, err := db.Exec(`UPDATE stage SET is_event=true WHERE id=$1`, event); err != nil {
		t.Fatal(err)
	}
	setDependsOn(t, db, event, "root")
	setDependsOn(t, db, last, "root")
	first := reliabilitySchedule(t, st)
	if first.StageID != root {
		t.Fatal("root was not scheduled first")
	}
	if _, err := st.UpdateStageResult(ctx, reliabilitySuccessResult(first, "root-done")); err != nil {
		t.Fatal(err)
	}
	branches := map[int]*types.StageNextMessage{}
	for range 2 {
		execution := reliabilitySchedule(t, st)
		branches[execution.StageID] = execution
	}
	if branches[event] == nil || branches[last] == nil {
		t.Fatalf("both branches must dispatch: %+v", branches)
	}
	partial, err := st.UpdateStageResult(ctx, reliabilitySuccessResult(branches[last], "last-id-done"))
	if err != nil || partial.IsTerminal {
		t.Fatalf("highest-ID completion fenced the event branch: %+v, %v", partial, err)
	}
	completed, err := st.UpdateStageResult(ctx, reliabilitySuccessResult(branches[event], "event-done"))
	if err != nil || completed.Status != types.PipelineStatusCompleted {
		t.Fatalf("final event completion: %+v, %v", completed, err)
	}
	if got := reliabilityStageOutput(t, db, event); got != "event-done" {
		t.Fatalf("event result was dropped: %q", got)
	}
}

func TestMixedEventFailureContinuationPreservesFailedOutcome(t *testing.T) {
	for eventIndex := 0; eventIndex < 2; eventIndex++ {
		t.Run(fmt.Sprintf("event-position-%d", eventIndex), func(t *testing.T) {
			st, db := setupSchedulingStore(t)
			ctx := context.Background()
			pid := insertPipeline(t, db, "event-failure-continuation")
			ids := []int{insertStage(t, db, pid, "fail", "handler"), insertStage(t, db, pid, "cleanup", "handler")}
			if _, err := db.Exec(`UPDATE stage SET is_event=true WHERE id=$1`, ids[eventIndex]); err != nil {
				t.Fatal(err)
			}
			insertStageRunNextIfFailedOption(t, db, ids[1], true)
			first := reliabilitySchedule(t, st)
			failed, err := st.UpdateStageResult(ctx, reliabilityFailureResult(first, types.ErrorCodeBusinessRejected, nil))
			if err != nil || failed.IsTerminal {
				t.Fatalf("event failure blocked valid continuation: %+v, %v", failed, err)
			}
			cleanup := reliabilitySchedule(t, st)
			if cleanup.StageID != ids[1] {
				t.Fatalf("wrong continuation: %+v", cleanup)
			}
			finished, err := st.UpdateStageResult(ctx, reliabilitySuccessResult(cleanup, "cleanup-done"))
			if err != nil || !finished.IsTerminal || finished.Status != types.PipelineStatusFailed {
				t.Fatalf("event failure was hidden by successful continuation: %+v, %v", finished, err)
			}
		})
	}
}

func TestEventHistoryParticipatesInRateLimitAndCircuitBreaker(t *testing.T) {
	for _, kind := range []types.PolicyType{types.PolicyTypeRateLimit, types.PolicyTypeCircuitBreaker} {
		t.Run(string(kind), func(t *testing.T) {
			st, db := setupPolicyRuntimeTestStore(t)
			policy := types.Policy{ID: "event-policy", Name: "event policy", Source: types.PolicySourceSystem,
				Type: kind, Status: types.PolicyStatusActive, Environment: types.PolicyEnvironmentAll,
				Targeting: types.PolicyTargeting{Handlers: []string{"event.handler"}}}
			previousStatus, expectedStatus := types.StageStatusCompleted, types.StageStatusThrottled
			if kind == types.PolicyTypeRateLimit {
				policy.Rule = types.PolicyRule{Limit: intPtr(1), WindowSeconds: intPtr(60), KeyBy: stringPtr("global")}
			} else {
				policy.Rule = types.PolicyRule{FailureThreshold: intPtr(1), WindowSeconds: intPtr(300), OpenSeconds: intPtr(120)}
				previousStatus, expectedStatus = types.StageStatusFailed, types.StageStatusFailed
			}
			st.SetStagePolicyRuntime(&fakeStagePolicyRuntime{policies: []types.Policy{policy}})
			priorPipeline := insertPolicyRuntimePipeline(t, db, "prior-event", types.PipelineStatusCompleted, 101)
			previous := insertPolicyRuntimeStage(t, db, priorPipeline, "prior", "event.handler", previousStatus, timePtr(time.Now().UTC().Add(-10*time.Second)))
			if _, err := db.Exec(`UPDATE stage SET is_event=true WHERE id=$1`, previous); err != nil {
				t.Fatal(err)
			}
			currentPipeline := insertPolicyRuntimePipeline(t, db, "new-event", types.PipelineStatusNotStarted, 101)
			candidate := insertPolicyRuntimeStage(t, db, currentPipeline, "next", "event.handler", types.StageStatusNotStarted, nil)
			if _, err := db.Exec(`UPDATE stage SET is_event=true WHERE id=$1`, candidate); err != nil {
				t.Fatal(err)
			}
			if next, err := st.GetStageToExecute(context.Background()); err != nil || next != nil {
				t.Fatalf("event bypassed policy: %+v, %v", next, err)
			}
			var status string
			if err := db.Get(&status, `SELECT status FROM stage WHERE id=$1`, candidate); err != nil || status != expectedStatus {
				t.Fatalf("event policy status=%q want=%q err=%v", status, expectedStatus, err)
			}
		})
	}
}

func TestStandaloneEventRecoveryRetryAndFencing(t *testing.T) {
	for _, recovery := range []string{"undispatched", "expired-lease"} {
		t.Run(recovery, func(t *testing.T) {
			st, db := setupSchedulingStore(t)
			ctx := context.Background()
			reliabilityCreateWorkerClientTable(t, db)
			reliabilityInsertWorker(t, db, "event-worker", 77, "event-token")
			pipelineID := insertPipeline(t, db, "recover-event")
			stageID := insertStage(t, db, pipelineID, "event", "event-handler")
			if _, err := db.Exec(`UPDATE pipeline SET application_id=77 WHERE id=$1`, pipelineID); err != nil {
				t.Fatal(err)
			}
			if _, err := db.Exec(`UPDATE stage SET is_event=true WHERE id=$1`, stageID); err != nil {
				t.Fatal(err)
			}
			reliabilityInsertStageOptions(t, db, stageID, 1, 1, types.ErrorCodeUpstreamError, "fixed", 1)
			first := reliabilitySchedule(t, st)
			if recovery == "expired-lease" {
				if marked, err := st.MarkStageDispatched(ctx, stageID, first.ExecutionID); err != nil || !marked {
					t.Fatalf("mark event dispatched: %t, %v", marked, err)
				}
				lease, err := st.AcquireStageLease(ctx, stageID, types.StageLeaseRequest{
					ExecutionID: first.ExecutionID, WorkerID: "event-worker",
				}, "event-token", time.Minute)
				if err != nil || !lease.Acquired {
					t.Fatalf("event lease: %+v, %v", lease, err)
				}
				if _, err := db.Exec(`UPDATE stage SET lease_expires_at=$1 WHERE id=$2`, time.Now().UTC().Add(-time.Minute), stageID); err != nil {
					t.Fatal(err)
				}
				if count, err := st.RecoverExpiredStageLeases(ctx); err != nil || count != 1 {
					t.Fatalf("recover event lease: %d, %v", count, err)
				}
			} else {
				if _, err := db.Exec(`UPDATE stage SET started_at=$1 WHERE id=$2`, time.Now().UTC().Add(-time.Minute), stageID); err != nil {
					t.Fatal(err)
				}
				if count, err := st.RecoverUndispatchedStages(ctx, time.Second); err != nil || count != 1 {
					t.Fatalf("recover event publication: %d, %v", count, err)
				}
			}
			second := reliabilitySchedule(t, st)
			if second.ExecutionID == first.ExecutionID || second.Attempt != 2 {
				t.Fatalf("recovered event reused execution fence: %+v", second)
			}
			if _, err := st.UpdateStageResult(ctx, reliabilitySuccessResult(first, "stale-event")); err != nil {
				t.Fatal(err)
			}
			if output := reliabilityStageOutput(t, db, stageID); output != "" {
				t.Fatalf("stale event wrote output %q", output)
			}
			retrying, err := st.UpdateStageResult(ctx, reliabilityFailureResult(second, types.ErrorCodeUpstreamError, nil))
			if err != nil || reliabilityOnlyStage(t, retrying).Status != types.StageStatusRetryScheduled {
				t.Fatalf("event retry: %+v, %v", retrying, err)
			}
			if early, err := st.GetStageToExecute(ctx); err != nil || early != nil {
				t.Fatalf("event ignored retry delay: %+v, %v", early, err)
			}
			reliabilityMakeRetryDue(t, db, stageID)
			third := reliabilitySchedule(t, st)
			if third.Attempt != 3 || third.ExecutionID == second.ExecutionID {
				t.Fatalf("retried event execution fence: %+v", third)
			}
			completed, err := st.UpdateStageResult(ctx, reliabilitySuccessResult(third, "confirmed-event"))
			if err != nil || completed.Status != types.PipelineStatusCompleted {
				t.Fatalf("event completion: %+v, %v", completed, err)
			}
			if stage := reliabilityOnlyStage(t, completed); stage.IsEvent == nil || !*stage.IsEvent || stage.RetryAttempt != 1 {
				t.Fatalf("completed event metadata: %+v", stage)
			}
		})
	}
}
