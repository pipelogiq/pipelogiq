package api

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestClientIPTrustBoundary(t *testing.T) {
	for _, tc := range []struct {
		name, peer, realIP, want string
	}{
		{"direct IPv4", "192.0.2.20:1234", "198.51.100.1", "192.0.2.20:1234"},
		{"direct IPv6", "[2001:db8::20]:1234", "198.51.100.1", "[2001:db8::20]:1234"},
		{"nginx IPv4", "127.0.0.1:1234", "198.51.100.1", "198.51.100.1"},
		{"nginx IPv6", "[::1]:1234", "2001:db8::5", "2001:db8::5"},
		{"invalid address", "127.0.0.1:1234", "attacker", "127.0.0.1:1234"},
		{"address chain", "127.0.0.1:1234", "198.51.100.1, 198.51.100.2", "127.0.0.1:1234"},
		{"missing real IP", "127.0.0.1:1234", "", "127.0.0.1:1234"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/", nil)
			r.RemoteAddr = tc.peer
			r.Header.Set("X-Real-IP", tc.realIP)
			r.Header.Set("True-Client-IP", "203.0.113.99")
			r.Header.Set("X-Forwarded-For", "203.0.113.98")
			loopbackProxyIP(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.RemoteAddr != tc.want {
					t.Errorf("client address = %q, want %q", r.RemoteAddr, tc.want)
				}
			})).ServeHTTP(httptest.NewRecorder(), r)
		})
	}
}

func TestForwardedHeadersCannotBypassLoginIPLimit(t *testing.T) {
	limiter := newLoginLimiter(time.Minute, 2, 100)
	handler := loopbackProxyIP(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if allowed, _ := limiter.allow(r.RemoteAddr, r.URL.Query().Get("email")); !allowed {
			w.WriteHeader(http.StatusTooManyRequests)
		}
	}))
	for i := 0; i < 3; i++ {
		r := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/auth/login?email=target%d@example.test", i), nil)
		r.RemoteAddr = "192.0.2.20:1234"
		for _, header := range []string{"X-Real-IP", "X-Forwarded-For", "True-Client-IP"} {
			r.Header.Set(header, fmt.Sprintf("198.51.100.%d", i+1))
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		want := http.StatusOK
		if i == 2 {
			want = http.StatusTooManyRequests
		}
		if w.Code != want {
			t.Fatalf("attempt %d status = %d, want %d", i+1, w.Code, want)
		}
	}
}
