package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
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

const (
	idempotencyTestApplicationID      = 201
	idempotencyTestOtherApplicationID = 202
	idempotencyTestAPIKey             = "idempotency-test-api-key"
	idempotencyTestOtherAPIKey        = "idempotency-test-other-api-key"
)

func TestIdempotentPipelineCreate_SequentialRequestsCreateOnePipeline(t *testing.T) {
	_, db, router := setupIdempotencyEndpointTest(t)
	body := idempotencyPipelineRequest("claim:tenant-1:process-101", "ensure-claim")

	first := serveIdempotencyJSON(t, router, http.MethodPost, "/pipelines/idempotent", idempotencyTestAPIKey, body)
	second := serveIdempotencyJSON(t, router, http.MethodPost, "/pipelines/idempotent", idempotencyTestAPIKey, body)

	if first.Code != http.StatusCreated {
		t.Fatalf("first status = %d, want %d; body=%s", first.Code, http.StatusCreated, first.Body.String())
	}
	if second.Code != http.StatusOK {
		t.Fatalf("second status = %d, want %d; body=%s", second.Code, http.StatusOK, second.Body.String())
	}

	firstResponse := decodeIdempotentCreateResponse(t, first)
	secondResponse := decodeIdempotentCreateResponse(t, second)
	if !firstResponse.Created || firstResponse.WasExisting {
		t.Fatalf("first outcome = created:%t existing:%t", firstResponse.Created, firstResponse.WasExisting)
	}
	if secondResponse.Created || !secondResponse.WasExisting {
		t.Fatalf("second outcome = created:%t existing:%t", secondResponse.Created, secondResponse.WasExisting)
	}
	if firstResponse.Pipeline == nil || secondResponse.Pipeline == nil {
		t.Fatal("pipeline response must not be nil")
	}
	if firstResponse.Pipeline.ID != secondResponse.Pipeline.ID {
		t.Fatalf("pipeline ids differ: first=%d second=%d", firstResponse.Pipeline.ID, secondResponse.Pipeline.ID)
	}
	assertPipelineCount(t, db, 1)
	assertStageCount(t, db, firstResponse.Pipeline.ID, 1)
}

func TestIdempotentPipelineCreate_ConcurrentRequestsCreateOnePipeline(t *testing.T) {
	_, db, router := setupIdempotencyEndpointTest(t)
	body := idempotencyPipelineRequest("claim:tenant-1:process-102", "ensure-claim")

	start := make(chan struct{})
	recorders := make(chan *httptest.ResponseRecorder, 2)
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			recorders <- serveIdempotencyJSON(
				t,
				router,
				http.MethodPost,
				"/pipelines/idempotent",
				idempotencyTestAPIKey,
				body,
			)
		}()
	}
	close(start)
	wg.Wait()
	close(recorders)

	var pipelineID int
	createdCount := 0
	existingCount := 0
	for recorder := range recorders {
		if recorder.Code != http.StatusCreated && recorder.Code != http.StatusOK {
			t.Fatalf("concurrent status = %d; body=%s", recorder.Code, recorder.Body.String())
		}
		response := decodeIdempotentCreateResponse(t, recorder)
		if response.Pipeline == nil {
			t.Fatal("pipeline response must not be nil")
		}
		if pipelineID == 0 {
			pipelineID = response.Pipeline.ID
		} else if pipelineID != response.Pipeline.ID {
			t.Fatalf("concurrent pipeline ids differ: %d and %d", pipelineID, response.Pipeline.ID)
		}
		if response.Created {
			createdCount++
		}
		if response.WasExisting {
			existingCount++
		}
	}

	if createdCount != 1 || existingCount != 1 {
		t.Fatalf("created=%d existing=%d, want 1 and 1", createdCount, existingCount)
	}
	assertPipelineCount(t, db, 1)
	assertStageCount(t, db, pipelineID, 1)
}

func TestIdempotentPipelineCreate_ResponseLossRetryReturnsExistingPipeline(t *testing.T) {
	_, db, router := setupIdempotencyEndpointTest(t)
	key := "claim:tenant-1:process-103"
	firstBody := idempotencyPipelineRequest(key, "ensure-claim")
	firstBody["traceId"] = strings.Repeat("1", 32)
	firstBody["pipelineContextItems"] = append(
		firstBody["pipelineContextItems"].([]map[string]any),
		map[string]any{
			"key":       "traceparent",
			"value":     `"00-11111111111111111111111111111111-1111111111111111-01"`,
			"valueType": "System.String",
		},
	)

	// The first response is intentionally ignored to model a client timeout
	// after the server transaction committed.
	lostResponse := serveIdempotencyJSON(
		t,
		router,
		http.MethodPost,
		"/pipelines/idempotent",
		idempotencyTestAPIKey,
		firstBody,
	)
	if lostResponse.Code != http.StatusCreated {
		t.Fatalf("lost response status = %d, want %d; body=%s", lostResponse.Code, http.StatusCreated, lostResponse.Body.String())
	}

	retryBody := idempotencyPipelineRequest(key, "ensure-claim")
	retryBody["traceId"] = strings.Repeat("2", 32)
	retryBody["pipelineContextItems"] = append(
		retryBody["pipelineContextItems"].([]map[string]any),
		map[string]any{
			"key":       "traceparent",
			"value":     `"00-22222222222222222222222222222222-2222222222222222-01"`,
			"valueType": "System.String",
		},
	)
	retry := serveIdempotencyJSON(
		t,
		router,
		http.MethodPost,
		"/pipelines/idempotent",
		idempotencyTestAPIKey,
		retryBody,
	)
	if retry.Code != http.StatusOK {
		t.Fatalf("retry status = %d, want %d; body=%s", retry.Code, http.StatusOK, retry.Body.String())
	}
	response := decodeIdempotentCreateResponse(t, retry)
	if response.Pipeline == nil || !response.WasExisting || response.Created {
		t.Fatalf("unexpected retry response: %+v", response)
	}

	var storedID int
	if err := db.Get(&storedID, `
		SELECT id FROM pipeline
		WHERE application_id = $1 AND idempotency_key = $2
	`, idempotencyTestApplicationID, key); err != nil {
		t.Fatalf("load stored pipeline: %v", err)
	}
	if response.Pipeline.ID != storedID {
		t.Fatalf("retry id = %d, stored id = %d", response.Pipeline.ID, storedID)
	}
	assertPipelineCount(t, db, 1)
}

func TestIdempotentPipelineCreate_ConflictingRequestReturns409(t *testing.T) {
	_, db, router := setupIdempotencyEndpointTest(t)
	key := "claim:tenant-1:process-104"
	firstBody := idempotencyPipelineRequest(key, "ensure-claim")
	conflictingBody := idempotencyPipelineRequest(key, "ensure-different-claim")

	first := serveIdempotencyJSON(t, router, http.MethodPost, "/pipelines/idempotent", idempotencyTestAPIKey, firstBody)
	if first.Code != http.StatusCreated {
		t.Fatalf("first status = %d, want %d; body=%s", first.Code, http.StatusCreated, first.Body.String())
	}
	conflict := serveIdempotencyJSON(t, router, http.MethodPost, "/pipelines/idempotent", idempotencyTestAPIKey, conflictingBody)
	if conflict.Code != http.StatusConflict {
		t.Fatalf("conflict status = %d, want %d; body=%s", conflict.Code, http.StatusConflict, conflict.Body.String())
	}
	assertPipelineCount(t, db, 1)
}

func TestPipelineIdempotencyLookup_IsApplicationScoped(t *testing.T) {
	_, _, router := setupIdempotencyEndpointTest(t)
	key := "claim:tenant-1:process-105"
	body := idempotencyPipelineRequest(key, "ensure-claim")

	created := serveIdempotencyJSON(t, router, http.MethodPost, "/pipelines/idempotent", idempotencyTestAPIKey, body)
	if created.Code != http.StatusCreated {
		t.Fatalf("create status = %d, want %d; body=%s", created.Code, http.StatusCreated, created.Body.String())
	}
	createdResponse := decodeIdempotentCreateResponse(t, created)

	lookupBody := map[string]any{"idempotencyKey": key}
	lookup := serveIdempotencyJSON(
		t,
		router,
		http.MethodPost,
		"/pipelines/by-idempotency-key",
		idempotencyTestAPIKey,
		lookupBody,
	)
	if lookup.Code != http.StatusOK {
		t.Fatalf("lookup status = %d, want %d; body=%s", lookup.Code, http.StatusOK, lookup.Body.String())
	}
	var pipeline types.PipelineResponse
	if err := json.Unmarshal(lookup.Body.Bytes(), &pipeline); err != nil {
		t.Fatalf("decode lookup response: %v", err)
	}
	if createdResponse.Pipeline == nil || pipeline.ID != createdResponse.Pipeline.ID {
		t.Fatalf("lookup pipeline id = %d, created = %+v", pipeline.ID, createdResponse.Pipeline)
	}

	otherApplicationLookup := serveIdempotencyJSON(
		t,
		router,
		http.MethodPost,
		"/pipelines/by-idempotency-key",
		idempotencyTestOtherAPIKey,
		lookupBody,
	)
	if otherApplicationLookup.Code != http.StatusNotFound {
		t.Fatalf(
			"cross-application lookup status = %d, want %d; body=%s",
			otherApplicationLookup.Code,
			http.StatusNotFound,
			otherApplicationLookup.Body.String(),
		)
	}
}

func TestIdempotentPipelineCreate_RequiresHeaderAndValidatesKey(t *testing.T) {
	_, _, router := setupIdempotencyEndpointTest(t)

	bodyKeyInLegacyField := idempotencyPipelineRequest("claim:tenant-1:process-106", "ensure-claim")
	bodyKeyInLegacyField["apiKey"] = idempotencyTestAPIKey
	withoutHeader := serveIdempotencyJSON(t, router, http.MethodPost, "/pipelines/idempotent", "", bodyKeyInLegacyField)
	if withoutHeader.Code != http.StatusUnauthorized {
		t.Fatalf("without header status = %d, want %d", withoutHeader.Code, http.StatusUnauthorized)
	}

	missingKey := idempotencyPipelineRequest("", "ensure-claim")
	invalid := serveIdempotencyJSON(t, router, http.MethodPost, "/pipelines/idempotent", idempotencyTestAPIKey, missingKey)
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("missing key status = %d, want %d; body=%s", invalid.Code, http.StatusBadRequest, invalid.Body.String())
	}

	tooLongKey := idempotencyPipelineRequest(strings.Repeat("k", maxIdempotencyKeyRunes+1), "ensure-claim")
	tooLong := serveIdempotencyJSON(t, router, http.MethodPost, "/pipelines/idempotent", idempotencyTestAPIKey, tooLongKey)
	if tooLong.Code != http.StatusBadRequest {
		t.Fatalf("long key status = %d, want %d; body=%s", tooLong.Code, http.StatusBadRequest, tooLong.Body.String())
	}

	largeContext := idempotencyPipelineRequest("claim:tenant-1:process-107", "ensure-claim")
	largeContext["pipelineContextItems"] = []map[string]any{
		{
			"key":       "too-large",
			"value":     strings.Repeat("v", maxContextValueBytes+1),
			"valueType": "System.String",
		},
	}
	tooLarge := serveIdempotencyJSON(t, router, http.MethodPost, "/pipelines/idempotent", idempotencyTestAPIKey, largeContext)
	if tooLarge.Code != http.StatusBadRequest {
		t.Fatalf("large context status = %d, want %d; body=%s", tooLarge.Code, http.StatusBadRequest, tooLarge.Body.String())
	}
}

func TestLegacyPipelineCreate_ContractStillCreatesDistinctPipelines(t *testing.T) {
	_, db, router := setupIdempotencyEndpointTest(t)
	body := map[string]any{
		"apiKey": idempotencyTestAPIKey,
		"name":   "legacy-create",
		"stages": []map[string]any{
			{
				"stageName":        "legacy-stage",
				"stageHandlerName": "LegacyHandler",
				"input":            "{}",
			},
		},
	}

	first := serveIdempotencyJSON(t, router, http.MethodPost, "/pipelines", "", body)
	second := serveIdempotencyJSON(t, router, http.MethodPost, "/pipelines", "", body)
	if first.Code != http.StatusOK || second.Code != http.StatusOK {
		t.Fatalf(
			"legacy statuses = %d/%d, want 200/200; bodies=%s / %s",
			first.Code,
			second.Code,
			first.Body.String(),
			second.Body.String(),
		)
	}

	var firstPipeline types.PipelineResponse
	var secondPipeline types.PipelineResponse
	if err := json.Unmarshal(first.Body.Bytes(), &firstPipeline); err != nil {
		t.Fatalf("decode first legacy response: %v", err)
	}
	if err := json.Unmarshal(second.Body.Bytes(), &secondPipeline); err != nil {
		t.Fatalf("decode second legacy response: %v", err)
	}
	if firstPipeline.ID == secondPipeline.ID {
		t.Fatalf("legacy endpoint unexpectedly deduplicated pipeline %d", firstPipeline.ID)
	}
	assertPipelineCount(t, db, 2)

	var idempotentMetadataCount int
	if err := db.Get(&idempotentMetadataCount, `
		SELECT COUNT(*) FROM pipeline
		WHERE idempotency_key IS NOT NULL OR request_hash IS NOT NULL
	`); err != nil {
		t.Fatalf("count legacy idempotency metadata: %v", err)
	}
	if idempotentMetadataCount != 0 {
		t.Fatalf("legacy endpoint populated idempotency metadata for %d pipelines", idempotentMetadataCount)
	}
}

func setupIdempotencyEndpointTest(t *testing.T) (*ExternalServer, *sqlx.DB, http.Handler) {
	t.Helper()

	dsn := fmt.Sprintf(
		"file:external_idempotency_%d?mode=memory&cache=shared&_pragma=foreign_keys(ON)&_pragma=busy_timeout(5000)",
		time.Now().UnixNano(),
	)
	db, err := sqlx.Open("sqlite", dsn)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	// Permit the concurrent-create test to reach the database through distinct
	// connections; the unique index, rather than a process-local serialisation
	// artefact, must remain the deduplication authority.
	db.SetMaxOpenConns(4)
	db.SetMaxIdleConns(4)

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
	CREATE UNIQUE INDEX uq_pipeline_application_idempotency_key
		ON pipeline (application_id, idempotency_key);
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
	CREATE TABLE keyword (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		key TEXT NOT NULL,
		value TEXT NOT NULL
	);
	CREATE TABLE pipeline_keyword (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		pipeline_id INTEGER NOT NULL,
		keyword_id INTEGER NOT NULL
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

	for _, apiKey := range []struct {
		applicationID int
		value         string
	}{
		{applicationID: idempotencyTestApplicationID, value: idempotencyTestAPIKey},
		{applicationID: idempotencyTestOtherApplicationID, value: idempotencyTestOtherAPIKey},
	} {
		if _, err := db.Exec(`
			INSERT INTO api_key (application_id, key, created_at, disabled_at, expires_at)
			VALUES ($1, $2, $3, NULL, NULL)
		`, apiKey.applicationID, apiKey.value, time.Now().UTC()); err != nil {
			_ = db.Close()
			t.Fatalf("insert api key: %v", err)
		}
	}

	t.Cleanup(func() {
		_ = db.Close()
	})

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	st := store.New(db, logger)
	server := NewExternalServer(config.APIConfig{}, st, nil, logger)

	router := chi.NewRouter()
	router.Use(middleware.RequestID)
	router.Post("/pipelines", server.handleCreatePipeline)
	router.Post("/pipelines/idempotent", server.handleCreatePipelineIdempotent)
	router.Post("/pipelines/by-idempotency-key", server.handleGetPipelineByIdempotencyKey)
	return server, db, router
}

func idempotencyPipelineRequest(idempotencyKey, pipelineName string) map[string]any {
	return map[string]any{
		"idempotencyKey": idempotencyKey,
		"name":           pipelineName,
		"stages": []map[string]any{
			{
				"stageName":        "ensure-claim",
				"stageHandlerName": "EnsureClaimHandler",
				"input":            `{"privateInsuranceProcessId":"process-1"}`,
			},
		},
		"pipelineKeywords": []map[string]any{
			{"key": "workflow", "value": "private-insurance-claim"},
		},
		"pipelineContextItems": []map[string]any{
			{"key": "tenantId", "value": `"tenant-1"`, "valueType": "System.String"},
			{"key": "privateInsuranceProcessId", "value": `"process-1"`, "valueType": "System.String"},
		},
	}
}

func serveIdempotencyJSON(
	t *testing.T,
	router http.Handler,
	method string,
	path string,
	apiKey string,
	body any,
) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	request := httptest.NewRequest(method, path, bytes.NewReader(raw))
	request.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		request.Header.Set("X-API-Key", apiKey)
	}
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	return recorder
}

func decodeIdempotentCreateResponse(
	t *testing.T,
	recorder *httptest.ResponseRecorder,
) types.IdempotentPipelineCreateResponse {
	t.Helper()
	var response types.IdempotentPipelineCreateResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode idempotent response: %v; body=%s", err, recorder.Body.String())
	}
	return response
}

func assertPipelineCount(t *testing.T, db *sqlx.DB, expected int) {
	t.Helper()
	var count int
	if err := db.Get(&count, `SELECT COUNT(*) FROM pipeline`); err != nil {
		t.Fatalf("count pipelines: %v", err)
	}
	if count != expected {
		t.Fatalf("pipeline count = %d, want %d", count, expected)
	}
}

func assertStageCount(t *testing.T, db *sqlx.DB, pipelineID, expected int) {
	t.Helper()
	var count int
	if err := db.Get(&count, `SELECT COUNT(*) FROM stage WHERE pipeline_id = $1`, pipelineID); err != nil {
		t.Fatalf("count stages: %v", err)
	}
	if count != expected {
		t.Fatalf("stage count = %d, want %d", count, expected)
	}
}
