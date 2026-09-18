package api

import (
	"net"
	"net/http"
	"strings"
)

// loopbackProxyIP supports the bundled same-container nginx proxy, which
// overwrites X-Real-IP. Public callers must not choose the address used for
// login throttling or the audit trail through arbitrary forwarding headers.
// Remote proxies retain their transport address unless a future explicit
// trusted-proxy configuration is introduced.
func loopbackProxyIP(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		peer, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			peer = r.RemoteAddr
		}
		if ip := net.ParseIP(peer); ip != nil && ip.IsLoopback() {
			if clientIP := net.ParseIP(strings.TrimSpace(r.Header.Get("X-Real-IP"))); clientIP != nil {
				r.RemoteAddr = clientIP.String()
			}
		}
		next.ServeHTTP(w, r)
	})
}
