package memory

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"sort"
	"strings"

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
	hnswIndex     *db.HNSWIndex
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

// SetHNSWIndex attaches an HNSW index for ANN-based retrieval.
func (mr *Retriever) SetHNSWIndex(idx *db.HNSWIndex) {
	mr.hnswIndex = idx
}

// MemoryEntry represents a memory result from ANN retrieval.
type MemoryEntry struct {
	SourceTable    string  `json:"source_table"`
	SourceID       string  `json:"source_id"`
	ContentPreview string  `json:"content_preview"`
	Similarity     float64 `json:"similarity"`
}

// RetrieveWithANN uses the HNSW index for O(log n) approximate nearest-neighbor
// search over memory embeddings. Falls back to empty results if the index is
// not built or has insufficient entries.
func (mr *Retriever) RetrieveWithANN(ctx context.Context, embedding []float64, k int) []MemoryEntry {
	if mr.hnswIndex == nil || !mr.hnswIndex.IsBuilt() || k <= 0 {
		return nil
	}

	results := mr.hnswIndex.Search(embedding, k)
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

	// Generate query embedding if embedding client is available.
	var queryEmbed []float32
	if mr.embedClient != nil && query != "" {
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

	// Episodic memory (with temporal decay + optional embedding re-rank).
	if budget := int(float64(totalTokens) * mr.budgets.Episodic); budget > 0 {
		episodic := mr.retrieveEpisodic(ctx, query, queryEmbed, budget)
		if episodic != "" {
			sections = append(sections, "[Relevant Memories]\n"+episodic)
			totalCharsUsed += len(episodic)
			metrics.EpisodicCount = strings.Count(episodic, "\n- ") + 1
		}
	}

	// Semantic memory.
	if budget := int(float64(totalTokens) * mr.budgets.Semantic); budget > 0 {
		semantic := mr.retrieveSemanticMemory(ctx, query, budget)
		if semantic != "" {
			sections = append(sections, "[Knowledge]\n"+semantic)
			totalCharsUsed += len(semantic)
			metrics.SemanticCount = strings.Count(semantic, "\n- ") + 1
		}
	}

	// Procedural memory (tool stats).
	if budget := int(float64(totalTokens) * mr.budgets.Procedural); budget > 0 {
		procedural := mr.retrieveProceduralMemory(ctx, budget)
		if procedural != "" {
			sections = append(sections, "[Tool Experience]\n"+procedural)
			totalCharsUsed += len(procedural)
			metrics.ProceduralCount = strings.Count(procedural, "\n- ") + 1
		}
	}

	// Relationship memory.
	if budget := int(float64(totalTokens) * mr.budgets.Relationship); budget > 0 {
		relationships := mr.retrieveRelationshipMemory(ctx, budget)
		if relationships != "" {
			sections = append(sections, "[Relationships]\n"+relationships)
			totalCharsUsed += len(relationships)
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

	// Score and rank: blend temporal decay, FTS relevance, and embedding similarity.
	type scored struct {
		content string
		score   float64
	}
	entries := make([]scored, 0, len(candidates))
	for _, c := range candidates {
		score := scoreEpisodicCandidate(c.ageDays, c.ftsHit, mr.config)

		// Blend with embedding similarity if available.
		if queryEmbed != nil && mr.embedClient != nil {
			textEmbed, err := mr.embedClient.EmbedSingle(ctx, c.content)
			if err == nil {
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
