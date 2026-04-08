package memory

import (
	"context"
	"strings"
	"unicode/utf8"

	"github.com/rs/zerolog/log"

	"roboticus/internal/db"
	"roboticus/internal/llm"
	"roboticus/internal/session"
)

// TierBudget defines token allocation percentages per tier.
type TierBudget struct {
	Working      float64
	Episodic     float64
	Semantic     float64
	Procedural   float64
	Relationship float64
}

// DefaultTierBudget returns the standard allocation.
func DefaultTierBudget() TierBudget {
	return TierBudget{
		Working:      0.30,
		Episodic:     0.25,
		Semantic:     0.20,
		Procedural:   0.15,
		Relationship: 0.10,
	}
}

// Config controls the memory manager.
type Config struct {
	TotalTokenBudget int
	Budgets          TierBudget
}

// DefaultConfig returns sensible defaults.
func DefaultConfig() Config {
	return Config{
		TotalTokenBudget: 2048,
		Budgets:          DefaultTierBudget(),
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

// Manager handles 5-tier memory ingestion and retrieval.
type Manager struct {
	config Config
	store  *db.Store
}

// NewManager creates a memory manager with the given config.
func NewManager(cfg Config, store *db.Store) *Manager {
	return &Manager{config: cfg, store: store}
}

// derivableTools are tools whose output is ephemeral and should NOT be stored
// as episodic memory (Rust: is_derivable). Storing these leads to stale-fact
// hallucinations (e.g., "5 files found" persisted forever).
var derivableTools = map[string]bool{
	"list_directory":     true,
	"list-subagents":     true,
	"get_wallet_balance": true,
	"read_file":          true,
	"list_skills":        true,
	"get_session":        true,
	"get_config":         true,
	"list_sessions":      true,
	"list_tools":         true,
	"search_web":         true,
}

// IngestTurn processes a completed turn and stores relevant memories.
// Each tier write is independent; failures in one tier don't cascade.
// Matches Rust's ingest_turn with all tier-specific logic.
func (mm *Manager) IngestTurn(ctx context.Context, session *session.Session) {
	if mm.store == nil {
		return
	}

	messages := session.Messages()
	if len(messages) == 0 {
		return
	}

	last := messages[len(messages)-1]
	turnType := classifyTurn(messages)

	// Working memory: store turn summary with correct importance (Rust: importance=3).
	if last.Role == "assistant" && last.Content != "" {
		summary := safeUTF8Truncate(last.Content, 200)
		mm.storeWorkingMemoryWithImportance(ctx, session.ID, "turn_summary", summary, 3)
	}

	// Episodic: tool use events (skip derivable tools to avoid stale facts).
	if turnType == TurnToolUse {
		for _, m := range messages {
			if m.Role == "tool" {
				if derivableTools[m.Name] {
					log.Debug().Str("tool", m.Name).Msg("skipping derivable tool in memory ingestion")
					continue
				}
				event := m.Name + ": " + safeUTF8Truncate(m.Content, 300)
				// Dedup check: don't store if identical episodic content exists.
				if !mm.episodicContentExists(ctx, event) {
					mm.storeEpisodicMemoryWithImportance(ctx, "tool_event", event, 7)
				}
			}
		}
	}

	// Episodic: financial interactions (importance=8).
	if turnType == TurnFinancial {
		content := safeUTF8Truncate(last.Content, 300)
		if !mm.episodicContentExists(ctx, content) {
			mm.storeEpisodicMemoryWithImportance(ctx, "financial_event", content, 8)
		}
	}

	// Semantic: long reasoning/creative responses (confidence=0.6).
	if (turnType == TurnReasoning || turnType == TurnCreative) && len(last.Content) >= 100 {
		key := extractFirstSentence(last.Content)
		mm.storeSemanticMemory(ctx, "knowledge", key, safeUTF8Truncate(last.Content, 500))
	}

	// Procedural: tool success/failure tracking.
	for _, m := range messages {
		if m.Role == "tool" {
			success := !isToolFailure(m.Content)
			mm.recordToolStat(ctx, m.Name, success)
		}
	}

	// Relationship: track interactions with entities mentioned in user messages.
	// Trust scores differentiated by turn type (Rust parity).
	trustScore := 0.65
	switch turnType {
	case TurnSocial:
		trustScore = 0.8
	case TurnFinancial:
		trustScore = 0.75
	}
	mm.ingestRelationshipsWithTrust(ctx, messages, trustScore)
}

// episodicContentExists checks if identical episodic content already exists.
// Matches Rust's dedup check before episodic insert.
func (mm *Manager) episodicContentExists(ctx context.Context, content string) bool {
	var count int
	row := mm.store.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM episodic_memory WHERE content = ? AND memory_state = 'active'`,
		content,
	)
	if err := row.Scan(&count); err != nil {
		return false
	}
	return count > 0
}

// safeUTF8Truncate truncates a string to maxBytes while respecting UTF-8
// character boundaries. Matches Rust's floor_char_boundary approach.
func safeUTF8Truncate(s string, maxBytes int) string {
	if len(s) <= maxBytes {
		return s
	}
	// Walk backward from maxBytes to find a valid UTF-8 boundary.
	for maxBytes > 0 && !utf8.RuneStart(s[maxBytes]) {
		maxBytes--
	}
	return s[:maxBytes]
}

// storeWorkingMemory writes to the working_memory table with default importance.
// Retained as convenience wrapper for Rust parity; will be called from upcoming memory subsystem integration.
func (mm *Manager) storeWorkingMemory(ctx context.Context, sessionID, entryType, content string) { //nolint:unused // Rust parity stub
	mm.storeWorkingMemoryWithImportance(ctx, sessionID, entryType, content, 5)
}

// storeWorkingMemoryWithImportance writes to working_memory with explicit importance.
func (mm *Manager) storeWorkingMemoryWithImportance(ctx context.Context, sessionID, entryType, content string, importance int) {
	_, err := mm.store.ExecContext(ctx,
		`INSERT INTO working_memory (id, session_id, entry_type, content, importance)
		 VALUES (?, ?, ?, ?, ?)`,
		db.NewID(), sessionID, entryType, content, importance,
	)
	if err != nil {
		log.Warn().Err(err).Msg("failed to store working memory")
	}
}

// storeEpisodicMemory writes to the episodic_memory table with default importance.
// Retained as convenience wrapper for Rust parity; will be called from upcoming memory subsystem integration.
func (mm *Manager) storeEpisodicMemory(ctx context.Context, classification, content string) { //nolint:unused // Rust parity stub
	mm.storeEpisodicMemoryWithImportance(ctx, classification, content, 5)
}

// storeEpisodicMemoryWithImportance writes with explicit importance (Rust parity).
func (mm *Manager) storeEpisodicMemoryWithImportance(ctx context.Context, classification, content string, importance int) {
	_, err := mm.store.ExecContext(ctx,
		`INSERT INTO episodic_memory (id, classification, content, importance)
		 VALUES (?, ?, ?, ?)`,
		db.NewID(), classification, content, importance,
	)
	if err != nil {
		log.Warn().Err(err).Msg("failed to store episodic memory")
	}
}

// storeSemanticMemory writes to the semantic_memory table with UPSERT.
// When a key is superseded, the old entry is marked stale rather than deleted.
func (mm *Manager) storeSemanticMemory(ctx context.Context, category, key, value string) {
	_, err := mm.store.ExecContext(ctx,
		`INSERT INTO semantic_memory (id, category, key, value)
		 VALUES (?, ?, ?, ?)
		 ON CONFLICT(category, key) DO UPDATE SET
		     value = excluded.value,
		     updated_at = datetime('now'),
		     memory_state = 'active',
		     state_reason = NULL`,
		db.NewID(), category, key, value,
	)
	if err != nil {
		log.Warn().Err(err).Msg("failed to store semantic memory")
	}
}

// MarkSemanticStale marks semantic entries as stale by category and key prefix.
func (mm *Manager) MarkSemanticStale(ctx context.Context, category, keyPrefix, reason string) {
	_, err := mm.store.ExecContext(ctx,
		`UPDATE semantic_memory SET memory_state = 'stale', state_reason = ?
		 WHERE category = ? AND key LIKE ? AND memory_state = 'active'`,
		reason, category, keyPrefix+"%",
	)
	if err != nil {
		log.Warn().Err(err).Msg("failed to mark semantic memory stale")
	}
}

// recordToolStat tracks tool success/failure in the procedural_memory table.
func (mm *Manager) recordToolStat(ctx context.Context, toolName string, success bool) {
	if success {
		_, _ = mm.store.ExecContext(ctx,
			`INSERT INTO procedural_memory (id, name, steps, success_count)
			 VALUES (?, ?, '', 1)
			 ON CONFLICT(name) DO UPDATE SET success_count = success_count + 1, updated_at = datetime('now')`,
			db.NewID(), toolName,
		)
	} else {
		_, _ = mm.store.ExecContext(ctx,
			`INSERT INTO procedural_memory (id, name, steps, failure_count)
			 VALUES (?, ?, '', 1)
			 ON CONFLICT(name) DO UPDATE SET failure_count = failure_count + 1, updated_at = datetime('now')`,
			db.NewID(), toolName,
		)
	}
}

// ingestRelationships extracts entity mentions from user messages and updates relationship memory.
// Retained as convenience wrapper for Rust parity; will be called from upcoming memory subsystem integration.
func (mm *Manager) ingestRelationships(ctx context.Context, messages []llm.Message) { //nolint:unused // Rust parity stub
	mm.ingestRelationshipsWithTrust(ctx, messages, 0.5)
}

// ingestRelationshipsWithTrust extracts entities and sets trust score by turn type (Rust parity).
func (mm *Manager) ingestRelationshipsWithTrust(ctx context.Context, messages []llm.Message, trustScore float64) {
	for _, m := range messages {
		if m.Role != "user" || m.Content == "" {
			continue
		}

		// Extract @mentions or explicit entity references.
		entities := extractEntities(m.Content)
		for _, entity := range entities {
			_, _ = mm.store.ExecContext(ctx,
				`INSERT INTO relationship_memory (id, entity_id, entity_name, trust_score, interaction_count, last_interaction)
				 VALUES (?, ?, ?, ?, 1, datetime('now'))
				 ON CONFLICT(entity_id) DO UPDATE SET
				   trust_score = MAX(trust_score, ?),
				   interaction_count = interaction_count + 1,
				   last_interaction = datetime('now')`,
				db.NewID(), entity, entity, trustScore, trustScore,
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
// Matches Rust's 5-type classification: ToolUse, Financial, Social, Creative, Reasoning.
func classifyTurn(messages []llm.Message) TurnType {
	// Check for tool results first (highest priority).
	for _, m := range messages {
		if m.Role == "tool" {
			return TurnToolUse
		}
	}

	// Check last user message for type-specific keywords.
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "user" {
			lower := strings.ToLower(messages[i].Content)

			// Financial: ≥2 keywords (Rust parity).
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

			// Social: greeting/courtesy patterns (Rust parity — was missing).
			socialWords := []string{"hello", "thanks", "thank you", "please", "how are you", "hi ", "hey ", "good morning", "good evening"}
			for _, word := range socialWords {
				if strings.Contains(lower, word) {
					return TurnSocial
				}
			}

			// Creative: content generation patterns.
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
