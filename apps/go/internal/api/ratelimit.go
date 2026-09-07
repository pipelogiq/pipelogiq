package api

import (
	"sync"
	"time"
)

// loginLimiter bounds sign-in attempts per address and per account with a
// sliding window. It is per process; a multi-instance deployment gets the bound
// multiplied by the instance count, which is still a bound.
type loginLimiter struct {
	mu       sync.Mutex
	window   time.Duration
	perIP    int
	perEmail int
	attempts map[string][]time.Time
	lastGC   time.Time
}

func newLoginLimiter(window time.Duration, perIP, perEmail int) *loginLimiter {
	return &loginLimiter{window: window, perIP: perIP, perEmail: perEmail, attempts: map[string][]time.Time{}}
}

// allow records an attempt and reports whether it may proceed. When refused, the
// second value is how long the caller should wait.
func (l *loginLimiter) allow(ip, email string) (bool, time.Duration) {
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()

	if now.Sub(l.lastGC) > l.window {
		for key, times := range l.attempts {
			if kept := l.prune(times, now); len(kept) == 0 {
				delete(l.attempts, key)
			} else {
				l.attempts[key] = kept
			}
		}
		l.lastGC = now
	}

	ipKey, emailKey := "ip:"+ip, "email:"+email
	ipTimes := l.prune(l.attempts[ipKey], now)
	emailTimes := l.prune(l.attempts[emailKey], now)

	if len(ipTimes) >= l.perIP {
		return false, l.retryAfter(ipTimes, now)
	}
	if email != "" && len(emailTimes) >= l.perEmail {
		return false, l.retryAfter(emailTimes, now)
	}

	l.attempts[ipKey] = append(ipTimes, now)
	if email != "" {
		l.attempts[emailKey] = append(emailTimes, now)
	}
	return true, 0
}

// reset clears the account's counter after a successful sign-in, so a user who
// mistyped twice is not punished for the rest of the window.
func (l *loginLimiter) reset(email string) {
	l.mu.Lock()
	delete(l.attempts, "email:"+email)
	l.mu.Unlock()
}

func (l *loginLimiter) prune(times []time.Time, now time.Time) []time.Time {
	cutoff := now.Add(-l.window)
	kept := times[:0]
	for _, t := range times {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	return kept
}

func (l *loginLimiter) retryAfter(times []time.Time, now time.Time) time.Duration {
	if len(times) == 0 {
		return l.window
	}
	wait := times[0].Add(l.window).Sub(now)
	if wait < time.Second {
		wait = time.Second
	}
	return wait
}
