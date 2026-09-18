package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

func seedPasswordUser(t *testing.T, fx *scopeFixture, id int, email, password string) {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fx.db.Exec(`INSERT INTO "user" (id, first_name, email, password, role, created_at) VALUES (?, 'P', ?, ?, 'Admin', CURRENT_TIMESTAMP)`, id, email, string(hash)); err != nil {
		t.Fatal(err)
	}
}

func postJSON(t *testing.T, fx *scopeFixture, path string, body any, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	encoded, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(string(encoded)))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = "203.0.113.7:1234"
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	fx.router.ServeHTTP(rec, req)
	return rec
}

func TestLogin_RateLimitsRepeatedFailures(t *testing.T) {
	fx := setupScopeTest(t)
	seedPasswordUser(t, fx, 20, "limited@example.com", "correct-horse-battery-staple")

	var last *httptest.ResponseRecorder
	for i := 0; i < loginPerEmail+1; i++ {
		last = postJSON(t, fx, "/auth/login", map[string]string{"email": "limited@example.com", "password": "wrong"})
	}
	if last.Code != http.StatusTooManyRequests {
		t.Fatalf("attempt %d status = %d, want 429", loginPerEmail+1, last.Code)
	}
	if last.Header().Get("Retry-After") == "" {
		t.Fatal("429 without Retry-After")
	}

	var failures int
	if err := fx.db.Get(&failures, `SELECT count(*) FROM audit_log WHERE action = 'auth.login' AND outcome = 'failed'`); err != nil {
		t.Fatal(err)
	}
	if failures != loginPerEmail {
		t.Fatalf("audited failures = %d, want %d", failures, loginPerEmail)
	}
}

func TestLogout_RevokesEverySessionOfTheAccount(t *testing.T) {
	fx := setupScopeTest(t)
	seedPasswordUser(t, fx, 21, "revoke@example.com", "correct-horse-battery-staple")

	login := postJSON(t, fx, "/auth/login", map[string]string{"email": "revoke@example.com", "password": "correct-horse-battery-staple"})
	if login.Code != http.StatusOK {
		t.Fatalf("login = %d: %s", login.Code, login.Body.String())
	}
	session := login.Result().Cookies()[0]

	// A second copy of the same token — say, stolen — works until logout.
	me := httptest.NewRequest(http.MethodGet, "/auth/me", nil)
	me.AddCookie(session)
	rec := httptest.NewRecorder()
	fx.router.ServeHTTP(rec, me)
	if rec.Code != http.StatusOK {
		t.Fatalf("/auth/me before logout = %d", rec.Code)
	}

	if out := postJSON(t, fx, "/auth/logout", nil, session); out.Code != http.StatusOK {
		t.Fatalf("logout = %d", out.Code)
	}

	rec = httptest.NewRecorder()
	fx.router.ServeHTTP(rec, me)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("stolen token still valid after logout: %d", rec.Code)
	}
}

func TestChangePassword_RequiresCurrentAndRotatesSession(t *testing.T) {
	fx := setupScopeTest(t)
	seedPasswordUser(t, fx, 22, "rotate@example.com", "correct-horse-battery-staple")

	login := postJSON(t, fx, "/auth/login", map[string]string{"email": "rotate@example.com", "password": "correct-horse-battery-staple"})
	session := login.Result().Cookies()[0]

	wrong := postJSON(t, fx, "/auth/password", map[string]string{"currentPassword": "nope", "newPassword": "a-much-longer-new-password"}, session)
	if wrong.Code != http.StatusForbidden {
		t.Fatalf("wrong current password = %d", wrong.Code)
	}
	short := postJSON(t, fx, "/auth/password", map[string]string{"currentPassword": "correct-horse-battery-staple", "newPassword": "short"}, session)
	if short.Code != http.StatusBadRequest {
		t.Fatalf("short password = %d", short.Code)
	}

	ok := postJSON(t, fx, "/auth/password", map[string]string{"currentPassword": "correct-horse-battery-staple", "newPassword": "a-much-longer-new-password"}, session)
	if ok.Code != http.StatusOK {
		t.Fatalf("change password = %d: %s", ok.Code, ok.Body.String())
	}
	fresh := ok.Result().Cookies()
	if len(fresh) == 0 {
		t.Fatal("password change did not reissue the session cookie")
	}

	// The old cookie is dead, the reissued one works, the new password signs in.
	me := httptest.NewRequest(http.MethodGet, "/auth/me", nil)
	me.AddCookie(session)
	rec := httptest.NewRecorder()
	fx.router.ServeHTTP(rec, me)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("old session after password change = %d", rec.Code)
	}
	me = httptest.NewRequest(http.MethodGet, "/auth/me", nil)
	me.AddCookie(fresh[0])
	rec = httptest.NewRecorder()
	fx.router.ServeHTTP(rec, me)
	if rec.Code != http.StatusOK {
		t.Fatalf("reissued session = %d", rec.Code)
	}
	relogin := postJSON(t, fx, "/auth/login", map[string]string{"email": "rotate@example.com", "password": "a-much-longer-new-password"})
	if relogin.Code != http.StatusOK {
		t.Fatalf("login with new password = %d", relogin.Code)
	}
}

func TestAdminMutationsAreAudited(t *testing.T) {
	fx := setupScopeTest(t)
	rec := fx.do(t, scopedUserID, http.MethodPost, "/applications", map[string]any{"name": "audited-app"})
	if rec.Code >= 500 {
		t.Fatalf("save application = %d: %s", rec.Code, rec.Body.String())
	}
	var rows []struct {
		Action  string `db:"action"`
		Outcome string `db:"outcome"`
	}
	if err := fx.db.Select(&rows, `SELECT action, outcome FROM audit_log`); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Action != "application.create" {
		t.Fatalf("audit rows = %+v", rows)
	}
	_ = fmt.Sprintf
}
