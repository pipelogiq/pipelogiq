//go:build pro && integration

package pro

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/jmoiron/sqlx"
)

// Pause the first redaction query after its PostgreSQL snapshot was taken,
// commit a new sensitive value and audit message, then let the export continue.
// A second statement using READ COMMITTED would disclose the new raw message.
func TestPostgresAuditExportUsesConsistentRedactionSnapshot(t *testing.T) {
	dsn := os.Getenv("PIPELOGIQ_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("PIPELOGIQ_TEST_DATABASE_URL not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	admin, err := sqlx.ConnectContext(ctx, "pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	lockID := time.Now().UnixNano()
	schema := fmt.Sprintf("audit_snapshot_%d", lockID)
	if _, err := admin.ExecContext(ctx, `CREATE SCHEMA `+schema); err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = admin.Exec(`DROP SCHEMA ` + schema + ` CASCADE`) }()
	config, err := pgx.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	config.RuntimeParams["search_path"] = schema
	config.RuntimeParams["application_name"] = schema
	db := sqlx.NewDb(stdlib.OpenDB(*config), "pgx")
	defer db.Close()
	_, err = db.ExecContext(ctx, fmt.Sprintf(`
		CREATE TABLE pipeline (id bigint PRIMARY KEY, application_id integer NOT NULL);
		CREATE TABLE stage (id bigint PRIMARY KEY, pipeline_id bigint NOT NULL);
		CREATE TABLE stage_log_storage (id bigint PRIMARY KEY, created_at timestamptz, log text, log_level text, stage_id bigint);
		CREATE TABLE pipeline_context_item (pipeline_id bigint, value text, is_sensitive boolean);
		CREATE TABLE pipeline_redaction_secret (pipeline_id bigint, value text);
		CREATE TABLE policy_event (id text, ts timestamptz, actor text, type text, policy_id text, details text);
		CREATE FUNCTION await_export_gate() RETURNS boolean LANGUAGE plpgsql AS $$
		BEGIN PERFORM pg_advisory_xact_lock(%d); RETURN true; END $$;
		CREATE VIEW stage_log AS SELECT * FROM stage_log_storage WHERE await_export_gate();
		INSERT INTO pipeline VALUES (1,1);
		INSERT INTO stage VALUES (1,1);
		INSERT INTO pipeline_context_item VALUES (1,'initial-secret',true);
	`, lockID))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if _, err := db.ExecContext(ctx, `INSERT INTO stage_log_storage VALUES (1,$1,'initial-secret','AUDIT',1)`, now); err != nil {
		t.Fatal(err)
	}
	lock, err := admin.Connx(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if _, err := lock.ExecContext(ctx, `SELECT pg_advisory_lock($1)`, lockID); err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = lock.ExecContext(context.Background(), `SELECT pg_advisory_unlock($1)`, lockID) }()
	type exportResult struct {
		result AuditExportResult
		output string
		err    error
	}
	completed := make(chan exportResult, 1)
	go func() {
		var output bytes.Buffer
		result, err := newAuditExport(db).Export(ctx, AuditExportRequest{ApplicationID: 1, From: now.Add(-time.Minute), To: now.Add(time.Minute)}, &output)
		completed <- exportResult{result, output.String(), err}
	}()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		var waiting bool
		if err := admin.GetContext(ctx, &waiting, `SELECT EXISTS (SELECT 1 FROM pg_stat_activity WHERE application_name=$1 AND wait_event='advisory')`, schema); err != nil {
			t.Fatal(err)
		}
		if waiting {
			break
		}
		select {
		case result := <-completed:
			t.Fatalf("export ended before snapshot gate: %+v", result)
		case <-ctx.Done():
			t.Fatal("export never reached snapshot gate")
		case <-ticker.C:
		}
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO pipeline_context_item VALUES (1,'new-secret',true); INSERT INTO stage_log_storage VALUES (2,now(),'new-secret','AUDIT',1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := lock.ExecContext(ctx, `SELECT pg_advisory_unlock($1)`, lockID); err != nil {
		t.Fatal(err)
	}
	select {
	case exported := <-completed:
		if exported.err != nil {
			t.Fatal(exported.err)
		}
		if exported.result.Records != 1 || strings.Contains(exported.output, "new-secret") || strings.Contains(exported.output, "initial-secret") || !strings.Contains(exported.output, "[REDACTED]") {
			t.Fatalf("export observed inconsistent redaction snapshot: %+v", exported)
		}
	case <-ctx.Done():
		t.Fatal("export did not finish after releasing snapshot gate")
	}
}
