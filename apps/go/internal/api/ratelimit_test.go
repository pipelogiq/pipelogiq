package api

import (
	"fmt"
	"testing"
	"time"
)

func TestLoginLimiterSharesBudgetAcrossConnections(t *testing.T) {
	for _, addresses := range [][]string{
		{"203.0.113.7:12001", "203.0.113.7:12002", "203.0.113.7:12003"},
		{"[2001:db8::1]:12001", "[2001:db8:0:0:0:0:0:1]:12002", "2001:db8::1"},
		{"[::ffff:203.0.113.7]:12001", "203.0.113.7", "203.0.113.7:12003"},
	} {
		t.Run(addresses[0], func(t *testing.T) {
			limiter := newLoginLimiter(time.Minute, 2, 10)
			for i, address := range addresses {
				allowed, retryAfter := limiter.allow(address, fmt.Sprintf("user%d@example.com", i))
				if allowed != (i < 2) {
					t.Fatalf("attempt %d from %s allowed=%v", i+1, address, allowed)
				}
				if !allowed && retryAfter <= 0 {
					t.Fatal("rate limited response has no retry delay")
				}
			}
			if allowed, _ := limiter.allow("203.0.113.8:12001", "other@example.com"); !allowed {
				t.Fatal("another client address shared the exhausted budget")
			}
		})
	}
}
