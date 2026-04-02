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

// GovernorConfig sets limits for the governor.
type GovernorConfig struct {
	MaxTurnsPerSession  int
	MaxTokensPerSession int
	MaxCostPerSession   float64
	CooldownAfterError  time.Duration
}

// Governor enforces rate-limiting and cost-control for inference.
type Governor struct {
	cfg       GovernorConfig
	mu        sync.Mutex
	lastError time.Time
}

// NewGovernor creates a governor with the given config.
func NewGovernor(cfg GovernorConfig) *Governor {
	return &Governor{cfg: cfg}
}

// Check evaluates whether the next inference should proceed.
func (g *Governor) Check(turnsSoFar int, tokensSoFar int, costSoFar float64) GovernorDecision {
	// Zero config = no limits.
	if g.cfg.MaxTurnsPerSession == 0 && g.cfg.MaxTokensPerSession == 0 && g.cfg.MaxCostPerSession == 0 {
		return GovernorAllow
	}

	// Hard deny: any limit exceeded.
	if g.cfg.MaxTurnsPerSession > 0 && turnsSoFar > g.cfg.MaxTurnsPerSession {
		return GovernorDeny
	}
	if g.cfg.MaxTokensPerSession > 0 && tokensSoFar > g.cfg.MaxTokensPerSession {
		return GovernorDeny
	}
	if g.cfg.MaxCostPerSession > 0 && costSoFar > g.cfg.MaxCostPerSession {
		return GovernorDeny
	}

	// Soft throttle: approaching 80% of any limit.
	if g.cfg.MaxTurnsPerSession > 0 && float64(turnsSoFar) >= float64(g.cfg.MaxTurnsPerSession)*0.8 {
		return GovernorThrottle
	}
	if g.cfg.MaxTokensPerSession > 0 && float64(tokensSoFar) >= float64(g.cfg.MaxTokensPerSession)*0.8 {
		return GovernorThrottle
	}
	if g.cfg.MaxCostPerSession > 0 && costSoFar >= g.cfg.MaxCostPerSession*0.8 {
		return GovernorThrottle
	}

	return GovernorAllow
}

// RecordError records that an error occurred, triggering cooldown.
func (g *Governor) RecordError() {
	g.mu.Lock()
	g.lastError = time.Now()
	g.mu.Unlock()
}

// InCooldown returns true if the governor is in error cooldown.
func (g *Governor) InCooldown() bool {
	if g.cfg.CooldownAfterError == 0 {
		return false
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	return time.Since(g.lastError) < g.cfg.CooldownAfterError
}
