package memory

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"

	"roboticus/internal/db"
)

// semanticAuthority returns the (is_canonical, authority_score) pair for a
// semantic_memory row given its persisted canonical flag and confidence.
//
// Milestone 3 follow-on: canonical is now a caller-asserted persisted flag
// (see internal/db/migrations/047_policy_ingestion.sql and
// Manager.IngestPolicyDocument). The inference-by-substring path the old
// implementation used is gone — authority is an explicit claim backed by
// provenance, never a guess from how a row happens to be named.
//
// The authority floor of 0.85 for canonical rows is retained: even a
// barely-indexed canonical doc should rank above generic high-confidence
// non-canonical entries so policy queries find the authoritative source
// first.
func semanticAuthority(isCanonical bool, confidence float64) (bool, float64) {
	score := confidence
	if score < 0 {
		score = 0
	}
	if score > 1 {
		score = 1
	}
	if isCanonical && score < 0.85 {
		score = 0.85
	}
	return isCanonical, score
}

func semanticSourceLabel(category, key string) string {
	if category == "" {
		return key
	}
	if key == "" {
		return category
	}
	return category + "/" + key
}

func relationshipSourceLabel(entityName, entityID string) string {
	if entityName == "" {
		return entityID
	}
	if entityID == "" {
		return entityName
	}
	return entityName + "/" + entityID
}

func knowledgeFactSourceLabel(subject, relation, object string) string {
	return subject + " " + relation + " " + object
}

// Retrieval-tier graph types are aliases for the exported KnowledgeGraph API
// types in graph.go. The alias keeps retrieval code readable while reusing
// one authoritative graph representation for the whole memory package.
type graphFactRow = GraphFactRow
type graphEdge = GraphEdge

type graphTraversalIntent int

const (
	graphTraversalDirect graphTraversalIntent = iota
	graphTraversalExpand
	graphTraversalImpact
	graphTraversalPath
)

// retrieveSemanticEvidence fetches semantic memory with richer provenance preserved.
//
// M3.2 / PAR-014: HybridSearch is the primary semantic read path. When the
// blended hybrid leg returns zero semantic candidates for a real search query,
// semantic retrieval now falls back to a tier-scoped FTS query over an
// enriched semantic FTS corpus (category + key + value), not a residual LIKE
// safety net. The unfiltered "newest 20" branch is retained for the no-query /
// non-search browse path (mode=Recency or empty query) and is not classified
// as a fallback — it is the intended browse behavior.
//
// Trace annotation: every hybrid-mode semantic search emits
// "retrieval.path.semantic" = "fts" | "vector" | "hybrid" | "empty".
// Semantic retrieval no longer uses RetrievalPathLikeFallback; that label is
// still valid for other tiers that intentionally retain a LIKE safety net.
func (mr *Retriever) retrieveSemanticEvidence(ctx context.Context, query string, queryEmbed []float32, mode RetrievalMode, budgetTokens int) []Evidence {
	maxChars := budgetTokens * mr.charsPerToken
	used := 0
	appendEvidence := func(dst []Evidence, ev Evidence) []Evidence {
		if ev.Content == "" {
			return dst
		}
		if used+len(ev.Content) > maxChars {
			return dst
		}
		used += len(ev.Content)
		return append(dst, ev)
	}

	var evidence []Evidence
	seen := make(map[string]struct{})
	isHybridMode := mode == RetrievalHybrid || mode == RetrievalSemantic || mode == RetrievalANN
	isSearch := query != ""

	if isHybridMode && isSearch {
		weight := mr.config.HybridWeight
		if weight <= 0 {
			weight = AdaptiveHybridWeight(mr.estimateCorpusSize(ctx))
		}
		if mode == RetrievalSemantic || mode == RetrievalANN {
			weight = 1.0
		}
		results := db.HybridSearch(ctx, mr.store, query, queryEmbed, 20, weight, mr.vectorIndex)
		var ftsHits, vecHits int
		for _, hr := range results {
			if hr.SourceTable != "semantic_memory" {
				continue
			}
			var (
				id             string
				category       string
				key            string
				value          string
				confidence     float64
				ageDays        float64
				isCanonicalCol sql.NullInt64
				sourceLabelCol sql.NullString
			)
			err := mr.store.QueryRowContext(ctx,
				`SELECT id, category, key, value, confidence,
				        julianday('now') - julianday(updated_at),
				        is_canonical, source_label
				   FROM semantic_memory
				  WHERE id = ? AND memory_state = 'active'`,
				hr.SourceID).Scan(&id, &category, &key, &value, &confidence, &ageDays, &isCanonicalCol, &sourceLabelCol)
			if err != nil {
				continue
			}
			seen[id] = struct{}{}
			if hr.FTSScore > 0 {
				ftsHits++
			}
			if hr.VectorScore > 0 {
				vecHits++
			}
			persistedCanonical := isCanonicalCol.Valid && isCanonicalCol.Int64 != 0
			isCanonical, authority := semanticAuthority(persistedCanonical, confidence)
			label := semanticSourceLabel(category, key)
			if sourceLabelCol.Valid && sourceLabelCol.String != "" {
				label = sourceLabelCol.String
			}
			evidence = appendEvidence(evidence, Evidence{
				Content:        fmt.Sprintf("[%s] %s: %s", category, key, value),
				SourceTier:     TierSemantic,
				SourceID:       id,
				SourceTable:    "semantic_memory",
				SourceLabel:    label,
				SourceCategory: category,
				Score:          hr.Similarity,
				FTSScore:       hr.FTSScore,
				VecScore:       hr.VectorScore,
				AgeDays:        ageDays,
				IsCanonical:    isCanonical,
				AuthorityScore: authority,
				RetrievalMode:  mode.String(),
			})
		}
		if len(evidence) > 0 {
			annotateRetrievalPath(ctx, RetrievalTierSemantic, classifyHybridPath(ftsHits, vecHits))
			return evidence
		}
		// Hybrid produced zero semantic rows for this tier — fall through to the
		// semantic-tier FTS fallback so we can preserve semantic key/category
		// lookup without using heuristic SQL.
	}

	var rows *sql.Rows
	var err error
	if isSearch {
		ftsQuery := db.SanitizeFTSQuery(query)
		if ftsQuery != "" {
			rows, err = mr.store.QueryContext(ctx,
				`SELECT sm.id, sm.category, sm.key, sm.value, sm.confidence,
				        julianday('now') - julianday(sm.updated_at) AS age_days,
				        sm.is_canonical, sm.source_label
				   FROM memory_fts fts
				   JOIN semantic_memory sm ON sm.id = fts.source_id
				  WHERE fts.source_table = 'semantic_memory'
				    AND memory_fts MATCH ?
				    AND sm.memory_state = 'active'
				  ORDER BY bm25(memory_fts), sm.is_canonical DESC, sm.confidence DESC, sm.updated_at DESC
				  LIMIT 20`,
				ftsQuery)
		}
	}
	if err != nil || rows == nil {
		rows, err = mr.store.QueryContext(ctx,
			`SELECT id, category, key, value, confidence,
			        julianday('now') - julianday(updated_at) AS age_days,
			        is_canonical, source_label
			   FROM semantic_memory
			  WHERE memory_state = 'active'
			  ORDER BY is_canonical DESC, confidence DESC, updated_at DESC LIMIT 20`)
	}
	if err != nil {
		if isHybridMode && isSearch {
			annotateRetrievalPath(ctx, RetrievalTierSemantic, RetrievalPathEmpty)
		}
		return nil
	}
	defer func() { _ = rows.Close() }()

	ftsFallbackProduced := 0
	for rows.Next() {
		var (
			id             string
			category       string
			key            string
			value          string
			confidence     float64
			ageDays        float64
			isCanonicalCol sql.NullInt64
			sourceLabelCol sql.NullString
		)
		if rows.Scan(&id, &category, &key, &value, &confidence, &ageDays, &isCanonicalCol, &sourceLabelCol) != nil {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		persistedCanonical := isCanonicalCol.Valid && isCanonicalCol.Int64 != 0
		isCanonical, authority := semanticAuthority(persistedCanonical, confidence)
		label := semanticSourceLabel(category, key)
		if sourceLabelCol.Valid && sourceLabelCol.String != "" {
			label = sourceLabelCol.String
		}
		before := len(evidence)
		evidence = appendEvidence(evidence, Evidence{
			Content:        fmt.Sprintf("[%s] %s: %s", category, key, value),
			SourceTier:     TierSemantic,
			SourceID:       id,
			SourceTable:    "semantic_memory",
			SourceLabel:    label,
			SourceCategory: category,
			Score:          confidence,
			AgeDays:        ageDays,
			IsCanonical:    isCanonical,
			AuthorityScore: authority,
			RetrievalMode:  mode.String(),
		})
		if len(evidence) > before {
			ftsFallbackProduced++
		}
	}

	if isHybridMode && isSearch {
		switch {
		case ftsFallbackProduced > 0:
			annotateRetrievalPath(ctx, RetrievalTierSemantic, RetrievalPathFTS)
		default:
			annotateRetrievalPath(ctx, RetrievalTierSemantic, RetrievalPathEmpty)
		}
	}
	return evidence
}

// retrieveSemanticMemory was removed in v1.0.6 — callers use
// retrieveSemanticEvidence() directly to preserve provenance metadata
// (source, age, authority) instead of flattening to "- content\n" lines.

// retrieveProceduralMemory formats procedural memory for a query. Rich
// workflows (category='workflow') are surfaced first — they carry steps,
// preconditions, error modes, and context tags. Bare tool statistics
// (category='tool' or unset) are retained as a lower-priority fallback so
// procedural retrieval never silently disappears when no workflow matches.
//
// M3.2: HybridSearch (FTS + vector) is the primary read path for both the
// workflow leg and the tool-stat leg. The residual LIKE block is kept as
// a safety net that only fires when the hybrid leg returns zero candidates
// for a real search query. Two trace annotations are emitted:
//
//	retrieval.path.workflow    — workflow-tier path classification
//	retrieval.path.procedural  — procedural-tier path classification
//
// The workflow annotation comes from findWorkflowsHybrid; the procedural
// annotation is emitted by this function based on which leg produced the
// tool-stat rows.
//
// learned_skills (Part 2) is intentionally out of M3.2 scope — it has no
// FTS coverage today and the dev spec defers it to a later milestone, so
// its retrieval shape is unchanged.
func (mr *Retriever) retrieveProceduralMemory(ctx context.Context, query string, queryEmbed []float32, mode RetrievalMode, budgetTokens int) string {
	var b strings.Builder

	filtered := query != "" && mode != RetrievalRecency
	isHybridMode := mode == RetrievalHybrid || mode == RetrievalSemantic || mode == RetrievalANN
	isSearch := query != ""

	// Part 0: Reusable workflows (rich records with steps + preconditions).
	// Use the hybrid-first variant so workflow retrieval rides FTS+vector
	// instead of a single LIKE prefilter.
	weight := mr.config.HybridWeight
	if weight <= 0 {
		weight = AdaptiveHybridWeight(mr.estimateCorpusSize(ctx))
	}
	workflows, wfErr := (&Manager{store: mr.store}).findWorkflowsHybrid(
		ctx, workflowQueryHint(query, filtered), queryEmbed, mr.vectorIndex, weight, 5)
	if wfErr == nil {
		for _, wf := range workflows {
			writeWorkflowSummary(&b, wf)
		}
	}

	// Part 1: Tool success/failure stats from procedural_memory. These rows
	// sit under category='tool' (or NULL for pre-migration data). We skip
	// category='workflow' here to avoid double-reporting a workflow that
	// Part 0 already surfaced.
	//
	// HybridSearch primary path: pull candidates via FTS+vector, filter to
	// non-workflow procedural rows, emit success/failure summaries. If
	// hybrid returned no relevant rows for this tier, fall through to the
	// existing LIKE safety net.
	hybridEmitted := 0
	var hybridFTS, hybridVec int
	if isHybridMode && isSearch {
		results := db.HybridSearch(ctx, mr.store, query, queryEmbed, 30, weight, mr.vectorIndex)
		for _, hr := range results {
			if hr.SourceTable != "procedural_memory" {
				continue
			}
			var (
				name         string
				category     sql.NullString
				successCount int
				failureCount int
			)
			err := mr.store.QueryRowContext(ctx,
				`SELECT name, category, success_count, failure_count
				   FROM procedural_memory
				  WHERE id = ? AND (category IS NULL OR category != 'workflow')`,
				hr.SourceID,
			).Scan(&name, &category, &successCount, &failureCount)
			if err != nil {
				continue
			}
			total := successCount + failureCount
			if total == 0 {
				continue
			}
			pct := float64(successCount) / float64(total) * 100
			fmt.Fprintf(&b, "- %s: %d/%d (%.0f%% success)\n", name, successCount, total, pct)
			hybridEmitted++
			if hr.FTSScore > 0 {
				hybridFTS++
			}
			if hr.VectorScore > 0 {
				hybridVec++
			}
		}
	}

	var rows *sql.Rows
	var err error
	likeEmitted := 0
	likeAttempted := false
	if hybridEmitted == 0 {
		// Hybrid leg returned zero relevant procedural rows; engage the
		// LIKE safety net (or the no-search browse path).
		if filtered {
			likeAttempted = true
			like := "%" + query + "%"
			rows, err = mr.store.QueryContext(ctx,
				`SELECT name, success_count, failure_count FROM procedural_memory
				 WHERE (category IS NULL OR category != 'workflow')
				   AND (name LIKE ? OR steps LIKE ? OR preconditions LIKE ? OR error_modes LIKE ?)
				 ORDER BY (success_count + failure_count) DESC LIMIT 15`,
				like, like, like, like)
		} else {
			rows, err = mr.store.QueryContext(ctx,
				`SELECT name, success_count, failure_count FROM procedural_memory
				 WHERE category IS NULL OR category != 'workflow'
				 ORDER BY (success_count + failure_count) DESC LIMIT 15`)
		}
		if err == nil {
			for rows.Next() {
				var name string
				var successCount, failureCount int
				if rows.Scan(&name, &successCount, &failureCount) != nil {
					continue
				}
				total := successCount + failureCount
				if total == 0 {
					continue
				}
				pct := float64(successCount) / float64(total) * 100
				fmt.Fprintf(&b, "- %s: %d/%d (%.0f%% success)\n", name, successCount, total, pct)
				likeEmitted++
			}
			_ = rows.Close()
			if filtered && likeEmitted == 0 && len(workflows) == 0 {
				// Final browse fallback when filtered LIKE matched nothing
				// and the workflow tier was empty too — keeps procedural
				// retrieval from silently disappearing.
				rows, err = mr.store.QueryContext(ctx,
					`SELECT name, success_count, failure_count FROM procedural_memory
					 WHERE category IS NULL OR category != 'workflow'
					 ORDER BY (success_count + failure_count) DESC LIMIT 15`)
				if err == nil {
					for rows.Next() {
						var name string
						var successCount, failureCount int
						if rows.Scan(&name, &successCount, &failureCount) != nil {
							continue
						}
						total := successCount + failureCount
						if total == 0 {
							continue
						}
						pct := float64(successCount) / float64(total) * 100
						fmt.Fprintf(&b, "- %s: %d/%d (%.0f%% success)\n", name, successCount, total, pct)
					}
					_ = rows.Close()
				}
			}
		}
	}

	if isHybridMode && isSearch {
		switch {
		case hybridEmitted > 0:
			annotateRetrievalPath(ctx, RetrievalTierProcedural, classifyHybridPath(hybridFTS, hybridVec))
		case likeAttempted && likeEmitted > 0:
			annotateRetrievalPath(ctx, RetrievalTierProcedural, RetrievalPathLikeFallback)
		default:
			annotateRetrievalPath(ctx, RetrievalTierProcedural, RetrievalPathEmpty)
		}
	}

	// Part 2: Learned procedures from learned_skills (auto-detected tool sequences).
	var skillRows *sql.Rows
	if filtered {
		like := "%" + query + "%"
		skillRows, err = mr.store.QueryContext(ctx,
			`SELECT name, success_count, priority FROM learned_skills
			 WHERE memory_state = 'active' AND success_count >= 2
			   AND (name LIKE ? OR description LIKE ? OR steps_json LIKE ?)
			 ORDER BY priority DESC, success_count DESC LIMIT 5`,
			like, like, like)
	} else {
		skillRows, err = mr.store.QueryContext(ctx,
			`SELECT name, success_count, priority FROM learned_skills
			 WHERE memory_state = 'active' AND success_count >= 2
			 ORDER BY priority DESC, success_count DESC LIMIT 5`)
	}
	if err == nil {
		emitted := 0
		for skillRows.Next() {
			var name string
			var successCount, priority int
			if skillRows.Scan(&name, &successCount, &priority) != nil {
				continue
			}
			fmt.Fprintf(&b, "- [learned] %s: %d runs, priority=%d\n", name, successCount, priority)
			emitted++
		}
		_ = skillRows.Close()
		if filtered && emitted == 0 {
			skillRows, err = mr.store.QueryContext(ctx,
				`SELECT name, success_count, priority FROM learned_skills
				 WHERE memory_state = 'active' AND success_count >= 2
				 ORDER BY priority DESC, success_count DESC LIMIT 5`)
			if err == nil {
				for skillRows.Next() {
					var name string
					var successCount, priority int
					if skillRows.Scan(&name, &successCount, &priority) != nil {
						continue
					}
					fmt.Fprintf(&b, "- [learned] %s: %d runs, priority=%d\n", name, successCount, priority)
				}
				_ = skillRows.Close()
			}
		}
	}

	// Part 3: Reusable procedural outcome patterns promoted from structured
	// episode summaries. These rows capture prior successes, failures, and
	// mixed outcomes so the agent can reuse or avoid known approaches instead
	// of treating only positive experience as learnable.
	var outcomeRows *sql.Rows
	if filtered {
		ftsQuery := db.SanitizeFTSQuery(query)
		if ftsQuery != "" {
			outcomeRows, err = mr.store.QueryContext(ctx,
				`SELECT sm.value, sm.confidence
				   FROM memory_fts fts
				   JOIN semantic_memory sm ON sm.id = fts.source_id
				  WHERE fts.source_table = 'semantic_memory'
				    AND memory_fts MATCH ?
				    AND sm.category = 'procedural_outcome'
				    AND sm.memory_state = 'active'
				  ORDER BY bm25(memory_fts), sm.confidence DESC, sm.updated_at DESC
				  LIMIT 5`,
				ftsQuery,
			)
		}
	} else {
		outcomeRows, err = mr.store.QueryContext(ctx,
			`SELECT value, confidence
			   FROM semantic_memory
			  WHERE category = 'procedural_outcome'
			    AND memory_state = 'active'
			  ORDER BY confidence DESC, updated_at DESC
			  LIMIT 5`)
	}
	if err == nil && outcomeRows != nil {
		emitted := 0
		for outcomeRows.Next() {
			var value string
			var confidence float64
			if outcomeRows.Scan(&value, &confidence) != nil {
				continue
			}
			if strings.TrimSpace(value) == "" {
				continue
			}
			fmt.Fprintf(&b, "- [outcome %.2f] %s\n", confidence, value)
			emitted++
		}
		_ = outcomeRows.Close()
		if filtered && emitted == 0 {
			like := "%" + query + "%"
			outcomeRows, err = mr.store.QueryContext(ctx,
				`SELECT value, confidence
				   FROM semantic_memory
				  WHERE category = 'procedural_outcome'
				    AND memory_state = 'active'
				    AND value LIKE ?
				  ORDER BY confidence DESC, updated_at DESC
				  LIMIT 5`,
				like,
			)
			if err == nil {
				for outcomeRows.Next() {
					var value string
					var confidence float64
					if outcomeRows.Scan(&value, &confidence) != nil {
						continue
					}
					if strings.TrimSpace(value) == "" {
						continue
					}
					fmt.Fprintf(&b, "- [outcome %.2f] %s\n", confidence, value)
				}
				_ = outcomeRows.Close()
			}
		}
	}

	return b.String()
}

// retrieveRelationshipMemory formats relationship data.
func (mr *Retriever) retrieveRelationshipMemory(ctx context.Context, query string, queryEmbed []float32, mode RetrievalMode, budgetTokens int) string {
	var b strings.Builder
	for _, ev := range mr.retrieveRelationshipEvidence(ctx, query, queryEmbed, mode, budgetTokens) {
		b.WriteString("- ")
		b.WriteString(ev.Content)
		b.WriteString("\n")
	}
	return b.String()
}

// retrieveRelationshipEvidence returns relationship evidence using the
// HybridSearch primary path with a LIKE safety net.
//
// M3.2: relationship_memory rows became FTS-discoverable in migration 048
// (the missing relationship_memory_fts_au UPDATE trigger was added there).
// HybridSearch is now the primary read path; the residual LIKE block only
// fires when the hybrid leg returns zero candidates for a real search query.
// The unfiltered "newest 20" branch is the no-search browse path and is
// retained unchanged.
//
// Trace annotation: emits "retrieval.path.relationship" when a search was
// attempted in a hybrid mode, with values fts | vector | hybrid |
// like_fallback | empty.
//
// The graph leg (retrieveKnowledgeFactEvidence) is annotated separately
// inside its own function — knowledge_facts is structurally different from
// the FTS-backed tiers and gets its own path classification later if needed.
func (mr *Retriever) retrieveRelationshipEvidence(ctx context.Context, query string, queryEmbed []float32, mode RetrievalMode, budgetTokens int) []Evidence {
	maxChars := budgetTokens * mr.charsPerToken
	used := 0
	appendEvidence := func(dst []Evidence, ev Evidence) []Evidence {
		if ev.Content == "" {
			return dst
		}
		if used+len(ev.Content) > maxChars {
			return dst
		}
		used += len(ev.Content)
		return append(dst, ev)
	}

	var evidence []Evidence
	for _, ev := range mr.retrieveKnowledgeFactEvidence(ctx, query, mode, budgetTokens/2) {
		evidence = appendEvidence(evidence, ev)
	}

	isHybridMode := mode == RetrievalHybrid || mode == RetrievalSemantic || mode == RetrievalANN
	isSearch := query != "" && mode != RetrievalRecency
	seenRel := make(map[string]struct{})
	hybridEmitted := 0
	var hybridFTS, hybridVec int

	if isHybridMode && isSearch {
		weight := mr.config.HybridWeight
		if weight <= 0 {
			weight = AdaptiveHybridWeight(mr.estimateCorpusSize(ctx))
		}
		results := db.HybridSearch(ctx, mr.store, query, queryEmbed, 30, weight, mr.vectorIndex)
		for _, hr := range results {
			if hr.SourceTable != "relationship_memory" {
				continue
			}
			var (
				id, entityID, entityName string
				trustScore               float64
				interactionSummary       sql.NullString
				interactionCount         int
				lastInteraction          sql.NullString
				ageDays                  float64
			)
			err := mr.store.QueryRowContext(ctx,
				`SELECT id, entity_id, entity_name, trust_score, interaction_summary,
				        interaction_count, last_interaction,
				        julianday('now') - julianday(COALESCE(updated_at, created_at))
				   FROM relationship_memory
				  WHERE id = ?`,
				hr.SourceID,
			).Scan(&id, &entityID, &entityName, &trustScore, &interactionSummary,
				&interactionCount, &lastInteraction, &ageDays)
			if err != nil {
				continue
			}
			seenRel[id] = struct{}{}
			if hr.FTSScore > 0 {
				hybridFTS++
			}
			if hr.VectorScore > 0 {
				hybridVec++
			}
			line := fmt.Sprintf("%s: trust=%.1f, interactions=%d", entityName, trustScore, interactionCount)
			if interactionSummary.Valid && interactionSummary.String != "" {
				line += ", relation=" + interactionSummary.String
			}
			if lastInteraction.Valid {
				line += ", last=" + lastInteraction.String
			}
			before := len(evidence)
			evidence = appendEvidence(evidence, Evidence{
				Content:        line,
				SourceTier:     TierRelationship,
				SourceID:       id,
				SourceTable:    "relationship_memory",
				SourceLabel:    relationshipSourceLabel(entityName, entityID),
				SourceCategory: "relationship",
				Score:          trustScore,
				FTSScore:       hr.FTSScore,
				VecScore:       hr.VectorScore,
				AgeDays:        ageDays,
				AuthorityScore: trustScore,
				RetrievalMode:  mode.String(),
			})
			if len(evidence) > before {
				hybridEmitted++
			}
		}
		if hybridEmitted > 0 {
			annotateRetrievalPath(ctx, RetrievalTierRelationship, classifyHybridPath(hybridFTS, hybridVec))
			return evidence
		}
		// Hybrid produced zero relationship rows — fall through to LIKE
		// safety net. Annotation deferred until we know whether LIKE
		// produced anything.
	}

	var rows *sql.Rows
	var err error
	likeAttempted := false
	if query != "" && mode != RetrievalRecency {
		likeAttempted = true
		like := "%" + query + "%"
		rows, err = mr.store.QueryContext(ctx,
			`SELECT id, entity_id, entity_name, trust_score, interaction_summary, interaction_count, last_interaction,
			        julianday('now') - julianday(COALESCE(updated_at, created_at)) AS age_days
			 FROM relationship_memory
			 WHERE entity_name LIKE ? OR interaction_summary LIKE ?
			 ORDER BY interaction_count DESC, trust_score DESC, COALESCE(updated_at, created_at) DESC LIMIT 20`,
			like, like)
	} else {
		rows, err = mr.store.QueryContext(ctx,
			`SELECT id, entity_id, entity_name, trust_score, interaction_summary, interaction_count, last_interaction,
			        julianday('now') - julianday(COALESCE(updated_at, created_at)) AS age_days
			 FROM relationship_memory
			 ORDER BY interaction_count DESC, COALESCE(updated_at, created_at) DESC LIMIT 20`)
	}
	if err != nil {
		if isHybridMode && isSearch {
			annotateRetrievalPath(ctx, RetrievalTierRelationship, RetrievalPathEmpty)
		}
		return evidence
	}
	defer func() { _ = rows.Close() }()

	likeEmitted := 0
	for rows.Next() {
		var id, entityID, entityName string
		var trustScore float64
		var interactionSummary sql.NullString
		var interactionCount int
		var lastInteraction *string
		var ageDays float64
		if rows.Scan(&id, &entityID, &entityName, &trustScore, &interactionSummary, &interactionCount, &lastInteraction, &ageDays) != nil {
			continue
		}
		if _, dup := seenRel[id]; dup {
			continue
		}
		line := fmt.Sprintf("%s: trust=%.1f, interactions=%d", entityName, trustScore, interactionCount)
		if interactionSummary.Valid && interactionSummary.String != "" {
			line += ", relation=" + interactionSummary.String
		}
		if lastInteraction != nil {
			line += ", last=" + *lastInteraction
		}
		before := len(evidence)
		evidence = appendEvidence(evidence, Evidence{
			Content:        line,
			SourceTier:     TierRelationship,
			SourceID:       id,
			SourceTable:    "relationship_memory",
			SourceLabel:    relationshipSourceLabel(entityName, entityID),
			SourceCategory: "relationship",
			Score:          trustScore,
			AgeDays:        ageDays,
			AuthorityScore: trustScore,
			RetrievalMode:  mode.String(),
		})
		if len(evidence) > before {
			likeEmitted++
		}
	}

	if query != "" && mode != RetrievalRecency && likeEmitted == 0 {
		// Final browse fallback when both hybrid and filtered-LIKE found
		// nothing for this entity. Mirrors the pre-M3.2 behaviour so we
		// never silently lose relationship context for searches that
		// don't lexically match.
		rows, err = mr.store.QueryContext(ctx,
			`SELECT id, entity_id, entity_name, trust_score, interaction_summary, interaction_count, last_interaction,
			        julianday('now') - julianday(COALESCE(updated_at, created_at)) AS age_days
			 FROM relationship_memory
			 ORDER BY interaction_count DESC, COALESCE(updated_at, created_at) DESC LIMIT 20`)
		if err != nil {
			if isHybridMode && isSearch {
				annotateRetrievalPath(ctx, RetrievalTierRelationship, RetrievalPathEmpty)
			}
			return evidence
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var id, entityID, entityName string
			var trustScore float64
			var interactionSummary sql.NullString
			var interactionCount int
			var lastInteraction *string
			var ageDays float64
			if rows.Scan(&id, &entityID, &entityName, &trustScore, &interactionSummary, &interactionCount, &lastInteraction, &ageDays) != nil {
				continue
			}
			if _, dup := seenRel[id]; dup {
				continue
			}
			line := fmt.Sprintf("%s: trust=%.1f, interactions=%d", entityName, trustScore, interactionCount)
			if interactionSummary.Valid && interactionSummary.String != "" {
				line += ", relation=" + interactionSummary.String
			}
			if lastInteraction != nil {
				line += ", last=" + *lastInteraction
			}
			evidence = appendEvidence(evidence, Evidence{
				Content:        line,
				SourceTier:     TierRelationship,
				SourceID:       id,
				SourceTable:    "relationship_memory",
				SourceLabel:    relationshipSourceLabel(entityName, entityID),
				SourceCategory: "relationship",
				Score:          trustScore,
				AgeDays:        ageDays,
				AuthorityScore: trustScore,
				RetrievalMode:  mode.String(),
			})
		}
	}

	if isHybridMode && isSearch {
		switch {
		case likeAttempted && likeEmitted > 0:
			annotateRetrievalPath(ctx, RetrievalTierRelationship, RetrievalPathLikeFallback)
		default:
			annotateRetrievalPath(ctx, RetrievalTierRelationship, RetrievalPathEmpty)
		}
	}
	return evidence
}

func (mr *Retriever) retrieveKnowledgeFactEvidence(ctx context.Context, query string, mode RetrievalMode, budgetTokens int) []Evidence {
	maxChars := budgetTokens * mr.charsPerToken
	used := 0
	appendEvidence := func(dst []Evidence, ev Evidence) []Evidence {
		if ev.Content == "" {
			return dst
		}
		if used+len(ev.Content) > maxChars {
			return dst
		}
		used += len(ev.Content)
		return append(dst, ev)
	}

	rows, err := mr.store.QueryContext(ctx,
		`SELECT id, subject, relation, object, confidence,
		        julianday('now') - julianday(updated_at) AS age_days
		 FROM knowledge_facts
		 ORDER BY updated_at DESC, confidence DESC LIMIT 200`)
	if err != nil {
		return nil
	}
	defer func() { _ = rows.Close() }()

	var facts []graphFactRow
	for rows.Next() {
		var fact graphFactRow
		if rows.Scan(&fact.ID, &fact.Subject, &fact.Relation, &fact.Object, &fact.Confidence, &fact.AgeDays) != nil {
			continue
		}
		facts = append(facts, fact)
	}

	ordered := rankKnowledgeFactsForQuery(facts, query, mode)
	var evidence []Evidence
	for _, ev := range synthesizeGraphTraversalEvidence(facts, query, mode) {
		evidence = appendEvidence(evidence, ev)
	}
	for _, fact := range ordered {
		evidence = appendEvidence(evidence, Evidence{
			Content:        fmt.Sprintf("%s %s %s", fact.Subject, fact.Relation, fact.Object),
			SourceTier:     TierRelationship,
			SourceID:       fact.ID,
			SourceTable:    "knowledge_facts",
			SourceLabel:    knowledgeFactSourceLabel(fact.Subject, fact.Relation, fact.Object),
			SourceCategory: "graph",
			Score:          fact.Confidence,
			AgeDays:        fact.AgeDays,
			AuthorityScore: fact.Confidence,
			RetrievalMode:  mode.String(),
		})
	}
	return evidence
}

func rankKnowledgeFactsForQuery(facts []graphFactRow, query string, mode RetrievalMode) []graphFactRow {
	if len(facts) == 0 {
		return nil
	}

	if query == "" || mode == RetrievalRecency {
		ordered := append([]graphFactRow(nil), facts...)
		sort.SliceStable(ordered, func(i, j int) bool {
			if ordered[i].Confidence == ordered[j].Confidence {
				return ordered[i].AgeDays < ordered[j].AgeDays
			}
			return ordered[i].Confidence > ordered[j].Confidence
		})
		if len(ordered) > 20 {
			ordered = ordered[:20]
		}
		return ordered
	}

	tokens := graphQueryTokens(query)
	if len(tokens) == 0 {
		return rankKnowledgeFactsForQuery(facts, "", mode)
	}

	type scoredFact struct {
		fact         graphFactRow
		seedScore    float64
		connected    bool
		connectScore float64
	}

	scored := make([]scoredFact, 0, len(facts))
	seedEntities := make(map[string]struct{})
	for _, fact := range facts {
		subject := strings.ToLower(fact.Subject)
		relation := strings.ToLower(fact.Relation)
		object := strings.ToLower(fact.Object)

		score := 0.0
		for _, token := range tokens {
			if strings.Contains(subject, token) || strings.Contains(object, token) {
				score += 2.0
			}
			if strings.Contains(relation, token) {
				score += 1.0
			}
		}
		if score > 0 {
			scored = append(scored, scoredFact{fact: fact, seedScore: score})
			seedEntities[strings.ToLower(fact.Subject)] = struct{}{}
			seedEntities[strings.ToLower(fact.Object)] = struct{}{}
		}
	}

	if len(scored) == 0 {
		return rankKnowledgeFactsForQuery(facts, "", mode)
	}

	if mode == RetrievalGraph {
		for _, fact := range facts {
			subject := strings.ToLower(fact.Subject)
			object := strings.ToLower(fact.Object)
			_, subjectHit := seedEntities[subject]
			_, objectHit := seedEntities[object]
			if !subjectHit && !objectHit {
				continue
			}

			alreadySeed := false
			for _, candidate := range scored {
				if candidate.fact.ID == fact.ID {
					alreadySeed = true
					break
				}
			}
			if alreadySeed {
				continue
			}

			connectScore := 1.0
			if subjectHit && objectHit {
				connectScore = 2.0
			}
			scored = append(scored, scoredFact{
				fact:         fact,
				connected:    true,
				connectScore: connectScore,
			})
		}
	}

	sort.SliceStable(scored, func(i, j int) bool {
		left := scored[i]
		right := scored[j]

		leftScore := left.seedScore*10 + left.connectScore*5 + left.fact.Confidence - left.fact.AgeDays/365
		rightScore := right.seedScore*10 + right.connectScore*5 + right.fact.Confidence - right.fact.AgeDays/365
		if leftScore == rightScore {
			return left.fact.ID < right.fact.ID
		}
		return leftScore > rightScore
	})

	ordered := make([]graphFactRow, 0, minInt(len(scored), 20))
	for _, item := range scored {
		ordered = append(ordered, item.fact)
		if len(ordered) == 20 {
			break
		}
	}
	return ordered
}

func synthesizeGraphTraversalEvidence(facts []graphFactRow, query string, mode RetrievalMode) []Evidence {
	if len(facts) == 0 || mode != RetrievalGraph || query == "" {
		return nil
	}

	intent := detectGraphTraversalIntent(query)
	matchedEntities := graphMatchedEntities(query, facts)
	if len(matchedEntities) == 0 {
		return nil
	}

	switch intent {
	case graphTraversalPath:
		if len(matchedEntities) < 2 {
			return nil
		}
		return buildGraphPathEvidence(facts, matchedEntities[0], matchedEntities[1])
	case graphTraversalImpact:
		return buildGraphExpansionEvidence(facts, matchedEntities, true)
	case graphTraversalExpand:
		return buildGraphExpansionEvidence(facts, matchedEntities, false)
	default:
		return nil
	}
}

func detectGraphTraversalIntent(query string) graphTraversalIntent {
	lower := strings.ToLower(query)
	switch {
	case containsAny(lower, "path", "chain", "connect", "connection", "between", "through", "via"):
		return graphTraversalPath
	case containsAny(lower, "impact", "impacted", "affected", "blast radius", "what breaks", "breaks if"):
		return graphTraversalImpact
	case containsAny(lower, "depends on", "dependency", "dependencies", "upstream", "downstream", "blocked by", "blocks", "uses", "owner", "owned by"):
		return graphTraversalExpand
	default:
		return graphTraversalDirect
	}
}

func graphMatchedEntities(query string, facts []graphFactRow) []string {
	lower := strings.ToLower(query)
	seen := make(map[string]struct{})
	var entities []string
	for _, fact := range facts {
		for _, entity := range []string{fact.Subject, fact.Object} {
			if entity == "" {
				continue
			}
			entityLower := strings.ToLower(entity)
			if !strings.Contains(lower, entityLower) {
				continue
			}
			if _, ok := seen[entityLower]; ok {
				continue
			}
			seen[entityLower] = struct{}{}
			entities = append(entities, entity)
		}
	}
	return entities
}

// retrievalGraphMaxPathDepth caps path search at six hops. Longer paths are
// almost always noise for retrieval purposes and the bound keeps BFS cost
// tight.
const retrievalGraphMaxPathDepth = 6

// retrievalGraphMaxExpansionDepth keeps the impact/dependency chains
// readable. The KnowledgeGraph API supports deeper walks; callers that want
// them should use the API directly.
const retrievalGraphMaxExpansionDepth = 2

// retrievalGraphMaxEvidence caps the number of graph evidence items returned
// from a single retrieval pass.
const retrievalGraphMaxEvidence = 3

func buildGraphPathEvidence(facts []graphFactRow, start, goal string) []Evidence {
	// Path evidence only travels canonical directed-dependency edges
	// (depends_on / uses / blocks / blocked_by / causes / caused_by /
	// version_of / owned_by). The production ingestion path
	// (Manager.extractKnowledgeFacts) writes only those relations, so the
	// canonical set is a superset of anything that can land in the table.
	// Non-canonical rows, if they ever appear, are not treated as valid
	// dependency edges and the path search correctly returns nil rather
	// than fabricating a connection through an unrelated relation.
	graph := NewKnowledgeGraph(facts)
	edges := graph.ShortestPath(start, goal, retrievalGraphMaxPathDepth)
	if len(edges) == 0 {
		return nil
	}
	return []Evidence{graphPathEvidence(start, goal, edges)}
}

func buildGraphExpansionEvidence(facts []graphFactRow, seeds []string, reverse bool) []Evidence {
	if len(seeds) == 0 {
		return nil
	}
	graph := NewKnowledgeGraph(facts)
	var evidence []Evidence
	for _, seed := range seeds {
		var paths []GraphPath
		if reverse {
			paths = graph.Impact(seed, retrievalGraphMaxExpansionDepth)
		} else {
			paths = graph.Dependencies(seed, retrievalGraphMaxExpansionDepth)
		}
		for _, path := range paths {
			if len(path.Edges) == 0 {
				continue
			}
			evidence = append(evidence, graphChainEvidence(seeds[0], path.Edges, reverse))
			if len(evidence) >= retrievalGraphMaxEvidence {
				return evidence
			}
		}
	}
	return evidence
}

func graphPathEvidence(start, goal string, edges []graphEdge) Evidence {
	if len(edges) == 0 {
		return Evidence{}
	}
	parts := []string{start}
	score := 0.0
	ageDays := 0.0
	var ids []string
	for _, edge := range edges {
		parts = append(parts, fmt.Sprintf("--%s--> %s", edge.Fact.Relation, edge.To))
		score += edge.Fact.Confidence
		if edge.Fact.AgeDays > ageDays {
			ageDays = edge.Fact.AgeDays
		}
		ids = append(ids, edge.Fact.ID)
	}
	return Evidence{
		Content:        fmt.Sprintf("Path between %s and %s: %s", start, goal, strings.Join(parts, " ")),
		SourceTier:     TierRelationship,
		SourceID:       strings.Join(ids, ","),
		SourceTable:    "knowledge_facts",
		SourceLabel:    fmt.Sprintf("%s->%s", start, goal),
		SourceCategory: "graph_path",
		Score:          score / float64(len(edges)),
		AgeDays:        ageDays,
		AuthorityScore: score / float64(len(edges)),
		RetrievalMode:  RetrievalGraph.String(),
	}
}

func graphChainEvidence(seed string, edges []graphEdge, reverse bool) Evidence {
	if len(edges) == 0 {
		return Evidence{}
	}
	label := "Dependency chain"
	if reverse {
		label = "Impact chain"
	}
	parts := []string{seed}
	score := 0.0
	ageDays := 0.0
	var ids []string
	for _, edge := range edges {
		parts = append(parts, fmt.Sprintf("--%s--> %s", edge.Fact.Relation, edge.To))
		score += edge.Fact.Confidence
		if edge.Fact.AgeDays > ageDays {
			ageDays = edge.Fact.AgeDays
		}
		ids = append(ids, edge.Fact.ID)
	}
	return Evidence{
		Content:        fmt.Sprintf("%s from %s: %s", label, seed, strings.Join(parts, " ")),
		SourceTier:     TierRelationship,
		SourceID:       strings.Join(ids, ","),
		SourceTable:    "knowledge_facts",
		SourceLabel:    seed,
		SourceCategory: "graph_chain",
		Score:          score / float64(len(edges)),
		AgeDays:        ageDays,
		AuthorityScore: score / float64(len(edges)),
		RetrievalMode:  RetrievalGraph.String(),
	}
}

func graphQueryTokens(query string) []string {
	stopwords := map[string]struct{}{
		"the": {}, "and": {}, "for": {}, "with": {}, "that": {}, "this": {},
		"what": {}, "which": {}, "who": {}, "does": {}, "did": {}, "from": {},
		"have": {}, "has": {}, "into": {}, "than": {}, "when": {}, "where": {},
		"why": {}, "how": {}, "again": {}, "keep": {}, "give": {}, "latest": {},
		"current": {}, "plan": {}, "debug": {}, "issue": {}, "error": {},
	}

	normalized := strings.ToLower(query)
	replacer := strings.NewReplacer("?", " ", ".", " ", ",", " ", ":", " ", ";", " ", "/", " ", "-", " ")
	normalized = replacer.Replace(normalized)
	fields := strings.Fields(normalized)

	seen := make(map[string]struct{}, len(fields))
	tokens := make([]string, 0, len(fields))
	for _, field := range fields {
		if len(field) < 3 {
			continue
		}
		if _, stop := stopwords[field]; stop {
			continue
		}
		if _, exists := seen[field]; exists {
			continue
		}
		seen[field] = struct{}{}
		tokens = append(tokens, field)
	}
	return tokens
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
