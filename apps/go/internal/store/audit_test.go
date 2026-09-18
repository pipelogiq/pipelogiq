package store

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jmoiron/sqlx"
	_ "modernc.org/sqlite"
)

func setupAuditStore(t *testing.T) (*Store, *sqlx.DB) {
	t.Helper()
	db, err := sqlx.Open("sqlite", testDSN("audit"))
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(`
		CREATE TABLE "user" (id INTEGER PRIMARY KEY, email TEXT NOT NULL, password TEXT);
		CREATE TABLE audit_log (
			id INTEGER PRIMARY KEY AUTOINCREMENT, ts TIMESTAMP NOT NULL,
			user_id INTEGER, email TEXT, action TEXT NOT NULL, target TEXT,
			outcome TEXT NOT NULL, remote_addr TEXT, details TEXT
		);
	`); err != nil {
		t.Fatal(err)
	}
	return New(db, slog.New(slog.NewTextHandler(io.Discard, nil))), db
}

func insertAuditRow(t *testing.T, db *sqlx.DB, ts time.Time, userID *int, email, action, outcome string) int64 {
	t.Helper()
	result, err := db.Exec(`INSERT INTO audit_log (ts, user_id, email, action, outcome) VALUES ($1, $2, $3, $4, $5)`,
		ts, userID, nullIfEmpty(email), action, outcome)
	if err != nil {
		t.Fatal(err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func auditIDs(rows []AuditRecord) []int64 {
	ids := make([]int64, len(rows))
	for i, row := range rows {
		ids[i] = row.ID
	}
	return ids
}

func TestAuditFiltersCountBeforePagingAndOrderStably(t *testing.T) {
	st, db := setupAuditStore(t)
	ctx := context.Background()
	actor, otherActor := 1, 2
	now := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)
	for range 9 {
		insertAuditRow(t, db, now, &actor, "one@example.com", "stage.rerun", "success")
	}
	// A large newer history must not hide matches from an earlier page.
	for range 120 {
		insertAuditRow(t, db, now.Add(time.Hour), &otherActor, "two@example.com", "stage.skip", "success")
	}
	oldestID := insertAuditRow(t, db, now.Add(-time.Hour), &actor, "one@example.com", "stage.rerun", "success")
	filter := AuditFilter{Action: "stage.rerun", UserID: &actor, Page: 2, PageSize: 3}
	page, err := st.ListAuditFiltered(ctx, filter)
	if err != nil {
		t.Fatal(err)
	}
	if page.TotalCount != 10 || page.TotalPages != 4 || page.Page != 2 || page.PageSize != 3 || !reflect.DeepEqual(auditIDs(page.Items), []int64{6, 5, 4}) {
		t.Fatalf("filtered page = %+v, IDs = %v", page, auditIDs(page.Items))
	}
	filter.Page = 4
	page, err = st.ListAuditFiltered(ctx, filter)
	if err != nil || page.TotalCount != 10 || !reflect.DeepEqual(auditIDs(page.Items), []int64{oldestID}) {
		t.Fatalf("final page must sort by timestamp before ID: %+v, %v", page, err)
	}
	filter.Page = 5
	page, err = st.ListAuditFiltered(ctx, filter)
	if err != nil || page.TotalCount != 10 || page.Items == nil || len(page.Items) != 0 {
		t.Fatalf("past last page = %+v, %v", page, err)
	}
}

func TestAuditFiltersIntersectAndSelectUnauthenticatedActors(t *testing.T) {
	st, db := setupAuditStore(t)
	actor, otherActor, anonymous := 1, 2, 0
	now := time.Now().UTC()
	insertAuditRow(t, db, now, &actor, "one@example.com", "stage.rerun", "success")
	insertAuditRow(t, db, now, &actor, "one@example.com", "stage.rerun", "denied")
	insertAuditRow(t, db, now, &otherActor, "two@example.com", "stage.rerun", "success")
	insertAuditRow(t, db, now, &actor, "one@example.com", "stage.skip", "success")
	insertAuditRow(t, db, now, nil, "someone@example.com", "auth.login", "failure")
	insertAuditRow(t, db, now, nil, "", "stage.rerun", "denied")
	for _, tc := range []struct {
		name   string
		filter AuditFilter
		want   []int64
	}{
		{"action", AuditFilter{Action: "stage.rerun"}, []int64{6, 3, 2, 1}},
		{"actor", AuditFilter{UserID: &actor}, []int64{4, 2, 1}},
		{"outcome", AuditFilter{Outcome: "denied"}, []int64{6, 2}},
		{"all intersect", AuditFilter{Action: "stage.rerun", UserID: &actor, Outcome: "success"}, []int64{1}},
		{"unauthenticated", AuditFilter{UserID: &anonymous}, []int64{6, 5}},
		{"unauthenticated intersect", AuditFilter{Action: "auth.login", UserID: &anonymous, Outcome: "failure"}, []int64{5}},
		{"literal action", AuditFilter{Action: "stage.rerun' OR 1=1 --"}, []int64{}},
		{"literal outcome", AuditFilter{Outcome: "success' OR 1=1 --"}, []int64{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			page, err := st.ListAuditFiltered(context.Background(), tc.filter)
			if err != nil {
				t.Fatal(err)
			}
			if page.TotalCount != len(tc.want) || !reflect.DeepEqual(auditIDs(page.Items), tc.want) {
				t.Fatalf("filtered page = %+v, IDs = %v; want %v", page, auditIDs(page.Items), tc.want)
			}
		})
	}
}

func TestAuditPaginationDefaultsAndLegacyLimits(t *testing.T) {
	st, db := setupAuditStore(t)
	ctx := context.Background()
	empty, err := st.ListAuditFiltered(ctx, AuditFilter{})
	if err != nil || empty.Items == nil || empty.TotalPages != 0 || empty.Page != 1 || empty.PageSize != 100 {
		t.Fatalf("empty default page = %+v, %v", empty, err)
	}
	now := time.Now().UTC()
	for range 120 {
		insertAuditRow(t, db, now, nil, "", "auth.login", "failure")
	}
	page, err := st.ListAuditFiltered(ctx, AuditFilter{Page: -2, PageSize: 500})
	if err != nil || page.Page != 1 || page.PageSize != 100 || page.TotalPages != 2 || len(page.Items) != 100 {
		t.Fatalf("bounded page = %+v, %v", page, err)
	}
	// Avoid overflowing an offset if an untrusted caller requests a huge page.
	page, err = st.ListAuditFiltered(ctx, AuditFilter{Page: int(^uint(0) >> 1), PageSize: 100})
	if err != nil || page.Items == nil || len(page.Items) != 0 || page.TotalCount != 120 {
		t.Fatalf("huge page = %+v, %v", page, err)
	}
	for _, tc := range []struct{ limit, count int }{{1, 1}, {120, 120}, {500, 120}, {501, 100}, {0, 100}, {-1, 100}} {
		rows, err := st.ListAudit(ctx, tc.limit)
		if err != nil || len(rows) != tc.count || rows[0].ID != 120 {
			t.Fatalf("legacy limit %d returned %d rows, error = %v", tc.limit, len(rows), err)
		}
	}
}

func TestAuditOptionsIncludeOldActionsAndDeletedUsers(t *testing.T) {
	st, db := setupAuditStore(t)
	ctx := context.Background()
	if _, err := db.Exec(`INSERT INTO "user" (id, email, password) VALUES
		(1, 'deleted@example.com', 'private-hash'),
		(2, 'current@example.com', 'other-private-hash'),
		(3, 'unused@example.com', 'unused-private-hash')`); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	deleted, current, noEmail := 1, 2, 4
	insertAuditRow(t, db, now.Add(-2*time.Hour), &deleted, "old@example.com", "POST /api/pipeline/{id}/rerun", "success")
	insertAuditRow(t, db, now.Add(-time.Hour), &deleted, "latest@example.com", "stage.rerun", "success")
	insertAuditRow(t, db, now, &deleted, "", "stage.rerun", "success")
	insertAuditRow(t, db, now, &noEmail, "", "stage.rerun", "success")
	insertAuditRow(t, db, now, nil, "anonymous@example.com", "auth.login", "failure")
	for range 110 {
		insertAuditRow(t, db, now.Add(time.Hour), &current, "previous@example.com", "stage.skip", "success")
	}
	if _, err := db.Exec(`DELETE FROM "user" WHERE id = 1`); err != nil {
		t.Fatal(err)
	}
	options, err := st.AuditFilterOptions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	wantActions := []string{"POST /api/pipeline/{id}/rerun", "auth.login", "stage.rerun", "stage.skip"}
	wantUsers := []AuditUserOption{{ID: noEmail, Email: ""}, {ID: current, Email: "current@example.com"}, {ID: deleted, Email: "latest@example.com"}}
	if !reflect.DeepEqual(options.Actions, wantActions) || !reflect.DeepEqual(options.Users, wantUsers) {
		t.Fatalf("filter options = %+v", options)
	}
	encoded, err := json.Marshal(options)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"password", "private-hash", "anonymous@example.com", "unused@example.com"} {
		if strings.Contains(string(encoded), secret) {
			t.Fatalf("filter options expose unrelated data %q: %s", secret, encoded)
		}
	}
}

func TestAuditEmptyOptionsAndLegacyRowsAreArrays(t *testing.T) {
	st, _ := setupAuditStore(t)
	ctx := context.Background()
	options, err := st.AuditFilterOptions(ctx)
	if err != nil || options.Actions == nil || options.Users == nil || len(options.Actions) != 0 || len(options.Users) != 0 {
		t.Fatalf("empty options = %+v, %v", options, err)
	}
	rows, err := st.ListAudit(ctx, 100)
	if err != nil || rows == nil || len(rows) != 0 {
		t.Fatalf("empty legacy history = %+v, %v", rows, err)
	}
}
