//go:build pro

package pro

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestAuditExportRedactsCurrentAndHistoricalSecrets(t *testing.T) {
	db := setupProStore(t)
	db.SetMaxOpenConns(1)
	pipelineID := seedPipeline(t, db, 1, false, time.Time{})
	foreignPipelineID := seedPipeline(t, db, 2, false, time.Time{})
	now := time.Now().UTC()
	for _, statement := range []struct {
		query string
		args  []any
	}{
		{`INSERT INTO pipeline_context_item (pipeline_id, key, value, is_sensitive) VALUES ($1,'password','"current-secret"',true),($1,'public','keep-public',false)`, []any{pipelineID}},
		{`INSERT INTO pipeline_redaction_secret (pipeline_id,value) VALUES ($1,'old-secret')`, []any{pipelineID}},
		{`INSERT INTO stage_log (stage_id, log, log_level, created_at) SELECT id, $1, 'AUDIT', $2 FROM stage WHERE pipeline_id=$3`, []any{`current-secret "current-secret" old-secret keep-public`, now, pipelineID}},
		{`INSERT INTO stage_log (stage_id, log, log_level, created_at) SELECT id, 'foreign-message', 'AUDIT', $1 FROM stage WHERE pipeline_id=$2`, []any{now, foreignPipelineID}},
	} {
		if _, err := db.Exec(statement.query, statement.args...); err != nil {
			t.Fatal(err)
		}
	}
	var output bytes.Buffer
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	result, err := newAuditExport(db).Export(ctx, AuditExportRequest{ApplicationID: 1, From: now.Add(-time.Minute), To: now.Add(time.Minute)}, &output)
	if err != nil {
		t.Fatal(err)
	}
	if result.Records != 1 {
		t.Fatalf("records = %d, want 1", result.Records)
	}
	var record struct {
		Details struct {
			Message string `json:"message"`
		} `json:"details"`
	}
	if err := json.NewDecoder(bytes.NewReader(output.Bytes())).Decode(&record); err != nil {
		t.Fatal(err)
	}
	if record.Details.Message != "[REDACTED] [REDACTED] [REDACTED] keep-public" {
		t.Fatalf("export did not consistently mask secrets: %q", record.Details.Message)
	}
	if strings.Contains(output.String(), "foreign-message") {
		t.Fatal("export included another application's audit message")
	}
}

func TestAuditExportFailsClosedWhenRedactionHistoryUnavailable(t *testing.T) {
	db := setupProStore(t)
	pipelineID := seedPipeline(t, db, 1, false, time.Time{})
	now := time.Now().UTC()
	if _, err := db.Exec(`DROP TABLE pipeline_redaction_secret; INSERT INTO stage_log (stage_id, log, log_level, created_at) SELECT id,'secret-from-history','AUDIT',$1 FROM stage WHERE pipeline_id=$2`, now, pipelineID); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	_, err := newAuditExport(db).Export(context.Background(), AuditExportRequest{ApplicationID: 1, From: now.Add(-time.Minute), To: now.Add(time.Minute)}, &output)
	if err == nil {
		t.Fatal("missing redaction history must fail export")
	}
	if strings.Contains(output.String(), "secret-from-history") {
		t.Fatal("export leaked raw audit messages after a redaction lookup failed")
	}
}
