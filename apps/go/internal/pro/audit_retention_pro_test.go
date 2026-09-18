//go:build pro

package pro

import (
	"bufio"
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jmoiron/sqlx"
	_ "modernc.org/sqlite"
)

const proTestSchema = `
CREATE TABLE application (id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT NOT NULL);
CREATE TABLE pipeline (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	name TEXT NOT NULL,
	status TEXT,
	created_at TIMESTAMP NOT NULL,
	finished_at TIMESTAMP NULL,
	is_completed BOOLEAN NOT NULL DEFAULT 0,
	application_id INTEGER NULL
);
CREATE TABLE stage (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	name TEXT NOT NULL,
	stage_handler_name TEXT NULL,
	status TEXT NULL,
	pipeline_id INTEGER NOT NULL,
	created_at TIMESTAMP NOT NULL
);
CREATE TABLE stage_io (id INTEGER PRIMARY KEY AUTOINCREMENT, input TEXT, output TEXT, stage_id INTEGER NOT NULL);
CREATE TABLE stage_options (id INTEGER PRIMARY KEY AUTOINCREMENT, stage_id INTEGER NOT NULL);
CREATE TABLE stage_log (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	log TEXT NULL,
	log_level TEXT NULL,
	created_at TIMESTAMP NULL,
	stage_id INTEGER NOT NULL
);
CREATE TABLE pipeline_redaction_secret (pipeline_id INTEGER NOT NULL, value TEXT NOT NULL);
CREATE TABLE pipeline_context_item (id INTEGER PRIMARY KEY AUTOINCREMENT, pipeline_id INTEGER NOT NULL, key TEXT, value TEXT, is_sensitive BOOLEAN NOT NULL DEFAULT 0);
CREATE TABLE pipeline_keyword (id INTEGER PRIMARY KEY AUTOINCREMENT, pipeline_id INTEGER NOT NULL, keyword_id INTEGER NOT NULL);
CREATE TABLE policy_event (
	id TEXT PRIMARY KEY,
	policy_id TEXT NOT NULL,
	ts TIMESTAMP NOT NULL,
	actor TEXT NOT NULL,
	type TEXT NOT NULL,
	details TEXT NOT NULL DEFAULT '{}'
);
CREATE TABLE retention_policy (
	application_id INTEGER PRIMARY KEY,
	keep_days INTEGER NOT NULL DEFAULT 90,
	enabled BOOLEAN NOT NULL DEFAULT 0,
	archive_target TEXT NULL,
	updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
`

var proTestCounter atomic.Uint64

func setupProStore(t *testing.T) *sqlx.DB {
	t.Helper()

	dsn := fmt.Sprintf(
		"file:pro_test_%d?mode=memory&cache=shared&_pragma=foreign_keys(ON)&_pragma=busy_timeout(5000)",
		proTestCounter.Add(1),
	)
	db, err := sqlx.Open("sqlite", dsn)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if _, err = db.Exec(proTestSchema); err != nil {
		_ = db.Close()
		t.Fatalf("create schema: %v", err)
	}
	if _, err = db.Exec(`INSERT INTO application (id, name) VALUES (1, 'tiqet'), (2, 'other')`); err != nil {
		_ = db.Close()
		t.Fatalf("seed applications: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestAuditExportFormat(t *testing.T) {
	db := setupProStore(t)
	ctx := context.Background()

	base := time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)
	for i := 0; i < 3; i++ {
		if _, err := db.Exec(`
			INSERT INTO policy_event (id, policy_id, ts, actor, type, details)
			VALUES ($1, $2, $3, $4, $5, $6)
		`, fmt.Sprintf("evt-%d", i), "policy-1", base.Add(time.Duration(i)*time.Minute),
			"user:10", "updated", `{"field":"status"}`); err != nil {
			t.Fatalf("seed policy event: %v", err)
		}
	}
	// An event outside the range must not appear.
	if _, err := db.Exec(`
		INSERT INTO policy_event (id, policy_id, ts, actor, type, details)
		VALUES ('evt-old', 'policy-1', $1, 'user:10', 'created', '{}')
	`, base.AddDate(0, 0, -60)); err != nil {
		t.Fatalf("seed old event: %v", err)
	}

	seed := bytes.Repeat([]byte{7}, ed25519.SeedSize)
	t.Setenv("AUDIT_EXPORT_SIGNING_KEY", base64.StdEncoding.EncodeToString(seed))

	var out bytes.Buffer
	result, err := newAuditExport(db).Export(ctx, AuditExportRequest{
		ApplicationID: 1,
		From:          base.Add(-time.Hour),
		To:            base.Add(time.Hour),
	}, &out)
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	if result.Records != 3 {
		t.Fatalf("records = %d, want 3", result.Records)
	}
	if result.Algorithm != "ed25519" || result.Signature == "" {
		t.Fatalf("a configured key must produce a signature: %+v", result)
	}

	// The file is NDJSON: one object per line, records then a trailer.
	lines := []string{}
	scanner := bufio.NewScanner(bytes.NewReader(out.Bytes()))
	for scanner.Scan() {
		if strings.TrimSpace(scanner.Text()) != "" {
			lines = append(lines, scanner.Text())
		}
	}
	if len(lines) != 4 {
		t.Fatalf("wrote %d lines, want 3 records and a trailer", len(lines))
	}

	digest := sha256.New()
	for i, line := range lines[:3] {
		var record auditRecord
		if err = json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatalf("line %d is not JSON: %v", i, err)
		}
		if record.Type != "record" || record.Actor != "user:10" {
			t.Fatalf("line %d = %+v", i, record)
		}
		if !strings.HasPrefix(record.Action, "policy.") {
			t.Fatalf("action = %q", record.Action)
		}
		digest.Write([]byte(line))
		digest.Write([]byte("\n"))
	}

	var trailer exportTrailer
	if err = json.Unmarshal([]byte(lines[3]), &trailer); err != nil {
		t.Fatalf("trailer is not JSON: %v", err)
	}
	if trailer.Type != "trailer" || trailer.Records != 3 {
		t.Fatalf("trailer = %+v", trailer)
	}

	// The signature must verify against the digest of the records above it, so a
	// consumer can check an export without the database.
	want := base64.StdEncoding.EncodeToString(digest.Sum(nil))
	if trailer.Digest != want {
		t.Fatalf("digest = %s, want %s", trailer.Digest, want)
	}
	rawDigest, err := base64.StdEncoding.DecodeString(trailer.Digest)
	if err != nil {
		t.Fatalf("decode digest: %v", err)
	}
	rawSignature, err := base64.StdEncoding.DecodeString(trailer.Signature)
	if err != nil {
		t.Fatalf("decode signature: %v", err)
	}
	if !ed25519.Verify(ed25519.NewKeyFromSeed(seed).Public().(ed25519.PublicKey), rawDigest, rawSignature) {
		t.Fatalf("the trailer signature does not verify")
	}

	// Dropping a record from the middle must break verification.
	tampered := sha256.New()
	for _, line := range []string{lines[0], lines[2]} {
		tampered.Write([]byte(line))
		tampered.Write([]byte("\n"))
	}
	if base64.StdEncoding.EncodeToString(tampered.Sum(nil)) == trailer.Digest {
		t.Fatalf("removing a record must change the digest")
	}
}

func TestAuditExportWithoutAKeyIsVisiblyUnsigned(t *testing.T) {
	db := setupProStore(t)
	t.Setenv("AUDIT_EXPORT_SIGNING_KEY", "")

	var out bytes.Buffer
	result, err := newAuditExport(db).Export(context.Background(), AuditExportRequest{
		ApplicationID: 1,
		From:          time.Now().Add(-time.Hour),
		To:            time.Now(),
	}, &out)
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	// An unsigned export says so rather than looking signed.
	if result.Algorithm != "none" || result.Signature != "" {
		t.Fatalf("result = %+v, want an explicitly unsigned export", result)
	}
}

func TestAuditExportRejectsBadRanges(t *testing.T) {
	db := setupProStore(t)
	var out bytes.Buffer

	if _, err := newAuditExport(db).Export(context.Background(), AuditExportRequest{
		ApplicationID: 0,
		From:          time.Now().Add(-time.Hour),
		To:            time.Now(),
	}, &out); err == nil {
		t.Fatalf("an export without an application must be refused")
	}

	if _, err := newAuditExport(db).Export(context.Background(), AuditExportRequest{
		ApplicationID: 1,
		From:          time.Now(),
		To:            time.Now().Add(-time.Hour),
	}, &out); err == nil {
		t.Fatalf("an inverted range must be refused")
	}
}

// seedPipeline inserts a pipeline with one stage, its IO, and a log line.
func seedPipeline(
	t *testing.T,
	db *sqlx.DB,
	applicationID int,
	completed bool,
	finishedAt time.Time,
) int {
	t.Helper()

	var finished any
	if !finishedAt.IsZero() {
		finished = finishedAt
	}

	var pipelineID int
	if err := db.QueryRow(`
		INSERT INTO pipeline (name, status, created_at, finished_at, is_completed, application_id)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id
	`, "p", "Completed", time.Now().UTC(), finished, completed, applicationID).Scan(&pipelineID); err != nil {
		t.Fatalf("insert pipeline: %v", err)
	}

	var stageID int
	if err := db.QueryRow(`
		INSERT INTO stage (name, stage_handler_name, status, pipeline_id, created_at)
		VALUES ('s', 'h', 'Completed', $1, $2)
		RETURNING id
	`, pipelineID, time.Now().UTC()).Scan(&stageID); err != nil {
		t.Fatalf("insert stage: %v", err)
	}

	for _, statement := range []struct {
		query string
		args  []any
	}{
		{`INSERT INTO stage_io (stage_id, input) VALUES ($1, '{}')`, []any{stageID}},
		{`INSERT INTO stage_options (stage_id) VALUES ($1)`, []any{stageID}},
		{`INSERT INTO stage_log (log, log_level, created_at, stage_id) VALUES ('x', 'INFO', $1, $2)`,
			[]any{time.Now().UTC(), stageID}},
		{`INSERT INTO pipeline_context_item (pipeline_id, key, value) VALUES ($1, 'k', 'v')`, []any{pipelineID}},
		{`INSERT INTO pipeline_keyword (pipeline_id, keyword_id) VALUES ($1, 1)`, []any{pipelineID}},
	} {
		if _, err := db.Exec(statement.query, statement.args...); err != nil {
			t.Fatalf("seed %q: %v", statement.query, err)
		}
	}
	return pipelineID
}

func countRows(t *testing.T, db *sqlx.DB, table string) int {
	t.Helper()
	var count int
	if err := db.Get(&count, "SELECT COUNT(*) FROM "+table); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return count
}

func TestRetentionPurge(t *testing.T) {
	db := setupProStore(t)
	ctx := context.Background()
	retention := newRetention(db)
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)

	old := seedPipeline(t, db, 1, true, now.AddDate(0, 0, -120))
	recent := seedPipeline(t, db, 1, true, now.AddDate(0, 0, -5))
	unfinished := seedPipeline(t, db, 1, false, time.Time{})
	foreign := seedPipeline(t, db, 2, true, now.AddDate(0, 0, -120))

	t.Run("no policy means the Community behaviour", func(t *testing.T) {
		policy, err := retention.Policy(ctx, 1)
		if err != nil {
			t.Fatalf("Policy: %v", err)
		}
		if policy.Enabled {
			t.Fatalf("an application with no policy must not purge: %+v", policy)
		}

		result, err := retention.Purge(ctx, 1, now)
		if err != nil {
			t.Fatalf("Purge: %v", err)
		}
		if result.Pipelines != 0 {
			t.Fatalf("purged %d pipelines with no policy, want 0", result.Pipelines)
		}
		if countRows(t, db, "pipeline") != 4 {
			t.Fatalf("nothing must be deleted without a policy")
		}
	})

	t.Run("an invalid window is refused", func(t *testing.T) {
		for _, days := range []int{0, 7, 45, 400} {
			if err := retention.SetPolicy(ctx, RetentionPolicy{
				ApplicationID: 1, KeepDays: days, Enabled: true,
			}); err == nil {
				t.Fatalf("keepDays=%d must be refused", days)
			}
		}
	})

	t.Run("purges only finished pipelines of this application past the window", func(t *testing.T) {
		if err := retention.SetPolicy(ctx, RetentionPolicy{
			ApplicationID: 1, KeepDays: 30, Enabled: true,
		}); err != nil {
			t.Fatalf("SetPolicy: %v", err)
		}

		result, err := retention.Purge(ctx, 1, now)
		if err != nil {
			t.Fatalf("Purge: %v", err)
		}
		if result.Pipelines != 1 {
			t.Fatalf("purged %d pipelines, want exactly the one past the window", result.Pipelines)
		}
		if result.Stages != 1 || result.Logs != 1 {
			t.Fatalf("result = %+v, want its stage and log removed too", result)
		}

		survivors := map[int]bool{}
		rows, err := db.Query(`SELECT id FROM pipeline`)
		if err != nil {
			t.Fatalf("list pipelines: %v", err)
		}
		defer rows.Close()
		for rows.Next() {
			var id int
			if err = rows.Scan(&id); err != nil {
				t.Fatalf("scan: %v", err)
			}
			survivors[id] = true
		}

		if survivors[old] {
			t.Fatalf("the pipeline past the window survived")
		}
		if !survivors[recent] {
			t.Fatalf("a pipeline inside the window was deleted")
		}
		// An unfinished pipeline is never eligible, whatever the window says: a
		// pipeline running for a year is a problem to investigate, not data to
		// delete.
		if !survivors[unfinished] {
			t.Fatalf("an unfinished pipeline was deleted")
		}
		if !survivors[foreign] {
			t.Fatalf("another application's pipeline was deleted")
		}

		// Everything hanging off the purged pipeline goes with it.
		if countRows(t, db, "stage") != 3 ||
			countRows(t, db, "stage_io") != 3 ||
			countRows(t, db, "stage_options") != 3 ||
			countRows(t, db, "stage_log") != 3 ||
			countRows(t, db, "pipeline_context_item") != 3 ||
			countRows(t, db, "pipeline_keyword") != 3 {
			t.Fatalf("purge left orphans behind")
		}
	})

	t.Run("a second purge is a no-op", func(t *testing.T) {
		result, err := retention.Purge(ctx, 1, now)
		if err != nil {
			t.Fatalf("Purge: %v", err)
		}
		if result.Pipelines != 0 {
			t.Fatalf("purged %d pipelines on a second run, want 0", result.Pipelines)
		}
	})

	t.Run("the policy round-trips", func(t *testing.T) {
		if err := retention.SetPolicy(ctx, RetentionPolicy{
			ApplicationID: 1, KeepDays: 365, Enabled: true, ArchiveTarget: "s3://bucket/prefix",
		}); err != nil {
			t.Fatalf("SetPolicy: %v", err)
		}
		policy, err := retention.Policy(ctx, 1)
		if err != nil {
			t.Fatalf("Policy: %v", err)
		}
		if policy.KeepDays != 365 || !policy.Enabled || policy.ArchiveTarget != "s3://bucket/prefix" {
			t.Fatalf("policy = %+v", policy)
		}
	})

	t.Run("disabling stops the purge", func(t *testing.T) {
		if err := retention.SetPolicy(ctx, RetentionPolicy{
			ApplicationID: 1, KeepDays: 30, Enabled: false,
		}); err != nil {
			t.Fatalf("SetPolicy: %v", err)
		}
		before := countRows(t, db, "pipeline")
		if _, err := retention.Purge(ctx, 1, now.AddDate(1, 0, 0)); err != nil {
			t.Fatalf("Purge: %v", err)
		}
		if countRows(t, db, "pipeline") != before {
			t.Fatalf("a disabled policy must delete nothing")
		}
	})
}
