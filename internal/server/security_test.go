package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestSecurityHeaders(t *testing.T) {
	h := securityHeaders(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	req := httptest.NewRequest("GET", "https://x/", nil)
	req.Header.Set("X-Forwarded-Proto", "https")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	for _, hdr := range []string{"Content-Security-Policy", "X-Content-Type-Options", "X-Frame-Options", "Strict-Transport-Security"} {
		if rec.Header().Get(hdr) == "" {
			t.Errorf("missing %s", hdr)
		}
	}
	// HSTS must not be sent over plain http
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, httptest.NewRequest("GET", "http://x/", nil))
	if rec2.Header().Get("Strict-Transport-Security") != "" {
		t.Error("HSTS sent over http")
	}
}

func TestRateLimiter(t *testing.T) {
	rl := newRateLimiter(3, time.Minute)
	for i := 0; i < 3; i++ {
		if !rl.allow("ip1") {
			t.Fatalf("request %d should pass", i)
		}
	}
	if rl.allow("ip1") {
		t.Error("4th request should be blocked")
	}
	if !rl.allow("ip2") {
		t.Error("different key should be independent")
	}
}
