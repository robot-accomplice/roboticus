package db

import (
	"context"
	"database/sql"
)

// --- Working Memory ---

// ListWorkingMemory returns working memory entries, optionally filtered by session.
func (rq *RouteQueries) ListWorkingMemory(ctx context.Context, sessionID string, limit int) (*sql.Rows, error) {
	if sessionID != "" {
		return rq.q.QueryContext(ctx,
			`SELECT id, session_id, entry_type, content, importance, created_at
			 FROM working_memory WHERE session_id = ? ORDER BY created_at DESC LIMIT ?`, sessionID, limit)
	}
	return rq.q.QueryContext(ctx,
		`SELECT id, session_id, entry_type, content, importance, created_at
		 FROM working_memory ORDER BY created_at DESC LIMIT ?`, limit)
}

// --- Episodic Memory ---

// ListEpisodicMemory returns recent episodic memories.
func (rq *RouteQueries) ListEpisodicMemory(ctx context.Context, limit int) (*sql.Rows, error) {
	return rq.q.QueryContext(ctx,
		`SELECT id, classification, content, importance, created_at
		 FROM episodic_memory ORDER BY created_at DESC LIMIT ?`, limit)
}

// --- Semantic Memory ---

// ListSemanticMemory returns semantic memory entries.
func (rq *RouteQueries) ListSemanticMemory(ctx context.Context, category string, limit int) (*sql.Rows, error) {
	if category != "" {
		return rq.q.QueryContext(ctx,
			`SELECT id, category, key, value, confidence
			 FROM semantic_memory WHERE category = ? ORDER BY confidence DESC LIMIT ?`, category, limit)
	}
	return rq.q.QueryContext(ctx,
		`SELECT id, category, key, value, confidence, created_at
		 FROM semantic_memory ORDER BY category, key LIMIT ?`, limit)
}

// --- Context Snapshots ---

// ListContextSnapshots returns context snapshots for a session.
func (rq *RouteQueries) ListContextSnapshots(ctx context.Context, sessionID string, limit int) (*sql.Rows, error) {
	return rq.q.QueryContext(ctx,
		`SELECT id, session_id, system_prompt_hash, memory_summary, conversation_digest, turn_count, created_at
		 FROM context_checkpoints WHERE session_id = ? ORDER BY created_at DESC LIMIT ?`, sessionID, limit)
}

// SemanticCategories returns semantic memory categories with counts.
func (rq *RouteQueries) SemanticCategories(ctx context.Context) (*sql.Rows, error) {
	return rq.q.QueryContext(ctx,
		`SELECT category, COUNT(*) as cnt FROM semantic_memory GROUP BY category ORDER BY cnt DESC`)
}

// --- Memory Search ---

// SearchWorkingMemory searches working memory by content pattern.
func (rq *RouteQueries) SearchWorkingMemory(ctx context.Context, pattern string, limit int) (*sql.Rows, error) {
	return rq.q.QueryContext(ctx,
		`SELECT id, 'working' as tier, entry_type, content, created_at
		 FROM working_memory WHERE content LIKE ? LIMIT ?`, pattern, limit)
}

// SearchEpisodicMemory searches episodic memory by content pattern.
func (rq *RouteQueries) SearchEpisodicMemory(ctx context.Context, pattern string, limit int) (*sql.Rows, error) {
	return rq.q.QueryContext(ctx,
		`SELECT id, 'episodic' as tier, classification, content, created_at
		 FROM episodic_memory WHERE content LIKE ? LIMIT ?`, pattern, limit)
}

// SearchSemanticMemory searches semantic memory by value or key pattern.
func (rq *RouteQueries) SearchSemanticMemory(ctx context.Context, pattern string, limit int) (*sql.Rows, error) {
	return rq.q.QueryContext(ctx,
		`SELECT id, 'semantic' as tier, category, value, created_at
		 FROM semantic_memory WHERE value LIKE ? OR key LIKE ? LIMIT ?`, pattern, pattern, limit)
}

// --- Memory Analytics ---

// RetrievalQualityAvg returns total turns and turns-with-memory counts.
func (rq *RouteQueries) RetrievalQualityAvg(ctx context.Context, offset string) (totalTurns, turnsWithMemory int64, err error) {
	err = rq.q.QueryRowContext(ctx,
		`SELECT COUNT(*),
		        COALESCE(SUM(CASE WHEN COALESCE(memory_tokens, 0) > 0 THEN 1 ELSE 0 END), 0)
		 FROM context_snapshots
		 WHERE created_at >= datetime('now', ? || ' hours')`, offset).Scan(&totalTurns, &turnsWithMemory)
	return
}

// CachePerformance returns average budget utilization.
func (rq *RouteQueries) CachePerformance(ctx context.Context, offset string) (float64, error) {
	var avg float64
	err := rq.q.QueryRowContext(ctx,
		`SELECT COALESCE(
		   AVG(CAST(COALESCE(memory_tokens, 0) + COALESCE(system_prompt_tokens, 0) + COALESCE(history_tokens, 0) AS REAL)
		       / NULLIF(token_budget, 0)),
		 0)
		 FROM context_snapshots
		 WHERE created_at >= datetime('now', ? || ' hours')
		   AND token_budget > 0`, offset).Scan(&avg)
	return avg, err
}

// ComplexityDistribution returns rows of (complexity_level, count).
func (rq *RouteQueries) ComplexityDistribution(ctx context.Context, offset string) (*sql.Rows, error) {
	return rq.q.QueryContext(ctx,
		`SELECT complexity_level, COUNT(*)
		 FROM context_snapshots
		 WHERE created_at >= datetime('now', ? || ' hours')
		 GROUP BY complexity_level ORDER BY complexity_level`, offset)
}

// MemoryROIWithMemory returns average feedback grade for turns that used memory.
func (rq *RouteQueries) MemoryROIWithMemory(ctx context.Context, offset string) (float64, error) {
	var avg float64
	err := rq.q.QueryRowContext(ctx,
		`SELECT COALESCE(AVG(tf.grade), 0)
		 FROM turn_feedback tf
		 JOIN context_snapshots cs ON cs.turn_id = tf.turn_id
		 WHERE cs.created_at >= datetime('now', ? || ' hours')
		   AND COALESCE(cs.memory_tokens, 0) > 0`, offset).Scan(&avg)
	return avg, err
}

// MemoryROIWithoutMemory returns average feedback grade for turns that did not use memory.
func (rq *RouteQueries) MemoryROIWithoutMemory(ctx context.Context, offset string) (float64, error) {
	var avg float64
	err := rq.q.QueryRowContext(ctx,
		`SELECT COALESCE(AVG(tf.grade), 0)
		 FROM turn_feedback tf
		 JOIN context_snapshots cs ON cs.turn_id = tf.turn_id
		 WHERE cs.created_at >= datetime('now', ? || ' hours')
		   AND COALESCE(cs.memory_tokens, 0) = 0`, offset).Scan(&avg)
	return avg, err
}

// CountWorkingMemory returns the number of working memory entries.
func (rq *RouteQueries) CountWorkingMemory(ctx context.Context) (int64, error) {
	var count int64
	err := rq.q.QueryRowContext(ctx, `SELECT COUNT(*) FROM working_memory`).Scan(&count)
	return count, err
}

// CountEpisodicMemory returns the number of episodic memory entries.
func (rq *RouteQueries) CountEpisodicMemory(ctx context.Context) (int64, error) {
	var count int64
	err := rq.q.QueryRowContext(ctx, `SELECT COUNT(*) FROM episodic_memory`).Scan(&count)
	return count, err
}

// CountSemanticMemory returns the number of semantic memory entries.
func (rq *RouteQueries) CountSemanticMemory(ctx context.Context) (int64, error) {
	var count int64
	err := rq.q.QueryRowContext(ctx, `SELECT COUNT(*) FROM semantic_memory`).Scan(&count)
	return count, err
}

// CountProceduralMemory returns the number of procedural memory entries.
func (rq *RouteQueries) CountProceduralMemory(ctx context.Context) (int64, error) {
	var count int64
	err := rq.q.QueryRowContext(ctx, `SELECT COUNT(*) FROM procedural_memory`).Scan(&count)
	return count, err
}

// CountRelationshipMemory returns the number of relationship memory entries.
func (rq *RouteQueries) CountRelationshipMemory(ctx context.Context) (int64, error) {
	var count int64
	err := rq.q.QueryRowContext(ctx, `SELECT COUNT(*) FROM relationship_memory`).Scan(&count)
	return count, err
}

// CountWorkingMemoryStale returns working memory entries older than 24 hours.
func (rq *RouteQueries) CountWorkingMemoryStale(ctx context.Context) (int64, error) {
	var count int64
	err := rq.q.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM working_memory WHERE created_at < datetime('now', '-24 hours')`).Scan(&count)
	return count, err
}

// CountEpisodicMemoryStale returns episodic memory entries older than 7 days.
func (rq *RouteQueries) CountEpisodicMemoryStale(ctx context.Context) (int64, error) {
	var count int64
	err := rq.q.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM episodic_memory WHERE created_at < datetime('now', '-7 days')`).Scan(&count)
	return count, err
}
