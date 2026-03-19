package api

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestJWTIncludesRoleClaims(t *testing.T) {
	secret := []byte("0123456789abcdef0123456789abcdef")

	token, err := generateJWT(secret, 42, "admin@pipelogiq.local", "Admin")
	if err != nil {
		t.Fatalf("generateJWT() error = %v", err)
	}

	claims, err := parseJWT(secret, token)
	if err != nil {
		t.Fatalf("parseJWT() error = %v", err)
	}

	if claims.UserID != 42 {
		t.Fatalf("claims.UserID = %d, want 42", claims.UserID)
	}
	if claims.Role != "Admin" {
		t.Fatalf("claims.Role = %q, want %q", claims.Role, "Admin")
	}
}

func TestRequireAdminMiddleware(t *testing.T) {
	server := &Server{}
	next := server.requireAdminMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	t.Run("rejects non admin role", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/admin", nil)
		req = req.WithContext(context.WithValue(req.Context(), userRoleKey, "regularuser"))
		rec := httptest.NewRecorder()

		next.ServeHTTP(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusForbidden)
		}
	})

	t.Run("allows admin role", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/admin", nil)
		req = req.WithContext(context.WithValue(req.Context(), userRoleKey, "admin"))
		rec := httptest.NewRecorder()

		next.ServeHTTP(rec, req)

		if rec.Code != http.StatusNoContent {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusNoContent)
		}
	})
}

func TestCORSMiddleware(t *testing.T) {
	allowed := allowedOriginsMap([]string{"http://localhost:3300"})
	middleware := newCORSMiddleware(allowed)

	handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	t.Run("allows configured origin", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/pipelines", nil)
		req.Header.Set("Origin", "http://localhost:3300")
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusNoContent {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusNoContent)
		}
		if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "http://localhost:3300" {
			t.Fatalf("allow origin = %q, want %q", got, "http://localhost:3300")
		}
	})

	t.Run("rejects unconfigured origin", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/pipelines", nil)
		req.Header.Set("Origin", "https://evil.example")
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusForbidden)
		}
	})
}

func TestHubOriginCheck(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	hub := NewHub(logger, []string{"http://localhost:3300"})

	req := httptest.NewRequest(http.MethodGet, "/ws", nil)
	req.Header.Set("Origin", "http://localhost:3300")
	if !hub.upgrader.CheckOrigin(req) {
		t.Fatal("expected configured origin to be allowed")
	}

	req.Header.Set("Origin", "https://evil.example")
	if hub.upgrader.CheckOrigin(req) {
		t.Fatal("expected unconfigured origin to be rejected")
	}
}
