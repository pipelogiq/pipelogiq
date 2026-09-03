package store

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/jmoiron/sqlx"
	_ "modernc.org/sqlite"

	"pipelogiq/internal/types"
)

func TestPipelineContextItem_AllowsLargeValueAndLongValueType(t *testing.T) {
	st, db := setupContextItemTestStore(t)

	pipelineID := insertContextPipelineRow(t, db, "context-pipeline", types.PipelineStatusRunning)

	largeValue := fmt.Sprintf(`{"agent":"planner","messages":[%q,%q,%q],"metadata":{"trace":%q}}`,
		strings.Repeat("alpha", 400),
		strings.Repeat("beta", 400),
		strings.Repeat("gamma", 400),
		strings.Repeat("trace-", 250),
	)
	longValueType := strings.Repeat("com.example.deep.namespace.TypeName", 8)
	tx, err := db.BeginTxx(context.Background(), nil)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	if err := st.insertContextItems(context.Background(), tx, pipelineID, []types.ContextItem{
		{
			Key:       "sdk.type",
			Value:     largeValue,
			ValueType: longValueType,
		},
	}); err != nil {
		t.Fatalf("insertContextItems() error = %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit insert tx: %v", err)
	}

	if _, err := db.ExecContext(context.Background(), `
		UPDATE pipeline_context_item SET value = $1, value_type = $2
		WHERE pipeline_id = $3 AND key = $4
	`, largeValue, longValueType, pipelineID, "sdk.type"); err != nil {
		t.Fatalf("update context item: %v", err)
	}

	var storedValue string
	var storedValueType string
	if err := db.QueryRow(`
		SELECT value, value_type FROM pipeline_context_item WHERE pipeline_id = $1 AND key = $2
	`, pipelineID, "sdk.type").Scan(&storedValue, &storedValueType); err != nil {
		t.Fatalf("scan stored context item: %v", err)
	}

	if storedValue != largeValue {
		t.Fatalf("stored value length = %d, want %d", len(storedValue), len(largeValue))
	}
	if storedValueType != longValueType {
		t.Fatalf("stored value_type length = %d, want %d", len(storedValueType), len(longValueType))
	}
}

func setupContextItemTestStore(t *testing.T) (*Store, *sqlx.DB) {
	t.Helper()

	dsn := fmt.Sprintf(
		"file:context_item_test_%d?mode=memory&cache=shared&_pragma=foreign_keys(ON)&_pragma=busy_timeout(5000)",
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
		trace_id TEXT NULL
	);
	CREATE TABLE pipeline_context_item (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		key TEXT NOT NULL,
		value TEXT NOT NULL,
		value_type TEXT NOT NULL,
		is_sensitive BOOLEAN NOT NULL DEFAULT 0,
		pipeline_id INTEGER NOT NULL
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

func insertContextPipelineRow(t *testing.T, db *sqlx.DB, name, status string) int {
	t.Helper()

	var id int
	if err := db.QueryRow(`
		INSERT INTO pipeline (name, status, created_at, is_completed)
		VALUES ($1, $2, $3, false)
		RETURNING id
	`, name, status, time.Now().UTC()).Scan(&id); err != nil {
		t.Fatalf("insert pipeline: %v", err)
	}
	return id
}
