package api

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"modernc.org/sqlite"

	"pipelogiq/internal/store"
	"pipelogiq/internal/types"
)

var auditSQLiteNowOnce sync.Once

func setupOperationAuditTest(t *testing.T) *scopeFixture {
	t.Helper()
	// The production skip/key-disable queries use PostgreSQL's NOW(). Give
	// the SQLite integration fixture the equivalent clock function.
	auditSQLiteNowOnce.Do(func() {
		sqlite.MustRegisterScalarFunction("now", 0, func(_ *sqlite.FunctionContext, _ []driver.Value) (driver.Value, error) {
			return time.Now().UTC().Format("2006-01-02 15:04:05"), nil
		})
	})
	fx := setupScopeTest(t)
	if _, err := fx.db.Exec(`UPDATE "user" SET role = 'operator' WHERE id = ?`, scopedUserID); err != nil {
		t.Fatal(err)
	}
	return fx
}

func operationAuditRows(t *testing.T, fx *scopeFixture) []store.AuditRecord {
	t.Helper()
	var rows []store.AuditRecord
	if err := fx.db.Select(&rows, `SELECT id, ts, user_id, email, action, target, outcome, remote_addr, details FROM audit_log ORDER BY id`); err != nil {
		t.Fatal(err)
	}
	return rows
}

func assertOperationAudit(t *testing.T, fx *scopeFixture, action, outcome, target string) map[string]any {
	t.Helper()
	rows := operationAuditRows(t, fx)
	if len(rows) != 1 {
		t.Fatalf("expected exactly one audit entry, got %+v", rows)
	}
	row := rows[0]
	if row.Action != action || row.Outcome != outcome {
		t.Fatalf("audit action/outcome = %q/%q, want %q/%q", row.Action, row.Outcome, action, outcome)
	}
	if row.UserID == nil || *row.UserID != scopedUserID || row.Email == nil || *row.Email != "user10@example.com" {
		t.Fatalf("audit lost the acting user: %+v", row)
	}
	if target != "" && (row.Target == nil || *row.Target != target) {
		t.Fatalf("audit target = %v, want %q", row.Target, target)
	}
	var details map[string]any
	if row.Details == nil || json.Unmarshal([]byte(*row.Details), &details) != nil {
		t.Fatalf("invalid audit details: %v", row.Details)
	}
	return details
}

func TestOperationAudit_StageAndPipelineMutations(t *testing.T) {
	for _, action := range []string{"stage.rerun", "stage.skip", "pipeline.pause", "pipeline.resume"} {
		t.Run(action, func(t *testing.T) {
			fx := setupOperationAuditTest(t)
			stageID := insertScopedStage(t, fx.db, fx.ownPipelineID)
			path := fmt.Sprintf("/pipelines/%d/pause", fx.ownPipelineID)
			target := fmt.Sprintf("pipeline:%d", fx.ownPipelineID)
			var body any
			switch action {
			case "stage.rerun":
				path, target = "/pipelines/rerunStage", fmt.Sprintf("stage:%d", stageID)
				body = map[string]any{"stageId": stageID, "rerunAllNextStages": true, "input": "DO-NOT-AUDIT-INPUT"}
			case "stage.skip":
				path, target = "/pipelines/skipStage", fmt.Sprintf("stage:%d", stageID)
				body = map[string]any{"stageId": stageID}
			case "pipeline.resume":
				if _, err := fx.db.Exec(`UPDATE pipeline SET status = ? WHERE id = ?`, types.PipelineStatusPaused, fx.ownPipelineID); err != nil {
					t.Fatal(err)
				}
				path = fmt.Sprintf("/pipelines/%d/resume", fx.ownPipelineID)
			}
			rec := fx.do(t, scopedUserID, http.MethodPost, path, body)
			if rec.Code != http.StatusOK {
				t.Fatalf("mutation = %d: %s", rec.Code, rec.Body.String())
			}
			details := assertOperationAudit(t, fx, action, "ok", target)
			if strings.HasPrefix(action, "stage.") && details["stageId"] != float64(stageID) {
				t.Fatalf("missing stage id: %+v", details)
			}
			if action == "stage.rerun" && details["rerunAllNextStages"] != true {
				t.Fatalf("missing rerun option: %+v", details)
			}
			if strings.HasPrefix(action, "pipeline.") && details["pipelineId"] != float64(fx.ownPipelineID) {
				t.Fatalf("missing pipeline id: %+v", details)
			}
			if strings.Contains(*operationAuditRows(t, fx)[0].Details, "DO-NOT-AUDIT") {
				t.Fatal("audit contains request input")
			}
		})
	}
}

func TestOperationAudit_BulkCountsTargetsAndOutcomes(t *testing.T) {
	for _, tc := range []struct {
		action, outcome string
		own, foreign    bool
	}{
		{"pause", "partial", true, true},
		{"resume", "ok", true, false},
		{"rerun", "partial", true, true},
		{"skip", "failed", false, true},
	} {
		t.Run(tc.action+"_"+tc.outcome, func(t *testing.T) {
			fx := setupOperationAuditTest(t)
			stageID := insertScopedStage(t, fx.db, fx.ownPipelineID)
			if tc.action == "resume" {
				if _, err := fx.db.Exec(`UPDATE pipeline SET status = ? WHERE id = ?`, types.PipelineStatusPaused, fx.ownPipelineID); err != nil {
					t.Fatal(err)
				}
			}
			scope, ownID, foreignID := "pipeline", fx.ownPipelineID, fx.otherPipelineID
			if tc.action == "rerun" || tc.action == "skip" {
				scope, ownID, foreignID = "stage", stageID, fx.otherStageID
			}
			ids := []int{}
			if tc.own {
				ids = append(ids, ownID)
			}
			if tc.foreign {
				ids = append(ids, foreignID)
			}
			body := map[string]any{"action": tc.action, scope + "Ids": ids, "rerunAllNextStages": true}
			rec := fx.do(t, scopedUserID, http.MethodPost, "/pipelines/bulkAction", body)
			if rec.Code != http.StatusOK {
				t.Fatalf("bulk mutation = %d: %s", rec.Code, rec.Body.String())
			}
			details := assertOperationAudit(t, fx, scope+".bulk_"+tc.action, tc.outcome, scope+"s")
			wantIDs := make([]any, 0, len(ids))
			for _, id := range ids {
				wantIDs = append(wantIDs, float64(id))
			}
			if !reflect.DeepEqual(details[scope+"Ids"], wantIDs) || details["requested"] != float64(len(ids)) {
				t.Fatalf("incorrect requested targets: %+v", details)
			}
			succeeded, failed := 0, 0
			if tc.own {
				succeeded = 1
			}
			if tc.foreign {
				failed = 1
			}
			if details["succeeded"] != float64(succeeded) || details["failed"] != float64(failed) {
				t.Fatalf("incorrect bulk counts: %+v", details)
			}
			results, ok := details["results"].([]any)
			if !ok || len(results) != len(ids) {
				t.Fatalf("missing individual outcomes: %+v", details)
			}
			for _, item := range results {
				result := item.(map[string]any)
				if result["scope"] != scope || result["success"] != (result["id"] == float64(ownID)) || len(result) != 3 {
					t.Fatalf("unexpected per-item audit fields: %+v", result)
				}
			}
		})
	}
}

func TestOperationAudit_FailedAndDeniedRequests(t *testing.T) {
	for _, tc := range []struct {
		name, role, action, outcome string
		status                      int
		foreign                     bool
	}{
		{"viewer", "viewer", "stage.skip", "denied", http.StatusForbidden, false},
		{"outside application", "operator", "stage.skip", "failed", http.StatusNotFound, true},
		{"invalid body", "operator", "stage.rerun", "failed", http.StatusBadRequest, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fx := setupOperationAuditTest(t)
			if _, err := fx.db.Exec(`UPDATE "user" SET role = ? WHERE id = ?`, tc.role, scopedUserID); err != nil {
				t.Fatal(err)
			}
			stageID := insertScopedStage(t, fx.db, fx.ownPipelineID)
			if tc.foreign {
				stageID = fx.otherStageID
			}
			path := "/pipelines/skipStage"
			var body any = map[string]any{"stageId": stageID}
			if tc.name == "invalid body" {
				path, body = "/pipelines/rerunStage", "invalid"
			}
			rec := fx.do(t, scopedUserID, http.MethodPost, path, body)
			if rec.Code != tc.status {
				t.Fatalf("mutation = %d: %s", rec.Code, rec.Body.String())
			}
			details := assertOperationAudit(t, fx, tc.action, tc.outcome, "")
			if details["status"] != float64(tc.status) {
				t.Fatalf("missing HTTP status: %+v", details)
			}
			var status string
			if err := fx.db.Get(&status, `SELECT status FROM stage WHERE id = ?`, stageID); err != nil || status != types.StageStatusFailed {
				t.Fatalf("rejected request changed stage: %q %v", status, err)
			}
		})
	}
}

func TestOperationAudit_ScheduleMutations(t *testing.T) {
	for _, action := range []string{"pause", "resume", "archive", "trigger", "update_policies"} {
		t.Run(action, func(t *testing.T) {
			fx := setupOperationAuditTest(t)
			definition, err := json.Marshal(scheduleDefinition())
			if err != nil {
				t.Fatal(err)
			}
			res, err := fx.db.Exec(`INSERT INTO schedule
				(application_id, name, kind, cron_expression, definition_json, created_at, updated_at)
				VALUES (?, 'audit-schedule', 'Cron', '* * * * *', ?, ?, ?)`, ownApplicationID, string(definition), time.Now().UTC(), time.Now().UTC())
			if err != nil {
				t.Fatal(err)
			}
			id, err := res.LastInsertId()
			if err != nil {
				t.Fatal(err)
			}
			method, suffix, status := http.MethodPost, action, http.StatusOK
			var body any
			if action == "trigger" {
				status = http.StatusCreated
				body = map[string]any{"input": map[string]any{"password": "DO-NOT-AUDIT-PASSWORD"}}
			}
			if action == "update_policies" {
				method, suffix = http.MethodPatch, "policies"
				body = map[string]any{"overlapPolicy": "Queue", "catchupPolicy": "All"}
			}
			rec := fx.do(t, scopedUserID, method, fmt.Sprintf("/schedules/%d/%s", id, suffix), body)
			if rec.Code != status {
				t.Fatalf("schedule mutation = %d: %s", rec.Code, rec.Body.String())
			}
			details := assertOperationAudit(t, fx, "schedule."+action, "ok", fmt.Sprintf("schedule:%d", id))
			if details["scheduleId"] != float64(id) || details["applicationId"] != float64(ownApplicationID) {
				t.Fatalf("missing schedule identity: %+v", details)
			}
			if action == "trigger" {
				var out struct {
					RunID      int64 `json:"runId"`
					PipelineID int   `json:"pipelineId"`
				}
				if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
					t.Fatal(err)
				}
				if details["scheduleRunId"] != float64(out.RunID) || details["pipelineId"] != float64(out.PipelineID) || out.PipelineID == 0 {
					t.Fatalf("missing triggered operation identity: %+v", details)
				}
			}
			if action == "update_policies" && (details["overlapPolicy"] != "Queue" || details["catchupPolicy"] != "All" || details["previousOverlapPolicy"] != "Skip") {
				t.Fatalf("missing policy changes: %+v", details)
			}
			if strings.Contains(*operationAuditRows(t, fx)[0].Details, "DO-NOT-AUDIT") {
				t.Fatal("audit contains schedule input")
			}
		})
	}
}

func TestOperationAudit_APIKeysRecordIDsWithoutCredentials(t *testing.T) {
	fx := setupOperationAuditTest(t)
	if _, err := fx.db.Exec(`UPDATE "user" SET role = 'admin' WHERE id = ?`, scopedUserID); err != nil {
		t.Fatal(err)
	}
	rec := fx.do(t, scopedUserID, http.MethodPost, "/apiKeys", map[string]any{
		"applicationId": ownApplicationID, "name": "DO-NOT-AUDIT-KEY-NAME",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("create key = %d: %s", rec.Code, rec.Body.String())
	}
	var key types.ApiKeyResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &key); err != nil {
		t.Fatal(err)
	}
	if key.Key == nil || *key.Key == "" {
		t.Fatal("create response did not contain key")
	}
	details := assertOperationAudit(t, fx, "api_key.create", "ok", fmt.Sprintf("api_key:%d", key.ID))
	if details["apiKeyId"] != float64(key.ID) || details["applicationId"] != float64(ownApplicationID) {
		t.Fatalf("missing key identity: %+v", details)
	}
	rec = fx.do(t, scopedUserID, http.MethodPut, "/apiKeys/disable", map[string]any{"apiKeyId": key.ID})
	if rec.Code != http.StatusOK {
		t.Fatalf("disable key = %d: %s", rec.Code, rec.Body.String())
	}
	rows := operationAuditRows(t, fx)
	if len(rows) != 2 || rows[1].Action != "api_key.disable" || rows[1].Outcome != "ok" {
		t.Fatalf("unexpected key audit entries: %+v", rows)
	}
	for _, row := range rows {
		if row.Details == nil || strings.Contains(*row.Details, *key.Key) || strings.Contains(*row.Details, "DO-NOT-AUDIT") {
			t.Fatalf("audit contains secret or lacks details: %+v", row)
		}
	}
}
