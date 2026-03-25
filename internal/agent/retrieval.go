package agent

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/rs/zerolog/log"

	"goboticus/internal/db"
	"goboticus/internal/llm"
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

// MemoryRetriever coordinates retrieval across all 5 memory tiers.
type MemoryRetriever struct {
	config        RetrievalConfig
	store         *db.Store
	budgets       MemoryTierBudget
	embedClient   *llm.EmbeddingClient
	charsPerToken int
}

// NewMemoryRetriever creates a retriever.
func NewMemoryRetriever(cfg RetrievalConfig, budgets MemoryTierBudget, store *db.Store) *MemoryRetriever {
	return &MemoryRetriever{
		config:        cfg,
		store:         store,
		budgets:       budgets,
		charsPerToken: 4,
	}
}

// SetEmbeddingClient attaches an embedding client for hybrid search.
func (mr *MemoryRetriever) SetEmbeddingClient(ec *llm.EmbeddingClient) {
	mr.embedClient = ec
}

// MemoryEntry represents a retrieved memory.
type MemoryEntry struct {
	ID         string
	Tier       string
	EntryType  string
	Content    string
	Similarity float64
	AgeDays    float64
}

// Retrieve fetches relevant memories across all tiers within the total token budget.
func (mr *MemoryRetriever) Retrieve(ctx context.Context, sessionID, query string, totalTokens int) string {
	if mr.store == nil {
		return ""
	}

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

	// Working memory.
	if budget := int(float64(totalTokens) * mr.budgets.Working); budget > 0 {
		working := mr.retrieveWorkingMemory(ctx, sessionID, budget)
		if working != "" {
			sections = append(sections, "[Working Memory]\n"+working)
		}
	}

	// Episodic memory (with temporal decay + optional embedding re-rank).
	if budget := int(float64(totalTokens) * mr.budgets.Episodic); budget > 0 {
		episodic := mr.retrieveEpisodic(ctx, query, queryEmbed, budget)
		if episodic != "" {
			sections = append(sections, "[Relevant Memories]\n"+episodic)
		}
	}

	// Semantic memory.
	if budget := int(float64(totalTokens) * mr.budgets.Semantic); budget > 0 {
		semantic := mr.retrieveSemanticMemory(ctx, query, budget)
		if semantic != "" {
			sections = append(sections, "[Knowledge]\n"+semantic)
		}
	}

	// Procedural memory (tool stats).
	if budget := int(float64(totalTokens) * mr.budgets.Procedural); budget > 0 {
		procedural := mr.retrieveProceduralMemory(ctx, budget)
		if procedural != "" {
			sections = append(sections, "[Tool Experience]\n"+procedural)
		}
	}

	// Relationship memory.
	if budget := int(float64(totalTokens) * mr.budgets.Relationship); budget > 0 {
		relationships := mr.retrieveRelationshipMemory(ctx, budget)
		if relationships != "" {
			sections = append(sections, "[Relationships]\n"+relationships)
		}
	}

	if len(sections) == 0 {
		return ""
	}
	return "[Active Memory]\n" + strings.Join(sections, "\n\n")
}

// retrieveWorkingMemory fetches from the working_memory table.
func (mr *MemoryRetriever) retrieveWorkingMemory(ctx context.Context, sessionID string, budgetTokens int) string {
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

// retrieveEpisodic fetches from episodic_memory with temporal decay and optional embedding re-ranking.
func (mr *MemoryRetriever) retrieveEpisodic(ctx context.Context, query string, queryEmbed []float32, budgetTokens int) string {
	maxChars := budgetTokens * mr.charsPerToken

	// Try FTS5 first if a query is provided.
	var rows *sql.Rows
	var err error
	if query != "" {
		// Try FTS5 match.
		rows, err = mr.store.QueryContext(ctx,
			`SELECT em.id, em.content, julianday('now') - julianday(em.created_at) as age_days
			 FROM episodic_memory em
			 LEFT JOIN memory_fts fts ON fts.rowid = (SELECT rowid FROM episodic_memory WHERE id = em.id)
			 ORDER BY em.created_at DESC LIMIT 30`)
	}
	if err != nil || rows == nil {
		rows, err = mr.store.QueryContext(ctx,
			`SELECT id, content, julianday('now') - julianday(created_at) as age_days
			 FROM episodic_memory ORDER BY created_at DESC LIMIT 30`)
	}
	if err != nil {
		return ""
	}
	defer func() { _ = rows.Close() }()

	type scored struct {
		content string
		score   float64
	}
	var entries []scored
	for rows.Next() {
		var id, content string
		var ageDays float64
		if rows.Scan(&id, &content, &ageDays) != nil {
			continue
		}

		// Temporal decay: score = 0.5^(age/halfLife), floored.
		decay := math.Pow(0.5, ageDays/mr.config.EpisodicHalfLife)
		if decay < mr.config.DecayFloor {
			decay = mr.config.DecayFloor
		}

		// Blend with embedding similarity if available.
		finalScore := decay
		if queryEmbed != nil && mr.embedClient != nil {
			textEmbed, err := mr.embedClient.EmbedSingle(ctx, content)
			if err == nil {
				sim := llm.CosineSimilarity(queryEmbed, textEmbed)
				// Hybrid blend: (1-w)*decay + w*similarity.
				finalScore = (1-mr.config.HybridWeight)*decay + mr.config.HybridWeight*sim
			}
		}

		entries = append(entries, scored{content: content, score: finalScore})
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

// retrieveSemanticMemory fetches from the semantic_memory table.
func (mr *MemoryRetriever) retrieveSemanticMemory(ctx context.Context, query string, budgetTokens int) string {
	maxChars := budgetTokens * mr.charsPerToken

	var rows *sql.Rows
	var err error
	if query != "" {
		rows, err = mr.store.QueryContext(ctx,
			`SELECT category, key, value FROM semantic_memory
			 WHERE value LIKE ? OR key LIKE ?
			 ORDER BY confidence DESC, updated_at DESC LIMIT 20`,
			"%"+query+"%", "%"+query+"%")
	}
	if err != nil || rows == nil {
		rows, err = mr.store.QueryContext(ctx,
			`SELECT category, key, value FROM semantic_memory
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
func (mr *MemoryRetriever) retrieveProceduralMemory(ctx context.Context, budgetTokens int) string {
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
func (mr *MemoryRetriever) retrieveRelationshipMemory(ctx context.Context, budgetTokens int) string {
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
