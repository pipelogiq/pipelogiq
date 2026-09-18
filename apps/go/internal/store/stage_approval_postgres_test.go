package store

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/jmoiron/sqlx"

	"pipelogiq/internal/types"
)

// Copy the migrated table definitions into a private schema. In production
// stage.status is varchar(255); SQLite and synthetic TEXT tables cannot detect
// PostgreSQL's conflicting parameter inference for SET status=$1 + CASE $1.
func TestPostgresApprovalUsesMigratedColumnTypes(t *testing.T) {
	dsn := os.Getenv("PIPELOGIQ_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("requires migrated isolated PostgreSQL PIPELOGIQ_TEST_DATABASE_URL")
	}
	admin, err := sqlx.Connect("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	schema := fmt.Sprintf("sdk_approval_%d", time.Now().UnixNano())
	if _, err := admin.Exec(`CREATE SCHEMA ` + schema); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, err := admin.Exec(`DROP SCHEMA ` + schema + ` CASCADE`); err != nil {
			t.Error(err)
		}
	}()
	for _, table := range []string{"pipeline", "stage", "stage_io", "stage_options", "stage_log", "pipeline_context_item"} {
		if _, err := admin.Exec(`CREATE TABLE ` + schema + `.` + table + ` (LIKE public.` + table + ` INCLUDING DEFAULTS INCLUDING IDENTITY)`); err != nil {
			t.Fatal(err)
		}
	}
	u, err := url.Parse(dsn)
	if err != nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") {
		t.Fatal("PIPELOGIQ_TEST_DATABASE_URL must be a PostgreSQL URL")
	}
	query := u.Query()
	query.Set("search_path", schema)
	u.RawQuery = query.Encode()
	db, err := sqlx.Connect("pgx", u.String())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	st := New(db, slog.New(slog.NewTextHandler(io.Discard, nil)))
	for index, approved := range []bool{true, false} {
		t.Run(fmt.Sprintf("approved_%t", approved), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			pipelineID := index + 1
			stageID := pipelineID * 10
			if _, err := db.Exec(`INSERT INTO pipeline (id,name,status,created_at,is_completed) VALUES ($1,'approval-regression','Running',now(),false)`, pipelineID); err != nil {
				t.Fatal(err)
			}
			if _, err := db.Exec(`INSERT INTO stage (id,pipeline_id,name,stage_handler_name,status,created_at,is_skipped)
				VALUES ($1,$2,'confirmation','AgentConfirmationHandler','WaitingForApproval',now(),false),
				($3,$2,'mutation','AgentToolHandler','NotStarted',now(),false),
				($4,$2,'responder','AgentResponderHandler','NotStarted',now(),false)`, stageID, pipelineID, stageID+1, stageID+2); err != nil {
				t.Fatal(err)
			}
			reason := "Lab regression rejected mutation"
			request := types.ResumeStageRequest{Approved: approved}
			if !approved {
				request.RejectionReason = &reason
			}
			if err := st.ResumeStageApproval(ctx, stageID, request, "local-regression"); err != nil {
				t.Fatalf("resume on migrated column types: %v", err)
			}
			if err := st.ResumeStageApproval(ctx, stageID, request, "local-regression"); err != nil {
				t.Fatalf("idempotent repeat: %v", err)
			}
			if err := st.ResumeStageApproval(ctx, stageID, types.ResumeStageRequest{Approved: !approved}, "local-regression"); !errors.Is(err, ErrStageResumeConflict) {
				t.Fatalf("opposite decision should conflict: %v", err)
			}
			var status string
			var skipped bool
			if err := db.QueryRow(`SELECT status,is_skipped FROM stage WHERE id=$1`, stageID).Scan(&status, &skipped); err != nil {
				t.Fatal(err)
			}
			want := types.StageStatusCompleted
			if !approved {
				want = types.StageStatusSkipped
			}
			if status != want || skipped != !approved {
				t.Fatalf("status=%s skipped=%t; want status=%s skipped=%t", status, skipped, want, !approved)
			}
			var mutation string
			if err := db.Get(&mutation, `SELECT status FROM stage WHERE id=$1`, stageID+1); err != nil {
				t.Fatal(err)
			}
			if approved && mutation != types.StageStatusNotStarted || !approved && mutation != types.StageStatusSkipped {
				t.Fatalf("unexpected mutation status after decision: %s", mutation)
			}
		})
	}
	for index, scenario := range []string{"blocked", "failure-continuation", "independent-branch"} {
		t.Run("generic-rejection-"+scenario, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			pid, gate := index+10, (index+10)*10
			if _, err := db.Exec(`INSERT INTO pipeline (id,name,status,created_at,is_completed) VALUES ($1,'generic-rejection','Running',now(),false)`, pid); err != nil {
				t.Fatal(err)
			}
			if _, err := db.Exec(`INSERT INTO stage (id,pipeline_id,name,stage_handler_name,status,created_at,is_skipped)
				VALUES ($1,$2,'root','RootHandler','Completed',now(),false),
				($3,$2,'gate','CustomApprovalHandler','WaitingForApproval',now(),false),
				($4,$2,'successor','SuccessorHandler','NotStarted',now(),false)`, gate-1, pid, gate, gate+1); err != nil {
				t.Fatal(err)
			}
			if scenario == "failure-continuation" {
				if _, err := db.Exec(`INSERT INTO stage_options(stage_id,run_next_if_failed) VALUES($1,true)`, gate+1); err != nil {
					t.Fatal(err)
				}
			}
			if scenario == "independent-branch" {
				if _, err := db.Exec(`INSERT INTO stage_options(stage_id,depends_on) VALUES($1,'root')`, gate+1); err != nil {
					t.Fatal(err)
				}
			}
			if err := st.ResumeStageApproval(ctx, gate, types.ResumeStageRequest{Approved: false}, "generic-regression"); err != nil {
				t.Fatal(err)
			}
			var status string
			var completed bool
			if err := db.QueryRow(`SELECT status,is_completed FROM pipeline WHERE id=$1`, pid).Scan(&status, &completed); err != nil {
				t.Fatal(err)
			}
			if scenario == "blocked" && (status != types.PipelineStatusFailed || !completed) {
				t.Fatalf("generic rejection hung: status=%s completed=%t", status, completed)
			}
			if scenario != "blocked" && completed {
				t.Fatalf("remaining work fenced out: status=%s completed=%t", status, completed)
			}
		})
	}
}
