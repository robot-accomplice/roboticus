package agent

import (
	"context"
	"fmt"
	"time"

	"github.com/rs/zerolog/log"

	"goboticus/internal/db"
)

// GovernorReport contains the counts from a single governance tick.
type GovernorReport struct {
	ExpiredSessions int64
	DecayedMemories int64
	AdjustedSkills  int64
	PrunedSkills    int64
}

// SessionGovernor performs periodic lifecycle maintenance on sessions,
// episodic memory, and learned skills.
type SessionGovernor struct {
	store       *db.Store
	sessionTTL  time.Duration
	lastTick    time.Time
	minInterval time.Duration // prevent running more than once per minute
}

// NewSessionGovernor creates a SessionGovernor with the given store and TTL.
func NewSessionGovernor(store *db.Store, ttl time.Duration) *SessionGovernor {
	return &SessionGovernor{
		store:       store,
		sessionTTL:  ttl,
		minInterval: 1 * time.Minute,
	}
}

// Tick runs all lifecycle maintenance tasks. It returns a report of what changed
// and skips execution if called more frequently than minInterval.
func (sg *SessionGovernor) Tick(ctx context.Context) (*GovernorReport, error) {
	now := time.Now()
	if !sg.lastTick.IsZero() && now.Sub(sg.lastTick) < sg.minInterval {
		return &GovernorReport{}, nil
	}
	sg.lastTick = now

	report := &GovernorReport{}

	expired, err := sg.expireStaleSessions(ctx)
	if err != nil {
		return report, fmt.Errorf("expire sessions: %w", err)
	}
	report.ExpiredSessions = expired

	decayed, err := sg.decayEpisodicImportance(ctx)
	if err != nil {
		return report, fmt.Errorf("decay episodic: %w", err)
	}
	report.DecayedMemories = decayed

	adjusted, err := sg.adjustSkillPriorities(ctx)
	if err != nil {
		return report, fmt.Errorf("adjust skills: %w", err)
	}
	report.AdjustedSkills = adjusted

	pruned, err := sg.pruneDeadSkills(ctx)
	if err != nil {
		return report, fmt.Errorf("prune skills: %w", err)
	}
	report.PrunedSkills = pruned

	log.Debug().
		Int64("expired", expired).
		Int64("decayed", decayed).
		Int64("adjusted", adjusted).
		Int64("pruned", pruned).
		Msg("session governor tick complete")

	return report, nil
}

// expireStaleSessions marks active sessions older than sessionTTL as expired.
func (sg *SessionGovernor) expireStaleSessions(ctx context.Context) (int64, error) {
	ttlSeconds := int(sg.sessionTTL.Seconds())
	result, err := sg.store.ExecContext(ctx,
		`UPDATE sessions SET status = 'expired'
		 WHERE status = 'active'
		 AND datetime(created_at, ?) < datetime('now')`,
		fmt.Sprintf("+%d seconds", ttlSeconds),
	)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

// decayEpisodicImportance reduces importance of episodic memories older than 7 days.
func (sg *SessionGovernor) decayEpisodicImportance(ctx context.Context) (int64, error) {
	result, err := sg.store.ExecContext(ctx,
		`UPDATE episodic_memory
		 SET importance = max(1, importance - 1)
		 WHERE importance > 1
		 AND julianday('now') - julianday(created_at) > 7`,
	)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

// adjustSkillPriorities adjusts learned skill priorities based on success/failure ratios.
// Skills with failure_count > success_count * 2 get priority reduced by 10%.
// Skills with success_count > 10 and failure_count == 0 get priority increased by 5% (cap at 100).
func (sg *SessionGovernor) adjustSkillPriorities(ctx context.Context) (int64, error) {
	// Reduce priority for high-failure skills.
	res1, err := sg.store.ExecContext(ctx,
		`UPDATE learned_skills
		 SET priority = max(0, priority * 90 / 100)
		 WHERE failure_count > success_count * 2
		 AND priority > 0`,
	)
	if err != nil {
		return 0, err
	}
	reduced, err := res1.RowsAffected()
	if err != nil {
		return 0, err
	}

	// Increase priority for high-success skills (cap at 100).
	res2, err := sg.store.ExecContext(ctx,
		`UPDATE learned_skills
		 SET priority = min(100, priority * 105 / 100)
		 WHERE success_count > 10
		 AND failure_count = 0
		 AND priority < 100`,
	)
	if err != nil {
		return reduced, err
	}
	increased, err := res2.RowsAffected()
	if err != nil {
		return reduced, err
	}

	return reduced + increased, nil
}

// pruneDeadSkills marks learned_skills with priority < 5 as pruned.
func (sg *SessionGovernor) pruneDeadSkills(ctx context.Context) (int64, error) {
	result, err := sg.store.ExecContext(ctx,
		`UPDATE learned_skills
		 SET memory_state = 'pruned'
		 WHERE priority < 5
		 AND (memory_state IS NULL OR memory_state != 'pruned')`,
	)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}
