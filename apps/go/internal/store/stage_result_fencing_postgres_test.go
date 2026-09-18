package store

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jmoiron/sqlx"

	"pipelogiq/internal/types"
)

// SQLite cannot verify PostgreSQL's row-lock ordering. Run this opt-in test
// against a disposable PostgreSQL; all records live in a unique private schema.
func TestPostgresResultConsumptionRacesLeaseRecovery(t *testing.T) {
	dsn := os.Getenv("PIPELOGIQ_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("requires isolated PostgreSQL PIPELOGIQ_TEST_DATABASE_URL")
	}
	admin, err := sqlx.Connect("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	schema := fmt.Sprintf("sdk_result_fence_%d", time.Now().UnixNano())
	if _, err := admin.Exec(`CREATE SCHEMA ` + schema); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, err := admin.Exec(`DROP SCHEMA ` + schema + ` CASCADE`); err != nil {
			t.Error(err)
		}
	}()
	databaseURL, err := url.Parse(dsn)
	if err != nil || (databaseURL.Scheme != "postgres" && databaseURL.Scheme != "postgresql") {
		t.Fatal("PIPELOGIQ_TEST_DATABASE_URL must be a PostgreSQL URL")
	}
	query := databaseURL.Query()
	query.Set("search_path", schema)
	databaseURL.RawQuery = query.Encode()
	db, err := sqlx.Connect("pgx", databaseURL.String())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ddl := strings.ReplaceAll(schedulingTestSchema, "INTEGER PRIMARY KEY AUTOINCREMENT", "SERIAL PRIMARY KEY")
	ddl = strings.ReplaceAll(ddl, "BOOLEAN NOT NULL DEFAULT 0", "BOOLEAN NOT NULL DEFAULT false")
	if _, err := db.Exec(ddl); err != nil {
		t.Fatal(err)
	}
	st := New(db, slog.New(slog.NewTextHandler(io.Discard, nil)))

	for iteration := 0; iteration < 30; iteration++ {
		t.Run(fmt.Sprintf("race-%02d", iteration), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			pipelineID := insertPipeline(t, db, "result-recovery-race")
			stageID := insertStage(t, db, pipelineID, "claim", "claim-handler")
			executionID := fmt.Sprintf("execution-%d", stageID)
			if _, err := db.ExecContext(ctx, `UPDATE stage
				SET status=$1, execution_id=$2, execution_attempt=1,
				lease_owner='worker-a', lease_expires_at=$3 WHERE id=$4`,
				types.StageStatusRunning, executionID, time.Now().UTC().Add(-time.Minute), stageID,
			); err != nil {
				t.Fatal(err)
			}
			result := types.StageResultMessage{
				PipelineID: &pipelineID, StageID: stageID, ExecutionID: executionID,
				Attempt: 1, IsSuccess: true, Result: "current-result",
			}
			start := make(chan struct{})
			resultDone := make(chan error, 1)
			type recoveryResult struct {
				count int64
				err   error
			}
			recoveryDone := make(chan recoveryResult, 1)
			go func() {
				<-start
				_, err := st.UpdateStageResult(ctx, result)
				resultDone <- err
			}()
			go func() {
				<-start
				count, err := st.RecoverExpiredStageLeases(ctx)
				recoveryDone <- recoveryResult{count, err}
			}()
			close(start)
			resultErr := <-resultDone
			recovery := <-recoveryDone
			if resultErr != nil || recovery.err != nil {
				t.Fatalf("result error=%v recovery error=%v", resultErr, recovery.err)
			}
			var status string
			if err := db.GetContext(ctx, &status, `SELECT status FROM stage WHERE id=$1`, stageID); err != nil {
				t.Fatal(err)
			}
			output := reliabilityStageOutput(t, db, stageID)
			switch status {
			case types.StageStatusCompleted:
				if recovery.count != 0 || output != "current-result" {
					t.Fatalf("result won: recovered=%d output=%q", recovery.count, output)
				}
			case types.StageStatusNotStarted:
				if recovery.count != 1 || output != "" {
					t.Fatalf("recovery won: recovered=%d output=%q", recovery.count, output)
				}
			default:
				t.Fatalf("neither result nor recovery committed: status=%q", status)
			}
		})
	}
}
