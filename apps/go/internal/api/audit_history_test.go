package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"pipelogiq/internal/store"
)

func TestAuditHistoryFiltersAndPagination(t *testing.T) {
	fx := setupScopeTest(t)
	ts := time.Now().UTC().Add(-time.Hour)
	for i := 0; i < 5; i++ {
		if _, err := fx.db.Exec(`INSERT INTO audit_log (ts,user_id,email,action,target,outcome) VALUES (?,?,?,?,?,?)`, ts, scopedUserID, "old-email@example.com", "stage.rerun", fmt.Sprintf("stage:%d", i+1), "ok"); err != nil {
			t.Fatal(err)
		}
	}
	// Matching history is older than the former latest-200-record window.
	for i := 0; i < 205; i++ {
		if _, err := fx.db.Exec(`INSERT INTO audit_log (ts,user_id,email,action,outcome) VALUES (?,?,?,?,?)`, time.Now().UTC(), strangerUserID, "other@example.com", "pipeline.pause", "ok"); err != nil {
			t.Fatal(err)
		}
	}
	res := fx.do(t, scopedUserID, http.MethodGet, "/audit?action=stage.rerun&userId=10&outcome=ok&page=2&pageSize=2", nil)
	if res.Code != http.StatusOK {
		t.Fatalf("list = %d: %s", res.Code, res.Body.String())
	}
	var page store.AuditPage
	if err := json.Unmarshal(res.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if page.TotalCount != 5 || page.Page != 2 || page.PageSize != 2 || page.TotalPages != 3 || len(page.Items) != 2 {
		t.Fatalf("page = %+v", page)
	}
	if *page.Items[0].Target != "stage:3" || *page.Items[1].Target != "stage:2" {
		t.Fatalf("unstable pagination: %+v", page.Items)
	}

	res = fx.do(t, scopedUserID, http.MethodGet, "/audit/options", nil)
	if res.Code != http.StatusOK {
		t.Fatalf("options = %d: %s", res.Code, res.Body.String())
	}
	var options store.AuditOptions
	if err := json.Unmarshal(res.Body.Bytes(), &options); err != nil {
		t.Fatal(err)
	}
	if len(options.Actions) != 2 || len(options.Users) != 2 || options.Users[0].ID != scopedUserID || options.Users[0].Email != "user10@example.com" {
		t.Fatalf("options omitted historical action/current actor: %+v", options)
	}
	res = fx.do(t, scopedUserID, http.MethodGet, "/audit?action=stage.skip&userId=10", nil)
	if err := json.Unmarshal(res.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if page.Items == nil || len(page.Items) != 0 || page.TotalCount != 0 {
		t.Fatalf("empty page = %+v", page)
	}
}

func TestAuditHistoryValidatesFiltersAndRemainsAdminOnly(t *testing.T) {
	fx := setupScopeTest(t)
	for _, query := range []string{"page=0", "page=bad", "pageSize=101", "pageSize=0", "userId=-1", "userId=abc", "outcome=unknown", "action=" + strings.Repeat("x", 101)} {
		res := fx.do(t, scopedUserID, http.MethodGet, "/audit?"+query, nil)
		if res.Code != http.StatusBadRequest {
			t.Errorf("%s = %d", query, res.Code)
		}
	}
	for _, role := range []string{"operator", "viewer"} {
		if _, err := fx.db.Exec(`UPDATE "user" SET role = ? WHERE id = ?`, role, strangerUserID); err != nil {
			t.Fatal(err)
		}
		for _, path := range []string{"/audit", "/audit/options"} {
			res := fx.do(t, strangerUserID, http.MethodGet, path, nil)
			if res.Code != http.StatusForbidden {
				t.Errorf("%s %s = %d", role, path, res.Code)
			}
		}
	}
}

func TestAuditAuthenticationAttributionAndNoDuplicateUserEvents(t *testing.T) {
	fx := setupScopeTest(t)
	seedPasswordUser(t, fx, 42, "actor@example.com", "correct-password-123")
	bad := postJSON(t, fx, "/auth/login", map[string]string{"email": "actor@example.com", "password": "wrong-password"})
	if bad.Code != http.StatusUnauthorized {
		t.Fatalf("bad login = %d", bad.Code)
	}
	login := postJSON(t, fx, "/auth/login", map[string]string{"email": "actor@example.com", "password": "correct-password-123"})
	if login.Code != http.StatusOK {
		t.Fatalf("login = %d", login.Code)
	}
	cookie := login.Result().Cookies()[0]
	changed := postJSON(t, fx, "/auth/password", map[string]string{"currentPassword": "correct-password-123", "newPassword": "replacement-password-123"}, cookie)
	if changed.Code != http.StatusOK {
		t.Fatalf("password = %d", changed.Code)
	}
	freshCookie := changed.Result().Cookies()[0]
	// A revoked cookie must not revoke a newer session or be reported as a
	// successful logout by a currently authenticated user.
	postJSON(t, fx, "/auth/logout", nil, cookie)
	assertSessionStatus(t, fx, freshCookie, http.StatusOK)
	postJSON(t, fx, "/auth/logout", nil, freshCookie)

	var rows []store.AuditRecord
	if err := fx.db.Select(&rows, `SELECT id,ts,user_id,email,action,target,outcome,remote_addr,details FROM audit_log ORDER BY id`); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 5 {
		t.Fatalf("expected one row per action, got %d", len(rows))
	}
	if rows[0].UserID != nil || rows[0].Email == nil || *rows[0].Email != "actor@example.com" || rows[0].Outcome != "failed" {
		t.Fatalf("failed attempt impersonated actor: %+v", rows[0])
	}
	for _, i := range []int{1, 2, 4} {
		if rows[i].UserID == nil || *rows[i].UserID != 42 || rows[i].Outcome != "ok" {
			t.Fatalf("action lacks verified identity: %+v", rows[i])
		}
	}
	if rows[3].Outcome != "denied" || rows[3].UserID != nil {
		t.Fatalf("stale logout = %+v", rows[3])
	}
	encoded, _ := json.Marshal(rows)
	for _, secret := range []string{"correct-password-123", "replacement-password-123", "wrong-password", cookie.Value, freshCookie.Value} {
		if strings.Contains(string(encoded), secret) {
			t.Fatal("audit exposed credential")
		}
	}
	// GET requests, including audit reads, must not create a polling feedback loop.
	fx.do(t, scopedUserID, http.MethodGet, "/audit", nil)
	fx.do(t, scopedUserID, http.MethodGet, "/audit/options", nil)
	var count int
	if err := fx.db.Get(&count, `SELECT count(*) FROM audit_log`); err != nil || count != 5 {
		t.Fatalf("read queries generated events: %d %v", count, err)
	}
}

func TestAuditRecordsRevokedSessionMutationAttempt(t *testing.T) {
	fx := setupScopeTest(t)
	if _, err := fx.db.Exec(`UPDATE "user" SET token_version = 1 WHERE id = ?`, scopedUserID); err != nil {
		t.Fatal(err)
	}
	res := fx.do(t, scopedUserID, http.MethodPost, fmt.Sprintf("/pipelines/%d/pause", fx.ownPipelineID), nil)
	if res.Code != http.StatusUnauthorized {
		t.Fatalf("revoked mutation = %d", res.Code)
	}
	var row store.AuditRecord
	if err := fx.db.Get(&row, `SELECT id,ts,user_id,email,action,target,outcome,remote_addr,details FROM audit_log`); err != nil {
		t.Fatal(err)
	}
	if row.Action != "pipeline.pause" || row.Outcome != "denied" || row.UserID == nil || *row.UserID != scopedUserID {
		t.Fatalf("revoked audit = %+v", row)
	}
}

func TestAuditMalformedLoginRecordedWithoutCredentials(t *testing.T) {
	fx := setupScopeTest(t)
	req := httptest.NewRequest(http.MethodPost, "/auth/login", strings.NewReader(`{"password":"do-not-store"`))
	res := httptest.NewRecorder()
	fx.router.ServeHTTP(res, req)
	if res.Code != http.StatusBadRequest {
		t.Fatalf("malformed login = %d", res.Code)
	}
	var rows []store.AuditRecord
	if err := fx.db.Select(&rows, `SELECT id,ts,user_id,email,action,target,outcome,remote_addr,details FROM audit_log`); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Action != "auth.login" || rows[0].Outcome != "failed" {
		t.Fatalf("malformed audit = %+v", rows)
	}
	encoded, _ := json.Marshal(rows)
	if strings.Contains(string(encoded), "do-not-store") {
		t.Fatal("audit captured malformed credentials")
	}
}

func TestAuditDeniedAdminOperationsKeepSemanticActionAndTarget(t *testing.T) {
	fx := setupScopeTest(t)
	if _, err := fx.db.Exec(`UPDATE "user" SET role = 'viewer' WHERE id = ?`, strangerUserID); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ method, path, action, target string }{
		{http.MethodPost, "/users", "user.create", "/users"},
		{http.MethodPut, "/users/10", "user.update", "user:10"},
		{http.MethodPost, "/users/10/password", "user.password_reset", "user:10"},
		{http.MethodPost, "/policies/42/enable", "policy.enable", "policy:42"},
		{http.MethodDelete, "/observability/config/grafana", "observability.delete", "integration:grafana"},
	} {
		res := fx.do(t, strangerUserID, tc.method, tc.path, map[string]string{"password": "do-not-record"})
		if res.Code != http.StatusForbidden {
			t.Fatalf("%s = %d", tc.path, res.Code)
		}
		var row store.AuditRecord
		if err := fx.db.Get(&row, `SELECT id,ts,user_id,email,action,target,outcome,remote_addr,details FROM audit_log ORDER BY id DESC LIMIT 1`); err != nil {
			t.Fatal(err)
		}
		if row.Action != tc.action || row.Target == nil || *row.Target != tc.target || row.Outcome != "denied" {
			t.Errorf("%s: %+v", tc.path, row)
		}
		if row.Details != nil && strings.Contains(*row.Details, "do-not-record") {
			t.Fatal("denied payload leaked")
		}
	}
}
