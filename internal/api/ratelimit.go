package api

import (
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

// RateLimitMiddleware returns chi-compatible middleware that enforces per-IP rate limits.
// Uses a fixed-window algorithm: each IP gets RequestsPerWindow requests per WindowSeconds window.
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

	var buckets sync.Map // map[string]*rateBucket

	// Background cleanup of stale buckets every 5 minutes.
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			cutoff := time.Now().Add(-2 * window)
			buckets.Range(func(key, value any) bool {
				b := value.(*rateBucket)
				b.mu.Lock()
				if b.windowStart.Before(cutoff) {
					buckets.Delete(key)
				}
				b.mu.Unlock()
				return true
			})
		}
	}()

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := extractIP(r)

			val, _ := buckets.LoadOrStore(ip, &rateBucket{windowStart: time.Now()})
			b := val.(*rateBucket)

			b.mu.Lock()
			now := time.Now()
			if now.Sub(b.windowStart) >= window {
				// New window.
				b.count = 0
				b.windowStart = now
			}
			b.count++
			exceeded := b.count > cfg.RequestsPerWindow
			b.mu.Unlock()

			if exceeded {
				w.Header().Set("Retry-After", "60")
				http.Error(w, `{"error":"rate limit exceeded"}`, http.StatusTooManyRequests)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
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
