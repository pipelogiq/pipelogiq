package store

import (
	"context"
	"fmt"
	"pipelogiq/internal/types"
	"testing"
)

func TestStructuredFiltersApplyBeforePagingAndExcludeSecrets(t *testing.T) {
	st, db := setupSchedulingStore(t)
	if _, err := db.Exec(`CREATE TABLE keyword (id INTEGER PRIMARY KEY, key TEXT,value TEXT); CREATE TABLE pipeline_keyword (pipeline_id INTEGER,keyword_id INTEGER); INSERT INTO keyword VALUES (1,'priority','critical'),(2,'critical','other')`); err != nil {
		t.Fatal(err)
	}
	var matching []int
	for i := 0; i < 8; i++ {
		id := insertPipeline(t, db, fmt.Sprintf("run %d", i))
		secret := i == 7
		value := "ordinary"
		if i%2 == 0 || secret {
			value = `"discount 50%_off"`
		}
		if i%2 == 0 {
			matching = append(matching, id)
		}
		if _, err := db.Exec(`INSERT INTO pipeline_context_item (pipeline_id,key,value,is_sensitive) VALUES ($1,'customer',$2,$3),($1,'environment','"prod"',false); INSERT INTO pipeline_keyword VALUES ($1,1)`, id, value, secret); err != nil {
			t.Fatal(err)
		}
	}
	page, size := 2, 2
	env := "PROD"
	req := types.GetPipelinesRequest{PageNumber: &page, PageSize: &size, ContextFilters: []types.ContextFilter{{Key: "CUSTOMER", Value: "50%_off"}}, Environment: &env, Tags: []string{"critical"}, Labels: []types.PipelineKeyword{{Key: "priority", Value: "critical"}}}
	result, err := st.GetPipelines(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if result.TotalCount != 4 || len(result.Items) != 2 {
		t.Fatalf("filter count/page: %+v", result)
	}
	for _, item := range result.Items {
		found := false
		for _, id := range matching {
			if item.ID == id {
				found = true
			}
		}
		if !found {
			t.Fatalf("unexpected/secret match: %d", item.ID)
		}
	}
	req.ContextFilters[0].Value = "50X_off"
	result, err = st.GetPipelines(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if result.TotalCount != 0 {
		t.Fatal("LIKE wildcards were not literal")
	}
	req.ContextFilters = nil
	req.Labels = []types.PipelineKeyword{{Key: "critical", Value: "critical"}}
	result, err = st.GetPipelines(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if result.TotalCount != 0 {
		t.Fatal("label key/value confused")
	}
}

func TestPolicyEnvironmentAcceptsSDKJSONStrings(t *testing.T) {
	for _, env := range []string{"prod", `"prod"`, `"PROD"`} {
		t.Run(env, func(t *testing.T) {
			st, db := setupPolicyRuntimeTestStore(t)
			pid := insertPolicyRuntimePipeline(t, db, "json-env", types.PipelineStatusNotStarted, 101)
			insertPolicyRuntimeEnv(t, db, pid, env)
			sid := insertPolicyRuntimeStage(t, db, pid, "stage", "handler", types.StageStatusNotStarted, nil)
			resolution, err := st.ResolveStagePolicies(context.Background(), sid)
			if err != nil {
				t.Fatal(err)
			}
			if resolution.Scope.Environment != types.PolicyEnvironmentProd {
				t.Fatalf("environment: %s", resolution.Scope.Environment)
			}
		})
	}
}
