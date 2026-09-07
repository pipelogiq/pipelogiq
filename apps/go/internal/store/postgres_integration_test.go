//go:build integration

package store

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/jmoiron/sqlx"

	"pipelogiq/internal/types"
)

// These tests run against a real PostgreSQL whose schema was produced by the
// Liquibase changelog — the same path production takes. They exist because the
// unit suite runs on SQLite, which cannot see PostgreSQL-only syntax or index
// behaviour. CI provides PIPELOGIQ_TEST_DATABASE_URL; locally, run
// database/test-reliability-upgrade.sh or point the variable at any migrated DB.
func openPostgres(t *testing.T) *Store {
	t.Helper()
	dsn := os.Getenv("PIPELOGIQ_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("PIPELOGIQ_TEST_DATABASE_URL not set")
	}
	db, err := sqlx.Connect("pgx", dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return New(db, slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn})))
}

func seedApplication(t *testing.T, st *Store, name string) int {
	t.Helper()
	var id int
	if err := st.db.QueryRowx(`INSERT INTO application (name) VALUES ($1) RETURNING id`, name).Scan(&id); err != nil {
		t.Fatalf("seed application: %v", err)
	}
	t.Cleanup(func() {
		_, _ = st.db.Exec(`DELETE FROM api_key WHERE application_id = $1`, id)
		_, _ = st.db.Exec(`DELETE FROM pipeline WHERE application_id = $1`, id)
		_, _ = st.db.Exec(`DELETE FROM application WHERE id = $1`, id)
	})
	return id
}

func TestPostgres_APIKeyIsStoredHashedAndValidates(t *testing.T) {
	st := openPostgres(t)
	ctx := context.Background()
	appID := seedApplication(t, st, "it-keys-"+time.Now().Format("150405.000"))

	key := "it-" + time.Now().Format("150405.000000")
	if _, err := st.db.Exec(`INSERT INTO api_key (application_id, name, key_hash, key_prefix, created_at) VALUES ($1, 'it', $2, $3, now())`,
		appID, HashAPIKey(key), KeyPrefix(key)); err != nil {
		t.Fatalf("insert key: %v", err)
	}

	got, err := st.ValidateAPIKey(ctx, key)
	if err != nil || got != appID {
		t.Fatalf("ValidateAPIKey = %d, %v; want %d", got, err, appID)
	}
	if _, err := st.ValidateAPIKey(ctx, key+"x"); err == nil {
		t.Fatal("a wrong key validated")
	}

	var clear *string
	if err := st.db.Get(&clear, `SELECT key FROM api_key WHERE key_hash = $1`, HashAPIKey(key)); err != nil {
		t.Fatal(err)
	}
	if clear != nil {
		t.Fatal("clear key persisted")
	}
}

func TestPostgres_IdempotentCreateIsUniquePerApplication(t *testing.T) {
	st := openPostgres(t)
	ctx := context.Background()
	appID := seedApplication(t, st, "it-idem-"+time.Now().Format("150405.000"))

	req := types.PipelineCreateRequest{
		Name:   "it-idempotent",
		Stages: []types.StageCreate{{Name: "s1", StageHandler: "H"}},
	}
	first, created, err := st.CreatePipelineIdempotent(ctx, req, appID, "key-1")
	if err != nil || !created {
		t.Fatalf("first create: created=%v err=%v", created, err)
	}
	second, created, err := st.CreatePipelineIdempotent(ctx, req, appID, "key-1")
	if err != nil || created || second.ID != first.ID {
		t.Fatalf("replay: created=%v id=%d err=%v; want existing %d", created, second.ID, err, first.ID)
	}

	req.Name = "it-idempotent-different"
	if _, _, err := st.CreatePipelineIdempotent(ctx, req, appID, "key-1"); err == nil {
		t.Fatal("same key with a different request did not conflict")
	}
}

func TestPostgres_ScopeQueriesUseTheRealSchema(t *testing.T) {
	st := openPostgres(t)
	ctx := context.Background()
	appID := seedApplication(t, st, "it-scope-"+time.Now().Format("150405.000"))

	pipeline, err := st.CreatePipeline(ctx, types.PipelineCreateRequest{
		Name:   "it-scope",
		Stages: []types.StageCreate{{Name: "s1", StageHandler: "H"}},
	}, appID)
	if err != nil {
		t.Fatalf("create pipeline: %v", err)
	}
	owner, err := st.PipelineApplicationID(ctx, pipeline.ID)
	if err != nil || owner != appID {
		t.Fatalf("PipelineApplicationID = %d, %v", owner, err)
	}
	if len(pipeline.Stages) == 0 {
		t.Fatal("pipeline has no stages")
	}
	owner, err = st.StageApplicationID(ctx, pipeline.Stages[0].ID)
	if err != nil || owner != appID {
		t.Fatalf("StageApplicationID = %d, %v", owner, err)
	}
}
