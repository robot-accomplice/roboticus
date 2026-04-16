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
	HybridWeight     float64        // FTS vs embedding blend (0=FTS only, 1=embedding only, 0.5=balanced)
	EpisodicHalfLife float64        // Days for episodic decay (default 7)
	DecayFloor       float64        // Minimum decay factor (default 0.05)
	Reranker         RerankerConfig // reranker tuning parameters
}

// DefaultRetrievalConfig returns sensible defaults.
func DefaultRetrievalConfig() RetrievalConfig {
	return RetrievalConfig{
		HybridWeight:     0.5,
		EpisodicHalfLife: 7.0,
		DecayFloor:       0.05,
		Reranker:         DefaultRerankerConfig(),
	}
}

// Retriever coordinates retrieval across all memory stores.
//
// Concurrency contract: *Retriever is constructed once at daemon startup
// and shared across every concurrent request. No per-turn mutable state
// may live on this struct — per-call inputs (query, sessionID, budget,
// intent classification) must travel via function parameters or
// context.Context. Pre-v1.0.6 this struct carried an `intents` field
// that was set by a call-site helper before every Retrieve; under
// concurrent traffic turn A's SetIntents raced turn B's read in the
// routing logic. See intents_context.go for the ctx-value replacement
// (mirrors the RetrievalTracer pattern in retrieval_path.go).
type Retriever struct {
	config        RetrievalConfig
	store         *db.Store
	budgets       TierBudget
	embedClient   *llm.EmbeddingClient
	vectorIndex   db.VectorIndex
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

// (v1.0.6) SetIntents was removed — intents now travel via
// memory.WithIntents(ctx, ...) and are read by intentsFromContext(ctx)
// inside Retrieve. See intents_context.go for the rationale. Old callers
// that still used SetIntents will fail at compile time, which is the
// intended behavior: the shared-state pattern is gone, not renamed.

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
	ScoreSpread    float64 `json:"score_spread"`     // top-1 minus top-k score delta
	AvgFTSScore    float64 `json:"avg_fts_score"`    // mean FTS leg score across results
	AvgVectorScore float64 `json:"avg_vector_score"` // mean vector leg score across results
	CorpusSize     int     `json:"corpus_size"`      // memory_index entries at query time
	HybridWeight   float64 `json:"hybrid_weight"`    // effective weight used (adaptive)
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

	totalCharsAllowed := totalTokens * mr.charsPerToken

	// ── Working state (direct injection, NOT searched) ──────────────
	// Working memory is active state, not a retrieval tier.
	var workingText, ambientText string
	if budget := int(float64(totalTokens) * mr.budgets.Working); budget > 0 {
		workingText = mr.retrieveWorkingMemory(ctx, sessionID, budget)
		if workingText != "" {
			metrics.WorkingCount = strings.Count(workingText, "\n- ") + 1
		}
	}
	ambientText = mr.retrieveAmbientRecent(ctx, 2)
	if ambientText != "" {
		metrics.AmbientCount = strings.Count(ambientText, "\n- ") + 1
	}

	// ── Agentic retrieval pipeline ──────────────────────────────────
	// 1. Decompose compound queries into subgoals.
	subgoals := Decompose(query)

	// 2. Route each subgoal to the appropriate memory tiers.
	router := NewRouter(corpusSize)
	var allEvidence []Evidence

	// Intents are per-call ambient state from the request's intent classifier.
	// See memory.WithIntents for the context-value contract.
	intents := intentsFromContext(ctx)
	for _, sg := range subgoals {
		plan := router.Plan(sg.Question, intents)

		// 3. Retrieve from each targeted tier.
		for _, target := range plan.Targets {
			tierBudget := int(float64(totalTokens) * target.Budget)
			if tierBudget <= 0 {
				continue
			}
			tierResults := mr.retrieveTier(ctx, target.Tier, target.Mode, sg.Question, queryEmbed, tierBudget/mr.charsPerToken, corpusSize)
			for i, ev := range tierResults {
				if ev.Score == 0 {
					ev.Score = target.Weight
				}
				// Position decay as tiebreaker for entries without explicit ranking metadata.
				positionDecay := 1.0 - (float64(i) * 0.02)
				if positionDecay < 0.1 {
					positionDecay = 0.1
				}
				ev.Score *= positionDecay
				ev.SourceTier = target.Tier
				if ev.RetrievalMode == "" {
					ev.RetrievalMode = target.Mode.String()
				}
				allEvidence = append(allEvidence, ev)
			}
		}
	}

	// 4. Rerank: discard weak evidence, boost authority, penalize stale.
	reranker := NewReranker(mr.config.Reranker)
	maxEvidence := totalCharsAllowed / (mr.charsPerToken * 50) // rough estimate: ~50 tokens per evidence item
	if maxEvidence < 5 {
		maxEvidence = 5
	}
	filtered := reranker.Filter(allEvidence, maxEvidence)

	// 5. Structured context assembly: evidence + gaps + contradictions.
	assembled := AssembleContext(ctx, mr.store, sessionID, filtered, workingText, ambientText)

	// ── Metrics ─────────────────────────────────────────────────────
	metrics.EpisodicCount = countByTier(filtered, TierEpisodic)
	metrics.SemanticCount = countByTier(filtered, TierSemantic)
	metrics.ProceduralCount = countByTier(filtered, TierProcedural)
	metrics.RelationCount = countByTier(filtered, TierRelationship)
	metrics.TotalEntries = metrics.WorkingCount + metrics.AmbientCount +
		metrics.EpisodicCount + metrics.SemanticCount +
		metrics.ProceduralCount + metrics.RelationCount
	metrics.MatchedEntries = metrics.TotalEntries

	result := assembled.Format()
	if totalCharsAllowed > 0 {
		metrics.BudgetUsedPct = float64(len(result)) / float64(totalCharsAllowed)
	}

	// Collapse detection.
	metrics.CorpusSize = corpusSize
	metrics.HybridWeight = AdaptiveHybridWeight(corpusSize)
	if len(filtered) >= 2 {
		metrics.ScoreSpread = filtered[0].Score - filtered[len(filtered)-1].Score
	}

	return result, metrics
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

// retrieveTier dispatches a query to a specific memory tier and returns content strings.
func (mr *Retriever) retrieveTier(ctx context.Context, tier MemoryTier, mode RetrievalMode, query string, queryEmbed []float32, budgetTokens int, corpusSize int) []Evidence {
	switch tier {
	case TierEpisodic:
		return wrapTierEntries(tier, mode, mr.retrieveEpisodic(ctx, query, queryEmbed, budgetTokens, corpusSize))
	case TierSemantic:
		return mr.retrieveSemanticEvidence(ctx, query, queryEmbed, mode, budgetTokens)
	case TierProcedural:
		return wrapTierEntries(tier, mode, mr.retrieveProceduralMemory(ctx, query, queryEmbed, mode, budgetTokens))
	case TierRelationship:
		return mr.retrieveRelationshipEvidence(ctx, query, queryEmbed, mode, budgetTokens)
	default:
		return nil
	}
}

func wrapTierEntries(tier MemoryTier, mode RetrievalMode, raw string) []Evidence {
	if raw == "" {
		return nil
	}
	var entries []Evidence
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || !strings.HasPrefix(line, "- ") {
			continue
		}
		content := strings.TrimPrefix(line, "- ")
		score := 0.0
		if strings.HasPrefix(content, "(sim=") {
			if end := strings.Index(content, ") "); end > 5 {
				var parsed float64
				if _, err := fmt.Sscanf(content[5:end], "%f", &parsed); err == nil {
					score = parsed
					content = content[end+2:]
				}
			}
		}
		entries = append(entries, Evidence{
			Content:       content,
			SourceTier:    tier,
			Score:         score,
			RetrievalMode: mode.String(),
		})
	}
	return entries
}

// countByTier counts evidence entries from a specific tier.
func countByTier(evidence []Evidence, tier MemoryTier) int {
	n := 0
	for _, e := range evidence {
		if e.SourceTier == tier {
			n++
		}
	}
	return n
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
