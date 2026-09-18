package api

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"pipelogiq/internal/types"
)

func userPayload(email, role string, active bool, applications ...int) map[string]any {
	if applications == nil {
		applications = []int{}
	}
	return map[string]any{"firstName": "Test", "lastName": "User", "email": email, "role": role, "isActive": active, "applicationIds": applications}
}

func TestUsers_AuthorizationUsesCurrentDatabaseRole(t *testing.T) {
	for _, role := range []string{"viewer", "operator", "unrecognized"} {
		t.Run(role, func(t *testing.T) {
			fx := setupScopeTest(t)
			if _, err := fx.db.Exec(`UPDATE "user" SET role = ? WHERE id = ?`, role, scopedUserID); err != nil {
				t.Fatal(err)
			}
			// fx.do deliberately mints an Admin JWT. The stored role must win.
			for _, path := range []string{"/users", "/users/roles", "/users/applications", "/applications", "/audit"} {
				rec := fx.do(t, scopedUserID, http.MethodGet, path, nil)
				if rec.Code != http.StatusForbidden {
					t.Errorf("GET %s = %d: %s", path, rec.Code, rec.Body.String())
				}
			}
			for _, mutation := range []struct{ method, path string }{
				{http.MethodPost, "/users"}, {http.MethodPut, "/users/11"}, {http.MethodPost, "/users/11/password"},
			} {
				rec := fx.do(t, scopedUserID, mutation.method, mutation.path, userPayload("blocked@example.com", "admin", true))
				if rec.Code != http.StatusForbidden {
					t.Errorf("%s %s = %d", mutation.method, mutation.path, rec.Code)
				}
			}
			if rec := fx.do(t, scopedUserID, http.MethodGet, fmt.Sprintf("/pipelines/%d", fx.ownPipelineID), nil); rec.Code != http.StatusOK {
				t.Fatalf("assigned read = %d", rec.Code)
			}
		})
	}
}

func TestViewer_CannotMutatePipelinesOrSchedules(t *testing.T) {
	fx := setupScopeTest(t)
	if _, err := fx.db.Exec(`UPDATE "user" SET role = 'viewer' WHERE id = ?`, scopedUserID); err != nil {
		t.Fatal(err)
	}
	for _, mutation := range []struct{ method, path string }{
		{http.MethodPost, "/pipelines/1/pause"}, {http.MethodPost, "/pipelines/1/resume"},
		{http.MethodPost, "/pipelines/bulkAction"}, {http.MethodPost, "/pipelines/rerunStage"}, {http.MethodPost, "/pipelines/skipStage"},
		{http.MethodPost, "/schedules/1/pause"}, {http.MethodPost, "/schedules/1/resume"}, {http.MethodPost, "/schedules/1/archive"},
		{http.MethodPost, "/schedules/1/trigger"}, {http.MethodPatch, "/schedules/1/policies"},
	} {
		rec := fx.do(t, scopedUserID, mutation.method, mutation.path, map[string]any{})
		if rec.Code != http.StatusForbidden {
			t.Errorf("%s %s = %d, expected 403: %s", mutation.method, mutation.path, rec.Code, rec.Body.String())
		}
	}
}

func TestOperator_CanOperateOnlyAssignedApplications(t *testing.T) {
	fx := setupScopeTest(t)
	if _, err := fx.db.Exec(`UPDATE "user" SET role = 'operator' WHERE id = ?`, scopedUserID); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ id, status int }{{fx.ownPipelineID, http.StatusOK}, {fx.otherPipelineID, http.StatusNotFound}} {
		rec := fx.do(t, scopedUserID, http.MethodPost, fmt.Sprintf("/pipelines/%d/pause", tc.id), nil)
		if rec.Code != tc.status {
			t.Errorf("pause %d = %d, want %d: %s", tc.id, rec.Code, tc.status, rec.Body.String())
		}
	}
}

func TestUsers_CreateUpdateAndResetPassword(t *testing.T) {
	fx := setupScopeTest(t)
	body := userPayload(" New@Example.com ", "viewer", true, ownApplicationID)
	body["password"] = "initial-password-123"
	created := fx.do(t, scopedUserID, http.MethodPost, "/users", body)
	if created.Code != http.StatusCreated {
		t.Fatalf("create = %d: %s", created.Code, created.Body.String())
	}
	var user types.ManagedUser
	if err := json.Unmarshal(created.Body.Bytes(), &user); err != nil {
		t.Fatal(err)
	}
	if user.Email != "new@example.com" || user.Role != "viewer" || !user.IsActive || len(user.ApplicationIDs) != 1 {
		t.Fatalf("created user = %+v", user)
	}
	for _, secret := range []string{"initial-password", "password", "tokenVersion", "$2a$"} {
		if strings.Contains(created.Body.String(), secret) {
			t.Fatalf("response exposed %q", secret)
		}
	}
	login := postJSON(t, fx, "/auth/login", map[string]string{"email": user.Email, "password": "initial-password-123"})
	if login.Code != http.StatusOK {
		t.Fatalf("login = %d: %s", login.Code, login.Body.String())
	}
	session := login.Result().Cookies()[0]

	updated := fx.do(t, scopedUserID, http.MethodPut, fmt.Sprintf("/users/%d", user.ID), userPayload(user.Email, "operator", true, otherApplicationID))
	if updated.Code != http.StatusOK {
		t.Fatalf("update = %d: %s", updated.Code, updated.Body.String())
	}
	assertSessionStatus(t, fx, session, http.StatusUnauthorized)
	access := fx.do(t, scopedUserID, http.MethodGet, "/users/applications", nil)
	if access.Code != http.StatusOK || strings.Contains(access.Body.String(), "apiKeys") {
		t.Fatalf("application options = %d: %s", access.Code, access.Body.String())
	}

	login = postJSON(t, fx, "/auth/login", map[string]string{"email": user.Email, "password": "initial-password-123"})
	if login.Code != http.StatusOK {
		t.Fatalf("relogin = %d", login.Code)
	}
	session = login.Result().Cookies()[0]
	reset := fx.do(t, scopedUserID, http.MethodPost, fmt.Sprintf("/users/%d/password", user.ID), map[string]string{"password": "replacement-password-456"})
	if reset.Code != http.StatusNoContent {
		t.Fatalf("reset = %d: %s", reset.Code, reset.Body.String())
	}
	assertSessionStatus(t, fx, session, http.StatusUnauthorized)
	if old := postJSON(t, fx, "/auth/login", map[string]string{"email": user.Email, "password": "initial-password-123"}); old.Code != http.StatusUnauthorized {
		t.Fatalf("old password still valid: %d", old.Code)
	}
	if fresh := postJSON(t, fx, "/auth/login", map[string]string{"email": user.Email, "password": "replacement-password-456"}); fresh.Code != http.StatusOK {
		t.Fatalf("new password = %d", fresh.Code)
	}
	var count int
	if err := fx.db.Get(&count, `SELECT count(*) FROM audit_log WHERE action IN ('user.create', 'user.update', 'user.password_reset') AND outcome = 'ok'`); err != nil || count != 3 {
		t.Fatalf("audit count = %d: %v", count, err)
	}
}

func assertSessionStatus(t *testing.T, fx *scopeFixture, session *http.Cookie, expected int) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/auth/me", nil)
	req.AddCookie(session)
	rec := httptest.NewRecorder()
	fx.router.ServeHTTP(rec, req)
	if rec.Code != expected {
		t.Fatalf("session status = %d, want %d: %s", rec.Code, expected, rec.Body.String())
	}
}

func TestUsers_InvalidRequestsLeaveAccessUnchanged(t *testing.T) {
	fx := setupScopeTest(t)
	for _, tc := range []struct {
		name   string
		modify func(map[string]any)
		status int
	}{
		{"invalid role", func(b map[string]any) { b["role"] = "owner" }, 400},
		{"invalid email", func(b map[string]any) { b["email"] = "no email" }, 400},
		{"duplicate email", func(b map[string]any) { b["email"] = " USER10@EXAMPLE.COM " }, 409},
		{"missing active", func(b map[string]any) { delete(b, "isActive") }, 400},
		{"missing applications", func(b map[string]any) { delete(b, "applicationIds") }, 400},
		{"unknown application", func(b map[string]any) { b["applicationIds"] = []int{999} }, 400},
		{"invalid application", func(b map[string]any) { b["applicationIds"] = []int{-1} }, 400},
		{"unknown field", func(b map[string]any) { b["permissions"] = []string{"all"} }, 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := userPayload("user11@example.com", "operator", true)
			tc.modify(body)
			rec := fx.do(t, scopedUserID, http.MethodPut, "/users/11", body)
			if rec.Code != tc.status {
				t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
			}
		})
	}
	var version int
	if err := fx.db.Get(&version, `SELECT token_version FROM "user" WHERE id=11`); err != nil || version != 0 {
		t.Fatalf("invalid requests mutated account: version %d, %v", version, err)
	}
	for _, role := range []string{"admin", "viewer"} {
		body := userPayload("user10@example.com", role, role != "admin", ownApplicationID)
		rec := fx.do(t, scopedUserID, http.MethodPut, "/users/10", body)
		if rec.Code != http.StatusConflict {
			t.Fatalf("self lockout = %d: %s", rec.Code, rec.Body.String())
		}
	}
}

func TestUsers_DisabledAccountCannotLoginOrUseExistingSession(t *testing.T) {
	fx := setupScopeTest(t)
	seedPasswordUser(t, fx, 30, "disabled@example.com", "correct-password-123")
	login := postJSON(t, fx, "/auth/login", map[string]string{"email": "disabled@example.com", "password": "correct-password-123"})
	if login.Code != http.StatusOK {
		t.Fatalf("initial login = %d", login.Code)
	}
	rec := fx.do(t, scopedUserID, http.MethodPut, "/users/30", userPayload("disabled@example.com", "viewer", false))
	if rec.Code != http.StatusOK {
		t.Fatalf("disable = %d: %s", rec.Code, rec.Body.String())
	}
	assertSessionStatus(t, fx, login.Result().Cookies()[0], http.StatusUnauthorized)
	login = postJSON(t, fx, "/auth/login", map[string]string{"email": "disabled@example.com", "password": "correct-password-123"})
	if login.Code != http.StatusUnauthorized || len(login.Result().Cookies()) != 0 {
		t.Fatalf("disabled login = %d", login.Code)
	}
}

func TestUserStreamsStopWhenAccessIsRevoked(t *testing.T) {
	hub := NewHub(slog.New(slog.NewTextHandler(io.Discard, nil)), nil)
	hub.authorizeUser = func(id, version int) bool { return id == 11 && version == 0 }
	revoked := &Client{hub: hub, userID: 10, expiresAt: time.Now().Add(time.Hour), send: make(chan []byte, 1), scope: newApplicationScope([]int{1})}
	active := &Client{hub: hub, userID: 11, expiresAt: time.Now().Add(time.Hour), send: make(chan []byte, 1), scope: newApplicationScope([]int{1})}
	hub.clients[revoked] = struct{}{}
	hub.clients[active] = struct{}{}
	hub.Broadcast([]byte(`{"applicationId":1}`))
	if _, ok := <-revoked.send; ok {
		t.Fatal("revoked client received event")
	}
	select {
	case <-active.send:
	default:
		t.Fatal("active client missed event")
	}
	hub.DisconnectUser(11)
	if hub.clientCount() != 0 {
		t.Fatal("streams remain after disconnect")
	}
}

func TestUserPasswordValidation(t *testing.T) {
	for _, tc := range []struct {
		name, password string
		valid          bool
	}{
		{"too short", "short", false},
		{"unicode is not counted as bytes", strings.Repeat("я", 6), false},
		{"unicode passphrase", strings.Repeat("я", 12), true},
		{"minimum", "abcdefghijkl", true},
		{"bcrypt boundary", strings.Repeat("a", 72), true},
		{"bcrypt overflow", strings.Repeat("a", 73), false},
		{"unicode overflow", strings.Repeat("я", 37), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if valid := validateUserPassword(tc.password) == nil; valid != tc.valid {
				t.Fatalf("valid = %v, want %v", valid, tc.valid)
			}
		})
	}
}
