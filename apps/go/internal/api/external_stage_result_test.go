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

	"github.com/jmoiron/sqlx"

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
		server, db, router := setupStageResultEndpointTest(t)
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
		server, db, router := setupStageResultEndpointTest(t)
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
		server, db, router := setupStageResultEndpointTest(t)
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
		_, db, router := setupStageResultEndpointTest(t)
		pipelineID := insertPipelineForExternalTest(t, db, "result-nokey", types.PipelineStatusRunning, false)
		stageID := insertStageForExternalTest(t, db, pipelineID, "pay", types.StageStatusRunning)

		res := postStageResult(t, router, stageID, stageResultBody(stageID, pipelineID), "wrong-key", "session-token")
		if res.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401; body=%s", res.Code, res.Body.String())
		}
	})

	t.Run("stage of another application is forbidden", func(t *testing.T) {
		server, db, router := setupStageResultEndpointTest(t)
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
		_, _, router := setupStageResultEndpointTest(t)

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
				server, db, router := setupStageResultEndpointTest(t)
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
		server, db, router := setupStageResultEndpointTest(t)
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

// TestPostStageResultAcceptsSdkExtraFields pins the cross-repo contract with the
// .NET SDK. StageResultDto carries fields this message does not declare —
// tracing metadata and the idempotency key — and it serialises PascalCase.
//
// A strict decoder here would couple every SDK version to a server version and
// make adding a field to a worker a breaking change for the control plane.
func TestPostStageResultAcceptsSdkExtraFields(t *testing.T) {
	server, db, router := setupStageResultEndpointTest(t)
	recorder := captureStageResults(server, nil)
	pipelineID := insertPipelineForExternalTest(t, db, "result-sdk", types.PipelineStatusRunning, false)
	stageID := insertStageForExternalTest(t, db, pipelineID, "pay", types.StageStatusRunning)

	// Exactly what PipelogiqApiClient.PostStageResultAsync puts on the wire:
	// PascalCase names, plus fields the server does not model.
	body := map[string]any{
		"PipelineId":             pipelineID,
		"StageId":                stageID,
		"ExecutionId":            "exec-1",
		"Attempt":                2,
		"IsSuccess":              true,
		"Result":                 `{"ok":true}`,
		"retryable":              nil,
		"IsWaitingForApproval":   false,
		"RunNextIfCurrentFailed": false,
		"Logs":                   []any{},
		"ContextItems":           []any{},
		"AppendedStages":         []any{},
		// Declared by the SDK DTO, not by types.StageResultMessage.
		"IdempotencyKey": "order-8891",
		"TimeoutSeconds": 120,
		"Traceparent":    "00-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa-bbbbbbbbbbbbbbbb-01",
		"Tracestate":     "",
		"TraceId":        "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"SpanId":         "bbbbbbbbbbbbbbbb",
	}

	res := postStageResult(t, router, stageID, body, "test-api-key", "session-token")
	if res.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202; body=%s", res.Code, res.Body.String())
	}
	if recorder.count() != 1 {
		t.Fatalf("published %d messages, want 1", recorder.count())
	}

	var published types.StageResultMessage
	if err := json.Unmarshal(recorder.last(), &published); err != nil {
		t.Fatalf("decode published payload: %v", err)
	}
	// PascalCase must still land on the right fields: Go matches case-insensitively.
	if published.StageID != stageID || published.ExecutionID != "exec-1" || published.Attempt != 2 {
		t.Fatalf("fencing fields lost from a PascalCase body: %+v", published)
	}
	if !published.IsSuccess {
		t.Fatalf("isSuccess lost from a PascalCase body")
	}
}

// TestPostStageResultRejectsMalformedBodies keeps the tolerant decode from
// becoming a decode that accepts anything.
func TestPostStageResultRejectsMalformedBodies(t *testing.T) {
	cases := []struct {
		name string
		raw  string
	}{
		{"not json", `this is not json`},
		{"two objects", `{"stageId":1,"executionId":"e","attempt":1}{"stageId":2}`},
		{"wrong type for stageId", `{"stageId":"eighty-one","executionId":"e","attempt":1}`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server, db, router := setupStageResultEndpointTest(t)
			recorder := captureStageResults(server, nil)
			pipelineID := insertPipelineForExternalTest(t, db, "result-malformed", types.PipelineStatusRunning, false)
			stageID := insertStageForExternalTest(t, db, pipelineID, "pay", types.StageStatusRunning)

			req := httptest.NewRequest(
				http.MethodPost,
				fmt.Sprintf("/stages/%d/result", stageID),
				strings.NewReader(tc.raw))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-API-Key", "test-api-key")
			req.Header.Set("X-Worker-Session", "session-token")

			res := httptest.NewRecorder()
			router.ServeHTTP(res, req)

			if res.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400; body=%s", res.Code, res.Body.String())
			}
			if recorder.count() != 0 {
				t.Fatalf("a malformed body must not publish")
			}
		})
	}
}

// Result submission requires a real application-bound bootstrap session.
func setupStageResultEndpointTest(t *testing.T) (*ExternalServer, *sqlx.DB, http.Handler) {
	t.Helper()
	server, db, router := setupExternalEndpointTest(t)
	if _, err := db.Exec(`CREATE TABLE worker_client (id TEXT PRIMARY KEY, application_id INTEGER NOT NULL, session_token TEXT NOT NULL, session_expires_at TIMESTAMP NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	for _, session := range []struct {
		id, token string
		appID     int
		expiry    time.Time
	}{
		{"active", "session-token", externalTestApplicationID, time.Now().UTC().Add(time.Hour)},
		{"expired", "expired-session", externalTestApplicationID, time.Now().UTC().Add(-time.Hour)},
		{"foreign", "foreign-session", externalTestApplicationID + 1, time.Now().UTC().Add(time.Hour)},
	} {
		if _, err := db.Exec(`INSERT INTO worker_client (id,application_id,session_token,session_expires_at) VALUES ($1,$2,$3,$4)`, session.id, session.appID, session.token, session.expiry); err != nil {
			t.Fatal(err)
		}
	}
	return server, db, router
}

func TestPostStageResultRejectsUntrustedWorkerSessions(t *testing.T) {
	for _, token := range []string{"unknown-session", "expired-session", "foreign-session"} {
		t.Run(token, func(t *testing.T) {
			server, db, router := setupStageResultEndpointTest(t)
			published := captureStageResults(server, nil)
			pipelineID := insertPipelineForExternalTest(t, db, "session-auth", types.PipelineStatusRunning, false)
			stageID := insertStageForExternalTest(t, db, pipelineID, "stage", types.StageStatusRunning)
			response := postStageResult(t, router, stageID, stageResultBody(stageID, pipelineID), "test-api-key", token)
			if response.Code != http.StatusUnauthorized || published.count() != 0 {
				t.Fatalf("status=%d published=%d body=%s", response.Code, published.count(), response.Body.String())
			}
		})
	}
}
