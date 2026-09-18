package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"pipelogiq/internal/types"
)

func TestScheduleSecurityAndPublicDefinitionRoundTrip(t *testing.T) {
	_, db, router := setupExternalEndpointTest(t)
	body := cronScheduleBody()
	definition := scheduleDefinition()
	definition["apiKey"] = "must-not-be-stored"
	definition["traceId"] = "deploy-trace"
	definition["idempotencyKey"] = "deploy-idempotency"
	definition["pipelineContextItems"] = []map[string]any{
		{"key": "traceparent", "value": `"deploy-trace"`},
		{"key": "token", "value": `"private-context-token"`, "valueType": "System.String", "isSensitive": true},
	}
	definition["stages"] = []map[string]any{{"stageName": "work", "stageHandlerName": "Handler", "input": `{"token":"private-context-token"}`}}
	body["definition"] = definition
	created := scheduleRequest(t, router, http.MethodPut, "/schedules/secure", body, "test-api-key")
	if created.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", created.Code, created.Body.String())
	}
	first := decodeSchedule(t, created)
	for _, secret := range []string{"must-not-be-stored", "deploy-trace", "deploy-idempotency", "private-context-token"} {
		if strings.Contains(created.Body.String(), secret) {
			t.Fatalf("response leaked %s", secret)
		}
	}
	// Reapply a GET result: secret placeholders are preserved under the same app/name row lock.
	body["definition"] = first.Definition
	repeated := scheduleRequest(t, router, http.MethodPut, "/schedules/secure", body, "test-api-key")
	if repeated.Code != http.StatusOK {
		t.Fatalf("reapply: %d %s", repeated.Code, repeated.Body.String())
	}
	second := decodeSchedule(t, repeated)
	if second.DefinitionVersion != first.DefinitionVersion || !second.NextRunAt.Equal(*first.NextRunAt) {
		t.Fatal("public round-trip changed schedule intent/cursor")
	}
	// A placeholder has no authority outside the existing schedule/application.
	rejected := scheduleRequest(t, router, http.MethodPut, "/schedules/new-secret", body, "test-api-key")
	if rejected.Code != http.StatusBadRequest {
		t.Fatalf("new placeholder: %d %s", rejected.Code, rejected.Body.String())
	}
	var public types.PipelineCreateRequest
	if err := json.Unmarshal(second.Definition, &public); err != nil {
		t.Fatal(err)
	}
	if len(public.PipelineContext) != 1 || public.PipelineContext[0].Value != types.RedactedContextValue {
		t.Fatalf("unexpected public context: %+v", public.PipelineContext)
	}
	triggered := scheduleRequest(t, router, http.MethodPost, "/schedules/secure/trigger", nil, "test-api-key")
	if triggered.Code != http.StatusCreated {
		t.Fatalf("trigger: %d %s", triggered.Code, triggered.Body.String())
	}
	var run struct {
		PipelineID int `json:"pipelineId"`
	}
	if err := json.Unmarshal(triggered.Body.Bytes(), &run); err != nil {
		t.Fatal(err)
	}
	var secret, input string
	if err := db.Get(&secret, `SELECT value FROM pipeline_context_item WHERE pipeline_id = $1 AND key = 'token'`, run.PipelineID); err != nil {
		t.Fatal(err)
	}
	if secret != `"private-context-token"` {
		t.Fatalf("execution lost secret: %q", secret)
	}
	if err := db.Get(&input, `SELECT io.input FROM stage_io io JOIN stage s ON s.id = io.stage_id WHERE s.pipeline_id = $1 ORDER BY s.id LIMIT 1`, run.PipelineID); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(input, "private-context-token") {
		t.Fatalf("execution lost input secret: %s", input)
	}
	var persisted string
	if err := db.Get(&persisted, `SELECT definition_json FROM schedule WHERE id = $1`, first.ID); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(persisted, "must-not-be-stored") || strings.Contains(persisted, "deploy-trace") {
		t.Fatal("credentials or traces persisted")
	}

}

func TestScheduleOmittedPoliciesPreserveStoredPoliciesAndVersion(t *testing.T) {
	_, _, router := setupExternalEndpointTest(t)
	body := cronScheduleBody()
	body["overlapPolicy"] = "Allow"
	body["catchupPolicy"] = "All"
	initial := scheduleRequest(t, router, http.MethodPut, "/schedules/policies", body, "test-api-key")
	if initial.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", initial.Code, initial.Body.String())
	}
	first := decodeSchedule(t, initial)
	delete(body, "overlapPolicy")
	delete(body, "catchupPolicy")
	updated := scheduleRequest(t, router, http.MethodPut, "/schedules/policies", body, "test-api-key")
	if updated.Code != http.StatusOK {
		t.Fatalf("update: %d %s", updated.Code, updated.Body.String())
	}
	second := decodeSchedule(t, updated)
	if second.OverlapPolicy != "Allow" || second.CatchupPolicy != "All" || second.DefinitionVersion != first.DefinitionVersion || !second.NextRunAt.Equal(*first.NextRunAt) {
		t.Fatalf("omitted policies changed schedule: %+v", second)
	}
}
