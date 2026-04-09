package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func TestRateLimitMiddleware_DisabledPassesThrough(t *testing.T) {
	handler := RateLimitMiddleware(context.Background(), false, 100, 60)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "10.0.0.1:1234"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("disabled rate limit: status = %d, want 200", rec.Code)
	}
}

func TestRateLimitMiddleware_ZeroRequestsDisables(t *testing.T) {
	handler := RateLimitMiddleware(context.Background(), true, 0, 60)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "10.0.0.50:1234"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("zero requests per window should disable, got %d", rec.Code)
	}
}

func TestRateLimitMiddleware_IPLimitExceeded(t *testing.T) {
	handler := RateLimitMiddleware(context.Background(), true, 3, 60)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	for i := 0; i < 5; i++ {
		req := httptest.NewRequest("GET", "/", nil)
		req.RemoteAddr = "10.0.0.99:1234"
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if i < 3 && rec.Code != http.StatusOK {
			t.Errorf("request %d should pass (under limit), got %d", i, rec.Code)
		}
		if i >= 3 && rec.Code != http.StatusTooManyRequests {
			t.Errorf("request %d should be rate limited, got %d", i, rec.Code)
		}
	}
}

func TestRateLimitMiddleware_ActorLimitExceeded(t *testing.T) {
	// Per-actor limit is actorRequestsPerWindow (5000), which is high.
	// But per-IP limit of 2 should be hit first.
	handler := RateLimitMiddleware(context.Background(), true, 2, 60)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	for i := 0; i < 4; i++ {
		req := httptest.NewRequest("GET", "/", nil)
		req.RemoteAddr = "10.0.0.200:1234"
		req.Header.Set("x-api-key", "actor-key-test")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if i < 2 && rec.Code != http.StatusOK {
			t.Errorf("request %d should pass, got %d", i, rec.Code)
		}
		if i >= 2 && rec.Code != http.StatusTooManyRequests {
			t.Errorf("request %d should be rate limited, got %d", i, rec.Code)
		}
	}
}

func TestRateLimitMiddleware_RetryAfterHeader(t *testing.T) {
	handler := RateLimitMiddleware(context.Background(), true, 1, 60)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// First request passes.
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "10.0.0.201:1234"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	// Second request should be rate limited and include Retry-After.
	req = httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "10.0.0.201:1234"
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d", rec.Code)
	}
	if got := rec.Header().Get("Retry-After"); got != "60" {
		t.Errorf("Retry-After = %q, want 60", got)
	}
}

func TestRateLimitMiddleware_DifferentIPsIndependent(t *testing.T) {
	handler := RateLimitMiddleware(context.Background(), true, 1, 60)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// IP 1 gets limited.
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "10.0.0.210:1234"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("first IP first request: %d", rec.Code)
	}

	// IP 2 should still work.
	req = httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "10.0.0.211:1234"
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("second IP first request: %d", rec.Code)
	}
}

func TestRateLimitMiddleware_ZeroWindow(t *testing.T) {
	// Window of 0 should default to 60 seconds.
	handler := RateLimitMiddleware(context.Background(), true, 2, 0)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "10.0.0.220:1234"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
}

func TestHashAPIKey(t *testing.T) {
	h1 := hashAPIKey("key-1")
	h2 := hashAPIKey("key-2")
	h3 := hashAPIKey("key-1")

	if h1 == h2 {
		t.Error("different keys should produce different hashes")
	}
	if h1 != h3 {
		t.Error("same key should produce same hash")
	}
	if len(h1) != 64 { // SHA-256 = 32 bytes = 64 hex chars
		t.Errorf("hash length = %d, want 64", len(h1))
	}
}

func TestExtractIP_XRealIP(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("X-Real-IP", "1.2.3.4")
	req.RemoteAddr = "10.0.0.1:1234"

	ip := extractIP(req)
	if ip != "1.2.3.4" {
		t.Errorf("extractIP = %q, want 1.2.3.4", ip)
	}
}

func TestExtractIP_XForwardedFor(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("X-Forwarded-For", "5.6.7.8, 10.0.0.1")
	req.RemoteAddr = "10.0.0.1:1234"

	ip := extractIP(req)
	if ip != "5.6.7.8" {
		t.Errorf("extractIP = %q, want 5.6.7.8", ip)
	}
}

func TestExtractIP_XForwardedForSingleIP(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("X-Forwarded-For", "5.6.7.8")
	req.RemoteAddr = "10.0.0.1:1234"

	ip := extractIP(req)
	if ip != "5.6.7.8" {
		t.Errorf("extractIP = %q, want 5.6.7.8", ip)
	}
}

func TestExtractIP_RemoteAddr(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "192.168.1.1:54321"

	ip := extractIP(req)
	if ip != "192.168.1.1" {
		t.Errorf("extractIP = %q, want 192.168.1.1", ip)
	}
}

func TestExtractIP_RemoteAddrNoPort(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "192.168.1.1" // no port

	ip := extractIP(req)
	if ip != "192.168.1.1" {
		t.Errorf("extractIP = %q, want 192.168.1.1", ip)
	}
}

func TestCheckBucket_WindowReset(t *testing.T) {
	var buckets sync.Map
	exceeded := checkBucket(&buckets, "test-key-direct", 1, 60*time.Second)
	if exceeded {
		t.Error("first request should not be rate limited")
	}
	exceeded = checkBucket(&buckets, "test-key-direct", 1, 60*time.Second)
	if !exceeded {
		t.Error("second request should be rate limited")
	}
}

// --- Wave 11: #97 Global capacity tiers ---

func TestGlobalCapacity(t *testing.T) {
	tiers := GlobalCapacity()
	if len(tiers) < 3 {
		t.Fatalf("expected at least 3 tiers, got %d", len(tiers))
	}
	names := map[string]bool{}
	for _, tier := range tiers {
		names[tier.Name] = true
		if tier.MaxRequestsPerSec <= 0 {
			t.Errorf("tier %q has non-positive MaxRequestsPerSec", tier.Name)
		}
	}
	for _, name := range []string{"free", "standard", "premium"} {
		if !names[name] {
			t.Errorf("missing tier %q", name)
		}
	}
}

func TestGlobalCapacityForTier(t *testing.T) {
	free := GlobalCapacityForTier("free")
	if free.Name != "free" {
		t.Errorf("expected free tier, got %q", free.Name)
	}

	premium := GlobalCapacityForTier("premium")
	if premium.MaxRequestsPerSec <= free.MaxRequestsPerSec {
		t.Error("premium should have higher limit than free")
	}

	// Unknown tier defaults to free.
	unknown := GlobalCapacityForTier("nonexistent")
	if unknown.Name != "free" {
		t.Errorf("unknown tier should default to free, got %q", unknown.Name)
	}
}

// --- Wave 11: #98 RFC 6585 response headers ---

func TestWriteRateLimitHeaders(t *testing.T) {
	rec := httptest.NewRecorder()
	reset := time.Now().Add(60 * time.Second)
	writeRateLimitHeaders(rec, 100, 50, reset)

	if got := rec.Header().Get("X-RateLimit-Limit"); got != "100" {
		t.Errorf("X-RateLimit-Limit = %q, want 100", got)
	}
	if got := rec.Header().Get("X-RateLimit-Remaining"); got != "50" {
		t.Errorf("X-RateLimit-Remaining = %q, want 50", got)
	}
	if got := rec.Header().Get("X-RateLimit-Reset"); got == "" {
		t.Error("X-RateLimit-Reset should be set")
	}
	// Should NOT have Retry-After when remaining > 0.
	if got := rec.Header().Get("Retry-After"); got != "" {
		t.Errorf("Retry-After should be empty when remaining > 0, got %q", got)
	}
}

func TestWriteRateLimitHeaders_Exhausted(t *testing.T) {
	rec := httptest.NewRecorder()
	reset := time.Now().Add(30 * time.Second)
	writeRateLimitHeaders(rec, 100, 0, reset)

	if got := rec.Header().Get("Retry-After"); got == "" {
		t.Error("Retry-After should be set when remaining = 0")
	}
}

// --- Wave 11: #99 Trusted proxy CIDR validation ---

func TestTrustedProxyCIDRs(t *testing.T) {
	tp := NewTrustedProxyCIDRs()
	if tp.CIDRCount() != 0 {
		t.Error("initial count should be 0")
	}

	err := tp.SetTrustedProxyCIDRs([]string{"10.0.0.0/8", "192.168.0.0/16"})
	if err != nil {
		t.Fatalf("valid CIDRs should not error: %v", err)
	}
	if tp.CIDRCount() != 2 {
		t.Errorf("count = %d, want 2", tp.CIDRCount())
	}

	if !tp.IsTrusted("10.1.2.3") {
		t.Error("10.1.2.3 should be trusted")
	}
	if !tp.IsTrusted("192.168.1.1") {
		t.Error("192.168.1.1 should be trusted")
	}
	if tp.IsTrusted("8.8.8.8") {
		t.Error("8.8.8.8 should not be trusted")
	}
	if tp.IsTrusted("invalid-ip") {
		t.Error("invalid IP should not be trusted")
	}
}

func TestTrustedProxyCIDRs_InvalidCIDR(t *testing.T) {
	tp := NewTrustedProxyCIDRs()
	err := tp.SetTrustedProxyCIDRs([]string{"not-a-cidr"})
	if err == nil {
		t.Error("invalid CIDR should return error")
	}
}

// --- Wave 11: #100 ThrottleSnapshot ---

func TestThrottleSnapshot(t *testing.T) {
	snap := ThrottleSnapshot{
		Timestamp:        time.Now(),
		ActiveBuckets:    42,
		TotalRequests:    10000,
		RejectedRequests: 150,
		TopOffenders:     []string{"10.0.0.1", "10.0.0.2"},
	}
	if snap.ActiveBuckets != 42 {
		t.Errorf("ActiveBuckets = %d", snap.ActiveBuckets)
	}
	if snap.TotalRequests != 10000 {
		t.Errorf("TotalRequests = %d", snap.TotalRequests)
	}
	if snap.RejectedRequests != 150 {
		t.Errorf("RejectedRequests = %d", snap.RejectedRequests)
	}
	if len(snap.TopOffenders) != 2 {
		t.Errorf("TopOffenders = %d", len(snap.TopOffenders))
	}
}

func TestCheckBucket_DifferentKeys(t *testing.T) {
	var buckets sync.Map
	exceeded := checkBucket(&buckets, "key-a", 1, 60*time.Second)
	if exceeded {
		t.Error("key-a first request should pass")
	}
	exceeded = checkBucket(&buckets, "key-b", 1, 60*time.Second)
	if exceeded {
		t.Error("key-b first request should pass (different bucket)")
	}
}
