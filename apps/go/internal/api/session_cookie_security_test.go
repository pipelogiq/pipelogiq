package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSessionRemovalPreservesCookieSecurity(t *testing.T) {
	for _, scenario := range []string{"logout", "invalid-token", "revoked-session"} {
		t.Run(scenario, func(t *testing.T) {
			fx := setupScopeTest(t)
			seedPasswordUser(t, fx, 71, "cookie@example.test", "password-for-cookie-test")
			token, err := generateJWT(fx.jwtSecret, 71, "cookie@example.test", "admin", 0)
			if err != nil {
				t.Fatal(err)
			}
			method, path, status := http.MethodGet, "/auth/me", http.StatusUnauthorized
			switch scenario {
			case "logout":
				method, path, status = http.MethodPost, "/auth/logout", http.StatusOK
			case "invalid-token":
				token = "invalid"
			case "revoked-session":
				if _, err := fx.db.Exec(`UPDATE "user" SET token_version=1 WHERE id=71`); err != nil {
					t.Fatal(err)
				}
			}
			req := httptest.NewRequest(method, "https://localhost"+path, nil)
			req.AddCookie(&http.Cookie{Name: authCookieName, Value: token})
			response := httptest.NewRecorder()
			fx.router.ServeHTTP(response, req)
			if response.Code != status {
				t.Fatalf("status = %d, want %d: %s", response.Code, status, response.Body.String())
			}
			cookies := response.Result().Cookies()
			if len(cookies) != 1 {
				t.Fatalf("expected one deletion cookie, got %d", len(cookies))
			}
			cookie := cookies[0]
			if cookie.Name != authCookieName || cookie.Value != "" || cookie.Path != "/" || cookie.MaxAge != -1 ||
				!cookie.HttpOnly || !cookie.Secure || cookie.SameSite != http.SameSiteLaxMode {
				t.Fatalf("unsafe deletion cookie: %+v", cookie)
			}
		})
	}
}
