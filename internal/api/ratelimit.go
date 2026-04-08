package api

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"strconv"
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

// GlobalCapacityTier defines tiered global rate limits that apply across all clients.
type GlobalCapacityTier struct {
	Name              string `json:"name"`
	MaxRequestsPerSec int    `json:"max_requests_per_sec"`
	BurstSize         int    `json:"burst_size"`
}

var defaultGlobalTiers = []GlobalCapacityTier{
	{Name: "free", MaxRequestsPerSec: 10, BurstSize: 20},
	{Name: "standard", MaxRequestsPerSec: 100, BurstSize: 200},
	{Name: "premium", MaxRequestsPerSec: 1000, BurstSize: 2000},
}

// GlobalCapacity returns the default global capacity tiers.
func GlobalCapacity() []GlobalCapacityTier {
	result := make([]GlobalCapacityTier, len(defaultGlobalTiers))
	copy(result, defaultGlobalTiers)
	return result
}

// GlobalCapacityForTier returns the tier config by name, or the free tier if not found.
func GlobalCapacityForTier(name string) GlobalCapacityTier {
	for _, t := range defaultGlobalTiers {
		if t.Name == name {
			return t
		}
	}
	return defaultGlobalTiers[0] // default to free
}

// writeRateLimitHeaders adds RFC 6585 compliant rate limit headers to a response.
// X-RateLimit-Limit, X-RateLimit-Remaining, X-RateLimit-Reset, and Retry-After.
func writeRateLimitHeaders(w http.ResponseWriter, limit, remaining int, resetAt time.Time) {
	w.Header().Set("X-RateLimit-Limit", strconv.Itoa(limit))
	w.Header().Set("X-RateLimit-Remaining", strconv.Itoa(remaining))
	w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(resetAt.Unix(), 10))
	if remaining <= 0 {
		retryAfter := int(time.Until(resetAt).Seconds())
		if retryAfter < 1 {
			retryAfter = 1
		}
		w.Header().Set("Retry-After", strconv.Itoa(retryAfter))
	}
}

// TrustedProxyCIDRs validates and stores trusted proxy CIDR ranges.
type TrustedProxyCIDRs struct {
	mu   sync.RWMutex
	nets []*net.IPNet
}

// NewTrustedProxyCIDRs creates an empty trusted proxy set.
func NewTrustedProxyCIDRs() *TrustedProxyCIDRs {
	return &TrustedProxyCIDRs{}
}

// SetTrustedProxyCIDRs parses and validates CIDR strings, replacing the current set.
// Returns an error if any CIDR string is invalid.
func (tp *TrustedProxyCIDRs) SetTrustedProxyCIDRs(cidrs []string) error {
	var nets []*net.IPNet
	for _, cidr := range cidrs {
		_, ipNet, err := net.ParseCIDR(cidr)
		if err != nil {
			return fmt.Errorf("invalid CIDR %q: %w", cidr, err)
		}
		nets = append(nets, ipNet)
	}
	tp.mu.Lock()
	tp.nets = nets
	tp.mu.Unlock()
	return nil
}

// IsTrusted checks if the given IP is within any trusted CIDR range.
func (tp *TrustedProxyCIDRs) IsTrusted(ip string) bool {
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return false
	}
	tp.mu.RLock()
	defer tp.mu.RUnlock()
	for _, n := range tp.nets {
		if n.Contains(parsed) {
			return true
		}
	}
	return false
}

// CIDRCount returns the number of configured trusted CIDRs.
func (tp *TrustedProxyCIDRs) CIDRCount() int {
	tp.mu.RLock()
	defer tp.mu.RUnlock()
	return len(tp.nets)
}

// ThrottleSnapshot captures rate limit state for admin reporting.
type ThrottleSnapshot struct {
	Timestamp        time.Time `json:"timestamp"`
	ActiveBuckets    int       `json:"active_buckets"`
	TotalRequests    int64     `json:"total_requests"`
	RejectedRequests int64     `json:"rejected_requests"`
	TopOffenders     []string  `json:"top_offenders,omitempty"`
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
