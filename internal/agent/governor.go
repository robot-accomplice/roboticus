package agent

import (
	"sync"
	"time"
)

// GovernorDecision represents whether inference should proceed.
type GovernorDecision int

const (
	GovernorAllow    GovernorDecision = iota // Proceed normally
	GovernorThrottle                         // Slow down (approaching limits)
	GovernorDeny                             // Stop (limits exceeded)
)

func (d GovernorDecision) String() string {
	switch d {
	case GovernorAllow:
		return "allow"
	case GovernorThrottle:
		return "throttle"
	case GovernorDeny:
		return "deny"
	default:
		return "unknown"
	}
}

// RateLimiterConfig sets limits for the rate limiter.
type RateLimiterConfig struct {
	MaxTurnsPerSession  int
	MaxTokensPerSession int
	MaxCostPerSession   float64
	CooldownAfterError  time.Duration
}

// RateLimiter enforces rate-limiting and cost-control for inference.
type RateLimiter struct {
	cfg       RateLimiterConfig
	mu        sync.Mutex
	lastError time.Time
}

// NewRateLimiter creates a rate limiter with the given config.
func NewRateLimiter(cfg RateLimiterConfig) *RateLimiter {
	return &RateLimiter{cfg: cfg}
}

// Check evaluates whether the next inference should proceed.
func (r *RateLimiter) Check(turnsSoFar int, tokensSoFar int, costSoFar float64) GovernorDecision {
	// Zero config = no limits.
	if r.cfg.MaxTurnsPerSession == 0 && r.cfg.MaxTokensPerSession == 0 && r.cfg.MaxCostPerSession == 0 {
		return GovernorAllow
	}

	// Hard deny: any limit exceeded.
	if r.cfg.MaxTurnsPerSession > 0 && turnsSoFar > r.cfg.MaxTurnsPerSession {
		return GovernorDeny
	}
	if r.cfg.MaxTokensPerSession > 0 && tokensSoFar > r.cfg.MaxTokensPerSession {
		return GovernorDeny
	}
	if r.cfg.MaxCostPerSession > 0 && costSoFar > r.cfg.MaxCostPerSession {
		return GovernorDeny
	}

	// Soft throttle: approaching 80% of any limit.
	if r.cfg.MaxTurnsPerSession > 0 && float64(turnsSoFar) >= float64(r.cfg.MaxTurnsPerSession)*0.8 {
		return GovernorThrottle
	}
	if r.cfg.MaxTokensPerSession > 0 && float64(tokensSoFar) >= float64(r.cfg.MaxTokensPerSession)*0.8 {
		return GovernorThrottle
	}
	if r.cfg.MaxCostPerSession > 0 && costSoFar >= r.cfg.MaxCostPerSession*0.8 {
		return GovernorThrottle
	}

	return GovernorAllow
}

// RecordError records that an error occurred, triggering cooldown.
func (r *RateLimiter) RecordError() {
	r.mu.Lock()
	r.lastError = time.Now()
	r.mu.Unlock()
}

// InCooldown returns true if the rate limiter is in error cooldown.
func (r *RateLimiter) InCooldown() bool {
	if r.cfg.CooldownAfterError == 0 {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return time.Since(r.lastError) < r.cfg.CooldownAfterError
}
