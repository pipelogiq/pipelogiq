package store

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/jmoiron/sqlx"
	"pipelogiq/internal/types"
)

// This opt-in test uses an already migrated disposable PostgreSQL database.
// It removes only records it created; it never truncates or drops shared tables.
func TestPipelineFiltersPostgres(t *testing.T) {
	dsn := os.Getenv("PIPELOGIQ_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("requires isolated PostgreSQL PIPELOGIQ_TEST_DATABASE_URL")
	}
	db, err := sqlx.Connect("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	st := New(db, slog.New(slog.NewTextHandler(io.Discard, nil)))
	key := fmt.Sprintf("sdk-filter-%d", time.Now().UnixNano())
	var ids []int
	var kid int
	if err := db.QueryRow(`INSERT INTO keyword (key,value) VALUES ($1,$2) RETURNING id`, key, "critical-"+key).Scan(&kid); err != nil {
		t.Fatal(err)
	}
	defer func() {
		for _, id := range ids {
			for _, table := range []string{"pipeline_keyword", "pipeline_context_item"} {
				if _, err := db.Exec("DELETE FROM "+table+" WHERE pipeline_id=$1", id); err != nil {
					t.Error(err)
				}
			}
			if _, err := db.Exec(`DELETE FROM pipeline WHERE id=$1`, id); err != nil {
				t.Error(err)
			}
		}
		if _, err := db.Exec(`DELETE FROM keyword WHERE id=$1`, kid); err != nil {
			t.Error(err)
		}
	}()
	for i := 0; i < 5; i++ {
		id := insertPipeline(t, db, key)
		ids = append(ids, id)
		if _, err := db.Exec(`INSERT INTO pipeline_context_item (pipeline_id,key,value,value_type,is_sensitive) VALUES ($1,$2,'"50%_off"','string',$3),($1,'environment','"prod"','string',false)`, id, key, i == 4); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`INSERT INTO pipeline_keyword (pipeline_id,keyword_id) VALUES ($1,$2)`, id, kid); err != nil {
			t.Fatal(err)
		}
	}
	page, size := 2, 2
	environment := "prod"
	req := types.GetPipelinesRequest{PageNumber: &page, PageSize: &size, ContextFilters: []types.ContextFilter{{Key: key, Value: "50%_off"}}, Tags: []string{"critical-" + key}, Labels: []types.PipelineKeyword{{Key: key, Value: "critical-" + key}}, Environment: &environment}
	result, err := st.GetPipelines(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if result.TotalCount != 4 || len(result.Items) != 2 {
		t.Fatalf("Postgres count/page: %+v", result)
	}
	req.Search = &key
	result, err = st.GetPipelines(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if result.TotalCount != 4 || len(result.Items) != 2 {
		t.Fatalf("Postgres search count/page: %+v", result)
	}
	req.ContextFilters[0].Value = "50X_off"
	result, err = st.GetPipelines(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if result.TotalCount != 0 {
		t.Fatal("Postgres LIKE did not treat wildcard literally")
	}
}
