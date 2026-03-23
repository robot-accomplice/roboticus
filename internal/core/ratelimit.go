package core

import (
	"sync"
	"time"
)

// RateLimiter implements a simple sliding window rate limiter.
type RateLimiter struct {
	mu         sync.Mutex
	maxRequests int
	window      time.Duration
	timestamps  []time.Time
}

// NewRateLimiter creates a rate limiter allowing maxRequests per window.
func NewRateLimiter(maxRequests int, window time.Duration) *RateLimiter {
	return &RateLimiter{
		maxRequests: maxRequests,
		window:      window,
	}
}

// Allow returns true if the request is within rate limits.
func (rl *RateLimiter) Allow() bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	cutoff := now.Add(-rl.window)

	// Prune old timestamps.
	fresh := rl.timestamps[:0]
	for _, t := range rl.timestamps {
		if t.After(cutoff) {
			fresh = append(fresh, t)
		}
	}
	rl.timestamps = fresh

	if len(rl.timestamps) >= rl.maxRequests {
		return false
	}
	rl.timestamps = append(rl.timestamps, now)
	return true
}
