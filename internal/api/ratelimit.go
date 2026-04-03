package api

import (
	"crypto/sha256"
	"encoding/hex"
	"net"
	"net/http"
	"sync"
	"time"
)

type rateBucket struct {
	mu          sync.Mutex
	count       int
	windowStart time.Time
}

// actorRequestsPerWindow is the default per-actor (API key) limit, higher than per-IP.
const actorRequestsPerWindow = 5000

// RateLimitMiddleware returns chi-compatible middleware that enforces per-IP and per-actor rate limits.
// Uses a fixed-window algorithm: each IP gets RequestsPerWindow requests per WindowSeconds window.
// If an x-api-key header is present, a separate per-actor bucket with a higher limit is used.
func RateLimitMiddleware(enabled bool, requestsPerWindow, windowSeconds int) func(http.Handler) http.Handler {
	cfg := struct {
		Enabled           bool
		RequestsPerWindow int
		WindowSeconds     int
	}{enabled, requestsPerWindow, windowSeconds}
	if !cfg.Enabled || cfg.RequestsPerWindow <= 0 {
		return func(next http.Handler) http.Handler { return next }
	}

	window := time.Duration(cfg.WindowSeconds) * time.Second
	if window <= 0 {
		window = 60 * time.Second
	}

	var ipBuckets sync.Map    // map[string]*rateBucket — keyed by IP
	var actorBuckets sync.Map // map[string]*rateBucket — keyed by hashed API key

	// Background cleanup of stale buckets every 5 minutes.
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			cutoff := time.Now().Add(-2 * window)
			cleanup := func(key, value any) bool {
				b := value.(*rateBucket)
				b.mu.Lock()
				if b.windowStart.Before(cutoff) {
					ipBuckets.Delete(key)
					actorBuckets.Delete(key)
				}
				b.mu.Unlock()
				return true
			}
			ipBuckets.Range(cleanup)
			actorBuckets.Range(cleanup)
		}
	}()

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Check per-actor limit if API key is present.
			if apiKey := r.Header.Get("x-api-key"); apiKey != "" {
				actorID := hashAPIKey(apiKey)
				if checkBucket(&actorBuckets, actorID, actorRequestsPerWindow, window) {
					w.Header().Set("Retry-After", "60")
					http.Error(w, `{"error":"rate limit exceeded (actor)"}`, http.StatusTooManyRequests)
					return
				}
			}

			// Check per-IP limit.
			ip := extractIP(r)
			if checkBucket(&ipBuckets, ip, cfg.RequestsPerWindow, window) {
				w.Header().Set("Retry-After", "60")
				http.Error(w, `{"error":"rate limit exceeded"}`, http.StatusTooManyRequests)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// checkBucket increments the count for the given key and returns true if the limit is exceeded.
func checkBucket(buckets *sync.Map, key string, limit int, window time.Duration) bool {
	val, _ := buckets.LoadOrStore(key, &rateBucket{windowStart: time.Now()})
	b := val.(*rateBucket)

	b.mu.Lock()
	defer b.mu.Unlock()

	now := time.Now()
	if now.Sub(b.windowStart) >= window {
		b.count = 0
		b.windowStart = now
	}
	b.count++
	return b.count > limit
}

// hashAPIKey produces a hex-encoded SHA-256 hash of the API key for use as a bucket key.
func hashAPIKey(key string) string {
	h := sha256.Sum256([]byte(key))
	return hex.EncodeToString(h[:])
}

// extractIP gets the client IP from X-Real-IP, X-Forwarded-For, or RemoteAddr.
func extractIP(r *http.Request) string {
	if ip := r.Header.Get("X-Real-IP"); ip != "" {
		return ip
	}
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		// Take the first IP in the chain.
		for i := 0; i < len(xff); i++ {
			if xff[i] == ',' {
				return xff[:i]
			}
		}
		return xff
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
