package server

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// rateLimiter is a small fixed-window per-key limiter used to throttle
// abuse-prone endpoints (login guessing, demo guest creation). It is
// intentionally simple: a map of key -> hits in the current window,
// swept lazily. Good enough to blunt automated abuse without a
// dependency or a background goroutine.
type rateLimiter struct {
	mu     sync.Mutex
	limit  int
	window time.Duration
	hits   map[string]*rlEntry
	last   time.Time // last sweep
}

type rlEntry struct {
	count int
	reset time.Time
}

func newRateLimiter(limit int, window time.Duration) *rateLimiter {
	return &rateLimiter{limit: limit, window: window, hits: map[string]*rlEntry{}}
}

// allow reports whether a request for key may proceed and records it.
func (rl *rateLimiter) allow(key string) bool {
	now := time.Now()
	rl.mu.Lock()
	defer rl.mu.Unlock()

	// Lazy sweep of expired buckets, at most once per window.
	if now.Sub(rl.last) > rl.window {
		for k, e := range rl.hits {
			if now.After(e.reset) {
				delete(rl.hits, k)
			}
		}
		rl.last = now
	}

	e := rl.hits[key]
	if e == nil || now.After(e.reset) {
		rl.hits[key] = &rlEntry{count: 1, reset: now.Add(rl.window)}
		return true
	}
	if e.count >= rl.limit {
		return false
	}
	e.count++
	return true
}

// clientIP extracts the caller's address, trusting X-Forwarded-For only
// for its last hop (the reverse proxy we sit behind appends the real
// client). Falls back to RemoteAddr.
func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		if ip := strings.TrimSpace(parts[len(parts)-1]); ip != "" {
			return ip
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// requestIsSecure reports whether the request reached us over TLS,
// directly or via a TLS-terminating proxy.
func requestIsSecure(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	return strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

// securityHeaders adds defensive response headers to every request.
// The CSP allows the embedded SPA (self scripts/styles, inline styles
// from React) and remote book covers from enrichment sources over https.
func securityHeaders(next http.Handler) http.Handler {
	const csp = "default-src 'self'; " +
		"script-src 'self'; " +
		"style-src 'self' 'unsafe-inline'; " +
		"img-src 'self' data: https:; " +
		"font-src 'self' data:; " +
		"connect-src 'self'; " +
		"object-src 'none'; " +
		"base-uri 'self'; " +
		"frame-ancestors 'none'"
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Content-Security-Policy", csp)
		if requestIsSecure(r) {
			h.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		}
		next.ServeHTTP(w, r)
	})
}
