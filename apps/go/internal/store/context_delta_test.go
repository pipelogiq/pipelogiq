package store

import (
	"context"
	"encoding/json"
	"pipelogiq/internal/types"
	"strings"
	"testing"
)

func TestContextDeltaDeletionRetainsHistoricalRedaction(t *testing.T) {
	for _, replacement := range []bool{false, true} {
		t.Run(map[bool]string{false: "delete", true: "replace"}[replacement], func(t *testing.T) {
			st, db := setupSchedulingStore(t)
			ctx := context.Background()
			pid := insertPipeline(t, db, "sensitive history")
			sid := insertStage(t, db, pid, "first", "handler")
			insertStage(t, db, pid, "next", "handler")
			oldSecret := `"old-secret-token"`
			newSecret := `"new-secret-token"`
			if _, err := db.Exec(`INSERT INTO pipeline_context_item (pipeline_id,key,value,is_sensitive) VALUES ($1,'token',$2,true),($1,'untouched','original',false),($1,'removeMe','value',false)`, pid, oldSecret); err != nil {
				t.Fatal(err)
			}
			execution := reliabilitySchedule(t, st)
			// Another parallel stage has updated a key after this execution took its snapshot.
			if _, err := db.Exec(`UPDATE pipeline_context_item SET value='parallel' WHERE pipeline_id=$1 AND key='untouched'`, pid); err != nil {
				t.Fatal(err)
			}
			item := types.ContextItem{Key: "token", IsDeleted: !replacement}
			if replacement {
				item.Value = newSecret
			} // Sticky sensitivity must protect a replacement even without the flag.
			result := types.StageResultMessage{StageID: sid, PipelineID: &pid, ExecutionID: execution.ExecutionID, Attempt: execution.Attempt, IsSuccess: true, Result: "old-secret-token new-secret-token", ContextItems: []types.ContextItem{item, {Key: "removeMe", IsDeleted: true}}}
			if _, err := st.UpdateStageResult(ctx, result); err != nil {
				t.Fatal(err)
			}
			next := reliabilitySchedule(t, st)
			if got := reliabilityContextValue(next.ContextItems, "untouched"); got != "parallel" {
				t.Fatalf("parallel update overwritten: %q", got)
			}
			for _, item := range next.ContextItems {
				if item.Key == "removeMe" || (!replacement && item.Key == "token") {
					t.Fatalf("deleted key reached worker: %+v", item)
				}
			}
			if replacement && reliabilityContextValue(next.ContextItems, "token") != newSecret {
				t.Fatal("new value did not reach worker")
			}
			detail, err := st.GetPipelineWithStages(ctx, pid)
			if err != nil {
				t.Fatal(err)
			}
			public := types.RedactPipelineResponse(detail)
			encoded, err := json.Marshal(public)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(encoded), "old-secret-token") || (replacement && strings.Contains(string(encoded), "new-secret-token")) {
				t.Fatalf("secret leaked: %s", encoded)
			}
			// Retained values must never be serialized even before public projection.
			for i := range detail.Stages {
				detail.Stages[i].Output = nil
				detail.Stages[i].Input = nil
			}
			detail.PipelineContext = nil
			encoded, _ = json.Marshal(detail)
			if strings.Contains(string(encoded), "old-secret-token") {
				t.Fatal("history serialized")
			}
		})
	}
}
