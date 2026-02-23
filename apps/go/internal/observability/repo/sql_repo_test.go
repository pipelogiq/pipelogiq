package repo

import (
	"context"
	"testing"
	"time"

	"github.com/jmoiron/sqlx"
	_ "modernc.org/sqlite"

	"pipelogiq/internal/observability/model"
)

func TestSQLRepository_UpsertAndGetIntegration(t *testing.T) {
	db := setupTestDB(t)
	repository := NewSQLRepository(db)
	ctx := context.Background()

	if err := repository.EnsureIntegrations(ctx, model.SupportedIntegrationTypes); err != nil {
		t.Fatalf("EnsureIntegrations() error = %v", err)
	}

	config := map[string]any{
		"endpoint": "http://localhost:4318",
		"protocol": "http",
	}

	if err := repository.UpsertIntegrationConfig(ctx, model.IntegrationTypeOpenTelemetry, config, model.IntegrationStatusConfigured); err != nil {
		t.Fatalf("UpsertIntegrationConfig() error = %v", err)
	}

	got, err := repository.GetIntegration(ctx, model.IntegrationTypeOpenTelemetry)
	if err != nil {
		t.Fatalf("GetIntegration() error = %v", err)
	}
	if got == nil {
		t.Fatal("GetIntegration() returned nil")
	}
	if got.Status != model.IntegrationStatusConfigured {
		t.Fatalf("status = %s, want %s", got.Status, model.IntegrationStatusConfigured)
	}
	if got.Config["endpoint"] != "http://localhost:4318" {
		t.Fatalf("endpoint = %#v, want %q", got.Config["endpoint"], "http://localhost:4318")
	}
}

func TestSQLRepository_RecordHealthSuccessAndFailure(t *testing.T) {
	db := setupTestDB(t)
	repository := NewSQLRepository(db)
	ctx := context.Background()

	if err := repository.EnsureIntegrations(ctx, []model.IntegrationType{model.IntegrationTypeOpenTelemetry}); err != nil {
		t.Fatalf("EnsureIntegrations() error = %v", err)
	}

	config := map[string]any{
		"endpoint": "collector:4317",
		"protocol": "grpc",
	}
	if err := repository.UpsertIntegrationConfig(ctx, model.IntegrationTypeOpenTelemetry, config, model.IntegrationStatusConfigured); err != nil {
		t.Fatalf("UpsertIntegrationConfig() error = %v", err)
	}

	successAt := time.Now().UTC().Add(-5 * time.Minute)
	if err := repository.RecordHealthSuccess(ctx, model.IntegrationTypeOpenTelemetry, successAt); err != nil {
		t.Fatalf("RecordHealthSuccess() error = %v", err)
	}

	integration, err := repository.GetIntegration(ctx, model.IntegrationTypeOpenTelemetry)
	if err != nil {
		t.Fatalf("GetIntegration() error = %v", err)
	}
	if integration == nil || integration.Health.LastSuccessAt == nil {
		t.Fatalf("LastSuccessAt = %#v, want non-nil", integration)
	}

	failureAt := time.Now().UTC()
	if err := repository.RecordHealthFailure(ctx, model.IntegrationTypeOpenTelemetry, failureAt, "dial timeout"); err != nil {
		t.Fatalf("RecordHealthFailure() error = %v", err)
	}

	integration, err = repository.GetIntegration(ctx, model.IntegrationTypeOpenTelemetry)
	if err != nil {
		t.Fatalf("GetIntegration() error = %v", err)
	}
	if integration.Health.LastError == nil || *integration.Health.LastError != "dial timeout" {
		t.Fatalf("LastError = %#v, want %q", integration.Health.LastError, "dial timeout")
	}
	if integration.Health.LastTestedAt == nil {
		t.Fatal("LastTestedAt = nil, want non-nil")
	}
}

func setupTestDB(t *testing.T) *sqlx.DB {
	t.Helper()

	db, err := sqlx.Open("sqlite", "file:observability_repo_test?mode=memory&cache=shared&_pragma=foreign_keys(ON)")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}

	schema := `
	CREATE TABLE observability_integration_config (
		type TEXT PRIMARY KEY,
		config_json TEXT NOT NULL DEFAULT '{}',
		status TEXT NOT NULL DEFAULT 'not_configured',
		created_at TIMESTAMP NOT NULL,
		updated_at TIMESTAMP NOT NULL
	);
	CREATE TABLE observability_integration_health (
		type TEXT PRIMARY KEY REFERENCES observability_integration_config(type),
		last_tested_at TIMESTAMP NULL,
		last_success_at TIMESTAMP NULL,
		last_error TEXT NULL,
		export_rate_per_min REAL NOT NULL DEFAULT 0,
		drop_rate REAL NOT NULL DEFAULT 0
	);
	`

	if _, err := db.Exec(schema); err != nil {
		_ = db.Close()
		t.Fatalf("create schema: %v", err)
	}

	t.Cleanup(func() {
		_ = db.Close()
	})

	return db
}
