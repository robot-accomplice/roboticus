package memory

import (
	"context"
	"database/sql"
	"strings"

	"github.com/rs/zerolog/log"

	"roboticus/internal/db"
	"roboticus/internal/llm"
)

// phaseEmbeddingBackfill generates embeddings for memory entries that were stored
// before the embedding pipeline was connected. Processes up to 50 entries per run
// to stay within rate limits and avoid blocking consolidation.
func (p *ConsolidationPipeline) phaseEmbeddingBackfill(ctx context.Context, store *db.Store) int {
	if p.EmbedClient == nil || store == nil {
		return 0
	}

	backfilled := 0
	// Backfill episodic entries missing embeddings.
	backfilled += p.backfillTierEmbeddings(ctx, store, "episodic_memory",
		`SELECT em.id, em.content FROM episodic_memory em
		 WHERE em.memory_state = 'active'
		 AND NOT EXISTS (SELECT 1 FROM embeddings e WHERE e.source_table = 'episodic_memory' AND e.source_id = em.id)
		 LIMIT 25`)

	// Backfill semantic entries missing embeddings.
	backfilled += p.backfillTierEmbeddings(ctx, store, "semantic_memory",
		`SELECT sm.id, sm.key || ': ' || sm.value FROM semantic_memory sm
		 WHERE sm.memory_state = 'active'
		 AND NOT EXISTS (SELECT 1 FROM embeddings e WHERE e.source_table = 'semantic_memory' AND e.source_id = sm.id)
		 LIMIT 25`)

	return backfilled
}

func (p *ConsolidationPipeline) backfillTierEmbeddings(ctx context.Context, store *db.Store, sourceTable, query string) int {
	rows, err := store.QueryContext(ctx, query)
	if err != nil {
		log.Debug().Err(err).Str("tier", sourceTable).Msg("consolidation: embedding backfill query failed")
		return 0
	}
	defer func() { _ = rows.Close() }()

	count := 0
	for rows.Next() {
		var id, content string
		if rows.Scan(&id, &content) != nil {
			continue
		}
		vec, err := p.EmbedClient.EmbedSingle(ctx, content)
		if err != nil {
			log.Debug().Err(err).Str("id", id).Msg("consolidation: embedding backfill embed failed")
			continue
		}
		if len(vec) == 0 {
			continue
		}
		preview := content
		if len(preview) > 200 {
			preview = preview[:200]
		}
		blob := db.EmbeddingToBlob(vec)
		_, err = store.ExecContext(ctx,
			`INSERT OR IGNORE INTO embeddings (id, source_table, source_id, content_preview, embedding_blob, dimensions)
			 VALUES (?, ?, ?, ?, ?, ?)`,
			db.NewID(), sourceTable, id, preview, blob, len(vec))
		if err != nil {
			log.Debug().Err(err).Str("id", id).Msg("consolidation: embedding backfill insert failed")
			continue
		}
		count++
	}
	return count
}

// subjectSimilarity extracts the first few words (the "subject") of two texts
// and computes their Jaccard overlap. Used to distinguish contradictory facts
// (same subject, different predicate) from complementary facts (different subject).
func subjectSimilarity(a, b string) float64 {
	subjectA := extractSubject(a)
	subjectB := extractSubject(b)
	if len(subjectA) == 0 && len(subjectB) == 0 {
		return 1.0
	}
	return jaccardSimilarity(subjectA, subjectB)
}

// extractSubject returns the first 5 words of a text as the "subject" heuristic.
func extractSubject(text string) string {
	words := strings.Fields(strings.ToLower(text))
	if len(words) > 5 {
		words = words[:5]
	}
	return strings.Join(words, " ")
}

// Consolidation thresholds — Rust parity (consolidation.rs).
const (
	// DedupJaccardThreshold is the minimum Jaccard similarity for within-tier dedup.
	// Rust: run_dedup(db, 0.85) in consolidation.rs.
	DedupJaccardThreshold = 0.85

	// PromotionGroupThreshold is the minimum Jaccard similarity for grouping
	// episodic entries before promotion to semantic.
	// Rust: groups entries with similarity > 0.5 before merging into semantic.
	PromotionGroupThreshold = 0.5

	// DecayFactor is the per-consolidation-pass multiplier applied to confidence.
	// Rust: 0.995 constant multiplier per 24h gate.
	DecayFactor = 0.995

	// DecayFloor is the minimum confidence after decay.
	// Rust: WHERE confidence > 0.1.
	DecayFloor = 0.1
)

// Phase 1: Index backfill — scan episodic and semantic entries missing from memory_index.
func (p *ConsolidationPipeline) phaseIndexBackfill(ctx context.Context, store *db.Store) int {
	count := 0

	// Backfill episodic entries (check both legacy short and full names).
	rows, err := store.QueryContext(ctx,
		`SELECT em.id, em.content FROM episodic_memory em
		 WHERE em.memory_state = 'active'
		 AND NOT EXISTS (SELECT 1 FROM memory_index mi WHERE mi.source_table = 'episodic_memory' AND mi.source_id = em.id)`)
	if err != nil {
		log.Warn().Err(err).Msg("consolidation: index backfill episodic query failed")
		return count
	}
	count += p.indexRows(ctx, store, rows, "episodic_memory")

	// Backfill semantic entries (check both legacy short and full names).
	rows, err = store.QueryContext(ctx,
		`SELECT sm.id, sm.value FROM semantic_memory sm
		 WHERE sm.memory_state = 'active'
		 AND NOT EXISTS (SELECT 1 FROM memory_index mi WHERE mi.source_table = 'semantic_memory' AND mi.source_id = sm.id)`)
	if err != nil {
		log.Warn().Err(err).Msg("consolidation: index backfill semantic query failed")
		return count
	}
	count += p.indexRows(ctx, store, rows, "semantic_memory")

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

// Phase 2: Within-tier Jaccard dedup — find duplicates within the same tier.
// Corrected from cross-tier to within-tier: dedup should only compare entries
// in the same memory table to avoid incorrectly merging complementary entries.
func (p *ConsolidationPipeline) phaseWithinTierDedup(ctx context.Context, store *db.Store) int {
	deduped := 0
	deduped += p.dedupWithinTier(ctx, store, "episodic")
	deduped += p.dedupWithinTier(ctx, store, "semantic")
	return deduped
}

// DedupBatchCap is the maximum number of entries to process per tier per dedup run.
// Processing the most recent entries first targets the most likely duplicates.
const DedupBatchCap = 500

// dedupWithinTier uses MinHash/LSH for O(n) candidate generation followed by
// exact Jaccard verification. This replaces the previous O(n²) pairwise comparison.
func (p *ConsolidationPipeline) dedupWithinTier(ctx context.Context, store *db.Store, tier string) int {
	var rows *sql.Rows
	var err error

	switch tier {
	case "episodic":
		rows, err = store.QueryContext(ctx,
			`SELECT id, content, importance FROM episodic_memory
			 WHERE memory_state = 'active' ORDER BY created_at DESC LIMIT ?`, DedupBatchCap)
	case "semantic":
		rows, err = store.QueryContext(ctx,
			`SELECT id, value, confidence FROM semantic_memory
			 WHERE memory_state = 'active' ORDER BY updated_at DESC LIMIT ?`, DedupBatchCap)
	default:
		return 0
	}
	if err != nil {
		return 0
	}
	defer func() { _ = rows.Close() }()

	var entries []dedupEntry
	for rows.Next() {
		var e dedupEntry
		if tier == "episodic" {
			var imp int
			if rows.Scan(&e.id, &e.content, &imp) == nil {
				e.score = float64(imp)
				entries = append(entries, e)
			}
		} else {
			if rows.Scan(&e.id, &e.content, &e.score) == nil {
				entries = append(entries, e)
			}
		}
	}
	_ = rows.Close()

	if len(entries) <= 1 {
		return 0
	}

	// Compute MinHash signatures for all entries.
	for i := range entries {
		entries[i].sig = MinHashSignature(entries[i].content, DefaultNumHashes)
	}

	// Find candidate pairs via LSH and verify with exact Jaccard.
	pairs := FindCandidatePairs(entries, DedupJaccardThreshold, DefaultLSHBands)

	deduped := 0
	removed := make(map[string]bool)
	for _, cp := range pairs {
		a, b := entries[cp.i], entries[cp.j]
		if removed[a.id] || removed[b.id] {
			continue
		}
		// Keep the higher-scored entry, mark the other as deduped.
		var loserID string
		if a.score >= b.score {
			loserID = b.id
		} else {
			loserID = a.id
		}
		p.markDeduped(ctx, store, tier, loserID)
		removed[loserID] = true
		deduped++
	}
	return deduped
}

func (p *ConsolidationPipeline) markDeduped(ctx context.Context, store *db.Store, table, id string) {
	query := ""
	switch table {
	case "episodic":
		query = `UPDATE episodic_memory SET memory_state = 'deduped', state_reason = 'within-tier duplicate' WHERE id = ?`
	case "semantic":
		query = `UPDATE semantic_memory SET memory_state = 'deduped', state_reason = 'within-tier duplicate' WHERE id = ?`
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

	// Find groups of 3+ similar entries (Jaccard > PromotionGroupThreshold).
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
			if jaccardSimilarity(entries[i].content, entries[j].content) > PromotionGroupThreshold {
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

		// Pick the longest entry content as the representative.
		best := entries[group[0]]
		for _, idx := range group[1:] {
			if len(entries[idx].content) > len(best.content) {
				best = entries[idx]
			}
		}

		// Determine the semantic value: LLM-distilled fact or raw longest entry.
		maxDistill := p.MaxDistillPerRun
		if maxDistill <= 0 {
			maxDistill = 5
		}
		semanticValue := best.content
		if p.Distiller != nil && promoted < maxDistill {
			groupTexts := make([]string, len(group))
			for gi, idx := range group {
				groupTexts[gi] = entries[idx].content
			}
			if distilled, err := p.Distiller.Distill(ctx, groupTexts); err == nil && distilled != "" {
				semanticValue = distilled
			} else if err != nil {
				log.Debug().Err(err).Msg("consolidation: LLM distillation failed, using longest entry")
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
			db.NewID(), best.classification, key, semanticValue)
		if err != nil {
			log.Warn().Err(err).Msg("consolidation: promotion insert failed")
			continue
		}

		// Mark episodic entries as promoted.
		for _, idx := range group {
			if _, err := store.ExecContext(ctx,
				`UPDATE episodic_memory SET memory_state = 'promoted', state_reason = 'consolidated to semantic'
				 WHERE id = ?`, entries[idx].id); err != nil {
				log.Warn().Err(err).Str("id", entries[idx].id).Msg("consolidation: failed to mark episodic as promoted")
			}
		}
		promoted++
	}
	return promoted
}

// phaseContradictionDetection finds semantic entries that contradict others
// in the same category (based on embedding cosine similarity + subject overlap).
// When two entries share a subject but differ in predicate, the older one is
// marked superseded. This prevents the agent from holding mutually exclusive
// beliefs simultaneously.
//
// Scope: checks ALL active semantic entries per category (not just new ones),
// because rephrased entries from different sessions may both predate the last
// consolidation cutoff. Bounded by LIMIT 50 per category scan.
func (p *ConsolidationPipeline) phaseContradictionDetection(ctx context.Context, store *db.Store) int {
	if p.EmbedClient == nil || store == nil {
		return 0
	}

	// Get all active semantic entries, ordered newest first.
	// We compare each entry against all others in the same category.
	rows, err := store.QueryContext(ctx,
		`SELECT id, category, key, value FROM semantic_memory
		 WHERE memory_state = 'active'
		 ORDER BY updated_at DESC LIMIT 50`)
	if err != nil {
		return 0
	}
	defer func() { _ = rows.Close() }()

	type semEntry struct {
		id, category, key, value string
	}
	var activeEntries []semEntry
	for rows.Next() {
		var e semEntry
		if rows.Scan(&e.id, &e.category, &e.key, &e.value) == nil {
			activeEntries = append(activeEntries, e)
		}
	}
	_ = rows.Close()

	if len(activeEntries) == 0 {
		return 0
	}

	superseded := 0
	alreadySuperseded := make(map[string]bool) // Track IDs superseded in this run.
	for _, entry := range activeEntries {
		if alreadySuperseded[entry.id] {
			continue // This entry was already superseded by a newer one — skip.
		}
		newVec, err := p.EmbedClient.EmbedSingle(ctx, entry.value)
		if err != nil || len(newVec) == 0 {
			continue
		}

		// Find other active entries in the same category.
		existRows, err := store.QueryContext(ctx,
			`SELECT id, value FROM semantic_memory
			 WHERE memory_state = 'active' AND category = ? AND id != ?
			 ORDER BY updated_at DESC LIMIT 20`,
			entry.category, entry.id)
		if err != nil {
			continue
		}

		for existRows.Next() {
			var existID, existValue string
			if existRows.Scan(&existID, &existValue) != nil {
				continue
			}
			// Skip if values are identical (not a contradiction, just a duplicate).
			if existValue == entry.value {
				continue
			}
			existVec, err := p.EmbedClient.EmbedSingle(ctx, existValue)
			if err != nil || len(existVec) == 0 {
				continue
			}
			sim := llm.CosineSimilarity(newVec, existVec)
			if sim > 0.7 {
				// High similarity + different content → check if same subject (contradiction)
				// vs different subject (complementary). Same-subject entries that differ
				// in predicate are contradictions; different-subject entries are complementary.
				if subjectSimilarity(entry.value, existValue) > 0.5 {
					if _, err := store.ExecContext(ctx,
						`UPDATE semantic_memory SET memory_state = 'stale', state_reason = 'superseded by newer entry'
						 WHERE id = ?`, existID); err == nil {
						alreadySuperseded[existID] = true
						superseded++
					}
				}
				// Different subjects → complementary facts → leave both active.
			}
		}
		_ = existRows.Close()
	}
	return superseded
}

// Phase 4: Exponential confidence decay on semantic entries not recently accessed.
func (p *ConsolidationPipeline) phaseConfidenceDecay(ctx context.Context, store *db.Store) int {
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
		// Rust parity: constant multiplier 0.995 applied once per 24h sentinel gate,
		// not exponential per-day decay. Each consolidation pass applies one decay step
		// for entries that haven't been updated in >= 24h.
		newConf := confidence * DecayFactor
		if newConf < DecayFloor {
			newConf = DecayFloor
		}
		if newConf < confidence {
			updates = append(updates, decayEntry{id: id, newConfidence: newConf})
		}
	}
	_ = rows.Close()

	for _, u := range updates {
		if _, err := store.ExecContext(ctx,
			`UPDATE semantic_memory SET confidence = ? WHERE id = ?`, u.newConfidence, u.id); err != nil {
			log.Warn().Err(err).Str("id", u.id).Msg("consolidation: confidence decay write failed")
		}
	}
	return len(updates)
}

// Phase 5: Importance decay on episodic entries older than 7 days.
func (p *ConsolidationPipeline) phaseImportanceDecay(ctx context.Context, store *db.Store) int {
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
		if _, err := store.ExecContext(ctx,
			`UPDATE episodic_memory SET importance = ? WHERE id = ?`, u.newImportance, u.id); err != nil {
			log.Warn().Err(err).Str("id", u.id).Msg("consolidation: importance decay write failed")
		}
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

// Phase 7: Orphan cleanup — delete orphaned embeddings, index entries, and FTS rows.
func (p *ConsolidationPipeline) phaseOrphanCleanup(ctx context.Context, store *db.Store) int {
	total := 0

	// 7a: Orphaned embeddings — source_id not in any memory table.
	// Migration 039 normalized all legacy short names; only full names remain.
	res, err := store.ExecContext(ctx,
		`DELETE FROM embeddings WHERE
		 (source_table = 'episodic_memory' AND source_id NOT IN (SELECT id FROM episodic_memory)) OR
		 (source_table = 'semantic_memory' AND source_id NOT IN (SELECT id FROM semantic_memory)) OR
		 (source_table = 'procedural_memory' AND source_id NOT IN (SELECT id FROM procedural_memory)) OR
		 (source_table = 'relationship_memory' AND source_id NOT IN (SELECT id FROM relationship_memory)) OR
		 (source_table = 'working_memory' AND source_id NOT IN (SELECT id FROM working_memory))`)
	if err != nil {
		log.Warn().Err(err).Msg("consolidation: orphan embedding cleanup failed")
	} else {
		n, _ := res.RowsAffected()
		total += int(n)
	}

	// 7b: Orphaned index entries — source_id not in source table.
	res, err = store.ExecContext(ctx,
		`DELETE FROM memory_index WHERE
		 (source_table = 'episodic_memory' AND source_id NOT IN (SELECT id FROM episodic_memory)) OR
		 (source_table = 'semantic_memory' AND source_id NOT IN (SELECT id FROM semantic_memory)) OR
		 (source_table = 'procedural_memory' AND source_id NOT IN (SELECT id FROM procedural_memory)) OR
		 (source_table = 'relationship_memory' AND source_id NOT IN (SELECT id FROM relationship_memory))`)
	if err != nil {
		log.Warn().Err(err).Msg("consolidation: orphan index cleanup failed")
	} else {
		n, _ := res.RowsAffected()
		total += int(n)
	}

	// 7c: Orphaned FTS entries — source_id not in memory_index.
	res, err = store.ExecContext(ctx,
		`DELETE FROM memory_fts WHERE rowid IN (
			SELECT mf.rowid FROM memory_fts mf
			WHERE NOT EXISTS (SELECT 1 FROM memory_index mi
				WHERE mi.source_table = mf.source_table AND mi.source_id = mf.source_id)
		)`)
	if err != nil {
		// FTS table may not exist — this is expected on fresh DBs.
		log.Debug().Err(err).Msg("consolidation: orphan FTS cleanup skipped")
	} else {
		n, _ := res.RowsAffected()
		total += int(n)
	}

	// 7d: Inactive working memory — sessions that are archived/expired.
	res, err = store.ExecContext(ctx,
		`DELETE FROM working_memory WHERE session_id IN (
			SELECT id FROM sessions WHERE status IN ('archived', 'expired')
		)`)
	if err != nil {
		log.Debug().Err(err).Msg("consolidation: inactive working memory cleanup failed")
	} else {
		n, _ := res.RowsAffected()
		total += int(n)
	}

	return total
}

// Phase0_MarkDerivableStale marks tool-output-derived memory entries as stale.
// Tool outputs from derivable tools (list_directory, get_wallet_balance, etc.)
// produce ephemeral facts that become stale quickly. This phase marks any
// episodic entries originating from derivable tools so they do not pollute
// retrieval with outdated facts.
func (p *ConsolidationPipeline) Phase0_MarkDerivableStale(ctx context.Context, store *db.Store) int {
	// derivableTools maps tool names whose output is ephemeral.
	derivableToolNames := []string{
		"list_directory", "list-subagents", "get_wallet_balance",
		"read_file", "list_skills", "get_session", "get_config",
		"list_sessions", "list_tools", "search_web",
	}

	marked := 0
	for _, toolName := range derivableToolNames {
		pattern := toolName + ":%"
		res, err := store.ExecContext(ctx,
			`UPDATE episodic_memory SET memory_state = 'stale', state_reason = 'derivable tool output'
			 WHERE memory_state = 'active' AND classification = 'tool_event'
			 AND content LIKE ?`, pattern)
		if err != nil {
			log.Warn().Err(err).Str("tool", toolName).Msg("consolidation: phase0 mark derivable failed")
			continue
		}
		n, _ := res.RowsAffected()
		marked += int(n)
	}
	return marked
}

// Phase2_ObsidianVaultScan indexes vault notes from the configured Obsidian vault
// path and cleans up index entries for notes that have been deleted from the vault.
// This bridges external knowledge management with the memory system.
func (p *ConsolidationPipeline) Phase2_ObsidianVaultScan(ctx context.Context, store *db.Store) int {
	// Count orphaned vault index entries (vault notes that no longer exist).
	// Vault entries are tracked in memory_index with source_table = 'obsidian'.
	res, err := store.ExecContext(ctx,
		`UPDATE memory_index SET confidence = 0.0
		 WHERE source_table = 'obsidian'
		 AND last_verified IS NOT NULL
		 AND julianday('now') - julianday(last_verified) > 7`)
	if err != nil {
		log.Debug().Err(err).Msg("consolidation: obsidian stale index decay skipped")
		return 0
	}
	n, _ := res.RowsAffected()

	// Clean up zero-confidence obsidian entries.
	res2, err := store.ExecContext(ctx,
		`DELETE FROM memory_index
		 WHERE source_table = 'obsidian' AND confidence <= 0.0`)
	if err != nil {
		log.Debug().Err(err).Msg("consolidation: obsidian orphan cleanup failed")
	} else {
		n2, _ := res2.RowsAffected()
		n += n2
	}
	return int(n)
}

// Phase4_TierStateSync synchronizes memory index confidence scores with tier
// lifecycle signals. When an entry's source has been promoted, pruned, or
// deduped, the index confidence should reflect that state change.
func (p *ConsolidationPipeline) Phase4_TierStateSync(ctx context.Context, store *db.Store) int {
	synced := 0

	// Sync episodic entries: if source is not active, lower index confidence.
	res, err := store.ExecContext(ctx,
		`UPDATE memory_index SET confidence = 0.1
		 WHERE source_table = 'episodic'
		 AND source_id IN (
			SELECT id FROM episodic_memory WHERE memory_state IN ('stale', 'deduped', 'pruned', 'promoted')
		 )
		 AND confidence > 0.1`)
	if err == nil {
		n, _ := res.RowsAffected()
		synced += int(n)
	}

	// Sync semantic entries.
	res, err = store.ExecContext(ctx,
		`UPDATE memory_index SET confidence = 0.1
		 WHERE source_table = 'semantic_memory'
		 AND source_id IN (
			SELECT id FROM semantic_memory WHERE memory_state IN ('stale', 'deduped', 'pruned')
		 )
		 AND confidence > 0.1`)
	if err == nil {
		n, _ := res.RowsAffected()
		synced += int(n)
	}

	// Boost confidence for recently verified active entries.
	res, err = store.ExecContext(ctx,
		`UPDATE memory_index SET confidence = MIN(1.0, confidence + 0.1)
		 WHERE source_table = 'semantic_memory'
		 AND source_id IN (
			SELECT id FROM semantic_memory
			WHERE memory_state = 'active' AND confidence > 0.7
			AND julianday('now') - julianday(updated_at) < 1
		 )
		 AND confidence < 1.0`)
	if err == nil {
		n, _ := res.RowsAffected()
		synced += int(n)
	}

	return synced
}

// phaseSkillsConfidenceSync synchronizes confidence scores in procedural_memory
// and learned_skills tables. Procedural entries with >80% failure rate get
// confidence floored to 0.1. Learned skills get confidence = max(0.1, priority/100).
func (p *ConsolidationPipeline) phaseSkillsConfidenceSync(ctx context.Context, store *db.Store) int {
	synced := 0

	// Procedural memory: if failure_count > success_count * 4 (>80% failure), floor confidence.
	res, err := store.ExecContext(ctx,
		`UPDATE procedural_memory SET confidence = 0.1
		 WHERE memory_state = 'active'
		 AND failure_count > success_count * 4
		 AND confidence > 0.1`)
	if err == nil {
		n, _ := res.RowsAffected()
		synced += int(n)
	} else {
		log.Debug().Err(err).Msg("consolidation: procedural confidence sync skipped (table may not exist)")
	}

	// Learned skills: sync confidence = max(0.1, priority / 100.0).
	res, err = store.ExecContext(ctx,
		`UPDATE learned_skills SET confidence = MAX(0.1, CAST(priority AS REAL) / 100.0)
		 WHERE ABS(confidence - MAX(0.1, CAST(priority AS REAL) / 100.0)) > 0.001`)
	if err == nil {
		n, _ := res.RowsAffected()
		synced += int(n)
	} else {
		log.Debug().Err(err).Msg("consolidation: learned_skills confidence sync skipped (table may not exist)")
	}

	return synced
}
