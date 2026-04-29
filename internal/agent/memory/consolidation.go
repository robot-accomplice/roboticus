package memory

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"github.com/rs/zerolog/log"

	"roboticus/internal/db"
	"roboticus/internal/llm"
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

// ConsolidationReport holds per-phase counts from a memory curation run.
// The type name is retained for compatibility with existing call sites.
type ConsolidationReport struct {
	Indexed            int
	Deduped            int
	Promoted           int
	Distilled          int
	Superseded         int
	ConfidenceDecayed  int
	ImportanceDecayed  int
	Pruned             int
	Orphaned           int
	DerivableStale     int
	ObsidianScanned    int
	TierSynced         int
	SkillsConfSynced   int
	EmbeddingsBackfill int
}

// LLMDistiller distills multiple episodic entries into a single semantic fact.
// This interface enables LLM-assisted episodic-to-semantic promotion during
// quiescent memory curation.
type LLMDistiller interface {
	Distill(ctx context.Context, entries []string) (string, error)
}

// ConsolidationPipeline runs the multi-phase memory curation lifecycle. The
// type name is retained for compatibility; Memory Consolidation is now the
// deduplication/promotion/distillation subset of this broader lifecycle.
type ConsolidationPipeline struct {
	// MinInterval prevents running more than once per this duration.
	MinInterval time.Duration
	// EmbedClient is used for backfilling embeddings on entries that were stored
	// before the embedding pipeline was connected. Nil disables backfill.
	EmbedClient *llm.EmbeddingClient
	// Distiller enables LLM-assisted episodic-to-semantic promotion.
	// When nil, falls back to longest-entry heuristic.
	Distiller LLMDistiller
	// MaxDistillPerRun limits the number of LLM distillation calls per memory
	// curation run for cost control. Default: 5.
	MaxDistillPerRun int
}

// NewConsolidationPipeline creates a memory curation pipeline with a default
// 1-hour minimum interval. The function name is retained for compatibility.
func NewConsolidationPipeline() *ConsolidationPipeline {
	return &ConsolidationPipeline{
		MinInterval: time.Hour,
	}
}

// Run executes memory curation and returns a report.
// Phase order:
//   - Phase 0: Mark derivable tool outputs stale
//   - Phase 1: Index backfill
//   - Phase 2: Embedding backfill + Obsidian vault scan
//   - Phase 3: Memory consolidation (dedup, promotion, distillation)
//   - Phase 4: Memory governance (supersession, tier/skill sync)
//   - Phase 5: Confidence + importance decay
//   - Phase 6: Pruning + orphan cleanup
func (p *ConsolidationPipeline) Run(ctx context.Context, store *db.Store) ConsolidationReport {
	var report ConsolidationReport

	// Gate: check last curation time.
	if !p.shouldRun(ctx, store) {
		log.Debug().Msg("memory curation: skipped (ran recently)")
		return report
	}

	// Curation mutates memory and derived memory surfaces. Do not run any phase
	// while a session is active; force bypasses interval gating only, not
	// quiescence safety.
	if !isQuiescent(ctx, store) {
		log.Debug().Msg("memory curation: skipped (session active within 5s)")
		return report
	}

	report.DerivableStale = p.Phase0_MarkDerivableStale(ctx, store)
	report.Indexed = p.phaseIndexBackfill(ctx, store)
	report.EmbeddingsBackfill = p.phaseEmbeddingBackfill(ctx, store)
	report.ObsidianScanned = p.Phase2_ObsidianVaultScan(ctx, store)
	report.Deduped = p.phaseWithinTierDedup(ctx, store)
	report.Promoted = p.phaseEpisodicPromotion(ctx, store)
	report.Distilled = p.phaseEpisodeDistillation(ctx, store)
	report.Superseded = p.phaseContradictionDetection(ctx, store)
	report.ConfidenceDecayed = p.phaseConfidenceDecay(ctx, store)
	report.TierSynced = p.Phase4_TierStateSync(ctx, store)
	report.SkillsConfSynced = p.phaseSkillsConfidenceSync(ctx, store)
	report.ImportanceDecayed = p.phaseImportanceDecay(ctx, store)
	report.Pruned = p.phasePruning(ctx, store)
	report.Orphaned = p.phaseOrphanCleanup(ctx, store)

	// Record the run.
	p.recordRun(ctx, store, report)

	log.Info().
		Int("derivable_stale", report.DerivableStale).
		Int("indexed", report.Indexed).
		Int("embeddings_backfill", report.EmbeddingsBackfill).
		Int("deduped", report.Deduped).
		Int("obsidian_scanned", report.ObsidianScanned).
		Int("promoted", report.Promoted).
		Int("superseded", report.Superseded).
		Int("confidence_decayed", report.ConfidenceDecayed).
		Int("tier_synced", report.TierSynced).
		Int("skills_conf_synced", report.SkillsConfSynced).
		Int("importance_decayed", report.ImportanceDecayed).
		Int("pruned", report.Pruned).
		Int("orphaned", report.Orphaned).
		Msg("memory curation: complete")

	return report
}

// isQuiescent returns true if no sessions have been active in the last 5 seconds.
// Rust parity: consolidation.rs is_quiescent() — gates data-moving phases.
func isQuiescent(ctx context.Context, store *db.Store) bool {
	var active int64
	err := store.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM sessions
		 WHERE status = 'active'
		   AND updated_at > datetime('now', '-5 seconds')`).Scan(&active)
	if err != nil {
		return false // default to non-quiescent on error (Rust: unwrap_or(1))
	}
	return active == 0
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
