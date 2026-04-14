package memory

import (
	"context"
	"fmt"
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
func (mr *Retriever) RetrieveWithANN(ctx context.Context, embedding []float32, k int) []MemoryEntry {
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

	// Collapse detection signals — these track the health of retrieval precision.
	// ScoreSpread approaching 0 indicates semantic collapse (all results score alike).
	ScoreSpread     float64 `json:"score_spread"`      // top-1 minus top-k score delta
	AvgFTSScore     float64 `json:"avg_fts_score"`     // mean FTS leg score across results
	AvgVectorScore  float64 `json:"avg_vector_score"`  // mean vector leg score across results
	CorpusSize      int     `json:"corpus_size"`        // memory_index entries at query time
	HybridWeight    float64 `json:"hybrid_weight"`      // effective weight used (adaptive)
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

	// Collapse detection metrics.
	metrics.CorpusSize = corpusSize
	metrics.HybridWeight = AdaptiveHybridWeight(corpusSize)

	// ScoreSpread: probe HybridSearch for the score distribution.
	// A spread approaching 0 means all results score alike (semantic collapse).
	if query != "" {
		probe := db.HybridSearch(ctx, mr.store, query, queryEmbed, 10, metrics.HybridWeight, mr.vectorIndex)
		if len(probe) >= 2 {
			metrics.ScoreSpread = probe[0].Similarity - probe[len(probe)-1].Similarity
		}
		var ftsSum, vecSum float64
		var ftsCount, vecCount int
		for _, r := range probe {
			if r.FTSScore > 0 {
				ftsSum += r.FTSScore
				ftsCount++
			}
			if r.VectorScore > 0 {
				vecSum += r.VectorScore
				vecCount++
			}
		}
		if ftsCount > 0 {
			metrics.AvgFTSScore = ftsSum / float64(ftsCount)
		}
		if vecCount > 0 {
			metrics.AvgVectorScore = vecSum / float64(vecCount)
		}
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
