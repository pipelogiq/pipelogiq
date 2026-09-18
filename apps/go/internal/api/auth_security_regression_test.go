package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

type authMutationReader struct {
	io.Reader
	beforeRead func()
	bytesRead  int
}

func TestLoginRejectsReactivatedDemoCredential(t *testing.T) {
	fx := setupScopeTest(t)
	const hash = "$2a$12$TGNopcSVE8D6P4GxsPWbneNjP0Iua1ngopHciWaOPYebih8O1myoC"
	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte("user1")); err != nil {
		t.Fatal("demo credential regression fixture must match its original password")
	}
	if _, err := fx.db.Exec(`INSERT INTO "user" (id, first_name,email,password,role,is_active,created_at) VALUES (61,'Renamed','renamed@example.test',?,'admin',TRUE,CURRENT_TIMESTAMP)`, hash); err != nil {
		t.Fatal(err)
	}
	response := postJSON(t, fx, "/auth/login", map[string]string{"email": "renamed@example.test", "password": "user1"})
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("demo login = %d", response.Code)
	}
	if len(response.Result().Cookies()) != 0 {
		t.Fatal("demo credential received a session")
	}
}

func (r *authMutationReader) Read(p []byte) (int, error) {
	if r.beforeRead != nil {
		mutate := r.beforeRead
		r.beforeRead = nil
		mutate()
	}
	n, err := r.Reader.Read(p)
	r.bytesRead += n
	return n, err
}

func TestChangePasswordRejectsSessionRevokedDuringRequest(t *testing.T) {
	fx := setupScopeTest(t)
	const id = 51
	const password = "current-password-123"
	seedPasswordUser(t, fx, id, "race@example.com", password)
	login := postJSON(t, fx, "/auth/login", map[string]string{"email": "race@example.com", "password": password})
	if login.Code != http.StatusOK {
		t.Fatalf("login = %d", login.Code)
	}
	session := login.Result().Cookies()[0]
	body := &authMutationReader{
		Reader: strings.NewReader(`{"currentPassword":"current-password-123","newPassword":"replacement-password-456"}`),
		// Body decoding starts only after the auth and scope middleware accept
		// the old session. Revoke it before password verification finishes.
		beforeRead: func() {
			if logout := postJSON(t, fx, "/auth/logout", nil, session); logout.Code != http.StatusOK {
				t.Fatalf("concurrent logout = %d", logout.Code)
			}
		},
	}
	req := httptest.NewRequest(http.MethodPost, "/auth/password", body)
	req.AddCookie(session)
	rec := httptest.NewRecorder()
	fx.router.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("revoked password change = %d, want 401: %s", rec.Code, rec.Body.String())
	}
	for _, cookie := range rec.Result().Cookies() {
		if cookie.Name == authCookieName && cookie.Value != "" {
			t.Fatal("revoked session received a new session")
		}
	}
	var row struct {
		Password string `db:"password"`
		Version  int    `db:"token_version"`
	}
	if err := fx.db.Get(&row, `SELECT password, token_version FROM "user" WHERE id = ?`, id); err != nil {
		t.Fatal(err)
	}
	if row.Version != 1 || bcrypt.CompareHashAndPassword([]byte(row.Password), []byte(password)) != nil {
		t.Fatal("revoked request changed the password or session version")
	}
}

func TestAuthRejectsUnboundedOrAmbiguousBodies(t *testing.T) {
	for _, path := range []string{"/auth/login", "/auth/password"} {
		t.Run(path, func(t *testing.T) {
			fx := setupScopeTest(t)
			seedPasswordUser(t, fx, 52, "body@example.com", "current-password-123")
			login := postJSON(t, fx, "/auth/login", map[string]string{"email": "body@example.com", "password": "current-password-123"})
			if login.Code != http.StatusOK {
				t.Fatalf("login = %d", login.Code)
			}
			valid := `{"email":"body@example.com","password":"current-password-123"}`
			if path == "/auth/password" {
				valid = `{"currentPassword":"current-password-123","newPassword":"replacement-password-456"}`
			}
			for _, body := range []string{
				valid + `{}`,
				strings.TrimSuffix(valid, "}") + `,"unexpected":true}`,
				strings.Replace(valid, "current-password-123", strings.Repeat("x", 128<<10), 1),
			} {
				reader := &authMutationReader{Reader: strings.NewReader(body)}
				req := httptest.NewRequest(http.MethodPost, path, reader)
				req.AddCookie(login.Result().Cookies()[0])
				rec := httptest.NewRecorder()
				fx.router.ServeHTTP(rec, req)
				if rec.Code != http.StatusBadRequest {
					t.Errorf("invalid body on %s = %d, want 400", path, rec.Code)
				}
				if reader.bytesRead > (64<<10)+1 {
					t.Errorf("auth decoded an unbounded body: %d bytes", reader.bytesRead)
				}
			}
		})
	}
}

func TestJWTRejectsMissingExpiryAndUntrustedIssuer(t *testing.T) {
	secret := []byte("0123456789abcdef0123456789abcdef")
	for _, tc := range []struct {
		name   string
		claims jwtClaims
	}{
		{"missing expiry", jwtClaims{UserID: 1, RegisteredClaims: jwt.RegisteredClaims{Issuer: jwtIssuer}}},
		{"wrong issuer", jwtClaims{UserID: 1, RegisteredClaims: jwt.RegisteredClaims{Issuer: "another-service", ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour))}}},
		{"missing issuer", jwtClaims{UserID: 1, RegisteredClaims: jwt.RegisteredClaims{ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour))}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, tc.claims).SignedString(secret)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := parseJWT(secret, token); err == nil {
				t.Fatal("invalid session claims accepted")
			}
		})
	}
}

func TestAuthMiddlewareKeepsVerifiedSessionExpiry(t *testing.T) {
	server := &Server{jwtSecret: []byte("0123456789abcdef0123456789abcdef")}
	token, err := generateJWT(server.jwtSecret, 7, "user@example.com", "viewer", 3)
	if err != nil {
		t.Fatal(err)
	}
	claims, err := parseJWT(server.jwtSecret, token)
	if err != nil {
		t.Fatal(err)
	}
	handler := server.authMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		expiresAt, ok := r.Context().Value(tokenExpiresAtKey).(time.Time)
		if !ok || !expiresAt.Equal(claims.ExpiresAt.Time) {
			t.Fatalf("verified expiry missing from request: %v", expiresAt)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	req := httptest.NewRequest(http.MethodGet, "/ws", nil)
	req.AddCookie(&http.Cookie{Name: authCookieName, Value: token})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("middleware status = %d", rec.Code)
	}
}

func TestUnknownAccountUsesAccountPasswordWorkFactor(t *testing.T) {
	cost, err := bcrypt.Cost(dummyHash)
	if err != nil || cost != bcrypt.DefaultCost {
		t.Fatalf("unknown account bcrypt cost = %d, want %d: %v", cost, bcrypt.DefaultCost, err)
	}
}
