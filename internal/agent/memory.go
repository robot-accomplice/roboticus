package agent

import (
	"context"
	"strings"

	"github.com/rs/zerolog/log"

	"goboticus/internal/db"
	"goboticus/internal/llm"
)

// MemoryTierBudget defines token allocation percentages per tier.
type MemoryTierBudget struct {
	Working      float64
	Episodic     float64
	Semantic     float64
	Procedural   float64
	Relationship float64
}

// DefaultMemoryTierBudget returns the standard allocation.
func DefaultMemoryTierBudget() MemoryTierBudget {
	return MemoryTierBudget{
		Working:      0.30,
		Episodic:     0.25,
		Semantic:     0.20,
		Procedural:   0.15,
		Relationship: 0.10,
	}
}

// MemoryConfig controls the memory manager.
type MemoryConfig struct {
	TotalTokenBudget int
	Budgets          MemoryTierBudget
}

// DefaultMemoryConfig returns sensible defaults.
func DefaultMemoryConfig() MemoryConfig {
	return MemoryConfig{
		TotalTokenBudget: 2048,
		Budgets:          DefaultMemoryTierBudget(),
	}
}

// TurnType classifies what kind of conversation turn occurred.
type TurnType int

const (
	TurnReasoning TurnType = iota
	TurnToolUse
	TurnFinancial
	TurnSocial
	TurnCreative
)

// MemoryManager handles 5-tier memory ingestion and retrieval.
type MemoryManager struct {
	config MemoryConfig
	store  *db.Store
}

// NewMemoryManager creates a memory manager with the given config.
func NewMemoryManager(cfg MemoryConfig, store *db.Store) *MemoryManager {
	return &MemoryManager{config: cfg, store: store}
}

// IngestTurn processes a completed turn and stores relevant memories.
// Each tier write is independent; failures in one tier don't cascade.
func (mm *MemoryManager) IngestTurn(ctx context.Context, session *Session) {
	if mm.store == nil {
		return
	}

	messages := session.Messages()
	if len(messages) == 0 {
		return
	}

	last := messages[len(messages)-1]
	turnType := classifyTurn(messages)

	// Working memory: store turn summary.
	if last.Role == "assistant" && last.Content != "" {
		summary := last.Content
		if len(summary) > 200 {
			summary = summary[:200]
		}
		mm.storeWorkingMemory(ctx, session.ID, "turn_summary", summary)
	}

	// Episodic: tool use events.
	if turnType == TurnToolUse {
		for _, m := range messages {
			if m.Role == "tool" {
				event := m.Name + ": " + truncate(m.Content, 300)
				mm.storeEpisodicMemory(ctx, "tool_event", event)
			}
		}
	}

	// Episodic: financial interactions.
	if turnType == TurnFinancial {
		mm.storeEpisodicMemory(ctx, "financial_event", truncate(last.Content, 300))
	}

	// Semantic: long reasoning/creative responses.
	if (turnType == TurnReasoning || turnType == TurnCreative) && len(last.Content) >= 100 {
		// Extract a key-value pair: use first sentence as key, full content as value.
		key := extractFirstSentence(last.Content)
		mm.storeSemanticMemory(ctx, "knowledge", key, truncate(last.Content, 500))
	}

	// Procedural: tool success/failure tracking.
	for _, m := range messages {
		if m.Role == "tool" {
			success := !isToolFailure(m.Content)
			mm.recordToolStat(ctx, m.Name, success)
		}
	}

	// Relationship: track interactions with entities mentioned in user messages.
	mm.ingestRelationships(ctx, messages)
}

// storeWorkingMemory writes to the working_memory table.
func (mm *MemoryManager) storeWorkingMemory(ctx context.Context, sessionID, entryType, content string) {
	_, err := mm.store.ExecContext(ctx,
		`INSERT INTO working_memory (id, session_id, entry_type, content)
		 VALUES (?, ?, ?, ?)`,
		db.NewID(), sessionID, entryType, content,
	)
	if err != nil {
		log.Warn().Err(err).Msg("failed to store working memory")
	}
}

// storeEpisodicMemory writes to the episodic_memory table.
func (mm *MemoryManager) storeEpisodicMemory(ctx context.Context, classification, content string) {
	_, err := mm.store.ExecContext(ctx,
		`INSERT INTO episodic_memory (id, classification, content)
		 VALUES (?, ?, ?)`,
		db.NewID(), classification, content,
	)
	if err != nil {
		log.Warn().Err(err).Msg("failed to store episodic memory")
	}
}

// storeSemanticMemory writes to the semantic_memory table with UPSERT.
func (mm *MemoryManager) storeSemanticMemory(ctx context.Context, category, key, value string) {
	_, err := mm.store.ExecContext(ctx,
		`INSERT INTO semantic_memory (id, category, key, value)
		 VALUES (?, ?, ?, ?)
		 ON CONFLICT(category, key) DO UPDATE SET value = excluded.value, updated_at = datetime('now')`,
		db.NewID(), category, key, value,
	)
	if err != nil {
		log.Warn().Err(err).Msg("failed to store semantic memory")
	}
}

// recordToolStat tracks tool success/failure in the procedural_memory table.
func (mm *MemoryManager) recordToolStat(ctx context.Context, toolName string, success bool) {
	if success {
		mm.store.ExecContext(ctx,
			`INSERT INTO procedural_memory (id, name, steps, success_count)
			 VALUES (?, ?, '', 1)
			 ON CONFLICT(name) DO UPDATE SET success_count = success_count + 1, updated_at = datetime('now')`,
			db.NewID(), toolName,
		)
	} else {
		mm.store.ExecContext(ctx,
			`INSERT INTO procedural_memory (id, name, steps, failure_count)
			 VALUES (?, ?, '', 1)
			 ON CONFLICT(name) DO UPDATE SET failure_count = failure_count + 1, updated_at = datetime('now')`,
			db.NewID(), toolName,
		)
	}
}

// ingestRelationships extracts entity mentions from user messages and updates relationship memory.
func (mm *MemoryManager) ingestRelationships(ctx context.Context, messages []llm.Message) {
	for _, m := range messages {
		if m.Role != "user" || m.Content == "" {
			continue
		}

		// Extract @mentions or explicit entity references.
		entities := extractEntities(m.Content)
		for _, entity := range entities {
			mm.store.ExecContext(ctx,
				`INSERT INTO relationship_memory (id, entity_id, entity_name, interaction_count, last_interaction)
				 VALUES (?, ?, ?, 1, datetime('now'))
				 ON CONFLICT(entity_id) DO UPDATE SET
				   interaction_count = interaction_count + 1,
				   last_interaction = datetime('now')`,
				db.NewID(), entity, entity,
			)
		}
	}
}

// extractEntities finds potential entity references in text (@ mentions, capitalized names).
func extractEntities(text string) []string {
	var entities []string
	seen := make(map[string]bool)

	words := strings.Fields(text)
	for _, w := range words {
		// @mentions.
		if strings.HasPrefix(w, "@") && len(w) > 1 {
			name := strings.Trim(w[1:], ".,!?;:")
			if name != "" && !seen[name] {
				entities = append(entities, name)
				seen[name] = true
			}
		}
	}
	return entities
}

// extractFirstSentence returns the first sentence (up to a period, question mark, or newline).
func extractFirstSentence(s string) string {
	for i, r := range s {
		if r == '.' || r == '?' || r == '!' || r == '\n' {
			if i > 0 {
				return s[:i]
			}
		}
		if i > 100 {
			return s[:100]
		}
	}
	if len(s) > 100 {
		return s[:100]
	}
	return s
}

// classifyTurn determines the type of the most recent exchange.
func classifyTurn(messages []llm.Message) TurnType {
	// Check for tool results.
	for _, m := range messages {
		if m.Role == "tool" {
			return TurnToolUse
		}
	}

	// Check last user message for financial keywords.
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "user" {
			lower := strings.ToLower(messages[i].Content)
			financialWords := []string{"transfer", "balance", "wallet", "payment", "usdc", "send funds"}
			count := 0
			for _, word := range financialWords {
				if strings.Contains(lower, word) {
					count++
				}
			}
			if count >= 2 {
				return TurnFinancial
			}

			// Check for creative.
			creativeWords := []string{"create", "write", "design", "compose", "generate"}
			for _, word := range creativeWords {
				if strings.Contains(lower, word) {
					return TurnCreative
				}
			}
			break
		}
	}

	return TurnReasoning
}

// isToolFailure checks if tool output indicates an error.
func isToolFailure(output string) bool {
	lower := strings.ToLower(output)
	prefixes := []string{"error:", "failed:", "failure:", "fatal:", "panic:"}
	for _, p := range prefixes {
		if strings.HasPrefix(lower, p) {
			return true
		}
	}
	if strings.HasPrefix(lower, `{"error`) || strings.HasPrefix(lower, `{"err`) {
		return true
	}
	return false
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

func boolStr(v int) string {
	if v == 1 {
		return "success"
	}
	return "failure"
}
