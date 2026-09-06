package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"pipelogiq/internal/store"
	"pipelogiq/internal/types"
)

func scheduleDefinition() map[string]any {
	return map[string]any{
		"name": "dispatch-due-emails",
		"stages": []map[string]any{
			{
				"stageName":        "dispatch",
				"stageHandlerName": "DispatchDueEmailsHandler",
				"input":            `{"batchSize":200}`,
			},
		},
	}
}

func cronScheduleBody() map[string]any {
	return map[string]any{
		"kind":           "Cron",
		"cronExpression": "*/1 * * * *",
		"timeZone":       "Europe/Madrid",
		"overlapPolicy":  "Skip",
		"catchupPolicy":  "None",
		"catchupMax":     10,
		"jitterSeconds":  15,
		"definition":     scheduleDefinition(),
	}
}

func scheduleRequest(
	t *testing.T,
	router http.Handler,
	method, path string,
	body any,
	apiKey string,
) *httptest.ResponseRecorder {
	t.Helper()

	var req *http.Request
	if body == nil {
		req = httptest.NewRequest(method, path, nil)
	} else {
		req = makeJSONRequest(t, method, path, body)
	}
	if apiKey != "" {
		req.Header.Set("X-API-Key", apiKey)
	}
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)
	return recorder
}

func decodeSchedule(t *testing.T, recorder *httptest.ResponseRecorder) types.ScheduleResponse {
	t.Helper()
	var out types.ScheduleResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode schedule: %v; body=%s", err, recorder.Body.String())
	}
	return out
}

func decodeProblem(t *testing.T, recorder *httptest.ResponseRecorder) problemDetails {
	t.Helper()
	if got := recorder.Header().Get("Content-Type"); got != "application/problem+json" {
		t.Fatalf("content type = %q, want application/problem+json", got)
	}
	var problem problemDetails
	if err := json.Unmarshal(recorder.Body.Bytes(), &problem); err != nil {
		t.Fatalf("decode problem: %v; body=%s", err, recorder.Body.String())
	}
	return problem
}

func TestUpsertScheduleEndpoint(t *testing.T) {
	t.Run("creates with 201 and updates with 200", func(t *testing.T) {
		_, _, router := setupExternalEndpointTest(t)

		created := scheduleRequest(t, router, http.MethodPut, "/schedules/emails", cronScheduleBody(), "test-api-key")
		if created.Code != http.StatusCreated {
			t.Fatalf("status = %d, want 201; body=%s", created.Code, created.Body.String())
		}
		first := decodeSchedule(t, created)
		if first.DefinitionVersion != 1 {
			t.Fatalf("definitionVersion = %d, want 1", first.DefinitionVersion)
		}
		if first.Status != types.ScheduleStatusActive {
			t.Fatalf("status = %q, want Active", first.Status)
		}
		if first.NextRunAt == nil || !first.NextRunAt.After(time.Now().UTC().Add(-time.Minute)) {
			t.Fatalf("nextRunAt must be planned, got %v", first.NextRunAt)
		}

		updated := scheduleRequest(t, router, http.MethodPut, "/schedules/emails", cronScheduleBody(), "test-api-key")
		if updated.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body=%s", updated.Code, updated.Body.String())
		}
		second := decodeSchedule(t, updated)
		if second.DefinitionVersion != first.DefinitionVersion {
			t.Fatalf("an unchanged body must not bump the version: %d then %d",
				first.DefinitionVersion, second.DefinitionVersion)
		}
	})

	t.Run("a changed body bumps the version", func(t *testing.T) {
		_, _, router := setupExternalEndpointTest(t)

		created := scheduleRequest(t, router, http.MethodPut, "/schedules/emails", cronScheduleBody(), "test-api-key")
		first := decodeSchedule(t, created)

		changed := cronScheduleBody()
		changed["cronExpression"] = "*/5 * * * *"
		updated := scheduleRequest(t, router, http.MethodPut, "/schedules/emails", changed, "test-api-key")
		if updated.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", updated.Code)
		}
		if second := decodeSchedule(t, updated); second.DefinitionVersion != first.DefinitionVersion+1 {
			t.Fatalf("definitionVersion = %d, want %d", second.DefinitionVersion, first.DefinitionVersion+1)
		}
	})

	t.Run("accepts interval and once schedules", func(t *testing.T) {
		_, _, router := setupExternalEndpointTest(t)

		interval := map[string]any{
			"kind":            "Interval",
			"intervalSeconds": 60,
			"definition":      scheduleDefinition(),
		}
		if res := scheduleRequest(t, router, http.MethodPut, "/schedules/interval", interval, "test-api-key"); res.Code != http.StatusCreated {
			t.Fatalf("interval status = %d, want 201; body=%s", res.Code, res.Body.String())
		}

		once := map[string]any{
			"kind":       "Once",
			"runAt":      time.Now().UTC().Add(time.Hour).Format(time.RFC3339),
			"definition": scheduleDefinition(),
		}
		if res := scheduleRequest(t, router, http.MethodPut, "/schedules/once", once, "test-api-key"); res.Code != http.StatusCreated {
			t.Fatalf("once status = %d, want 201; body=%s", res.Code, res.Body.String())
		}
	})

	t.Run("validation failures name the field", func(t *testing.T) {
		cases := []struct {
			name  string
			field string
			patch func(body map[string]any)
		}{
			{
				name:  "invalid cron",
				field: "cronExpression",
				patch: func(b map[string]any) { b["cronExpression"] = "not a cron" },
			},
			{
				name:  "six field cron",
				field: "cronExpression",
				patch: func(b map[string]any) { b["cronExpression"] = "0 */5 * * * *" },
			},
			{
				name:  "unknown zone",
				field: "timeZone",
				patch: func(b map[string]any) { b["timeZone"] = "Mars/Olympus" },
			},
			{
				name:  "unknown kind",
				field: "kind",
				patch: func(b map[string]any) { b["kind"] = "Whenever" },
			},
			{
				name:  "once without runAt",
				field: "runAt",
				patch: func(b map[string]any) {
					b["kind"] = "Once"
					delete(b, "cronExpression")
				},
			},
			{
				name:  "interval below the minimum",
				field: "intervalSeconds",
				patch: func(b map[string]any) {
					b["kind"] = "Interval"
					b["intervalSeconds"] = 5
					delete(b, "cronExpression")
				},
			},
			{
				name:  "unknown overlap policy",
				field: "overlapPolicy",
				patch: func(b map[string]any) { b["overlapPolicy"] = "Whenever" },
			},
			{
				name:  "unknown catchup policy",
				field: "catchupPolicy",
				patch: func(b map[string]any) { b["catchupPolicy"] = "Sometimes" },
			},
			{
				name:  "negative jitter",
				field: "jitterSeconds",
				patch: func(b map[string]any) { b["jitterSeconds"] = -1 },
			},
			{
				name:  "definition without stages",
				field: "definition.stages",
				patch: func(b map[string]any) {
					b["definition"] = map[string]any{"name": "empty", "stages": []map[string]any{}}
				},
			},
			{
				name:  "stage without a handler",
				field: "definition.stages[0].stageHandlerName",
				patch: func(b map[string]any) {
					b["definition"] = map[string]any{
						"name":   "no-handler",
						"stages": []map[string]any{{"stageName": "x"}},
					}
				},
			},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				_, _, router := setupExternalEndpointTest(t)
				body := cronScheduleBody()
				tc.patch(body)

				res := scheduleRequest(t, router, http.MethodPut, "/schedules/emails", body, "test-api-key")
				if res.Code != http.StatusBadRequest {
					t.Fatalf("status = %d, want 400; body=%s", res.Code, res.Body.String())
				}
				problem := decodeProblem(t, res)
				if _, ok := problem.Errors[tc.field]; !ok {
					t.Fatalf("problem must name %q, got %+v", tc.field, problem.Errors)
				}
			})
		}
	})

	t.Run("a name that is too long is rejected", func(t *testing.T) {
		_, _, router := setupExternalEndpointTest(t)
		long := strings.Repeat("n", maxScheduleNameRunes+1)

		res := scheduleRequest(t, router, http.MethodPut, "/schedules/"+long, cronScheduleBody(), "test-api-key")
		if res.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400", res.Code)
		}
		if _, ok := decodeProblem(t, res).Errors["name"]; !ok {
			t.Fatalf("problem must name the name field")
		}
	})

	t.Run("without an api key it is unauthorized", func(t *testing.T) {
		_, _, router := setupExternalEndpointTest(t)

		res := scheduleRequest(t, router, http.MethodPut, "/schedules/emails", cronScheduleBody(), "")
		if res.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", res.Code)
		}
	})
}

func TestScheduleLifecycleEndpoints(t *testing.T) {
	t.Run("get, list, pause, resume, archive", func(t *testing.T) {
		_, _, router := setupExternalEndpointTest(t)
		scheduleRequest(t, router, http.MethodPut, "/schedules/emails", cronScheduleBody(), "test-api-key")

		got := scheduleRequest(t, router, http.MethodGet, "/schedules/emails", nil, "test-api-key")
		if got.Code != http.StatusOK {
			t.Fatalf("get status = %d, want 200; body=%s", got.Code, got.Body.String())
		}
		if decodeSchedule(t, got).Name != "emails" {
			t.Fatalf("wrong schedule returned")
		}

		list := scheduleRequest(t, router, http.MethodGet, "/schedules", nil, "test-api-key")
		if list.Code != http.StatusOK {
			t.Fatalf("list status = %d, want 200", list.Code)
		}
		var listed types.ScheduleListResponse
		if err := json.Unmarshal(list.Body.Bytes(), &listed); err != nil {
			t.Fatalf("decode list: %v", err)
		}
		if len(listed.Schedules) != 1 {
			t.Fatalf("len(schedules) = %d, want 1", len(listed.Schedules))
		}
		if listed.Schedules[0].Health == nil {
			t.Fatalf("the list must carry a health summary")
		}

		paused := scheduleRequest(t, router, http.MethodPost, "/schedules/emails/pause", nil, "test-api-key")
		if paused.Code != http.StatusOK || decodeSchedule(t, paused).Status != types.ScheduleStatusPaused {
			t.Fatalf("pause status = %d body=%s", paused.Code, paused.Body.String())
		}

		resumed := scheduleRequest(t, router, http.MethodPost, "/schedules/emails/resume", nil, "test-api-key")
		if resumed.Code != http.StatusOK {
			t.Fatalf("resume status = %d; body=%s", resumed.Code, resumed.Body.String())
		}
		revived := decodeSchedule(t, resumed)
		if revived.Status != types.ScheduleStatusActive {
			t.Fatalf("status = %q, want Active", revived.Status)
		}
		// Resuming re-plans from now rather than replaying the pause window.
		if revived.NextRunAt == nil || revived.NextRunAt.Before(time.Now().UTC().Add(-time.Second)) {
			t.Fatalf("nextRunAt = %v must be in the future after a resume", revived.NextRunAt)
		}

		archived := scheduleRequest(t, router, http.MethodPost, "/schedules/emails/archive", nil, "test-api-key")
		if archived.Code != http.StatusOK || decodeSchedule(t, archived).Status != types.ScheduleStatusArchived {
			t.Fatalf("archive status = %d body=%s", archived.Code, archived.Body.String())
		}
	})

	t.Run("an unknown schedule is not found", func(t *testing.T) {
		_, _, router := setupExternalEndpointTest(t)

		for _, path := range []string{
			"/schedules/missing",
			"/schedules/missing/runs",
		} {
			res := scheduleRequest(t, router, http.MethodGet, path, nil, "test-api-key")
			if res.Code != http.StatusNotFound {
				t.Fatalf("GET %s status = %d, want 404", path, res.Code)
			}
			decodeProblem(t, res)
		}

		for _, path := range []string{
			"/schedules/missing/pause",
			"/schedules/missing/resume",
			"/schedules/missing/archive",
			"/schedules/missing/trigger",
		} {
			res := scheduleRequest(t, router, http.MethodPost, path, nil, "test-api-key")
			if res.Code != http.StatusNotFound {
				t.Fatalf("POST %s status = %d, want 404; body=%s", path, res.Code, res.Body.String())
			}
		}
	})

	t.Run("another application's schedule is invisible", func(t *testing.T) {
		_, _, router := setupExternalEndpointTest(t)
		scheduleRequest(t, router, http.MethodPut, "/schedules/emails", cronScheduleBody(), "test-api-key")

		// The other application's key must not resolve the name at all: schedules
		// are addressed by name, so a leak would be a cross-tenant read.
		res := scheduleRequest(t, router, http.MethodGet, "/schedules/emails", nil, "other-api-key")
		if res.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want 404; body=%s", res.Code, res.Body.String())
		}

		list := scheduleRequest(t, router, http.MethodGet, "/schedules", nil, "other-api-key")
		var listed types.ScheduleListResponse
		if err := json.Unmarshal(list.Body.Bytes(), &listed); err != nil {
			t.Fatalf("decode list: %v", err)
		}
		if len(listed.Schedules) != 0 {
			t.Fatalf("the other application must see no schedules, got %d", len(listed.Schedules))
		}
	})

	t.Run("an archived schedule cannot be triggered", func(t *testing.T) {
		_, _, router := setupExternalEndpointTest(t)
		scheduleRequest(t, router, http.MethodPut, "/schedules/emails", cronScheduleBody(), "test-api-key")
		scheduleRequest(t, router, http.MethodPost, "/schedules/emails/archive", nil, "test-api-key")

		res := scheduleRequest(t, router, http.MethodPost, "/schedules/emails/trigger", nil, "test-api-key")
		if res.Code != http.StatusConflict {
			t.Fatalf("status = %d, want 409; body=%s", res.Code, res.Body.String())
		}
		decodeProblem(t, res)
	})

	t.Run("an invalid list limit is rejected", func(t *testing.T) {
		_, _, router := setupExternalEndpointTest(t)

		for _, query := range []string{"limit=0", "limit=201", "limit=abc"} {
			res := scheduleRequest(t, router, http.MethodGet, "/schedules?"+query, nil, "test-api-key")
			if res.Code != http.StatusBadRequest {
				t.Fatalf("%s status = %d, want 400", query, res.Code)
			}
		}

		res := scheduleRequest(t, router, http.MethodGet, "/schedules?status=Whenever", nil, "test-api-key")
		if res.Code != http.StatusBadRequest {
			t.Fatalf("status filter = %d, want 400", res.Code)
		}
	})
}

func TestTriggerScheduleEndpoint(t *testing.T) {
	t.Run("creates a manual run and its pipeline", func(t *testing.T) {
		_, db, router := setupExternalEndpointTest(t)
		scheduleRequest(t, router, http.MethodPut, "/schedules/emails", cronScheduleBody(), "test-api-key")

		res := scheduleRequest(t, router, http.MethodPost, "/schedules/emails/trigger", nil, "test-api-key")
		if res.Code != http.StatusCreated {
			t.Fatalf("status = %d, want 201; body=%s", res.Code, res.Body.String())
		}

		var out struct {
			RunID      int64  `json:"runId"`
			PipelineID int    `json:"pipelineId"`
			Trigger    string `json:"trigger"`
		}
		if err := json.Unmarshal(res.Body.Bytes(), &out); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if out.RunID == 0 || out.PipelineID == 0 {
			t.Fatalf("unexpected response %+v", out)
		}
		if out.Trigger != types.ScheduleTriggerManual {
			t.Fatalf("trigger = %q, want Manual", out.Trigger)
		}

		var status string
		if err := db.Get(&status, `SELECT status FROM pipeline WHERE id = $1`, out.PipelineID); err != nil {
			t.Fatalf("load pipeline: %v", err)
		}
		if status != types.PipelineStatusNotStarted {
			t.Fatalf("pipeline status = %q, want NotStarted", status)
		}

		var linkedRuns int
		if err := db.Get(&linkedRuns, `
			SELECT COUNT(*) FROM schedule_run WHERE id = $1 AND pipeline_id = $2 AND trigger = $3
		`, out.RunID, out.PipelineID, types.ScheduleTriggerManual); err != nil {
			t.Fatalf("count runs: %v", err)
		}
		if linkedRuns != 1 {
			t.Fatalf("the manual run must link its pipeline")
		}
	})

	t.Run("an input override replaces the first stage input", func(t *testing.T) {
		_, db, router := setupExternalEndpointTest(t)
		scheduleRequest(t, router, http.MethodPut, "/schedules/emails", cronScheduleBody(), "test-api-key")

		res := scheduleRequest(t, router, http.MethodPost, "/schedules/emails/trigger",
			map[string]any{"input": map[string]any{"batchSize": 1}}, "test-api-key")
		if res.Code != http.StatusCreated {
			t.Fatalf("status = %d, want 201; body=%s", res.Code, res.Body.String())
		}

		var out struct {
			PipelineID int `json:"pipelineId"`
		}
		if err := json.Unmarshal(res.Body.Bytes(), &out); err != nil {
			t.Fatalf("decode response: %v", err)
		}

		var input string
		if err := db.Get(&input, `
			SELECT COALESCE(io.input, '')
			FROM stage_io io JOIN stage s ON s.id = io.stage_id
			WHERE s.pipeline_id = $1
		`, out.PipelineID); err != nil {
			t.Fatalf("load input: %v", err)
		}
		if !strings.Contains(input, `"batchSize":1`) {
			t.Fatalf("input = %q, want the override", input)
		}
	})

	t.Run("runs are listed newest first with their lag", func(t *testing.T) {
		_, _, router := setupExternalEndpointTest(t)
		scheduleRequest(t, router, http.MethodPut, "/schedules/emails", cronScheduleBody(), "test-api-key")

		for i := 0; i < 2; i++ {
			if res := scheduleRequest(t, router, http.MethodPost, "/schedules/emails/trigger", nil, "test-api-key"); res.Code != http.StatusCreated {
				t.Fatalf("trigger %d status = %d; body=%s", i, res.Code, res.Body.String())
			}
			time.Sleep(time.Second) // Manual runs are stamped to the second.
		}

		res := scheduleRequest(t, router, http.MethodGet, "/schedules/emails/runs", nil, "test-api-key")
		if res.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body=%s", res.Code, res.Body.String())
		}
		var runs types.ScheduleRunListResponse
		if err := json.Unmarshal(res.Body.Bytes(), &runs); err != nil {
			t.Fatalf("decode runs: %v", err)
		}
		if len(runs.Runs) != 2 {
			t.Fatalf("len(runs) = %d, want 2", len(runs.Runs))
		}
		if runs.Runs[0].ID < runs.Runs[1].ID {
			t.Fatalf("runs must be newest first")
		}
		for _, run := range runs.Runs {
			if run.PipelineID == nil {
				t.Fatalf("run %d has no pipeline", run.ID)
			}
			if run.Trigger != types.ScheduleTriggerManual {
				t.Fatalf("trigger = %q, want Manual", run.Trigger)
			}
		}
	})
}

func TestScheduleNameIsCaseInsensitive(t *testing.T) {
	_, _, router := setupExternalEndpointTest(t)

	if res := scheduleRequest(t, router, http.MethodPut, "/schedules/Emails", cronScheduleBody(), "test-api-key"); res.Code != http.StatusCreated {
		t.Fatalf("create status = %d", res.Code)
	}
	res := scheduleRequest(t, router, http.MethodPut, "/schedules/emails", cronScheduleBody(), "test-api-key")
	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (the same schedule); body=%s", res.Code, res.Body.String())
	}

	list := scheduleRequest(t, router, http.MethodGet, "/schedules", nil, "test-api-key")
	var listed types.ScheduleListResponse
	if err := json.Unmarshal(list.Body.Bytes(), &listed); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(listed.Schedules) != 1 {
		t.Fatalf("len(schedules) = %d, want 1 (%s)", len(listed.Schedules), fmt.Sprint(listed.Schedules))
	}
}

// TestManualTriggerDoesNotCollideWithAScheduledTick is a regression test for a
// bug the schedules smoke found on real PostgreSQL: keying a manual run on the
// clock collides with a scheduled tick that fired in the same second, and the
// two definitions are not identical because {{runId}} differs, so the collision
// surfaced as an idempotency conflict instead of resolving.
func TestManualTriggerDoesNotCollideWithAScheduledTick(t *testing.T) {
	_, db, router := setupExternalEndpointTest(t)
	scheduleRequest(t, router, http.MethodPut, "/schedules/emails", cronScheduleBody(), "test-api-key")

	var scheduleID int64
	if err := db.Get(&scheduleID, `SELECT id FROM schedule WHERE name = 'emails'`); err != nil {
		t.Fatalf("load schedule: %v", err)
	}

	// Stand in for a scheduled tick that already fired this second, taking the
	// idempotency key a clock-derived manual key would have wanted.
	tick := time.Now().UTC()
	var pipelineID int
	if err := db.QueryRow(`
		INSERT INTO pipeline (name, status, created_at, is_completed, application_id, idempotency_key, request_hash, schedule_id)
		VALUES ($1, $2, $3, false, $4, $5, $6, $7)
		RETURNING id
	`, "dispatch-due-emails", types.PipelineStatusRunning, tick, externalTestApplicationID,
		store.ScheduleIdempotencyKey(scheduleID, tick), "a-different-hash", scheduleID).Scan(&pipelineID); err != nil {
		t.Fatalf("seed scheduled pipeline: %v", err)
	}

	res := scheduleRequest(t, router, http.MethodPost, "/schedules/emails/trigger", nil, "test-api-key")
	if res.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body=%s", res.Code, res.Body.String())
	}

	var out struct {
		RunID      int64 `json:"runId"`
		PipelineID int   `json:"pipelineId"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.PipelineID == pipelineID {
		t.Fatalf("the manual run resolved the scheduled tick's pipeline instead of creating its own")
	}

	var key string
	if err := db.Get(&key, `SELECT idempotency_key FROM pipeline WHERE id = $1`, out.PipelineID); err != nil {
		t.Fatalf("load key: %v", err)
	}
	if want := store.ManualScheduleIdempotencyKey(scheduleID, out.RunID); key != want {
		t.Fatalf("idempotency key = %q, want %q", key, want)
	}
}
