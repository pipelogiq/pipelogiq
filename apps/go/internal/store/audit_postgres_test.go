//go:build integration

package store

import (
	"context"
	"fmt"
	"reflect"
	"slices"
	"testing"
	"time"
)

// Exercise the actual migrated PostgreSQL schema: SQLite cannot verify the
// production indexes, placeholder typing, or historical-actor subquery.
func TestPostgresAuditFiltersAndHistoricalOptions(t *testing.T) {
	st := openPostgres(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	suffix := fmt.Sprint(time.Now().UnixNano())
	action := "it.audit." + suffix
	legacyAction := "POST /it-audit-" + suffix
	var current, deleted int
	for _, actor := range []struct {
		id    *int
		email string
	}{{&current, "current-" + suffix + "@example.com"}, {&deleted, "deleted-" + suffix + "@example.com"}} {
		if err := st.db.QueryRowContext(ctx, `
			INSERT INTO "user" (first_name, email, password, role)
			VALUES ('Audit test', $1, 'test-unused-hash', 'viewer') RETURNING id
		`, actor.email).Scan(actor.id); err != nil {
			t.Fatal(err)
		}
		id := *actor.id
		t.Cleanup(func() { _, _ = st.db.Exec(`DELETE FROM "user" WHERE id = $1`, id) })
	}
	t.Cleanup(func() { _, _ = st.db.Exec(`DELETE FROM audit_log WHERE action IN ($1, $2)`, action, legacyAction) })
	insert := func(ts time.Time, userID *int, email, rowAction, outcome string) int64 {
		t.Helper()
		var id int64
		if err := st.db.QueryRowContext(ctx, `
			INSERT INTO audit_log (ts, user_id, email, action, outcome)
			VALUES ($1, $2, $3, $4, $5) RETURNING id
		`, ts, userID, email, rowAction, outcome).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	now := time.Now().UTC()
	ids := make([]int64, 5)
	for i := range ids {
		ids[i] = insert(now, &current, "previous@example.com", action, "ok")
	}
	insert(now, &current, "previous@example.com", action, "failed")
	insert(now.Add(-2*time.Hour), &deleted, "old@example.com", action, "ok")
	insert(now.Add(-time.Hour), &deleted, "latest@example.com", legacyAction, "ok")
	insert(now, nil, "anonymous@example.com", action, "failed")
	if _, err := st.db.ExecContext(ctx, `DELETE FROM "user" WHERE id = $1`, deleted); err != nil {
		t.Fatal(err)
	}

	page, err := st.ListAuditFiltered(ctx, AuditFilter{Action: action, UserID: &current, Outcome: "ok", Page: 2, PageSize: 2})
	if err != nil {
		t.Fatal(err)
	}
	if page.TotalCount != 5 || page.TotalPages != 3 || !reflect.DeepEqual(auditIDs(page.Items), []int64{ids[2], ids[1]}) {
		t.Fatalf("filtered PostgreSQL page = %+v, IDs = %v", page, auditIDs(page.Items))
	}
	anonymous := 0
	page, err = st.ListAuditFiltered(ctx, AuditFilter{Action: action, UserID: &anonymous, Outcome: "failed"})
	if err != nil || page.TotalCount != 1 || len(page.Items) != 1 || page.Items[0].UserID != nil {
		t.Fatalf("anonymous PostgreSQL page = %+v, error = %v", page, err)
	}
	options, err := st.AuditFilterOptions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(options.Actions, action) || !slices.Contains(options.Actions, legacyAction) {
		t.Fatalf("PostgreSQL filter actions omit history: %v", options.Actions)
	}
	users := make(map[int]string, len(options.Users))
	for _, user := range options.Users {
		users[user.ID] = user.Email
	}
	if users[current] != "current-"+suffix+"@example.com" || users[deleted] != "latest@example.com" {
		t.Fatalf("PostgreSQL actor options lost current/deleted identity: %v", users)
	}
	var indexCount int
	if err := st.db.GetContext(ctx, &indexCount, `
		SELECT count(*) FROM pg_indexes WHERE schemaname = current_schema()
			AND indexname IN ('idx_audit_log_action_ts_id', 'idx_audit_log_user_action_ts_id')
	`); err != nil || indexCount != 2 {
		t.Fatalf("audit query indexes = %d, error = %v", indexCount, err)
	}
}
