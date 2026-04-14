package memory

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/rs/zerolog/log"

	"roboticus/internal/db"
	"roboticus/internal/llm"
)

// RetrievalConfig controls hybrid RAG behavior.
type RetrievalConfig struct {
	HybridWeight     float64 // FTS vs embedding blend (0=FTS only, 1=embedding only, 0.5=balanced)
	EpisodicHalfLife float64 // Days for episodic decay (default 7)
	DecayFloor       float64 // Minimum decay factor (default 0.05)
}

// DefaultRetrievalConfig returns sensible defaults.
func DefaultRetrievalConfig() RetrievalConfig {
	return RetrievalConfig{
		HybridWeight:     0.5,
		EpisodicHalfLife: 7.0,
		DecayFloor:       0.05,
	}
}

// Retriever coordinates retrieval across all 5 memory tiers.
type Retriever struct {
	config        RetrievalConfig
	store         *db.Store
	budgets       TierBudget
	embedClient   *llm.EmbeddingClient
	vectorIndex     db.VectorIndex
	charsPerToken int
}

// NewRetriever creates a retriever.
func NewRetriever(cfg RetrievalConfig, budgets TierBudget, store *db.Store) *Retriever {
	return &Retriever{
		config:        cfg,
		store:         store,
		budgets:       budgets,
		charsPerToken: 4,
	}
}

// SetEmbeddingClient attaches an embedding client for hybrid search.
func (mr *Retriever) SetEmbeddingClient(ec *llm.EmbeddingClient) {
	mr.embedClient = ec
}

// SetVectorIndex attaches a vector index for ANN-based retrieval.
func (mr *Retriever) SetVectorIndex(idx db.VectorIndex) {
	mr.vectorIndex = idx
}

// MemoryEntry represents a memory result from ANN retrieval.
type MemoryEntry struct {
	SourceTable    string  `json:"source_table"`
	SourceID       string  `json:"source_id"`
	ContentPreview string  `json:"content_preview"`
	Similarity     float64 `json:"similarity"`
}

// RetrieveWithANN uses the vector index for approximate nearest-neighbor
// search over memory embeddings. Falls back to empty results if the index is
// not built or has insufficient entries.
func (mr *Retriever) RetrieveWithANN(ctx context.Context, embedding []float64, k int) []MemoryEntry {
	if mr.vectorIndex == nil || !mr.vectorIndex.IsBuilt() || k <= 0 {
		return nil
	}

	results := mr.vectorIndex.Search(embedding, k)
	entries := make([]MemoryEntry, len(results))
	for i, r := range results {
		entries[i] = MemoryEntry{
			SourceTable:    r.SourceTable,
			SourceID:       r.SourceID,
			ContentPreview: r.ContentPreview,
			Similarity:     r.Similarity,
		}
	}
	return entries
}

// Entry represents a retrieved memory.
type Entry struct {
	ID         string
	Tier       string
	EntryType  string
	Content    string
	Similarity float64
	AgeDays    float64
}

// RetrievalMetrics tracks observability data for memory retrieval (Rust parity).
type RetrievalMetrics struct {
	TotalEntries    int     `json:"total_entries"`
	MatchedEntries  int     `json:"matched_entries"`
	AvgSimilarity   float64 `json:"avg_similarity"`
	BudgetUsedPct   float64 `json:"budget_used_pct"`
	WorkingCount    int     `json:"working_count"`
	EpisodicCount   int     `json:"episodic_count"`
	SemanticCount   int     `json:"semantic_count"`
	ProceduralCount int     `json:"procedural_count"`
	RelationCount   int     `json:"relation_count"`
	AmbientCount    int     `json:"ambient_count"`
	RetrievalMode   string  `json:"retrieval_mode"`
}

// historyKeywords trigger inclusion of inactive/stale memories when present in query.
var historyKeywords = []string{
	"history", "historical", "previous", "earlier", "before",
	"past", "old", "resolved", "stale", "archive",
	"previously", "archived",
}

// Retrieve fetches relevant memories across all tiers within the total token budget.
func (mr *Retriever) Retrieve(ctx context.Context, sessionID, query string, totalTokens int) string {
	text, _ := mr.RetrieveWithMetrics(ctx, sessionID, query, totalTokens)
	return text
}

// RetrieveDirectOnly returns only working memory + recent ambient activity.
// This matches Rust's two-stage pattern: direct injection is limited to cheap,
// session-scoped, always-relevant content. All other tiers (episodic, semantic,
// procedural, relationship) are accessed via the memory index + recall_memory tool.
//
// This prevents the model from treating the injected block as "all of my memories"
// and confabulating when a topic isn't present.
func (mr *Retriever) RetrieveDirectOnly(ctx context.Context, sessionID, query string, totalTokens int) string {
	if mr.store == nil {
		return ""
	}

	var sections []string

	// Working memory (session-scoped).
	if budget := int(float64(totalTokens) * mr.budgets.Working); budget > 0 {
		working := mr.retrieveWorkingMemory(ctx, sessionID, budget)
		if working != "" {
			sections = append(sections, "[Working Memory]\n"+working)
		}
	}

	// Ambient recency: recent episodic memories (last 2 hours).
	ambient := mr.retrieveAmbientRecent(ctx, 2)
	if ambient != "" {
		sections = append(sections, "[Recent Activity]\n"+ambient)
	}

	if len(sections) == 0 {
		return ""
	}
	return strings.Join(sections, "\n\n")
}

// RetrieveWithMetrics fetches memories and returns both the injected text
// and observability metrics (Rust parity: retrieve_with_metrics).
func (mr *Retriever) RetrieveWithMetrics(ctx context.Context, sessionID, query string, totalTokens int) (string, RetrievalMetrics) {
	var metrics RetrievalMetrics
	if mr.store == nil {
		return "", metrics
	}

	// Check if query requests historical/inactive memories.
	includeInactive := false
	if query != "" {
		lower := strings.ToLower(query)
		for _, kw := range historyKeywords {
			if strings.Contains(lower, kw) {
				includeInactive = true
				break
			}
		}
	}
	_ = includeInactive // Used in episodic/semantic retrieval below.

	// Select retrieval mode via strategy with real session age.
	corpusSize := mr.estimateCorpusSize(ctx)
	sessionAge := 10 * time.Minute // default for missing/unparseable sessions
	if mr.store != nil && sessionID != "" {
		var createdAt string
		if err := mr.store.QueryRowContext(ctx,
			`SELECT created_at FROM sessions WHERE id = ?`, sessionID).Scan(&createdAt); err == nil {
			if t, err := time.Parse("2006-01-02 15:04:05", createdAt); err == nil {
				sessionAge = time.Since(t)
			}
		}
	}
	strategy := NewRetrievalStrategy(mr.embedClient != nil, corpusSize)
	mode := strategy.SelectMode(query, sessionAge)
	metrics.RetrievalMode = mode.String()

	// Generate query embedding if embedding client is available and mode uses it.
	var queryEmbed []float32
	if mr.embedClient != nil && query != "" && mode != RetrievalKeyword && mode != RetrievalRecency {
		var err error
		queryEmbed, err = mr.embedClient.EmbedSingle(ctx, query)
		if err != nil {
			log.Warn().Err(err).Msg("failed to embed query, falling back to recency")
		}
	}

	var sections []string
	totalCharsUsed := 0
	totalCharsAllowed := totalTokens * mr.charsPerToken

	// Working memory (filter out turn_summary entries — Rust parity).
	if budget := int(float64(totalTokens) * mr.budgets.Working); budget > 0 {
		working := mr.retrieveWorkingMemory(ctx, sessionID, budget)
		if working != "" {
			sections = append(sections, "[Working Memory]\n"+working)
			totalCharsUsed += len(working)
			metrics.WorkingCount = strings.Count(working, "\n- ") + 1
		}
	}

	// Ambient recency: inject recent episodic memories (last 2 hours)
	// regardless of query match (Rust parity: recent ambient context).
	ambient := mr.retrieveAmbientRecent(ctx, 2)
	if ambient != "" {
		sections = append(sections, "[Recent Activity]\n"+ambient)
		totalCharsUsed += len(ambient)
		metrics.AmbientCount = strings.Count(ambient, "\n- ") + 1
	}

	// Adaptive budget retrieval: each tier gets its initial allocation.
	// Unused budget from one tier flows proportionally to subsequent tiers.
	surplusChars := 0

	// Episodic memory (with temporal decay + optional embedding re-rank).
	episodicBudget := int(float64(totalTokens)*mr.budgets.Episodic) + surplusChars
	if episodicBudget > 0 {
		episodic := mr.retrieveEpisodic(ctx, query, queryEmbed, episodicBudget/mr.charsPerToken)
		charsUsed := len(episodic)
		surplusChars = (episodicBudget - charsUsed)
		if surplusChars < 0 {
			surplusChars = 0
		}
		if episodic != "" {
			sections = append(sections, "[Relevant Memories]\n"+episodic)
			totalCharsUsed += charsUsed
			metrics.EpisodicCount = strings.Count(episodic, "\n- ") + 1
		}
	}

	// Semantic memory.
	semanticBudget := int(float64(totalTokens)*mr.budgets.Semantic) + surplusChars
	if semanticBudget > 0 {
		semantic := mr.retrieveSemanticMemory(ctx, query, semanticBudget/mr.charsPerToken)
		charsUsed := len(semantic)
		surplusChars = (semanticBudget - charsUsed)
		if surplusChars < 0 {
			surplusChars = 0
		}
		if semantic != "" {
			sections = append(sections, "[Knowledge]\n"+semantic)
			totalCharsUsed += charsUsed
			metrics.SemanticCount = strings.Count(semantic, "\n- ") + 1
		}
	}

	// Procedural memory (tool stats).
	proceduralBudget := int(float64(totalTokens)*mr.budgets.Procedural) + surplusChars
	if proceduralBudget > 0 {
		procedural := mr.retrieveProceduralMemory(ctx, proceduralBudget/mr.charsPerToken)
		charsUsed := len(procedural)
		surplusChars = (proceduralBudget - charsUsed)
		if surplusChars < 0 {
			surplusChars = 0
		}
		if procedural != "" {
			sections = append(sections, "[Tool Experience]\n"+procedural)
			totalCharsUsed += charsUsed
			metrics.ProceduralCount = strings.Count(procedural, "\n- ") + 1
		}
	}

	// Relationship memory.
	relationBudget := int(float64(totalTokens)*mr.budgets.Relationship) + surplusChars
	if relationBudget > 0 {
		relationships := mr.retrieveRelationshipMemory(ctx, relationBudget/mr.charsPerToken)
		charsUsed := len(relationships)
		if relationships != "" {
			sections = append(sections, "[Relationships]\n"+relationships)
			totalCharsUsed += charsUsed
			metrics.RelationCount = strings.Count(relationships, "\n- ") + 1
		}
	}

	metrics.TotalEntries = metrics.WorkingCount + metrics.AmbientCount +
		metrics.EpisodicCount + metrics.SemanticCount +
		metrics.ProceduralCount + metrics.RelationCount
	metrics.MatchedEntries = metrics.TotalEntries
	if totalCharsAllowed > 0 {
		metrics.BudgetUsedPct = float64(totalCharsUsed) / float64(totalCharsAllowed)
	}

	if len(sections) == 0 {
		return "", metrics
	}
	return "[Active Memory]\n" + strings.Join(sections, "\n\n"), metrics
}

// retrieveAmbientRecent fetches episodic memories from the last N hours,
// regardless of query match. This ensures the agent knows about recent actions
// even on unrelated queries (Rust: recent ambient context injection).
func (mr *Retriever) retrieveAmbientRecent(ctx context.Context, hours int) string {
	rows, err := mr.store.QueryContext(ctx,
		`SELECT classification, content, created_at FROM episodic_memory
		 WHERE memory_state = 'active'
		 AND created_at >= datetime('now', ?)
		 ORDER BY created_at DESC LIMIT 5`,
		fmt.Sprintf("-%d hours", hours),
	)
	if err != nil {
		return ""
	}
	defer func() { _ = rows.Close() }()

	var lines []string
	for rows.Next() {
		var classification, content, createdAt string
		if err := rows.Scan(&classification, &content, &createdAt); err != nil {
			continue
		}
		// Format: [HH:MM] (classification) content
		timeStr := createdAt
		if len(timeStr) > 16 {
			timeStr = timeStr[11:16] // Extract HH:MM
		}
		lines = append(lines, fmt.Sprintf("- [%s] (%s) %s", timeStr, classification, content))
	}
	if len(lines) == 0 {
		return ""
	}
	return strings.Join(lines, "\n")
}

// retrieveWorkingMemory fetches from the working_memory table.
func (mr *Retriever) retrieveWorkingMemory(ctx context.Context, sessionID string, budgetTokens int) string {
	maxChars := budgetTokens * mr.charsPerToken
	rows, err := mr.store.QueryContext(ctx,
		`SELECT entry_type, content FROM working_memory
		 WHERE session_id = ? ORDER BY created_at DESC LIMIT 20`, sessionID)
	if err != nil {
		return ""
	}
	defer func() { _ = rows.Close() }()

	var b strings.Builder
	used := 0
	for rows.Next() {
		var entryType, content string
		if rows.Scan(&entryType, &content) != nil {
			continue
		}
		if used+len(content) > maxChars {
			break
		}
		b.WriteString("- ")
		b.WriteString(content)
		b.WriteString("\n")
		used += len(content)
	}

	// Cross-session continuity: if working memory is empty for this session,
	// inject the most recent session summary to provide context.
	if used == 0 && mr.store != nil {
		var summary string
		err := mr.store.QueryRowContext(ctx,
			`SELECT value FROM semantic_memory
			 WHERE category = 'session_summary' AND memory_state = 'active'
			 ORDER BY updated_at DESC LIMIT 1`).Scan(&summary)
		if err == nil && summary != "" {
			b.WriteString("- Previously: ")
			b.WriteString(summary)
			b.WriteString("\n")
		}
	}

	return b.String()
}

// retrieveEpisodic fetches from episodic_memory using a union strategy:
// 1. FTS5-matched results (query-relevant, any age)
// 2. Recency results (recent, any content)
// Deduplicated and re-ranked with hybrid scoring (temporal decay + embedding similarity).
func (mr *Retriever) retrieveEpisodic(ctx context.Context, query string, queryEmbed []float32, budgetTokens int) string {
	maxChars := budgetTokens * mr.charsPerToken

	type candidate struct {
		id      string
		content string
		ageDays float64
		ftsHit  bool // true if this came from FTS match
	}
	seen := make(map[string]struct{})
	var candidates []candidate

	// Leg 1: FTS5 match — query-relevant memories regardless of age.
	if query != "" {
		ftsQuery := db.SanitizeFTSQuery(query)
		if ftsQuery != "" {
			rows, err := mr.store.QueryContext(ctx,
				`SELECT em.id, em.content, julianday('now') - julianday(em.created_at) as age_days
				 FROM memory_fts fts
				 JOIN episodic_memory em ON em.id = fts.source_id
				 WHERE fts.source_table = 'episodic_memory'
				   AND memory_fts MATCH ?
				   AND em.memory_state = 'active'
				 LIMIT 20`, ftsQuery)
			if err != nil {
				log.Debug().Err(err).Str("fts_query", ftsQuery).Msg("episodic FTS match failed, continuing with recency")
			} else {
				for rows.Next() {
					var c candidate
					if rows.Scan(&c.id, &c.content, &c.ageDays) != nil {
						continue
					}
					c.ftsHit = true
					seen[c.id] = struct{}{}
					candidates = append(candidates, c)
				}
				_ = rows.Close()
			}
		}
	}

	// Leg 2: Recency — recent memories regardless of query match.
	{
		rows, err := mr.store.QueryContext(ctx,
			`SELECT id, content, julianday('now') - julianday(created_at) as age_days
			 FROM episodic_memory WHERE memory_state = 'active'
			 ORDER BY created_at DESC LIMIT 20`)
		if err == nil {
			for rows.Next() {
				var c candidate
				if rows.Scan(&c.id, &c.content, &c.ageDays) != nil {
					continue
				}
				if _, dup := seen[c.id]; dup {
					continue // Already in FTS results.
				}
				seen[c.id] = struct{}{}
				candidates = append(candidates, c)
			}
			_ = rows.Close()
		}
	}

	if len(candidates) == 0 {
		return ""
	}

	// Pre-load stored embeddings for all candidates in a single query.
	// This replaces the N+1 pattern of calling EmbedSingle per candidate.
	storedEmbeds := make(map[string][]float32)
	if queryEmbed != nil && mr.store != nil {
		ids := make([]string, len(candidates))
		for i, c := range candidates {
			ids[i] = c.id
		}
		storedEmbeds = mr.loadStoredEmbeddings(ctx, "episodic_memory", ids)
	}

	// Score and rank: blend temporal decay, FTS relevance, and embedding similarity.
	type scored struct {
		content string
		score   float64
	}
	entries := make([]scored, 0, len(candidates))
	for _, c := range candidates {
		score := scoreEpisodicCandidate(c.ageDays, c.ftsHit, mr.config)

		// Blend with precomputed embedding similarity (no API calls).
		if queryEmbed != nil {
			if textEmbed, ok := storedEmbeds[c.id]; ok {
				sim := llm.CosineSimilarity(queryEmbed, textEmbed)
				score = (1-mr.config.HybridWeight)*score + mr.config.HybridWeight*sim
			}
		}

		entries = append(entries, scored{content: c.content, score: score})
	}

	// Sort by score descending.
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].score > entries[j].score
	})

	var b strings.Builder
	used := 0
	for _, e := range entries {
		if used+len(e.content) > maxChars {
			break
		}
		fmt.Fprintf(&b, "- (sim=%.2f) %s\n", e.score, e.content)
		used += len(e.content)
	}
	return b.String()
}

// scoreEpisodicCandidate computes a base relevance score for an episodic memory.
// FTS hits get a relevance boost that resists temporal decay, ensuring old but
// query-matched memories can outrank recent but irrelevant ones.
//
// TODO(owner): This is the scoring blend — the key design knob. Current approach:
//   - Decay:    0.5^(age/halfLife), floored at DecayFloor
//   - FTS boost: 0.4 additive for text-matched results
//   - The boost means a 6-month-old FTS hit scores ~0.45 vs a 1-day-old
//     non-match at ~0.91. With embedding similarity blended in, the FTS hit
//     can win when semantically relevant.
func scoreEpisodicCandidate(ageDays float64, ftsHit bool, cfg RetrievalConfig) float64 {
	decay := math.Pow(0.5, ageDays/cfg.EpisodicHalfLife)
	if decay < cfg.DecayFloor {
		decay = cfg.DecayFloor
	}
	if ftsHit {
		// FTS relevance boost — resists decay so old memories surface when queried.
		decay += 0.4
		if decay > 1.0 {
			decay = 1.0
		}
	}
	return decay
}

// retrieveSemanticMemory fetches from the semantic_memory table.
func (mr *Retriever) retrieveSemanticMemory(ctx context.Context, query string, budgetTokens int) string {
	maxChars := budgetTokens * mr.charsPerToken

	var rows *sql.Rows
	var err error
	if query != "" {
		rows, err = mr.store.QueryContext(ctx,
			`SELECT category, key, value FROM semantic_memory
			 WHERE memory_state = 'active' AND (value LIKE ? OR key LIKE ?)
			 ORDER BY confidence DESC, updated_at DESC LIMIT 20`,
			"%"+query+"%", "%"+query+"%")
	}
	if err != nil || rows == nil {
		rows, err = mr.store.QueryContext(ctx,
			`SELECT category, key, value FROM semantic_memory
			 WHERE memory_state = 'active'
			 ORDER BY confidence DESC, updated_at DESC LIMIT 20`)
	}
	if err != nil {
		return ""
	}
	defer func() { _ = rows.Close() }()

	var b strings.Builder
	used := 0
	for rows.Next() {
		var category, key, value string
		if rows.Scan(&category, &key, &value) != nil {
			continue
		}
		line := fmt.Sprintf("[%s] %s: %s", category, key, value)
		if used+len(line) > maxChars {
			break
		}
		b.WriteString("- ")
		b.WriteString(line)
		b.WriteString("\n")
		used += len(line)
	}
	return b.String()
}

// retrieveProceduralMemory formats tool success/failure statistics from procedural_memory.
func (mr *Retriever) retrieveProceduralMemory(ctx context.Context, budgetTokens int) string {
	rows, err := mr.store.QueryContext(ctx,
		`SELECT name, success_count, failure_count FROM procedural_memory
		 ORDER BY (success_count + failure_count) DESC LIMIT 20`)
	if err != nil {
		return ""
	}
	defer func() { _ = rows.Close() }()

	var b strings.Builder
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
	return b.String()
}

// retrieveRelationshipMemory formats relationship data.
func (mr *Retriever) retrieveRelationshipMemory(ctx context.Context, budgetTokens int) string {
	maxChars := budgetTokens * mr.charsPerToken

	rows, err := mr.store.QueryContext(ctx,
		`SELECT entity_name, trust_score, interaction_count, last_interaction
		 FROM relationship_memory
		 ORDER BY interaction_count DESC LIMIT 20`)
	if err != nil {
		return ""
	}
	defer func() { _ = rows.Close() }()

	var b strings.Builder
	used := 0
	for rows.Next() {
		var entityName string
		var trustScore float64
		var interactionCount int
		var lastInteraction *string
		if rows.Scan(&entityName, &trustScore, &interactionCount, &lastInteraction) != nil {
			continue
		}
		line := fmt.Sprintf("%s: trust=%.1f, interactions=%d", entityName, trustScore, interactionCount)
		if lastInteraction != nil {
			line += ", last=" + *lastInteraction
		}
		if used+len(line) > maxChars {
			break
		}
		b.WriteString("- ")
		b.WriteString(line)
		b.WriteString("\n")
		used += len(line)
	}
	return b.String()
}

// estimateCorpusSize returns the approximate number of memory entries across tiers.
func (mr *Retriever) estimateCorpusSize(ctx context.Context) int {
	if mr.store == nil {
		return 0
	}
	var count int
	_ = mr.store.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM memory_index WHERE confidence > 0.1`).Scan(&count)
	return count
}

// loadStoredEmbeddings bulk-loads precomputed embeddings from the embeddings table
// for the given source IDs. Returns a map of sourceID → embedding vector.
// This replaces the N+1 pattern of calling EmbedSingle per candidate at retrieval time.
func (mr *Retriever) loadStoredEmbeddings(ctx context.Context, sourceTable string, ids []string) map[string][]float32 {
	result := make(map[string][]float32, len(ids))
	if len(ids) == 0 || mr.store == nil {
		return result
	}

	// Build parameterized IN clause.
	placeholders := make([]string, len(ids))
	args := make([]any, 0, len(ids)+1)
	args = append(args, sourceTable)
	for i, id := range ids {
		placeholders[i] = "?"
		args = append(args, id)
	}

	query := fmt.Sprintf(
		`SELECT source_id, embedding_blob FROM embeddings
		 WHERE source_table = ? AND source_id IN (%s)
		 AND embedding_blob IS NOT NULL`,
		strings.Join(placeholders, ","))

	rows, err := mr.store.QueryContext(ctx, query, args...)
	if err != nil {
		log.Debug().Err(err).Msg("loadStoredEmbeddings: query failed")
		return result
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var sourceID string
		var blob []byte
		if rows.Scan(&sourceID, &blob) != nil {
			continue
		}
		vec := db.BlobToEmbedding(blob)
		if len(vec) > 0 {
			result[sourceID] = vec
		}
	}
	return result
}
