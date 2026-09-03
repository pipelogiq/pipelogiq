package store

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/jmoiron/sqlx"
	_ "modernc.org/sqlite"

	"pipelogiq/internal/types"
)

type fakeStagePolicyRuntime struct {
	policies []types.Policy
	records  []types.PolicyRuntimeEvaluation
}

func (f *fakeStagePolicyRuntime) RuntimePolicies(context.Context) ([]types.Policy, error) {
	return append([]types.Policy(nil), f.policies...), nil
}

func (f *fakeStagePolicyRuntime) RecordRuntimeEvaluation(_ context.Context, _ types.PolicyRuntimeScope, evaluation types.PolicyRuntimeEvaluation, _ string) error {
	f.records = append(f.records, evaluation)
	return nil
}

func TestResolveStagePoliciesReturnsEffectiveTimeout(t *testing.T) {
	st, db := setupPolicyRuntimeTestStore(t)
	runtime := &fakeStagePolicyRuntime{
		policies: []types.Policy{
			{
				ID:          "timeout-system",
				Name:        "system timeout",
				Source:      types.PolicySourceSystem,
				Type:        types.PolicyTypeTimeout,
				Status:      types.PolicyStatusActive,
				Environment: types.PolicyEnvironmentProd,
				Targeting: types.PolicyTargeting{
					Handlers: []string{"handler.a"},
				},
				Rule: types.PolicyRule{
					TimeoutMs: intPtr(3000),
					AppliesTo: stringPtr("step"),
				},
			},
			{
				ID:          "timeout-1",
				Name:        "inline critical timeout",
				Source:      types.PolicySourcePipelineInline,
				Type:        types.PolicyTypeTimeout,
				Status:      types.PolicyStatusActive,
				Environment: types.PolicyEnvironmentProd,
				Targeting: types.PolicyTargeting{
					Stages:      []string{"stage-1"},
					Handlers:    []string{"handler.a"},
					TagsInclude: []string{"critical"},
				},
				Rule: types.PolicyRule{
					TimeoutMs: intPtr(1000),
					AppliesTo: stringPtr("step"),
				},
			},
		},
	}
	st.SetStagePolicyRuntime(runtime)

	pipelineID := insertPolicyRuntimePipeline(t, db, "effective-timeout", types.PipelineStatusNotStarted, 101)
	insertPolicyRuntimeEnv(t, db, pipelineID, "prod")
	insertPolicyRuntimeTag(t, db, pipelineID, "critical")
	stageID := insertPolicyRuntimeStage(t, db, pipelineID, "stage-1", "handler.a", types.StageStatusNotStarted, nil)
	insertPolicyRuntimeOptions(t, db, stageID, 5)

	resolution, err := st.ResolveStagePolicies(context.Background(), stageID)
	if err != nil {
		t.Fatalf("ResolveStagePolicies() error = %v", err)
	}
	if resolution.Action != types.PolicyRuntimeActionAllow {
		t.Fatalf("action = %q, want allow", resolution.Action)
	}
	if resolution.EffectiveTimeoutMs == nil || *resolution.EffectiveTimeoutMs != 1000 {
		t.Fatalf("effective timeout = %#v, want 1000", resolution.EffectiveTimeoutMs)
	}
	if len(resolution.MatchedPolicies) != 2 {
		t.Fatalf("matched policies = %d, want 2", len(resolution.MatchedPolicies))
	}
	if len(resolution.ResolvedRules) == 0 {
		t.Fatalf("resolved rules = %d, want at least 1", len(resolution.ResolvedRules))
	}

	var timeoutRule *types.PolicyRuntimeResolvedRule
	for i := range resolution.ResolvedRules {
		if resolution.ResolvedRules[i].Type == types.PolicyTypeTimeout {
			timeoutRule = &resolution.ResolvedRules[i]
			break
		}
	}
	if timeoutRule == nil {
		t.Fatal("expected timeout resolved rule")
	}
	if timeoutRule.WinningPolicyID == nil || *timeoutRule.WinningPolicyID != "timeout-1" {
		t.Fatalf("winning timeout policy = %#v, want timeout-1", timeoutRule.WinningPolicyID)
	}

	outcomes := make(map[string]string, len(resolution.MatchedPolicies))
	for _, policy := range resolution.MatchedPolicies {
		outcomes[policy.PolicyID] = policy.Outcome
	}
	if outcomes["timeout-1"] != "selected" {
		t.Fatalf("inline timeout outcome = %q, want selected", outcomes["timeout-1"])
	}
	if outcomes["timeout-system"] != "shadowed" {
		t.Fatalf("system timeout outcome = %q, want shadowed", outcomes["timeout-system"])
	}
}

func TestResolveStagePoliciesMergesSystemAndInlineByPrecedence(t *testing.T) {
	st, db := setupPolicyRuntimeTestStore(t)
	runtime := &fakeStagePolicyRuntime{
		policies: []types.Policy{
			{
				ID:          "retry-system",
				Name:        "system retry",
				Source:      types.PolicySourceSystem,
				Type:        types.PolicyTypeRetry,
				Status:      types.PolicyStatusActive,
				Environment: types.PolicyEnvironmentAll,
				Targeting: types.PolicyTargeting{
					Handlers: []string{"handler.retry"},
				},
				Rule: types.PolicyRule{
					MaxAttempts: intPtr(2),
					Backoff:     stringPtr("fixed"),
					BaseDelayMs: intPtr(100),
				},
			},
			{
				ID:          "retry-inline",
				Name:        "inline retry",
				Source:      types.PolicySourcePipelineInline,
				Type:        types.PolicyTypeRetry,
				Status:      types.PolicyStatusActive,
				Environment: types.PolicyEnvironmentAll,
				Targeting: types.PolicyTargeting{
					Stages:   []string{"stage-retry"},
					Handlers: []string{"handler.retry"},
				},
				Rule: types.PolicyRule{
					MaxAttempts: intPtr(5),
					Backoff:     stringPtr("exponential"),
					BaseDelayMs: intPtr(250),
				},
			},
		},
	}
	st.SetStagePolicyRuntime(runtime)

	pipelineID := insertPolicyRuntimePipeline(t, db, "retry-merge", types.PipelineStatusNotStarted, 101)
	stageID := insertPolicyRuntimeStage(t, db, pipelineID, "stage-retry", "handler.retry", types.StageStatusNotStarted, nil)

	resolution, err := st.ResolveStagePolicies(context.Background(), stageID)
	if err != nil {
		t.Fatalf("ResolveStagePolicies() error = %v", err)
	}

	var retryRule *types.PolicyRuntimeResolvedRule
	for i := range resolution.ResolvedRules {
		if resolution.ResolvedRules[i].Type == types.PolicyTypeRetry {
			retryRule = &resolution.ResolvedRules[i]
			break
		}
	}
	if retryRule == nil {
		t.Fatal("expected retry resolved rule")
	}
	if retryRule.WinningPolicyID == nil || *retryRule.WinningPolicyID != "retry-inline" {
		t.Fatalf("winning retry policy = %#v, want retry-inline", retryRule.WinningPolicyID)
	}

	outcomes := make(map[string]string, len(resolution.MatchedPolicies))
	for _, policy := range resolution.MatchedPolicies {
		outcomes[policy.PolicyID] = policy.Outcome
	}
	if outcomes["retry-inline"] != "selected" {
		t.Fatalf("inline retry outcome = %q, want selected", outcomes["retry-inline"])
	}
	if outcomes["retry-system"] != "shadowed" {
		t.Fatalf("system retry outcome = %q, want shadowed", outcomes["retry-system"])
	}
	if len(resolution.Explain) == 0 {
		t.Fatal("expected explainability entries to be present")
	}
}

func TestGetStageToExecuteThrottlesByRateLimitPolicy(t *testing.T) {
	st, db := setupPolicyRuntimeTestStore(t)
	runtime := &fakeStagePolicyRuntime{
		policies: []types.Policy{
			{
				ID:          "rate-1",
				Name:        "global stage rate limit",
				Source:      types.PolicySourceSystem,
				Type:        types.PolicyTypeRateLimit,
				Status:      types.PolicyStatusActive,
				Environment: types.PolicyEnvironmentAll,
				Targeting: types.PolicyTargeting{
					Handlers: []string{"handler.rate"},
				},
				Rule: types.PolicyRule{
					Limit:         intPtr(1),
					WindowSeconds: intPtr(60),
					KeyBy:         stringPtr("global"),
				},
			},
		},
	}
	st.SetStagePolicyRuntime(runtime)

	pipelineID := insertPolicyRuntimePipeline(t, db, "rate-limited", types.PipelineStatusNotStarted, 101)
	insertPolicyRuntimeStage(t, db, pipelineID, "previous", "handler.rate", types.StageStatusCompleted, timePtr(time.Now().UTC().Add(-10*time.Second)))
	stageID := insertPolicyRuntimeStage(t, db, pipelineID, "candidate", "handler.rate", types.StageStatusNotStarted, nil)

	next, err := st.GetStageToExecute(context.Background())
	if err != nil {
		t.Fatalf("GetStageToExecute() error = %v", err)
	}
	if next != nil {
		t.Fatalf("expected nil stage because policy throttled dispatch, got %+v", next)
	}

	var status string
	var nextRetryAt *time.Time
	if err := db.QueryRow(`SELECT status, next_retry_at FROM stage WHERE id = $1`, stageID).Scan(&status, &nextRetryAt); err != nil {
		t.Fatalf("query throttled stage: %v", err)
	}
	if status != types.StageStatusThrottled {
		t.Fatalf("status = %q, want %q", status, types.StageStatusThrottled)
	}
	if nextRetryAt == nil {
		t.Fatal("expected next_retry_at to be set for throttled stage")
	}
}

func TestGetStageToExecuteBlocksByCircuitBreakerPolicy(t *testing.T) {
	st, db := setupPolicyRuntimeTestStore(t)
	runtime := &fakeStagePolicyRuntime{
		policies: []types.Policy{
			{
				ID:          "cb-1",
				Name:        "handler circuit breaker",
				Source:      types.PolicySourceSystem,
				Type:        types.PolicyTypeCircuitBreaker,
				Status:      types.PolicyStatusActive,
				Environment: types.PolicyEnvironmentAll,
				Targeting: types.PolicyTargeting{
					Handlers: []string{"handler.cb"},
				},
				Rule: types.PolicyRule{
					FailureThreshold: intPtr(1),
					WindowSeconds:    intPtr(300),
					OpenSeconds:      intPtr(120),
					HalfOpenMaxCalls: intPtr(1),
				},
			},
		},
	}
	st.SetStagePolicyRuntime(runtime)

	insertPolicyRuntimeStage(
		t,
		db,
		insertPolicyRuntimePipeline(t, db, "historical-failure", types.PipelineStatusFailed, 101),
		"failed-before",
		"handler.cb",
		types.StageStatusFailed,
		timePtr(time.Now().UTC().Add(-15*time.Second)),
	)
	pipelineID := insertPolicyRuntimePipeline(t, db, "circuit-break", types.PipelineStatusNotStarted, 101)
	stageID := insertPolicyRuntimeStage(t, db, pipelineID, "candidate", "handler.cb", types.StageStatusNotStarted, nil)

	next, err := st.GetStageToExecute(context.Background())
	if err != nil {
		t.Fatalf("GetStageToExecute() error = %v", err)
	}
	if next != nil {
		t.Fatalf("expected nil stage because policy blocked dispatch, got %+v", next)
	}

	var stageStatus string
	var output string
	if err := db.QueryRow(`
		SELECT s.status, COALESCE(io.output, '')
		FROM stage s
		LEFT JOIN stage_io io ON io.stage_id = s.id
		WHERE s.id = $1
	`, stageID).Scan(&stageStatus, &output); err != nil {
		t.Fatalf("query blocked stage: %v", err)
	}
	if stageStatus != types.StageStatusFailed {
		t.Fatalf("stage status = %q, want %q", stageStatus, types.StageStatusFailed)
	}
	if output == "" {
		t.Fatal("expected blocked stage output to contain policy reason")
	}

	var pipelineStatus string
	var completed bool
	if err := db.QueryRow(`SELECT status, is_completed FROM pipeline WHERE id = $1`, pipelineID).Scan(&pipelineStatus, &completed); err != nil {
		t.Fatalf("query pipeline: %v", err)
	}
	if pipelineStatus != types.PipelineStatusFailed || !completed {
		t.Fatalf("pipeline status/completed = %q/%v, want Failed/true", pipelineStatus, completed)
	}
}

func setupPolicyRuntimeTestStore(t *testing.T) (*Store, *sqlx.DB) {
	t.Helper()

	dsn := fmt.Sprintf(
		"file:policy_runtime_test_%d?mode=memory&cache=shared&_pragma=foreign_keys(ON)&_pragma=busy_timeout(5000)",
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
	`

	if _, err = db.Exec(schema); err != nil {
		_ = db.Close()
		t.Fatalf("create schema: %v", err)
	}

	t.Cleanup(func() {
		_ = db.Close()
	})

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return New(db, logger), db
}

func insertPolicyRuntimePipeline(t *testing.T, db *sqlx.DB, name, status string, applicationID int) int {
	t.Helper()
	var id int
	if err := db.QueryRow(`
		INSERT INTO pipeline (name, status, created_at, is_completed, application_id)
		VALUES ($1, $2, $3, false, $4)
		RETURNING id
	`, name, status, time.Now().UTC(), applicationID).Scan(&id); err != nil {
		t.Fatalf("insert pipeline: %v", err)
	}
	return id
}

func insertPolicyRuntimeStage(t *testing.T, db *sqlx.DB, pipelineID int, name, handler, status string, startedAt *time.Time) int {
	t.Helper()
	var startedAtValue any
	var finishedAtValue any
	if startedAt != nil {
		formatted := startedAt.UTC().Format(time.RFC3339Nano)
		startedAtValue = formatted
		finishedAtValue = formatted
	}
	var id int
	if err := db.QueryRow(`
		INSERT INTO stage (
			name, stage_handler_name, status, pipeline_id, created_at, started_at, finished_at, is_skipped, is_event, span_id
		) VALUES ($1, $2, $3, $4, $5, $6, $7, false, false, $8)
		RETURNING id
	`, name, handler, status, pipelineID, time.Now().UTC().Format(time.RFC3339Nano), startedAtValue, finishedAtValue, name+"-span").Scan(&id); err != nil {
		t.Fatalf("insert stage: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO stage_io (stage_id) VALUES ($1)`, id); err != nil {
		t.Fatalf("insert stage_io: %v", err)
	}
	return id
}

func insertPolicyRuntimeOptions(t *testing.T, db *sqlx.DB, stageID int, timeoutSeconds int) {
	t.Helper()
	if _, err := db.Exec(`INSERT INTO stage_options (stage_id, time_out) VALUES ($1, $2)`, stageID, timeoutSeconds); err != nil {
		t.Fatalf("insert stage options: %v", err)
	}
}

func insertPolicyRuntimeEnv(t *testing.T, db *sqlx.DB, pipelineID int, env string) {
	t.Helper()
	if _, err := db.Exec(`
		INSERT INTO pipeline_context_item (key, value, value_type, pipeline_id)
		VALUES ('environment', $1, 'string', $2)
	`, env, pipelineID); err != nil {
		t.Fatalf("insert env context: %v", err)
	}
}

func insertPolicyRuntimeTag(t *testing.T, db *sqlx.DB, pipelineID int, tag string) {
	t.Helper()
	var keywordID int
	if err := db.QueryRow(`INSERT INTO keyword (key, value) VALUES ('tag', $1) RETURNING id`, tag).Scan(&keywordID); err != nil {
		t.Fatalf("insert keyword: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO pipeline_keyword (pipeline_id, keyword_id) VALUES ($1, $2)`, pipelineID, keywordID); err != nil {
		t.Fatalf("link keyword: %v", err)
	}
}

func intPtr(value int) *int {
	return &value
}

func stringPtr(value string) *string {
	return &value
}

func timePtr(value time.Time) *time.Time {
	return &value
}
