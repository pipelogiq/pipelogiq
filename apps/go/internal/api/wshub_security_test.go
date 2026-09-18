package api

import (
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestWebSocketSessionExpiresWithoutBroadcasts(t *testing.T) {
	hub := NewHub(slog.New(slog.NewTextHandler(io.Discard, nil)), []string{"http://localhost"})
	expiresAt := time.Now().Add(250 * time.Millisecond)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := context.WithValue(r.Context(), applicationScopeKey, newApplicationScope([]int{ownApplicationID}))
		ctx = context.WithValue(ctx, tokenExpiresAtKey, expiresAt)
		hub.ServeWS(w, r.WithContext(ctx))
	}))
	defer server.Close()
	conn, response, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), http.Header{"Origin": []string{"http://localhost"}})
	if err != nil {
		t.Fatalf("upgrade: %v, response=%v", err, response)
	}
	defer conn.Close()
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, _, err = conn.ReadMessage()
	if err == nil {
		t.Fatal("expired connection delivered a message")
	}
	if timeout, ok := err.(net.Error); ok && timeout.Timeout() {
		t.Fatalf("connection stayed open past session expiry: %v", err)
	}
	if time.Now().Before(expiresAt) {
		t.Fatal("connection closed before session expiry")
	}
	if hub.clientCount() != 0 {
		t.Fatal("expired client remains registered")
	}
}

func TestWebSocketRejectsMissingOrExpiredSessionExpiry(t *testing.T) {
	for _, tc := range []struct {
		name      string
		expiresAt time.Time
	}{
		{"missing", time.Time{}}, {"expired", time.Now().Add(-time.Second)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			hub := NewHub(slog.New(slog.NewTextHandler(io.Discard, nil)), []string{"http://localhost"})
			ctx := context.WithValue(context.Background(), applicationScopeKey, newApplicationScope([]int{ownApplicationID}))
			if !tc.expiresAt.IsZero() {
				ctx = context.WithValue(ctx, tokenExpiresAtKey, tc.expiresAt)
			}
			response := httptest.NewRecorder()
			hub.ServeWS(response, httptest.NewRequest(http.MethodGet, "/ws", nil).WithContext(ctx))
			if response.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401", response.Code)
			}
		})
	}
}

func TestHubBroadcastDisconnectsExpiredSessions(t *testing.T) {
	hub := NewHub(slog.New(slog.NewTextHandler(io.Discard, nil)), []string{"http://localhost"})
	active := &Client{hub: hub, send: make(chan []byte, 1), scope: newApplicationScope([]int{ownApplicationID}), expiresAt: time.Now().Add(time.Hour)}
	expired := &Client{hub: hub, send: make(chan []byte, 1), scope: newApplicationScope([]int{ownApplicationID}), expiresAt: time.Now().Add(-time.Second)}
	expiredOther := &Client{hub: hub, send: make(chan []byte, 1), scope: newApplicationScope([]int{otherApplicationID}), expiresAt: time.Now().Add(-time.Second)}
	for _, client := range []*Client{active, expired, expiredOther} {
		hub.register(client)
	}
	hub.Broadcast([]byte(`{"applicationId":1}`))
	if hub.clientCount() != 1 {
		t.Fatalf("retained %d clients, want only active", hub.clientCount())
	}
	for _, client := range []*Client{expired, expiredOther} {
		if _, ok := <-client.send; ok {
			t.Fatal("expired client received a message")
		}
	}
	select {
	case <-active.send:
	default:
		t.Fatal("active session did not receive its update")
	}
}
