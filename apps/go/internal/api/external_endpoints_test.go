package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jmoiron/sqlx"
	_ "modernc.org/sqlite"

	"pipelogiq/internal/config"
	"pipelogiq/internal/store"
	"pipelogiq/internal/types"
)

const externalTestApplicationID = 101

func TestAppendStagesEndpoint(t *testing.T) {
	t.Run("accepts StageId/PipelineId as null", func(t *testing.T) {
		_, db, router := setupExternalEndpointTest(t)
		pipelineID := insertPipelineForExternalTest(t, db, "append-null-ids", types.PipelineStatusRunning, false)

		body := map[string]any{
			"Stages": []map[string]any{
				{
					"StageId":          nil,
					"PipelineId":       nil,
					"StageName":        "agent:think",
					"StageHandlerName": "AgentThinkHandler",
					"Input": map[string]any{
						"ToolName": "getOrder",
					},
					"Options": nil,
					"IsEvent": nil,
				},
			},
		}

		req := makeJSONRequest(t, http.MethodPost, fmt.Sprintf("/pipelines/%d/stages", pipelineID), body)
		req.Header.Set("X-API-Key", "test-api-key")

		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, req)

		if recorder.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
		}

		var response types.AppendStagesResponse
		if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if len(response.Stages) != 1 {
			t.Fatalf("len(response.Stages) = %d, want 1", len(response.Stages))
		}
		if response.Stages[0].PipelineID != pipelineID {
			t.Fatalf("pipelineId must come from route, got %d want %d", response.Stages[0].PipelineID, pipelineID)
		}

		var stageCount int
		if err := db.Get(&stageCount, `SELECT COUNT(*) FROM stage WHERE pipeline_id = $1`, pipelineID); err != nil {
			t.Fatalf("count stages: %v", err)
		}
		if stageCount != 1 {
			t.Fatalf("stage count = %d, want 1", stageCount)
		}
	})

	t.Run("accepts StageId/PipelineId and ignores them", func(t *testing.T) {
		_, db, router := setupExternalEndpointTest(t)
		pipelineID := insertPipelineForExternalTest(t, db, "append-ignore-ids", types.PipelineStatusRunning, false)

		body := map[string]any{
			"Stages": []map[string]any{
				{
					"StageId":          999999,
					"PipelineId":       123456,
					"StageName":        "agent:think",
					"StageHandlerName": "AgentThinkHandler",
					"Input": map[string]any{
						"ToolName": "getOrder",
					},
					"IsEvent": nil,
				},
			},
		}

		req := makeJSONRequest(t, http.MethodPost, fmt.Sprintf("/pipelines/%d/stages", pipelineID), body)
		req.Header.Set("X-API-Key", "test-api-key")

		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, req)

		if recorder.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
		}

		var response types.AppendStagesResponse
		if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if len(response.Stages) != 1 {
			t.Fatalf("len(response.Stages) = %d, want 1", len(response.Stages))
		}
		if response.Stages[0].ID == 999999 {
			t.Fatalf("stageId from request must be ignored")
		}
		if response.Stages[0].PipelineID != pipelineID {
			t.Fatalf("pipelineId must come from route, got %d want %d", response.Stages[0].PipelineID, pipelineID)
		}

		var persistedInput string
		if err := db.Get(&persistedInput, `
			SELECT COALESCE(input, '')
			FROM stage_io
			WHERE stage_id = $1
		`, response.Stages[0].ID); err != nil {
			t.Fatalf("load persisted input: %v", err)
		}
		if persistedInput != `{"ToolName":"getOrder"}` {
			t.Fatalf("persisted input = %s, want %s", persistedInput, `{"ToolName":"getOrder"}`)
		}
	})

	t.Run("happy path append multiple stages", func(t *testing.T) {
		_, db, router := setupExternalEndpointTest(t)
		pipelineID := insertPipelineForExternalTest(t, db, "append-ok", types.PipelineStatusRunning, false)

		body := map[string]any{
			"stages": []map[string]any{
				{
					"stageName":        "agent.tool.a",
					"stageHandlerName": "AgentHandler",
					"input":            map[string]any{"a": 1},
				},
				{
					"stageName":        "agent.tool.b",
					"stageHandlerName": "AgentHandler",
					"options": map[string]any{
						"maxRetries": 1,
					},
				},
			},
		}

		req := makeJSONRequest(t, http.MethodPost, fmt.Sprintf("/pipelines/%d/stages", pipelineID), body)
		req.Header.Set("X-API-Key", "test-api-key")

		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, req)

		if recorder.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
		}

		var response types.AppendStagesResponse
		if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if len(response.Stages) != 2 {
			t.Fatalf("len(response.Stages) = %d, want 2", len(response.Stages))
		}
	})

	t.Run("400 invalid request empty stages", func(t *testing.T) {
		_, db, router := setupExternalEndpointTest(t)
		pipelineID := insertPipelineForExternalTest(t, db, "append-400", types.PipelineStatusRunning, false)

		req := makeJSONRequest(t, http.MethodPost, fmt.Sprintf("/pipelines/%d/stages", pipelineID), map[string]any{
			"stages": []any{},
		})
		req.Header.Set("X-API-Key", "test-api-key")

		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, req)

		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusBadRequest, recorder.Body.String())
		}
	})

	t.Run("404 pipeline not found", func(t *testing.T) {
		_, _, router := setupExternalEndpointTest(t)

		req := makeJSONRequest(t, http.MethodPost, "/pipelines/99999/stages", map[string]any{
			"stages": []map[string]any{
				{"stageName": "s", "stageHandlerName": "h"},
			},
		})
		req.Header.Set("X-API-Key", "test-api-key")

		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, req)

		if recorder.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusNotFound, recorder.Body.String())
		}
	})

	t.Run("409 append on completed pipeline", func(t *testing.T) {
		_, db, router := setupExternalEndpointTest(t)
		pipelineID := insertPipelineForExternalTest(t, db, "append-409", types.PipelineStatusCompleted, true)

		req := makeJSONRequest(t, http.MethodPost, fmt.Sprintf("/pipelines/%d/stages", pipelineID), map[string]any{
			"stages": []map[string]any{
				{"stageName": "s", "stageHandlerName": "h"},
			},
		})
		req.Header.Set("X-API-Key", "test-api-key")

		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, req)

		if recorder.Code != http.StatusConflict {
			t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusConflict, recorder.Body.String())
		}
	})

	t.Run("400 missing StageName", func(t *testing.T) {
		_, db, router := setupExternalEndpointTest(t)
		pipelineID := insertPipelineForExternalTest(t, db, "append-missing-name", types.PipelineStatusRunning, false)

		req := makeJSONRequest(t, http.MethodPost, fmt.Sprintf("/pipelines/%d/stages", pipelineID), map[string]any{
			"stages": []map[string]any{
				{"stageHandlerName": "h"},
			},
		})
		req.Header.Set("X-API-Key", "test-api-key")

		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, req)

		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusBadRequest, recorder.Body.String())
		}
	})

	t.Run("400 missing StageHandlerName", func(t *testing.T) {
		_, db, router := setupExternalEndpointTest(t)
		pipelineID := insertPipelineForExternalTest(t, db, "append-missing-handler", types.PipelineStatusRunning, false)

		req := makeJSONRequest(t, http.MethodPost, fmt.Sprintf("/pipelines/%d/stages", pipelineID), map[string]any{
			"stages": []map[string]any{
				{"stageName": "s"},
			},
		})
		req.Header.Set("X-API-Key", "test-api-key")

		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, req)

		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusBadRequest, recorder.Body.String())
		}
	})
}

func TestResumeStageEndpoint(t *testing.T) {
	t.Run("happy path approved and rejected", func(t *testing.T) {
		_, db, router := setupExternalEndpointTest(t)

		pipelineApproved := insertPipelineForExternalTest(t, db, "resume-approved", types.PipelineStatusRunning, false)
		stageApproved := insertStageForExternalTest(t, db, pipelineApproved, "waiting-approved", types.StageStatusWaitingApproval)

		reqApproved := makeJSONRequest(t, http.MethodPost, fmt.Sprintf("/stages/%d/resume", stageApproved), map[string]any{
			"approved": true,
		})
		reqApproved.Header.Set("X-API-Key", "test-api-key")
		recApproved := httptest.NewRecorder()
		router.ServeHTTP(recApproved, reqApproved)

		if recApproved.Code != http.StatusNoContent {
			t.Fatalf("approved status = %d, want %d; body=%s", recApproved.Code, http.StatusNoContent, recApproved.Body.String())
		}

		pipelineRejected := insertPipelineForExternalTest(t, db, "resume-rejected", types.PipelineStatusRunning, false)
		stageRejected := insertStageForExternalTest(t, db, pipelineRejected, "waiting-rejected", types.StageStatusWaitingApproval)

		reqRejected := makeJSONRequest(t, http.MethodPost, fmt.Sprintf("/stages/%d/resume", stageRejected), map[string]any{
			"approved":        false,
			"rejectionReason": "rejected by user",
		})
		reqRejected.Header.Set("X-API-Key", "test-api-key")
		recRejected := httptest.NewRecorder()
		router.ServeHTTP(recRejected, reqRejected)

		if recRejected.Code != http.StatusNoContent {
			t.Fatalf("rejected status = %d, want %d; body=%s", recRejected.Code, http.StatusNoContent, recRejected.Body.String())
		}
	})

	t.Run("404 stage not found", func(t *testing.T) {
		_, _, router := setupExternalEndpointTest(t)

		req := makeJSONRequest(t, http.MethodPost, "/stages/99999/resume", map[string]any{
			"approved": true,
		})
		req.Header.Set("X-API-Key", "test-api-key")

		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, req)

		if recorder.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusNotFound, recorder.Body.String())
		}
	})

	t.Run("409 stage not in waiting state", func(t *testing.T) {
		_, db, router := setupExternalEndpointTest(t)
		pipelineID := insertPipelineForExternalTest(t, db, "resume-409", types.PipelineStatusRunning, false)
		stageID := insertStageForExternalTest(t, db, pipelineID, "not-waiting", types.StageStatusPending)

		req := makeJSONRequest(t, http.MethodPost, fmt.Sprintf("/stages/%d/resume", stageID), map[string]any{
			"approved": true,
		})
		req.Header.Set("X-API-Key", "test-api-key")

		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, req)

		if recorder.Code != http.StatusConflict {
			t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusConflict, recorder.Body.String())
		}
	})

	t.Run("idempotent repeated request", func(t *testing.T) {
		_, db, router := setupExternalEndpointTest(t)
		pipelineID := insertPipelineForExternalTest(t, db, "resume-idempotent", types.PipelineStatusRunning, false)
		stageID := insertStageForExternalTest(t, db, pipelineID, "waiting", types.StageStatusWaitingApproval)

		requestBody := map[string]any{"approved": true}

		req1 := makeJSONRequest(t, http.MethodPost, fmt.Sprintf("/stages/%d/resume", stageID), requestBody)
		req1.Header.Set("X-API-Key", "test-api-key")
		rec1 := httptest.NewRecorder()
		router.ServeHTTP(rec1, req1)
		if rec1.Code != http.StatusNoContent {
			t.Fatalf("first request status = %d, want %d", rec1.Code, http.StatusNoContent)
		}

		req2 := makeJSONRequest(t, http.MethodPost, fmt.Sprintf("/stages/%d/resume", stageID), requestBody)
		req2.Header.Set("X-API-Key", "test-api-key")
		rec2 := httptest.NewRecorder()
		router.ServeHTTP(rec2, req2)
		if rec2.Code != http.StatusNoContent {
			t.Fatalf("second request status = %d, want %d", rec2.Code, http.StatusNoContent)
		}
	})
}

func TestConcurrencyEndpoints(t *testing.T) {
	t.Run("two concurrent resume calls", func(t *testing.T) {
		_, db, router := setupExternalEndpointTest(t)
		pipelineID := insertPipelineForExternalTest(t, db, "resume-concurrent", types.PipelineStatusRunning, false)
		stageID := insertStageForExternalTest(t, db, pipelineID, "waiting", types.StageStatusWaitingApproval)

		var wg sync.WaitGroup
		start := make(chan struct{})
		codes := make(chan int, 2)

		worker := func(body map[string]any) {
			defer wg.Done()
			<-start
			req := makeJSONRequest(t, http.MethodPost, fmt.Sprintf("/stages/%d/resume", stageID), body)
			req.Header.Set("X-API-Key", "test-api-key")
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			codes <- rec.Code
		}

		wg.Add(2)
		go worker(map[string]any{"approved": true})
		go worker(map[string]any{"approved": true})

		close(start)
		wg.Wait()
		close(codes)

		for code := range codes {
			if code != http.StatusNoContent {
				t.Fatalf("concurrent resume status = %d, want %d", code, http.StatusNoContent)
			}
		}
	})

	t.Run("append during concurrent pipeline completion", func(t *testing.T) {
		_, db, router := setupExternalEndpointTest(t)
		pipelineID := insertPipelineForExternalTest(t, db, "append-concurrent", types.PipelineStatusRunning, false)

		start := make(chan struct{})
		var wg sync.WaitGroup
		results := make(chan int, 1)

		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			req := makeJSONRequest(t, http.MethodPost, fmt.Sprintf("/pipelines/%d/stages", pipelineID), map[string]any{
				"stages": []map[string]any{
					{"stageName": "s", "stageHandlerName": "h"},
				},
			})
			req.Header.Set("X-API-Key", "test-api-key")
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			results <- rec.Code
		}()

		go func() {
			defer wg.Done()
			<-start
			_, _ = db.Exec(`
				UPDATE pipeline
				SET status = $1, is_completed = true, finished_at = $2
				WHERE id = $3
			`, types.PipelineStatusCompleted, time.Now().UTC(), pipelineID)
		}()

		close(start)
		wg.Wait()
		close(results)

		code := <-results
		if code != http.StatusOK && code != http.StatusConflict {
			t.Fatalf("append concurrent status = %d, want 200 or 409", code)
		}
	})
}

func setupExternalEndpointTest(t *testing.T) (*ExternalServer, *sqlx.DB, http.Handler) {
	t.Helper()

	dsn := fmt.Sprintf(
		"file:external_endpoint_%d?mode=memory&cache=shared&_pragma=foreign_keys(ON)&_pragma=busy_timeout(5000)",
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
		trace_id TEXT NULL,
		idempotency_key TEXT NULL,
		request_hash TEXT NULL
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
		approval_resumed_by TEXT NULL,
		execution_id TEXT NULL,
		execution_attempt INTEGER NOT NULL DEFAULT 0,
		dispatched_at TIMESTAMP NULL,
		lease_owner TEXT NULL,
		lease_expires_at TIMESTAMP NULL,
		last_error_code TEXT NULL,
		failure_disposition TEXT NULL
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
		retry_on_error_codes TEXT NULL,
		retry_backoff TEXT NULL,
		max_retry_interval INTEGER NULL,
		retry_jitter BOOLEAN NULL,
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
		is_sensitive BOOLEAN NOT NULL DEFAULT 0,
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
	if _, err := db.Exec(schema); err != nil {
		_ = db.Close()
		t.Fatalf("create schema: %v", err)
	}

	if _, err := db.Exec(`
		INSERT INTO api_key (application_id, key, created_at, disabled_at, expires_at)
		VALUES ($1, $2, $3, NULL, NULL)
	`, externalTestApplicationID, "test-api-key", time.Now().UTC()); err != nil {
		_ = db.Close()
		t.Fatalf("insert api key: %v", err)
	}

	t.Cleanup(func() {
		_ = db.Close()
	})

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	st := store.New(db, logger)
	server := NewExternalServer(config.APIConfig{}, st, nil, logger)

	router := chi.NewRouter()
	router.Use(middleware.RequestID)
	router.Post("/pipelines/{pipelineId}/stages", server.handleAppendPipelineStages)
	router.Post("/stages/{stageId}/resume", server.handleResumeStage)

	return server, db, router
}

func insertPipelineForExternalTest(t *testing.T, db *sqlx.DB, name, status string, completed bool) int {
	t.Helper()

	var id int
	if err := db.QueryRow(`
		INSERT INTO pipeline (name, status, created_at, is_completed, application_id)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id
	`, name, status, time.Now().UTC(), completed, externalTestApplicationID).Scan(&id); err != nil {
		t.Fatalf("insert pipeline: %v", err)
	}
	return id
}

func insertStageForExternalTest(t *testing.T, db *sqlx.DB, pipelineID int, name, status string) int {
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

func makeJSONRequest(t *testing.T, method, path string, body any) *http.Request {
	t.Helper()

	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal request body: %v", err)
	}
	req := httptest.NewRequest(method, path, bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	return req
}
