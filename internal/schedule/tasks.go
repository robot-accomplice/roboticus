package schedule

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/rs/zerolog/log"
)

// ---------------------------------------------------------------------------
// Concrete HeartbeatTask implementations matching Rust's heartbeat task bodies.
// Each task is a thin adapter: the heavy lifting lives in pipeline/daemon code
// and these tasks just invoke it on the heartbeat cadence.
// ---------------------------------------------------------------------------

// DB is the minimal database interface needed by heartbeat tasks.
// Avoids importing internal/db directly.
type DB interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// ---------------------------------------------------------------------------
// TreasuryLoopTask — reads wallet balance from DB cache, logs state.
// Matches Rust's treasury loop: reads wallet balance, persists state.
// Actual on-chain polling is done by wallet_poller.go; this task reads the
// cached balances and logs them for observability.
// ---------------------------------------------------------------------------

type TreasuryLoopTask struct {
	Store DB
}

func (t *TreasuryLoopTask) Kind() HeartbeatTaskKind { return TaskSurvivalCheck }

func (t *TreasuryLoopTask) Run(ctx context.Context, tctx *TickContext) TaskResult {
	if t.Store == nil {
		return TaskResult{Success: false, Message: "no store configured"}
	}

	var totalBalance float64
	row := t.Store.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(balance), 0) FROM wallet_balances`)
	if err := row.Scan(&totalBalance); err != nil {
		return TaskResult{Success: false, Message: fmt.Sprintf("query balance: %v", err)}
	}

	tctx.USDCBalance = totalBalance

	// Read aToken/yield balance for persistence.
	var atokenBalance float64
	aRow := t.Store.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(balance), 0) FROM wallet_balances WHERE symbol LIKE 'a%' OR symbol LIKE 'st%'`)
	_ = aRow.Scan(&atokenBalance)

	// Persist treasury state to DB (Rust parity: treasury loop writes state).
	_, err := t.Store.ExecContext(ctx,
		`INSERT INTO treasury_state (id, usdc_balance, atoken_balance, survival_tier, updated_at)
		 VALUES (1, ?, ?, ?, datetime('now'))
		 ON CONFLICT(id) DO UPDATE SET
		   usdc_balance = excluded.usdc_balance,
		   atoken_balance = excluded.atoken_balance,
		   survival_tier = excluded.survival_tier,
		   updated_at = datetime('now')`,
		totalBalance, atokenBalance, tctx.SurvivalTier.String())
	if err != nil {
		log.Debug().Err(err).Msg("treasury loop: state persistence failed (table may not exist)")
	}

	log.Debug().
		Float64("total_balance", totalBalance).
		Str("tier", tctx.SurvivalTier.String()).
		Msg("treasury loop: balance check")

	return TaskResult{Success: true, Message: fmt.Sprintf("balance=%.4f", totalBalance)}
}

// ---------------------------------------------------------------------------
// YieldLoopTask — checks aToken balance for earned yield.
// Matches Rust's yield loop. Reads from wallet_balances for any yield-bearing
// token entries.
// ---------------------------------------------------------------------------

type YieldLoopTask struct {
	Store DB
}

func (t *YieldLoopTask) Kind() HeartbeatTaskKind { return TaskYield }

func (t *YieldLoopTask) Run(ctx context.Context, _ *TickContext) TaskResult {
	if t.Store == nil {
		return TaskResult{Success: false, Message: "no store configured"}
	}

	// Check for yield-bearing token balances (aTokens, staked positions).
	var yieldBalance float64
	row := t.Store.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(balance), 0) FROM wallet_balances WHERE symbol LIKE 'a%' OR symbol LIKE 'st%'`)
	if err := row.Scan(&yieldBalance); err != nil {
		return TaskResult{Success: false, Message: fmt.Sprintf("query yield: %v", err)}
	}

	log.Debug().
		Float64("yield_balance", yieldBalance).
		Msg("yield loop: balance check")

	return TaskResult{Success: true, Message: fmt.Sprintf("yield=%.6f", yieldBalance)}
}

// ---------------------------------------------------------------------------
// MemoryLoopTask — runs memory consolidation.
// Matches Rust's memory loop. Delegates to the consolidation function that
// must be injected to avoid importing pipeline directly.
// ---------------------------------------------------------------------------

// ConsolidationFunc runs memory consolidation and returns a summary string.
type ConsolidationFunc func(ctx context.Context, force bool) string

type MemoryLoopTask struct {
	Consolidate ConsolidationFunc
}

func (t *MemoryLoopTask) Kind() HeartbeatTaskKind { return TaskMemoryPrune }

func (t *MemoryLoopTask) Run(ctx context.Context, _ *TickContext) TaskResult {
	if t.Consolidate == nil {
		return TaskResult{Success: false, Message: "no consolidation function configured"}
	}

	summary := t.Consolidate(ctx, false)
	log.Debug().Str("summary", summary).Msg("memory loop: consolidation completed")

	return TaskResult{Success: true, Message: summary}
}

// ---------------------------------------------------------------------------
// MaintenanceLoopTask — cache eviction and metric snapshot.
// Matches Rust's maintenance loop.
// ---------------------------------------------------------------------------

type MaintenanceLoopTask struct {
	Store DB
}

func (t *MaintenanceLoopTask) Kind() HeartbeatTaskKind { return TaskCacheEvict }

func (t *MaintenanceLoopTask) Run(ctx context.Context, _ *TickContext) TaskResult {
	if t.Store == nil {
		return TaskResult{Success: false, Message: "no store configured"}
	}

	// Evict stale cache entries (response cache older than 24h).
	res, err := t.Store.ExecContext(ctx,
		`DELETE FROM response_cache WHERE created_at < datetime('now', '-24 hours')`)
	evicted := int64(0)
	if err == nil {
		evicted, _ = res.RowsAffected()
	}

	// Clean up expired leases.
	res2, err2 := t.Store.ExecContext(ctx,
		`UPDATE cron_jobs SET lease_holder = NULL, lease_expires_at = NULL
		 WHERE lease_expires_at IS NOT NULL AND lease_expires_at < datetime('now')`)
	leases := int64(0)
	if err2 == nil {
		leases, _ = res2.RowsAffected()
	}

	log.Debug().
		Int64("cache_evicted", evicted).
		Int64("leases_cleared", leases).
		Msg("maintenance loop: cleanup")

	return TaskResult{
		Success: true,
		Message: fmt.Sprintf("evicted=%d leases_cleared=%d", evicted, leases),
	}
}

// ---------------------------------------------------------------------------
// MetricSnapshotTask — periodic metric snapshot for observability.
// ---------------------------------------------------------------------------

type MetricSnapshotTask struct {
	Store DB
}

func (t *MetricSnapshotTask) Kind() HeartbeatTaskKind { return TaskMetricSnapshot }

func (t *MetricSnapshotTask) Run(ctx context.Context, tctx *TickContext) TaskResult {
	if t.Store == nil {
		return TaskResult{Success: false, Message: "no store configured"}
	}

	// Record a metric snapshot row for historical tracking.
	_, err := t.Store.ExecContext(ctx,
		`INSERT OR IGNORE INTO metric_snapshots (timestamp, tier, usdc_balance)
		 VALUES (?, ?, ?)`,
		tctx.Timestamp.Format(time.RFC3339), tctx.SurvivalTier.String(), tctx.USDCBalance)
	if err != nil {
		// Table may not exist yet; this is non-fatal.
		log.Debug().Err(err).Msg("metric snapshot: insert skipped (table may not exist)")
		return TaskResult{Success: true, Message: "snapshot skipped (no table)"}
	}

	return TaskResult{Success: true, Message: "snapshot recorded"}
}

// ---------------------------------------------------------------------------
// SessionGovernorTask — session rotation/governance.
// Matches Rust's session loop: closes stale sessions, enforces max session age.
// ---------------------------------------------------------------------------

type SessionGovernorTask struct {
	Store         DB
	MaxSessionAge time.Duration // default 24h
	ResetSchedule string        // optional cron expression for session rotation
}

func (t *SessionGovernorTask) Kind() HeartbeatTaskKind { return TaskSessionGovernor }

func (t *SessionGovernorTask) Run(ctx context.Context, tctx *TickContext) TaskResult {
	if t.Store == nil {
		return TaskResult{Success: false, Message: "no store configured"}
	}

	maxAge := t.MaxSessionAge
	if maxAge <= 0 {
		maxAge = 24 * time.Hour
	}

	cutoff := time.Now().Add(-maxAge).Format(time.RFC3339)

	// Mark stale sessions as expired.
	res, err := t.Store.ExecContext(ctx,
		`UPDATE sessions SET status = 'expired'
		 WHERE status = 'active' AND updated_at < ?`, cutoff)
	expired := int64(0)
	if err == nil {
		expired, _ = res.RowsAffected()
	}

	if expired > 0 {
		log.Info().Int64("expired", expired).Msg("session governor: expired stale sessions")
	}

	// Session rotation via reset_schedule cron expression.
	rotated := int64(0)
	if t.ResetSchedule != "" && matchesCron(t.ResetSchedule, tctx.Timestamp) {
		// Archive the current active session and create a new one.
		res, err := t.Store.ExecContext(ctx,
			`UPDATE sessions SET status = 'archived', updated_at = datetime('now')
			 WHERE status = 'active'
			 ORDER BY updated_at DESC LIMIT 1`)
		if err == nil {
			rotated, _ = res.RowsAffected()
		}
		if rotated > 0 {
			// Create a fresh session.
			if _, err := t.Store.ExecContext(ctx,
				`INSERT INTO sessions (id, status, created_at, updated_at)
				 VALUES (hex(randomblob(16)), 'active', datetime('now'), datetime('now'))`); err != nil {
				log.Warn().Err(err).Msg("session governor: failed to create rotated session")
			}
			log.Info().Str("schedule", t.ResetSchedule).Msg("session governor: rotated session via reset_schedule")
		}
	}

	return TaskResult{
		Success: true,
		Message: fmt.Sprintf("expired=%d rotated=%d", expired, rotated),
	}
}
