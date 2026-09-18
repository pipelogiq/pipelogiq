package store

import (
	"context"
	"testing"

	"pipelogiq/internal/types"
)

func TestGenericApprovalRejectionTerminatesBlockedPipeline(t *testing.T) {
	for _, scenario := range []string{"blocked", "blocked-event", "unreachable-failure-continuation", "latest-option-disables-continuation", "paused"} {
		t.Run(scenario, func(t *testing.T) {
			st, db := setupStageOpsTestStore(t)
			pipelineStatus := types.PipelineStatusRunning
			if scenario == "paused" {
				pipelineStatus = types.PipelineStatusPaused
			}
			pid := insertPipelineRow(t, db, scenario, pipelineStatus, false)
			gate := insertStageRow(t, db, pid, "gate", types.StageStatusWaitingApproval)
			blocked := insertStageRow(t, db, pid, "blocked", types.StageStatusNotStarted)
			if scenario == "blocked-event" {
				if _, err := db.Exec(`UPDATE stage SET is_event=true WHERE id=$1`, blocked); err != nil {
					t.Fatal(err)
				}
			}
			if scenario == "unreachable-failure-continuation" {
				last := insertStageRow(t, db, pid, "cleanup", types.StageStatusNotStarted)
				insertStageRunNextIfFailedOption(t, db, last, true)
			}
			if scenario == "latest-option-disables-continuation" {
				insertStageRunNextIfFailedOption(t, db, blocked, true)
				insertStageRunNextIfFailedOption(t, db, blocked, false)
			}
			ctx := context.Background()
			reason := "Owner rejected the gate"
			request := types.ResumeStageRequest{Approved: false, RejectionReason: &reason}
			if err := st.ResumeStageApproval(ctx, gate, request, "test"); err != nil {
				t.Fatal(err)
			}
			if err := st.ResumeStageApproval(ctx, gate, request, "test"); err != nil {
				t.Fatalf("idempotent rejected decision: %v", err)
			}
			var status string
			var completed, finished bool
			if err := db.QueryRow(`SELECT status,is_completed,finished_at IS NOT NULL FROM pipeline WHERE id=$1`, pid).Scan(&status, &completed, &finished); err != nil {
				t.Fatal(err)
			}
			if status != types.PipelineStatusFailed || !completed || !finished {
				t.Fatalf("rejection left pipeline status=%s completed=%t finished=%t", status, completed, finished)
			}
			if work, err := st.GetStageToExecute(ctx); err != nil || work != nil {
				t.Fatalf("blocked mutation was dispatched: %+v, %v", work, err)
			}
			if err := db.Get(&status, `SELECT status FROM stage WHERE id=$1`, blocked); err != nil || status != types.StageStatusNotStarted {
				t.Fatalf("blocked stage must remain unexecuted: %s, %v", status, err)
			}
		})
	}
}

func TestGenericApprovalRejectionAllowsExplicitFailureContinuation(t *testing.T) {
	st, db := setupStageOpsTestStore(t)
	pid := insertPipelineRow(t, db, "rejected-with-cleanup", types.PipelineStatusRunning, false)
	gate := insertStageRow(t, db, pid, "gate", types.StageStatusWaitingApproval)
	next := insertStageRow(t, db, pid, "cleanup", types.StageStatusNotStarted)
	insertStageRunNextIfFailedOption(t, db, next, true)
	ctx := context.Background()
	if err := st.ResumeStageApproval(ctx, gate, types.ResumeStageRequest{Approved: false}, "test"); err != nil {
		t.Fatal(err)
	}
	work := reliabilitySchedule(t, st)
	if work.StageID != next {
		t.Fatalf("failure continuation = %d, want %d", work.StageID, next)
	}
	result, err := st.UpdateStageResult(ctx, reliabilitySuccessResult(work, "cleanup completed"))
	if err != nil || !result.IsTerminal || result.Status != types.PipelineStatusFailed {
		t.Fatalf("cleanup must preserve rejected outcome: %+v, %v", result, err)
	}
}

func TestGenericApprovalRejectionPreservesIndependentBranchesThenTerminates(t *testing.T) {
	for _, alreadyDispatched := range []bool{false, true} {
		t.Run(map[bool]string{false: "not-yet-dispatched", true: "already-in-flight"}[alreadyDispatched], func(t *testing.T) {
			st, db := setupStageOpsTestStore(t)
			pid := insertPipelineRow(t, db, "rejected-parallel", types.PipelineStatusRunning, false)
			insertStageRow(t, db, pid, "root", types.StageStatusCompleted)
			gate := insertStageRow(t, db, pid, "gate", types.StageStatusWaitingApproval)
			blocked := insertStageRow(t, db, pid, "blocked", types.StageStatusNotStarted)
			branch := insertStageRow(t, db, pid, "parallel-event", types.StageStatusNotStarted)
			setDependsOn(t, db, branch, "root")
			if _, err := db.Exec(`UPDATE stage SET is_event=true WHERE id=$1`, branch); err != nil {
				t.Fatal(err)
			}
			var work *types.StageNextMessage
			if alreadyDispatched {
				work = reliabilitySchedule(t, st)
			}
			ctx := context.Background()
			if err := st.ResumeStageApproval(ctx, gate, types.ResumeStageRequest{Approved: false}, "test"); err != nil {
				t.Fatal(err)
			}
			var complete bool
			if err := db.Get(&complete, `SELECT is_completed FROM pipeline WHERE id=$1`, pid); err != nil || complete {
				t.Fatalf("independent branch was fenced out: completed=%t err=%v", complete, err)
			}
			if !alreadyDispatched {
				work = reliabilitySchedule(t, st)
			}
			if work.StageID != branch {
				t.Fatalf("independent branch = %d, want %d", work.StageID, branch)
			}
			result, err := st.UpdateStageResult(ctx, reliabilitySuccessResult(work, "parallel branch completed"))
			if err != nil || !result.IsTerminal || result.Status != types.PipelineStatusFailed {
				t.Fatalf("blocked descendants kept completed branch pending: %+v, %v", result, err)
			}
			var status string
			if err := db.Get(&status, `SELECT status FROM stage WHERE id=$1`, blocked); err != nil || status != types.StageStatusNotStarted {
				t.Fatalf("blocked branch changed: %s, %v", status, err)
			}
		})
	}
}

func TestApprovalOfLastIndependentBranchFinalizesEarlierRejection(t *testing.T) {
	st, db := setupStageOpsTestStore(t)
	pid := insertPipelineRow(t, db, "two-independent-gates", types.PipelineStatusRunning, false)
	gate := insertStageRow(t, db, pid, "gate", types.StageStatusWaitingApproval)
	insertStageRow(t, db, pid, "blocked", types.StageStatusNotStarted)
	parallel := insertStageRow(t, db, pid, "parallel-gate", types.StageStatusWaitingApproval)
	ctx := context.Background()
	if err := st.ResumeStageApproval(ctx, gate, types.ResumeStageRequest{Approved: false}, "test"); err != nil {
		t.Fatal(err)
	}
	var completed bool
	if err := db.Get(&completed, `SELECT is_completed FROM pipeline WHERE id=$1`, pid); err != nil || completed {
		t.Fatalf("independent waiting gate was fenced out: %t, %v", completed, err)
	}
	if err := st.ResumeStageApproval(ctx, parallel, types.ResumeStageRequest{Approved: true}, "test"); err != nil {
		t.Fatal(err)
	}
	var status string
	if err := db.QueryRow(`SELECT status,is_completed FROM pipeline WHERE id=$1`, pid).Scan(&status, &completed); err != nil {
		t.Fatal(err)
	}
	if status != types.PipelineStatusFailed || !completed {
		t.Fatalf("last independent approval left rejected pipeline pending: %s, %t", status, completed)
	}
}
