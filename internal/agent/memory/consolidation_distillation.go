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

// MinRelationDistillSupport is the threshold for promoting a recurring
// (subject, relation, object) triple into knowledge_facts via the M8
// relational distillation phase. The bar is intentionally stricter than
// MinFixPatternSupport (which is 2) and equal to MinDistillSupport (3):
// promoted relations participate in graph traversals (path / impact /
// dependency) and a wrong relation contaminates downstream queries far
// more visibly than a wrong free-text learning, so a coincidence between
// two episodes isn't enough.
const MinRelationDistillSupport = 3

// distilledRelationConfidenceCap is below the per-document extraction
// confidence (0.95) so a later canonical document or explicit assertion
// can still supersede a distilled relation without a special-case rule.
const distilledRelationConfidenceCap = 0.9

// distilledRelationSourceTable tags promoted relations as having come from
// episode distillation rather than per-document semantic ingestion. The
// canonical relation gate (db.IsCanonicalGraphRelation) applies to both
// paths uniformly; this label exists for provenance / debugging only.
const distilledRelationSourceTable = "episodic_distillation"

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
	// relationCount keys on the lowercased canonical signature
	// "subject|relation|object" so two episodes that mention the same
	// triple in different cases or whitespace count as one observation.
	relationCount := make(map[string]int)
	// relationExemplar preserves the original-cased triple for the first
	// episode that mentioned it so the promoted knowledge_facts row keeps
	// human-readable casing rather than the lowercased tally key.
	relationExemplar := make(map[string]EpisodeRelation)
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
		// Tally relations once per episode; a single chatty episode that
		// mentions the same triple twice still counts as one observation
		// of that relation.
		seenInEpisode := make(map[string]struct{})
		for _, rel := range fields.Relations {
			subj := strings.TrimSpace(rel.Subject)
			relType := strings.TrimSpace(rel.Relation)
			obj := strings.TrimSpace(rel.Object)
			if subj == "" || relType == "" || obj == "" {
				continue
			}
			if !db.IsCanonicalGraphRelation(relType) {
				// Relation didn't pass the canonical gate (the per-document
				// extractor enforces this too, but defending in depth here
				// means a malformed episode_summary can't sneak a non-
				// canonical relation past the write step).
				continue
			}
			signature := strings.ToLower(subj) + "|" + relType + "|" + strings.ToLower(obj)
			if _, dup := seenInEpisode[signature]; dup {
				continue
			}
			seenInEpisode[signature] = struct{}{}
			relationCount[signature]++
			if _, ok := relationExemplar[signature]; !ok {
				relationExemplar[signature] = EpisodeRelation{
					Subject:  subj,
					Relation: relType,
					Object:   obj,
				}
			}
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

	// M8 relational distillation: promote (subject, relation, object)
	// triples that recurred across enough high-quality episodes to count
	// as learned graph relations. Reuses the canonical-relation write
	// gate via db.MemoryRepository.StoreKnowledgeFact, so the same
	// vocabulary and validation rules apply to distilled and per-document
	// extractions alike.
	relationsPromoted := 0
	repo := db.NewMemoryRepository(store)
	for signature, count := range relationCount {
		if count < MinRelationDistillSupport {
			continue
		}
		exemplar, ok := relationExemplar[signature]
		if !ok {
			continue
		}
		factID := distilledRelationFactID(exemplar.Subject, exemplar.Relation, exemplar.Object)
		// Confidence scales with support but is capped below the
		// per-document extraction confidence so a later canonical
		// document can supersede a distilled relation without a
		// special-case rule.
		confidence := 0.5 + float64(count)*0.1
		if confidence > distilledRelationConfidenceCap {
			confidence = distilledRelationConfidenceCap
		}
		err := repo.StoreKnowledgeFact(ctx,
			factID,
			exemplar.Subject, exemplar.Relation, exemplar.Object,
			distilledRelationSourceTable, factID,
			confidence,
		)
		if err != nil {
			// Non-canonical relations are filtered above, so the only
			// expected errors here are transient store failures. Logged
			// at Debug because the next consolidation pass will retry.
			log.Debug().
				Err(err).
				Str("subject", exemplar.Subject).
				Str("relation", exemplar.Relation).
				Str("object", exemplar.Object).
				Msg("consolidation: distilled relation promotion failed")
			continue
		}
		relationsPromoted++
	}
	promoted += relationsPromoted

	if promoted > 0 {
		log.Info().
			Int("promoted", promoted).
			Int("fix_patterns", len(fixPatternCount)).
			Int("evidence_refs", len(evidenceCount)).
			Int("relations_distilled", relationsPromoted).
			Int("episodes", len(episodes)).
			Str("category", "consolidation_distillation").
			Msg("consolidation: distilled patterns into semantic memory and knowledge_facts")
	}
	return promoted
}

// distilledRelationFactID is the stable knowledge_facts id used for a
// distilled triple. We hash on (subject, relation, object) only — not on a
// per-episode source id — so re-running consolidation upserts the same row
// rather than creating a new one each time. The "distill_" prefix
// distinguishes distilled rows from per-document extractions (which use
// the `fact_` prefix from manager.knowledgeFactID).
func distilledRelationFactID(subject, relation, object string) string {
	signature := strings.ToLower(subject) + "|" + relation + "|" + strings.ToLower(object)
	return "distill_" + shortHash(signature)
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
	Outcome      string
	FixPatterns  []string
	EvidenceRefs []string
	QualityLabel string
	HighQuality  bool

	// Relations is the M8 surface: the (subject, relation, object) triples
	// extracted from the episode at AnalyzeEpisode time, persisted via
	// FormatForStorage's "Relations: ..." segment, and tallied across
	// high-quality episodes by phaseEpisodeDistillation for promotion into
	// knowledge_facts.
	Relations []EpisodeRelation
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
		case strings.HasPrefix(segment, "Relations: "):
			fields.Relations = parseRelationsList(strings.TrimPrefix(segment, "Relations: "))
		}
	}
	// Only episodes we are confident about contribute to distillation so
	// failures and low-quality partials cannot drag patterns into semantic
	// memory.
	fields.HighQuality = fields.Outcome == "success" || fields.QualityLabel == "high" || fields.QualityLabel == "medium"
	return fields
}

// parseRelationsList decodes the "Relations: subj||rel||obj; subj||rel||obj"
// segment written by EpisodeSummary.FormatForStorage. Triples that don't
// match the three-part shape are silently dropped — the persisted format is
// well-defined and any malformed entry is more likely a corrupted row than
// a real signal worth chasing into the graph.
func parseRelationsList(s string) []EpisodeRelation {
	var out []EpisodeRelation
	for _, raw := range strings.Split(s, ";") {
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" {
			continue
		}
		parts := strings.Split(trimmed, "||")
		if len(parts) != 3 {
			continue
		}
		subj := strings.TrimSpace(parts[0])
		rel := strings.TrimSpace(parts[1])
		obj := strings.TrimSpace(parts[2])
		if subj == "" || rel == "" || obj == "" {
			continue
		}
		out = append(out, EpisodeRelation{Subject: subj, Relation: rel, Object: obj})
	}
	return out
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
