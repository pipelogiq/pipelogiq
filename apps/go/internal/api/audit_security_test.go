package api

import (
	"net/http"
	"strings"
	"testing"
	"unicode/utf8"

	"pipelogiq/internal/store"
)

func TestAuditMalformedRouteTargetCannotPreventRecording(t *testing.T) {
	fx := setupScopeTest(t)
	for _, target := range []string{strings.Repeat("9", 400), "bad%00id", "%FF"} {
		response := fx.do(t, scopedUserID, http.MethodPost, "/pipelines/"+target+"/pause", nil)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("invalid ID status = %d, want 400", response.Code)
		}
		var record store.AuditRecord
		if err := fx.db.Get(&record, `SELECT id,ts,user_id,email,action,target,outcome,remote_addr,details FROM audit_log ORDER BY id DESC LIMIT 1`); err != nil {
			t.Fatal(err)
		}
		if record.Action != "pipeline.pause" || record.Outcome != "failed" || record.UserID == nil || *record.UserID != scopedUserID {
			t.Fatalf("malformed route lost operation attribution: %+v", record)
		}
		if record.Target == nil || utf8.RuneCountInString(*record.Target) > 255 || !utf8.ValidString(*record.Target) || strings.ContainsRune(*record.Target, 0) {
			t.Fatalf("target cannot be persisted to PostgreSQL: %+v", record)
		}
	}
	var count int
	if err := fx.db.Get(&count, `SELECT count(*) FROM audit_log`); err != nil || count != 3 {
		t.Fatalf("malformed requests recorded = %d, err=%v", count, err)
	}
}
