// consolidation_distillation.go implements Milestone 8's cross-tier promotion
// phase: parse enriched episode_summary entries, detect patterns that recur
// across multiple successful episodes, and promote them into semantic memory.
//
// Why a dedicated phase? The existing phaseEpisodicPromotion uses Jaccard
// similarity over raw episodic content, which is good at clustering near-
// duplicate episodes but loses the structured signal AnalyzeEpisode now
// writes (FixPatterns, EvidenceRefs, FailedHypotheses, ResultQuality).
// This phase reads those structured fields back out of storage and uses them
// directly so repeat successes turn into reusable semantic knowledge without
// waiting for the episode count to hit the Jaccard threshold.

package memory

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"strings"

	"github.com/rs/zerolog/log"

	"roboticus/internal/db"
)

// MinDistillSupport is the minimum number of episodes a fix pattern or
// evidence reference must appear in before it is promoted. Keeping this at
// 3 avoids anecdote hijacking — one episode is noise, two is a coincidence,
// three is a pattern.
const MinDistillSupport = 3

// MinFixPatternSupport is more permissive than evidence support because fix
// patterns already carry a success-after-failure signal, so 2 occurrences
// are meaningful.
const MinFixPatternSupport = 2

// phaseEpisodeDistillation inspects recent episode_summary entries, extracts
// the enriched structured fields, and promotes recurring patterns into
// semantic_memory. The phase is idempotent: semantic_memory uses UPSERT on
// (category, key) so re-running never duplicates.
func (p *ConsolidationPipeline) phaseEpisodeDistillation(ctx context.Context, store *db.Store) int {
	if store == nil {
		return 0
	}

	rows, err := store.QueryContext(ctx,
		`SELECT content FROM episodic_memory
		  WHERE classification = 'episode_summary'
		    AND memory_state = 'active'
		  ORDER BY created_at DESC
		  LIMIT 200`)
	if err != nil {
		log.Debug().Err(err).Msg("consolidation: episode distillation load failed")
		return 0
	}
	defer func() { _ = rows.Close() }()

	fixPatternCount := make(map[string]int)
	evidenceCount := make(map[string]int)
	var episodes []episodeFields
	for rows.Next() {
		var content string
		if err := rows.Scan(&content); err != nil {
			continue
		}
		fields := parseEpisodeSummary(content)
		if !fields.HighQuality {
			continue
		}
		episodes = append(episodes, fields)
		for _, pattern := range fields.FixPatterns {
			key := strings.ToLower(strings.TrimSpace(pattern))
			if key == "" {
				continue
			}
			fixPatternCount[key]++
		}
		for _, evidence := range fields.EvidenceRefs {
			key := strings.ToLower(strings.TrimSpace(evidence))
			if key == "" {
				continue
			}
			evidenceCount[key]++
		}
	}

	promoted := 0
	// Promote fix patterns that have cleared the retry-success bar at least
	// MinFixPatternSupport times. These become semantic facts of the form
	// "tool X recovers from failure on retry — applied in N episodes".
	for rawKey, count := range fixPatternCount {
		if count < MinFixPatternSupport {
			continue
		}
		key := "fix_pattern:" + shortHash(rawKey)
		value := firstMatchingPattern(episodes, rawKey, func(f episodeFields) []string { return f.FixPatterns })
		if value == "" {
			continue
		}
		if p.upsertDistilledFact(ctx, store, "fix_pattern", key, value, count) {
			promoted++
		}
	}

	// Promote evidence previews that recurred across enough successful
	// episodes to count as learned facts.
	for rawKey, count := range evidenceCount {
		if count < MinDistillSupport {
			continue
		}
		key := "learned_fact:" + shortHash(rawKey)
		value := firstMatchingPattern(episodes, rawKey, func(f episodeFields) []string { return f.EvidenceRefs })
		if value == "" {
			continue
		}
		if p.upsertDistilledFact(ctx, store, "learned_fact", key, value, count) {
			promoted++
		}
	}

	if promoted > 0 {
		log.Info().
			Int("promoted", promoted).
			Int("fix_patterns", len(fixPatternCount)).
			Int("evidence_refs", len(evidenceCount)).
			Int("episodes", len(episodes)).
			Str("category", "consolidation_distillation").
			Msg("consolidation: distilled patterns into semantic memory")
	}
	return promoted
}

// upsertDistilledFact writes a distilled semantic fact. The confidence scales
// with support count but is capped at 0.95 — even repeated patterns benefit
// from a small uncertainty margin so they can still be superseded.
func (p *ConsolidationPipeline) upsertDistilledFact(ctx context.Context, store *db.Store, category, key, value string, support int) bool {
	confidence := 0.5 + float64(support)*0.1
	if confidence > 0.95 {
		confidence = 0.95
	}
	entryID := db.NewID()
	_, err := store.ExecContext(ctx,
		`INSERT INTO semantic_memory (id, category, key, value, confidence)
		 VALUES (?, ?, ?, ?, ?)
		 ON CONFLICT(category, key) DO UPDATE SET
		     value = excluded.value,
		     confidence = MAX(semantic_memory.confidence, excluded.confidence),
		     memory_state = 'active',
		     state_reason = NULL,
		     updated_at = datetime('now')`,
		entryID, category, key, value, confidence,
	)
	if err != nil {
		log.Debug().Err(err).Str("category", category).Msg("consolidation: distilled fact upsert failed")
		return false
	}
	return true
}

// episodeFields is the parsed shape of an EpisodeSummary.FormatForStorage
// output. Keep the field list minimal — only what the distillation phase
// uses — so the parser can remain a plain string splitter.
type episodeFields struct {
	Outcome       string
	FixPatterns   []string
	EvidenceRefs  []string
	QualityLabel  string
	HighQuality   bool
}

// parseEpisodeSummary pulls the enriched fields out of the pipe-delimited
// storage format written by EpisodeSummary.FormatForStorage. Fields the
// format omits become empty slices.
func parseEpisodeSummary(content string) episodeFields {
	fields := episodeFields{}
	if content == "" {
		return fields
	}
	for _, segment := range strings.Split(content, " | ") {
		segment = strings.TrimSpace(segment)
		if segment == "" {
			continue
		}
		switch {
		case strings.HasPrefix(segment, "Outcome: "):
			fields.Outcome = strings.TrimSpace(strings.TrimPrefix(segment, "Outcome: "))
		case strings.HasPrefix(segment, "FixPatterns: "):
			fields.FixPatterns = splitSemicolonList(strings.TrimPrefix(segment, "FixPatterns: "))
		case strings.HasPrefix(segment, "EvidenceRefs: "):
			fields.EvidenceRefs = splitPipeList(strings.TrimPrefix(segment, "EvidenceRefs: "))
		case strings.HasPrefix(segment, "Quality: "):
			fields.QualityLabel = strings.TrimSpace(strings.TrimPrefix(segment, "Quality: "))
		}
	}
	// Only episodes we are confident about contribute to distillation so
	// failures and low-quality partials cannot drag patterns into semantic
	// memory.
	fields.HighQuality = fields.Outcome == "success" || fields.QualityLabel == "high" || fields.QualityLabel == "medium"
	return fields
}

func splitSemicolonList(s string) []string {
	var out []string
	for _, part := range strings.Split(s, ";") {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func splitPipeList(s string) []string {
	var out []string
	for _, part := range strings.Split(s, " | ") {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func firstMatchingPattern(episodes []episodeFields, target string, pick func(episodeFields) []string) string {
	target = strings.ToLower(strings.TrimSpace(target))
	for _, ep := range episodes {
		for _, item := range pick(ep) {
			if strings.ToLower(strings.TrimSpace(item)) == target {
				return item
			}
		}
	}
	return ""
}

func shortHash(value string) string {
	sum := sha1.Sum([]byte(strings.ToLower(strings.TrimSpace(value))))
	return hex.EncodeToString(sum[:8])
}
