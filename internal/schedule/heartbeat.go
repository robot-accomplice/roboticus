package schedule

import (
	"context"
	"time"

	"github.com/rs/zerolog/log"

	"roboticus/internal/core"
)

// HeartbeatTaskKind enumerates recurring heartbeat tasks.
type HeartbeatTaskKind string

const (
	TaskSurvivalCheck    HeartbeatTaskKind = "survival_check"
	TaskUSDCMonitor      HeartbeatTaskKind = "usdc_monitor"
	TaskYield            HeartbeatTaskKind = "yield"
	TaskMemoryPrune      HeartbeatTaskKind = "memory_prune"
	TaskCacheEvict       HeartbeatTaskKind = "cache_evict"
	TaskMetricSnapshot   HeartbeatTaskKind = "metric_snapshot"
	TaskAgentCardRefresh HeartbeatTaskKind = "agent_card_refresh"
	TaskSessionGovernor  HeartbeatTaskKind = "session_governor"
)

// TickContext provides runtime state to heartbeat tasks.
type TickContext struct {
	CreditBalance float64
	USDCBalance   float64
	SurvivalTier  core.SurvivalTier
	Timestamp     time.Time
}

// TaskResult reports the outcome of a heartbeat task.
type TaskResult struct {
	Success    bool   `json:"success"`
	Message    string `json:"message"`
	ShouldWake bool   `json:"should_wake"` // trigger immediate follow-up
}

// HeartbeatTask is the interface for periodic maintenance tasks.
type HeartbeatTask interface {
	Kind() HeartbeatTaskKind
	Run(ctx context.Context, tctx *TickContext) TaskResult
}

// HeartbeatDaemon runs periodic maintenance tasks with tier-aware interval adjustment.
type HeartbeatDaemon struct {
	interval         time.Duration
	originalInterval time.Duration
	tasks            []HeartbeatTask
}

// NewHeartbeatDaemon creates a heartbeat daemon with the given base interval.
func NewHeartbeatDaemon(interval time.Duration, tasks []HeartbeatTask) *HeartbeatDaemon {
	return &HeartbeatDaemon{
		interval:         interval,
		originalInterval: interval,
		tasks:            tasks,
	}
}

// Run starts the heartbeat loop. Blocks until context is cancelled.
func (d *HeartbeatDaemon) Run(ctx context.Context, tickCtxFn func() *TickContext) {
	ticker := time.NewTicker(d.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Info().Msg("heartbeat daemon stopping")
			return
		case <-ticker.C:
			tctx := tickCtxFn()
			d.adjustInterval(tctx.SurvivalTier)
			ticker.Reset(d.interval)

			var shouldWake bool
			for _, task := range d.tasks {
				result := task.Run(ctx, tctx)
				if !result.Success {
					log.Warn().
						Str("task", string(task.Kind())).
						Str("error", result.Message).
						Msg("heartbeat task failed")
				}
				if result.ShouldWake {
					shouldWake = true
				}
			}

			if shouldWake {
				log.Debug().Msg("heartbeat: wake triggered, running immediate tick")
				// Re-run tasks on wake.
				tctx = tickCtxFn()
				for _, task := range d.tasks {
					task.Run(ctx, tctx)
				}
			}
		}
	}
}

// adjustInterval changes the tick interval based on survival tier.
// LowCompute/Critical: 2x slower. Dead: 10x slower.
// Clamps to [10s, 5min] (or [10s, 1h] for Dead).
func (d *HeartbeatDaemon) adjustInterval(tier core.SurvivalTier) {
	base := d.originalInterval
	var adjusted time.Duration

	switch tier {
	case core.SurvivalTierThriving, core.SurvivalTierGrowth:
		adjusted = base
	case core.SurvivalTierStable:
		adjusted = base
	case core.SurvivalTierSurvival:
		adjusted = base * 2
	case core.SurvivalTierDead:
		adjusted = base * 10
	default:
		adjusted = base
	}

	// Clamp.
	minInterval := 10 * time.Second
	maxInterval := 5 * time.Minute
	if tier == core.SurvivalTierDead {
		maxInterval = 1 * time.Hour
	}
	if adjusted < minInterval {
		adjusted = minInterval
	}
	if adjusted > maxInterval {
		adjusted = maxInterval
	}

	d.interval = adjusted
}
