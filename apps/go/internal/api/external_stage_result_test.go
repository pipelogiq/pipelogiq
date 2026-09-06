package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"pipelogiq/internal/types"
)

// captureStageResults replaces the broker publish hook and records every payload
// the endpoint queues, so the tests assert on the exact bytes an AMQP consumer
// would receive.
func captureStageResults(server *ExternalServer, publishErr error) *stageResultRecorder {
	recorder := &stageResultRecorder{err: publishErr}
	server.publishStageResult = recorder.publish
	return recorder
}

type stageResultRecorder struct {
	mu       sync.Mutex
	err      error
	payloads [][]byte
}

func (r *stageResultRecorder) publish(_ context.Context, body []byte) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.err != nil {
		return r.err
	}
	r.payloads = append(r.payloads, append([]byte(nil), body...))
	return nil
}

func (r *stageResultRecorder) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.payloads)
}

func (r *stageResultRecorder) last() []byte {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.payloads) == 0 {
		return nil
	}
	return r.payloads[len(r.payloads)-1]
}

func postStageResult(
	t *testing.T,
	router http.Handler,
	stageID int,
	body any,
	apiKey string,
	sessionToken string,
) *httptest.ResponseRecorder {
	t.Helper()

	req := makeJSONRequest(t, http.MethodPost, fmt.Sprintf("/stages/%d/result", stageID), body)
	if apiKey != "" {
		req.Header.Set("X-API-Key", apiKey)
	}
	if sessionToken != "" {
		req.Header.Set("X-Worker-Session", sessionToken)
	}
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)
	return recorder
}

func stageResultBody(stageID, pipelineID int) map[string]any {
	return map[string]any{
		"pipelineId":  pipelineID,
		"stageId":     stageID,
		"executionId": "exec-1",
		"attempt":     2,
		"isSuccess":   false,
		"result":      "Stripe reports the checkout is still open.",
		"errorCode":   "PAYMENT_PENDING",
		"retryable":   true,
	}
}

func TestPostStageResultEndpoint(t *testing.T) {
	t.Run("accepted result is published verbatim", func(t *testing.T) {
		server, db, router := setupExternalEndpointTest(t)
		recorder := captureStageResults(server, nil)
		pipelineID := insertPipelineForExternalTest(t, db, "result-ok", types.PipelineStatusRunning, false)
		stageID := insertStageForExternalTest(t, db, pipelineID, "pay", types.StageStatusRunning)

		body := stageResultBody(stageID, pipelineID)
		body["logs"] = []map[string]any{
			{"message": "waiting", "logLevel": "Info", "created": time.Now().UTC().Format(time.RFC3339Nano)},
		}
		body["contextItems"] = []map[string]any{
			{"key": "paymentOutcome", "value": `"Pending"`, "valueType": "System.String"},
		}

		res := postStageResult(t, router, stageID, body, "test-api-key", "session-token")
		if res.Code != http.StatusAccepted {
			t.Fatalf("status = %d, want 202; body=%s", res.Code, res.Body.String())
		}

		var accepted types.StageResultAcceptedResponse
		if err := json.Unmarshal(res.Body.Bytes(), &accepted); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if !accepted.Accepted || accepted.StageID != stageID || accepted.ExecutionID != "exec-1" {
			t.Fatalf("unexpected response %+v", accepted)
		}

		if recorder.count() != 1 {
			t.Fatalf("published %d messages, want 1", recorder.count())
		}

		var published types.StageResultMessage
		if err := json.Unmarshal(recorder.last(), &published); err != nil {
			t.Fatalf("decode published payload: %v", err)
		}
		if published.StageID != stageID {
			t.Fatalf("published stageId = %d, want %d", published.StageID, stageID)
		}
		if published.PipelineID == nil || *published.PipelineID != pipelineID {
			t.Fatalf("published pipelineId = %v, want %d", published.PipelineID, pipelineID)
		}
		if published.ExecutionID != "exec-1" || published.Attempt != 2 {
			t.Fatalf("fencing fields lost: %+v", published)
		}
		if published.IsSuccess {
			t.Fatalf("isSuccess must survive the round trip")
		}
		if published.ErrorCode != "PAYMENT_PENDING" {
			t.Fatalf("errorCode = %q", published.ErrorCode)
		}
		if published.Retryable == nil || !*published.Retryable {
			t.Fatalf("retryable = %v, want true", published.Retryable)
		}
		if len(published.Logs) != 1 || len(published.ContextItems) != 1 {
			t.Fatalf("logs/context lost: %+v", published)
		}
	})

	t.Run("result for a terminal stage is still accepted", func(t *testing.T) {
		server, db, router := setupExternalEndpointTest(t)
		recorder := captureStageResults(server, nil)
		pipelineID := insertPipelineForExternalTest(t, db, "result-terminal", types.PipelineStatusCompleted, true)
		stageID := insertStageForExternalTest(t, db, pipelineID, "pay", types.StageStatusCompleted)

		res := postStageResult(t, router, stageID, stageResultBody(stageID, pipelineID), "test-api-key", "session-token")
		if res.Code != http.StatusAccepted {
			t.Fatalf("status = %d, want 202; body=%s", res.Code, res.Body.String())
		}
		if recorder.count() != 1 {
			t.Fatalf("stale results must still reach the consumer, published %d", recorder.count())
		}
	})

	t.Run("missing worker session token is unauthorized", func(t *testing.T) {
		server, db, router := setupExternalEndpointTest(t)
		recorder := captureStageResults(server, nil)
		pipelineID := insertPipelineForExternalTest(t, db, "result-nosession", types.PipelineStatusRunning, false)
		stageID := insertStageForExternalTest(t, db, pipelineID, "pay", types.StageStatusRunning)

		res := postStageResult(t, router, stageID, stageResultBody(stageID, pipelineID), "test-api-key", "")
		if res.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401; body=%s", res.Code, res.Body.String())
		}
		if recorder.count() != 0 {
			t.Fatalf("unauthenticated request must not publish")
		}
	})

	t.Run("invalid api key is unauthorized", func(t *testing.T) {
		_, db, router := setupExternalEndpointTest(t)
		pipelineID := insertPipelineForExternalTest(t, db, "result-nokey", types.PipelineStatusRunning, false)
		stageID := insertStageForExternalTest(t, db, pipelineID, "pay", types.StageStatusRunning)

		res := postStageResult(t, router, stageID, stageResultBody(stageID, pipelineID), "wrong-key", "session-token")
		if res.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401; body=%s", res.Code, res.Body.String())
		}
	})

	t.Run("stage of another application is forbidden", func(t *testing.T) {
		server, db, router := setupExternalEndpointTest(t)
		recorder := captureStageResults(server, nil)

		var foreignPipelineID int
		if err := db.QueryRow(`
			INSERT INTO pipeline (name, status, created_at, is_completed, application_id)
			VALUES ($1, $2, $3, false, $4)
			RETURNING id
		`, "foreign", types.PipelineStatusRunning, time.Now().UTC(), externalTestApplicationID+1).
			Scan(&foreignPipelineID); err != nil {
			t.Fatalf("insert foreign pipeline: %v", err)
		}
		stageID := insertStageForExternalTest(t, db, foreignPipelineID, "pay", types.StageStatusRunning)

		res := postStageResult(t, router, stageID, stageResultBody(stageID, foreignPipelineID), "test-api-key", "session-token")
		if res.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want 403; body=%s", res.Code, res.Body.String())
		}
		if recorder.count() != 0 {
			t.Fatalf("cross-application result must not publish")
		}
	})

	t.Run("unknown stage is not found", func(t *testing.T) {
		_, _, router := setupExternalEndpointTest(t)

		res := postStageResult(t, router, 987654, stageResultBody(987654, 1), "test-api-key", "session-token")
		if res.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want 404; body=%s", res.Code, res.Body.String())
		}
	})

	t.Run("validation failures", func(t *testing.T) {
		cases := []struct {
			name  string
			field string
			patch func(body map[string]any, stageID int)
		}{
			{
				name:  "missing executionId",
				field: "executionId",
				patch: func(body map[string]any, _ int) { delete(body, "executionId") },
			},
			{
				name:  "missing attempt",
				field: "attempt",
				patch: func(body map[string]any, _ int) { delete(body, "attempt") },
			},
			{
				name:  "route and body stageId mismatch",
				field: "stageId",
				patch: func(body map[string]any, stageID int) { body["stageId"] = stageID + 1 },
			},
			{
				name:  "result exceeds the stage io limit",
				field: "result",
				patch: func(body map[string]any, _ int) {
					body["result"] = strings.Repeat("x", maxStageResultBytes+1)
				},
			},
			{
				name:  "errorCode carries control characters",
				field: "errorCode",
				patch: func(body map[string]any, _ int) { body["errorCode"] = "BAD\x07CODE" },
			},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				server, db, router := setupExternalEndpointTest(t)
				recorder := captureStageResults(server, nil)
				pipelineID := insertPipelineForExternalTest(t, db, "result-invalid", types.PipelineStatusRunning, false)
				stageID := insertStageForExternalTest(t, db, pipelineID, "pay", types.StageStatusRunning)

				body := stageResultBody(stageID, pipelineID)
				tc.patch(body, stageID)

				res := postStageResult(t, router, stageID, body, "test-api-key", "session-token")
				if res.Code != http.StatusBadRequest {
					t.Fatalf("status = %d, want 400; body=%s", res.Code, res.Body.String())
				}
				if contentType := res.Header().Get("Content-Type"); contentType != "application/problem+json" {
					t.Fatalf("content type = %q, want application/problem+json", contentType)
				}

				var problem problemDetails
				if err := json.Unmarshal(res.Body.Bytes(), &problem); err != nil {
					t.Fatalf("decode problem: %v", err)
				}
				if _, ok := problem.Errors[tc.field]; !ok {
					t.Fatalf("problem must name %q, got %+v", tc.field, problem.Errors)
				}
				if recorder.count() != 0 {
					t.Fatalf("invalid result must not publish")
				}
			})
		}
	})

	t.Run("broker failure is service unavailable", func(t *testing.T) {
		server, db, router := setupExternalEndpointTest(t)
		captureStageResults(server, errBrokerUnavailable)
		pipelineID := insertPipelineForExternalTest(t, db, "result-broker", types.PipelineStatusRunning, false)
		stageID := insertStageForExternalTest(t, db, pipelineID, "pay", types.StageStatusRunning)

		res := postStageResult(t, router, stageID, stageResultBody(stageID, pipelineID), "test-api-key", "session-token")
		if res.Code != http.StatusServiceUnavailable {
			t.Fatalf("status = %d, want 503; body=%s", res.Code, res.Body.String())
		}

		var problem problemDetails
		if err := json.Unmarshal(res.Body.Bytes(), &problem); err != nil {
			t.Fatalf("decode problem: %v", err)
		}
		if problem.Type != problemTypeBrokerUnavailable {
			t.Fatalf("problem type = %q, want %q", problem.Type, problemTypeBrokerUnavailable)
		}
	})
}
