package agent

import (
	"context"
	"fmt"
	"time"

	"github.com/rs/zerolog/log"

	"roboticus/internal/db"
)

// GovernorReport contains the counts from a single governance tick.
type GovernorReport struct {
	ExpiredSessions int64
	RotatedSessions int64
	DecayedMemories int64
	AdjustedSkills  int64
	PrunedSkills    int64
}

// SessionGovernor performs periodic lifecycle maintenance on sessions,
// episodic memory, and learned skills.
type SessionGovernor struct {
	store            *db.Store
	sessionTTL       time.Duration
	rotationInterval time.Duration
	lastTick         time.Time
	minInterval      time.Duration // prevent running more than once per minute
}

// NewSessionGovernor creates a SessionGovernor with the given store and TTL.
// rotationInterval controls how often active sessions are rotated (archived
// and replaced with a fresh successor). Pass 0 to disable rotation.
func NewSessionGovernor(store *db.Store, ttl, rotationInterval time.Duration) *SessionGovernor {
	if rotationInterval == 0 {
		rotationInterval = 24 * time.Hour
	}
	return &SessionGovernor{
		store:            store,
		sessionTTL:       ttl,
		rotationInterval: rotationInterval,
		minInterval:      1 * time.Minute,
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

	rotated, err := sg.rotateOldSessions(ctx)
	if err != nil {
		return report, fmt.Errorf("rotate sessions: %w", err)
	}
	report.RotatedSessions = rotated

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
		Int64("rotated", rotated).
		Int64("decayed", decayed).
		Int64("adjusted", adjusted).
		Int64("pruned", pruned).
		Msg("session governor tick complete")

	return report, nil
}

// CompactBeforeArchive performs progressive compaction on a session before archiving.
// It summarizes the oldest 50% of turns while preserving the 4 most recent messages.
func (sg *SessionGovernor) CompactBeforeArchive(ctx context.Context, sessionID string) error {
	// Fetch all messages ordered by time.
	rows, err := sg.store.QueryContext(ctx,
		`SELECT id, role, content FROM session_messages
		 WHERE session_id = ? ORDER BY created_at ASC`, sessionID)
	if err != nil {
		return fmt.Errorf("query messages: %w", err)
	}
	defer func() { _ = rows.Close() }()

	type msg struct {
		id, role, content string
	}
	var messages []msg
	for rows.Next() {
		var m msg
		if err := rows.Scan(&m.id, &m.role, &m.content); err != nil {
			continue
		}
		messages = append(messages, m)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate messages: %w", err)
	}

	const preserveRecent = 4
	if len(messages) <= preserveRecent {
		// Nothing to compact — too few messages.
		return nil
	}

	// Split: oldest 50% becomes summary candidates, but always preserve last 4.
	halfPoint := len(messages) / 2
	cutoff := len(messages) - preserveRecent
	if halfPoint > cutoff {
		halfPoint = cutoff
	}
	if halfPoint <= 0 {
		return nil
	}

	toCompact := messages[:halfPoint]

	// Build a summary from the compacted messages.
	var summaryParts []string
	for _, m := range toCompact {
		snippet := m.content
		if len(snippet) > 120 {
			snippet = snippet[:120] + "..."
		}
		summaryParts = append(summaryParts, fmt.Sprintf("[%s] %s", m.role, snippet))
	}
	summary := fmt.Sprintf("[compacted %d messages]\n%s",
		len(toCompact), joinLines(summaryParts))

	// Delete old messages and insert the summary.
	for _, m := range toCompact {
		_, err := sg.store.ExecContext(ctx,
			`DELETE FROM session_messages WHERE id = ?`, m.id)
		if err != nil {
			log.Warn().Err(err).Str("msg_id", m.id).Msg("failed to delete compacted message")
		}
	}

	// Insert summary message as a system message at the beginning.
	summaryID := db.NewID()
	_, err = sg.store.ExecContext(ctx,
		`INSERT INTO session_messages (id, session_id, role, content, created_at)
		 VALUES (?, ?, 'system', ?, datetime('now', '-1 hour'))`,
		summaryID, sessionID, summary)
	if err != nil {
		return fmt.Errorf("insert summary: %w", err)
	}

	log.Debug().
		Str("session_id", sessionID).
		Int("compacted", len(toCompact)).
		Int("preserved", len(messages)-halfPoint).
		Msg("compact-before-archive complete")

	return nil
}

// joinLines joins string slices with newlines.
func joinLines(parts []string) string {
	result := ""
	for i, p := range parts {
		if i > 0 {
			result += "\n"
		}
		result += p
	}
	return result
}

// AdjustPriority calculates an adjusted priority based on success/failure counts.
// It boosts priority by PriorityBoostOnSuccess (default 5) when success rate >= 80%,
// and decays by PriorityDecayOnFailure (default 10) when failure ratio is high.
// The 2:1 asymmetry (penalizing failure harder than rewarding success) is intentional.
func AdjustPriority(successCount, failureCount int, boostOnSuccess, decayOnFailure int) int {
	if boostOnSuccess <= 0 {
		boostOnSuccess = 5
	}
	if decayOnFailure <= 0 {
		decayOnFailure = 10
	}

	total := successCount + failureCount
	if total == 0 {
		return 0
	}

	successRate := float64(successCount) / float64(total)

	if successRate >= 0.8 {
		return boostOnSuccess
	}

	failureRate := float64(failureCount) / float64(total)
	if failureRate > 0.5 {
		return -decayOnFailure
	}

	return 0
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

// rotateOldSessions archives active sessions that haven't been updated within
// the rotation interval and creates replacement sessions with the same agent
// and scope prefix, ensuring continuity for long-lived agents.
func (sg *SessionGovernor) rotateOldSessions(ctx context.Context) (int64, error) {
	rotationSeconds := int(sg.rotationInterval.Seconds())
	rows, err := sg.store.QueryContext(ctx,
		`SELECT id, agent_id, scope_key
		 FROM sessions
		 WHERE status = 'active'
		 AND datetime(updated_at, ?) < datetime('now')`,
		fmt.Sprintf("+%d seconds", rotationSeconds),
	)
	if err != nil {
		return 0, err
	}
	defer func() { _ = rows.Close() }()

	type sessionInfo struct {
		id, agentID, scopeKey string
	}
	var toRotate []sessionInfo
	for rows.Next() {
		var s sessionInfo
		if err := rows.Scan(&s.id, &s.agentID, &s.scopeKey); err != nil {
			continue
		}
		toRotate = append(toRotate, s)
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}

	var rotated int64
	for _, s := range toRotate {
		// Archive the old session.
		_, err := sg.store.ExecContext(ctx,
			`UPDATE sessions SET status = 'archived' WHERE id = ? AND status = 'active'`, s.id)
		if err != nil {
			continue
		}

		// Create successor with same agent and scope prefix.
		newID := db.NewID()
		scopePrefix := s.scopeKey
		if idx := len(scopePrefix) - 1; idx > 0 {
			// Strip the old session ID suffix (format: "scope:oldid").
			if colonIdx := lastIndexByte(scopePrefix, ':'); colonIdx > 0 {
				scopePrefix = scopePrefix[:colonIdx]
			}
		}
		newScope := scopePrefix + ":" + newID
		_, err = sg.store.ExecContext(ctx,
			`INSERT INTO sessions (id, agent_id, scope_key) VALUES (?, ?, ?)`,
			newID, s.agentID, newScope)
		if err != nil {
			continue
		}
		rotated++
	}
	return rotated, nil
}

// lastIndexByte returns the index of the last occurrence of c in s, or -1.
func lastIndexByte(s string, c byte) int {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == c {
			return i
		}
	}
	return -1
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
