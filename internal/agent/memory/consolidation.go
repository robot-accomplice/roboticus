package memory

import (
	"context"
	"database/sql"
	"math"
	"strings"
	"time"

	"github.com/rs/zerolog/log"

	"goboticus/internal/db"
)

// ConsolidationEntry is a memory candidate for deduplication with a relevance score.
type ConsolidationEntry struct {
	ID       string
	Content  string
	Category string
	Score    float64
}

// Consolidator merges similar memory entries using Jaccard similarity.
type Consolidator struct {
	similarityThreshold float64
}

// NewConsolidator creates a Consolidator with the given similarity threshold (0-1).
func NewConsolidator(threshold float64) *Consolidator {
	return &Consolidator{similarityThreshold: threshold}
}

// Consolidate deduplicates entries by merging groups that are similar within
// the same category. The highest-scored entry in each group wins.
func (c *Consolidator) Consolidate(entries []ConsolidationEntry) []ConsolidationEntry {
	if len(entries) <= 1 {
		return entries
	}
	var merged []ConsolidationEntry
	used := make(map[int]bool)

	for i := range entries {
		if used[i] {
			continue
		}
		group := []ConsolidationEntry{entries[i]}
		for j := i + 1; j < len(entries); j++ {
			if used[j] {
				continue
			}
			if c.areSimilar(entries[i], entries[j]) {
				group = append(group, entries[j])
				used[j] = true
			}
		}
		merged = append(merged, c.mergeGroup(group))
		used[i] = true
	}
	return merged
}

func (c *Consolidator) areSimilar(a, b ConsolidationEntry) bool {
	if a.Category != b.Category {
		return false
	}
	return jaccardSimilarity(a.Content, b.Content) >= c.similarityThreshold
}

func (c *Consolidator) mergeGroup(group []ConsolidationEntry) ConsolidationEntry {
	if len(group) == 1 {
		return group[0]
	}
	best := group[0]
	for _, e := range group[1:] {
		if e.Score > best.Score {
			best.Content = e.Content
			best.Score = e.Score
		}
	}
	return best
}

// jaccardSimilarity computes word-level Jaccard similarity between two strings.
func jaccardSimilarity(a, b string) float64 {
	wordsA := strings.Fields(strings.ToLower(a))
	wordsB := strings.Fields(strings.ToLower(b))
	if len(wordsA) == 0 && len(wordsB) == 0 {
		return 1.0
	}
	setA := make(map[string]bool)
	for _, w := range wordsA {
		setA[w] = true
	}
	setB := make(map[string]bool)
	for _, w := range wordsB {
		setB[w] = true
	}
	intersection := 0
	for w := range setA {
		if setB[w] {
			intersection++
		}
	}
	union := len(setA) + len(setB) - intersection
	if union == 0 {
		return 0
	}
	return float64(intersection) / float64(union)
}

// ConsolidationReport holds per-phase counts from a consolidation run.
type ConsolidationReport struct {
	Indexed           int
	Deduped           int
	Promoted          int
	ConfidenceDecayed int
	ImportanceDecayed int
	Pruned            int
	Orphaned          int
}

// ConsolidationPipeline runs a 7-phase memory consolidation pipeline.
type ConsolidationPipeline struct {
	// MinInterval prevents running more than once per this duration.
	MinInterval time.Duration
}

// NewConsolidationPipeline creates a pipeline with a default 1-hour minimum interval.
func NewConsolidationPipeline() *ConsolidationPipeline {
	return &ConsolidationPipeline{
		MinInterval: time.Hour,
	}
}

// Run executes the 7-phase consolidation pipeline and returns a report.
func (p *ConsolidationPipeline) Run(ctx context.Context, store *db.Store) ConsolidationReport {
	var report ConsolidationReport

	// Gate: check last consolidation time.
	if !p.shouldRun(ctx, store) {
		log.Debug().Msg("consolidation: skipped (ran recently)")
		return report
	}

	report.Indexed = p.phaseIndexBackfill(ctx, store)
	report.Deduped = p.phaseCrossTierDedup(ctx, store)
	report.Promoted = p.phaseEpisodicPromotion(ctx, store)
	report.ConfidenceDecayed = p.phaseConfidenceDecay(ctx, store)
	report.ImportanceDecayed = p.phaseImportanceDecay(ctx, store)
	report.Pruned = p.phasePruning(ctx, store)
	report.Orphaned = p.phaseOrphanCleanup(ctx, store)

	// Record the run.
	p.recordRun(ctx, store, report)

	log.Info().
		Int("indexed", report.Indexed).
		Int("deduped", report.Deduped).
		Int("promoted", report.Promoted).
		Int("confidence_decayed", report.ConfidenceDecayed).
		Int("importance_decayed", report.ImportanceDecayed).
		Int("pruned", report.Pruned).
		Int("orphaned", report.Orphaned).
		Msg("consolidation: complete")

	return report
}

// shouldRun checks if enough time has passed since the last consolidation.
func (p *ConsolidationPipeline) shouldRun(ctx context.Context, store *db.Store) bool {
	var lastRun sql.NullString
	err := store.QueryRowContext(ctx,
		`SELECT MAX(created_at) FROM consolidation_log`).Scan(&lastRun)
	if err != nil || !lastRun.Valid {
		return true
	}
	t, err := time.Parse("2006-01-02 15:04:05", lastRun.String)
	if err != nil {
		return true
	}
	return time.Since(t) >= p.MinInterval
}

// recordRun persists the consolidation report.
func (p *ConsolidationPipeline) recordRun(ctx context.Context, store *db.Store, r ConsolidationReport) {
	_, err := store.ExecContext(ctx,
		`INSERT INTO consolidation_log (id, indexed, deduped, promoted, confidence_decayed, importance_decayed, pruned, orphaned)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		db.NewID(), r.Indexed, r.Deduped, r.Promoted, r.ConfidenceDecayed, r.ImportanceDecayed, r.Pruned, r.Orphaned)
	if err != nil {
		log.Warn().Err(err).Msg("consolidation: failed to record run")
	}
}

// Phase 1: Index backfill — scan episodic and semantic entries missing from memory_index.
func (p *ConsolidationPipeline) phaseIndexBackfill(ctx context.Context, store *db.Store) int {
	count := 0

	// Backfill episodic entries.
	rows, err := store.QueryContext(ctx,
		`SELECT em.id, em.content FROM episodic_memory em
		 WHERE em.memory_state = 'active'
		 AND NOT EXISTS (SELECT 1 FROM memory_index mi WHERE mi.source_table = 'episodic' AND mi.source_id = em.id)`)
	if err != nil {
		log.Warn().Err(err).Msg("consolidation: index backfill episodic query failed")
		return count
	}
	count += p.indexRows(ctx, store, rows, "episodic")

	// Backfill semantic entries.
	rows, err = store.QueryContext(ctx,
		`SELECT sm.id, sm.value FROM semantic_memory sm
		 WHERE sm.memory_state = 'active'
		 AND NOT EXISTS (SELECT 1 FROM memory_index mi WHERE mi.source_table = 'semantic' AND mi.source_id = sm.id)`)
	if err != nil {
		log.Warn().Err(err).Msg("consolidation: index backfill semantic query failed")
		return count
	}
	count += p.indexRows(ctx, store, rows, "semantic")

	return count
}

func (p *ConsolidationPipeline) indexRows(ctx context.Context, store *db.Store, rows *sql.Rows, sourceTable string) int {
	defer func() { _ = rows.Close() }()
	count := 0
	for rows.Next() {
		var id, content string
		if rows.Scan(&id, &content) != nil {
			continue
		}
		summary := content
		if len(summary) > 200 {
			summary = summary[:200]
		}
		_, err := store.ExecContext(ctx,
			`INSERT OR IGNORE INTO memory_index (id, source_table, source_id, summary)
			 VALUES (?, ?, ?, ?)`,
			db.NewID(), sourceTable, id, summary)
		if err != nil {
			log.Warn().Err(err).Str("source", sourceTable).Msg("consolidation: index insert failed")
			continue
		}
		count++
	}
	return count
}

// Phase 2: Cross-tier Jaccard dedup — find duplicates across all 5 memory tiers.
func (p *ConsolidationPipeline) phaseCrossTierDedup(ctx context.Context, store *db.Store) int {
	// Collect active entries from episodic and semantic (the two content-rich tiers).
	type memEntry struct {
		id         string
		table      string
		content    string
		importance float64
		confidence float64
	}

	var entries []memEntry

	// Episodic.
	rows, err := store.QueryContext(ctx,
		`SELECT id, content, importance FROM episodic_memory WHERE memory_state = 'active'`)
	if err == nil {
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var e memEntry
			var imp int
			if rows.Scan(&e.id, &e.content, &imp) == nil {
				e.table = "episodic"
				e.importance = float64(imp)
				entries = append(entries, e)
			}
		}
		_ = rows.Close()
	}

	// Semantic.
	rows, err = store.QueryContext(ctx,
		`SELECT id, value, confidence FROM semantic_memory WHERE memory_state = 'active'`)
	if err == nil {
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var e memEntry
			if rows.Scan(&e.id, &e.content, &e.confidence) == nil {
				e.table = "semantic"
				entries = append(entries, e)
			}
		}
		_ = rows.Close()
	}

	// Pairwise cross-tier dedup.
	deduped := 0
	removed := make(map[string]bool)
	for i := 0; i < len(entries); i++ {
		if removed[entries[i].id] {
			continue
		}
		for j := i + 1; j < len(entries); j++ {
			if removed[entries[j].id] {
				continue
			}
			// Only dedup across different tiers.
			if entries[i].table == entries[j].table {
				continue
			}
			sim := jaccardSimilarity(entries[i].content, entries[j].content)
			if sim < 0.7 {
				continue
			}
			// Mark the episodic entry as deduped (prefer keeping semantic).
			var loser *memEntry
			if entries[i].table == "episodic" {
				loser = &entries[i]
			} else {
				loser = &entries[j]
			}
			p.markDeduped(ctx, store, loser.table, loser.id)
			removed[loser.id] = true
			deduped++
		}
	}
	return deduped
}

func (p *ConsolidationPipeline) markDeduped(ctx context.Context, store *db.Store, table, id string) {
	query := ""
	switch table {
	case "episodic":
		query = `UPDATE episodic_memory SET memory_state = 'deduped', state_reason = 'cross-tier duplicate' WHERE id = ?`
	case "semantic":
		query = `UPDATE semantic_memory SET memory_state = 'deduped', state_reason = 'cross-tier duplicate' WHERE id = ?`
	default:
		return
	}
	_, err := store.ExecContext(ctx, query, id)
	if err != nil {
		log.Warn().Err(err).Str("table", table).Msg("consolidation: dedup mark failed")
	}
}

// Phase 3: Episodic to Semantic promotion — find recurring themes in episodic memories.
func (p *ConsolidationPipeline) phaseEpisodicPromotion(ctx context.Context, store *db.Store) int {
	rows, err := store.QueryContext(ctx,
		`SELECT id, content, classification FROM episodic_memory WHERE memory_state = 'active'`)
	if err != nil {
		return 0
	}
	defer func() { _ = rows.Close() }()

	type epEntry struct {
		id             string
		content        string
		classification string
	}
	var entries []epEntry
	for rows.Next() {
		var e epEntry
		if rows.Scan(&e.id, &e.content, &e.classification) == nil {
			entries = append(entries, e)
		}
	}
	_ = rows.Close()

	// Find groups of 3+ similar entries (Jaccard > 0.5).
	promoted := 0
	used := make(map[int]bool)
	for i := 0; i < len(entries); i++ {
		if used[i] {
			continue
		}
		group := []int{i}
		for j := i + 1; j < len(entries); j++ {
			if used[j] {
				continue
			}
			if jaccardSimilarity(entries[i].content, entries[j].content) > 0.5 {
				group = append(group, j)
			}
		}
		if len(group) < 3 {
			continue
		}
		// Mark all members as used.
		for _, idx := range group {
			used[idx] = true
		}

		// Pick the longest entry content as the consolidated value.
		best := entries[group[0]]
		for _, idx := range group[1:] {
			if len(entries[idx].content) > len(best.content) {
				best = entries[idx]
			}
		}

		// Create semantic entry.
		key := best.content
		if len(key) > 80 {
			key = key[:80]
		}
		_, err := store.ExecContext(ctx,
			`INSERT INTO semantic_memory (id, category, key, value, confidence, memory_state, state_reason)
			 VALUES (?, ?, ?, ?, 0.7, 'active', 'promoted from episodic')
			 ON CONFLICT(category, key) DO UPDATE SET
			   confidence = MAX(confidence, 0.7),
			   updated_at = datetime('now')`,
			db.NewID(), best.classification, key, best.content)
		if err != nil {
			log.Warn().Err(err).Msg("consolidation: promotion insert failed")
			continue
		}

		// Mark episodic entries as promoted.
		for _, idx := range group {
			_, _ = store.ExecContext(ctx,
				`UPDATE episodic_memory SET memory_state = 'promoted', state_reason = 'consolidated to semantic'
				 WHERE id = ?`, entries[idx].id)
		}
		promoted++
	}
	return promoted
}

// Phase 4: Exponential confidence decay on semantic entries not recently accessed.
func (p *ConsolidationPipeline) phaseConfidenceDecay(ctx context.Context, store *db.Store) int {
	// Calculate days since last update for each active semantic entry.
	// Apply confidence *= 0.95^days_since_update, floor at 0.1.
	rows, err := store.QueryContext(ctx,
		`SELECT id, confidence, julianday('now') - julianday(updated_at) as days_stale
		 FROM semantic_memory
		 WHERE memory_state = 'active' AND confidence > 0.1`)
	if err != nil {
		return 0
	}
	defer func() { _ = rows.Close() }()

	type decayEntry struct {
		id            string
		newConfidence float64
	}
	var updates []decayEntry
	for rows.Next() {
		var id string
		var confidence, daysStale float64
		if rows.Scan(&id, &confidence, &daysStale) != nil {
			continue
		}
		if daysStale < 1 {
			continue
		}
		newConf := confidence * math.Pow(0.95, daysStale)
		if newConf < 0.1 {
			newConf = 0.1
		}
		if newConf < confidence {
			updates = append(updates, decayEntry{id: id, newConfidence: newConf})
		}
	}
	_ = rows.Close()

	for _, u := range updates {
		_, _ = store.ExecContext(ctx,
			`UPDATE semantic_memory SET confidence = ? WHERE id = ?`, u.newConfidence, u.id)
	}
	return len(updates)
}

// Phase 5: Importance decay on episodic entries older than 7 days.
func (p *ConsolidationPipeline) phaseImportanceDecay(ctx context.Context, store *db.Store) int {
	// importance = max(1, importance - 1) per week of age beyond 7 days.
	rows, err := store.QueryContext(ctx,
		`SELECT id, importance, julianday('now') - julianday(created_at) as age_days
		 FROM episodic_memory
		 WHERE memory_state = 'active' AND importance > 1`)
	if err != nil {
		return 0
	}
	defer func() { _ = rows.Close() }()

	type decayEntry struct {
		id            string
		newImportance int
	}
	var updates []decayEntry
	for rows.Next() {
		var id string
		var importance int
		var ageDays float64
		if rows.Scan(&id, &importance, &ageDays) != nil {
			continue
		}
		if ageDays <= 7 {
			continue
		}
		weeksOld := int((ageDays - 7) / 7)
		if weeksOld < 1 {
			weeksOld = 1
		}
		newImp := importance - weeksOld
		if newImp < 1 {
			newImp = 1
		}
		if newImp < importance {
			updates = append(updates, decayEntry{id: id, newImportance: newImp})
		}
	}
	_ = rows.Close()

	for _, u := range updates {
		_, _ = store.ExecContext(ctx,
			`UPDATE episodic_memory SET importance = ? WHERE id = ?`, u.newImportance, u.id)
	}
	return len(updates)
}

// Phase 6: Pruning — mark entries for pruning based on thresholds.
func (p *ConsolidationPipeline) phasePruning(ctx context.Context, store *db.Store) int {
	pruned := 0

	// Prune semantic entries with confidence < 0.15.
	res, err := store.ExecContext(ctx,
		`UPDATE semantic_memory SET memory_state = 'pruned', state_reason = 'low confidence'
		 WHERE memory_state = 'active' AND confidence < 0.15`)
	if err == nil {
		n, _ := res.RowsAffected()
		pruned += int(n)
	}

	// Prune episodic entries with importance <= 1 AND age > 30 days.
	res, err = store.ExecContext(ctx,
		`UPDATE episodic_memory SET memory_state = 'pruned', state_reason = 'low importance and old'
		 WHERE memory_state = 'active' AND importance <= 1
		 AND julianday('now') - julianday(created_at) > 30`)
	if err == nil {
		n, _ := res.RowsAffected()
		pruned += int(n)
	}

	return pruned
}

// Phase 7: Orphan cleanup — delete embeddings whose source_id no longer exists.
func (p *ConsolidationPipeline) phaseOrphanCleanup(ctx context.Context, store *db.Store) int {
	// Find orphaned embeddings: source_id not in any memory table.
	res, err := store.ExecContext(ctx,
		`DELETE FROM embeddings WHERE
		 (source_table = 'episodic' AND source_id NOT IN (SELECT id FROM episodic_memory)) OR
		 (source_table = 'semantic' AND source_id NOT IN (SELECT id FROM semantic_memory)) OR
		 (source_table = 'procedural' AND source_id NOT IN (SELECT id FROM procedural_memory)) OR
		 (source_table = 'relationship' AND source_id NOT IN (SELECT id FROM relationship_memory)) OR
		 (source_table = 'working' AND source_id NOT IN (SELECT id FROM working_memory))`)
	if err != nil {
		log.Warn().Err(err).Msg("consolidation: orphan cleanup failed")
		return 0
	}
	n, _ := res.RowsAffected()
	return int(n)
}
