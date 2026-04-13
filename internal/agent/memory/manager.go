package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/rs/zerolog/log"

	"roboticus/internal/core"
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
	config      Config
	store       *db.Store
	errBus      *core.ErrorBus
	embedClient *llm.EmbeddingClient
	hnswIndex   *db.HNSWIndex
}

// NewManager creates a memory manager with the given config.
func NewManager(cfg Config, store *db.Store) *Manager {
	return &Manager{config: cfg, store: store}
}

// SetErrBus wires the centralized error bus (called after construction in daemon).
func (mm *Manager) SetErrBus(eb *core.ErrorBus) { mm.errBus = eb }

// SetEmbeddingClient attaches an embedding client for ingestion-time embedding.
// When set, newly stored episodic and semantic memories are embedded and persisted
// to the embeddings table, enabling hybrid retrieval via cosine similarity.
func (mm *Manager) SetEmbeddingClient(ec *llm.EmbeddingClient) { mm.embedClient = ec }

// SetHNSWIndex attaches an HNSW index for incremental updates during ingestion.
func (mm *Manager) SetHNSWIndex(idx *db.HNSWIndex) { mm.hnswIndex = idx }

// embedAndStore generates an embedding for content and persists it to the
// embeddings table. If an HNSW index is attached, the entry is also added
// incrementally for immediate retrieval availability.
//
// This is a best-effort operation: embedding failures are logged and swallowed
// so they never block memory ingestion. The consolidation pipeline backfills
// any entries that were missed (e.g., due to transient provider errors).
func (mm *Manager) embedAndStore(ctx context.Context, sourceTable, sourceID, content string) {
	if mm.embedClient == nil || mm.store == nil {
		return
	}
	vec, err := mm.embedClient.EmbedSingle(ctx, content)
	if err != nil {
		log.Debug().Err(err).Str("source", sourceTable).Msg("embedAndStore: embedding failed, will backfill later")
		return
	}
	if len(vec) == 0 {
		return
	}

	preview := content
	if len(preview) > 200 {
		preview = preview[:200]
	}
	blob := db.EmbeddingToBlob(vec)

	_, err = mm.store.ExecContext(ctx,
		`INSERT OR IGNORE INTO embeddings (id, source_table, source_id, content_preview, embedding_blob, dimensions)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		db.NewID(), sourceTable, sourceID, preview, blob, len(vec))
	if err != nil {
		log.Debug().Err(err).Str("source", sourceTable).Msg("embedAndStore: insert failed")
		return
	}

	// Incremental HNSW update for immediate retrieval availability.
	if mm.hnswIndex != nil {
		embed64 := make([]float64, len(vec))
		for i, v := range vec {
			embed64[i] = float64(v)
		}
		mm.hnswIndex.AddEntry(db.HNSWEntry{
			SourceTable:    sourceTable,
			SourceID:       sourceID,
			ContentPreview: preview,
			Embedding:      embed64,
		})
	}
}

// PromoteSessionSummary extracts the top working memory entries for a session
// and stores them as a semantic memory summary. Called on session archival so
// new sessions can start with context from the previous session.
func (mm *Manager) PromoteSessionSummary(ctx context.Context, sessionID string) {
	if mm.store == nil {
		return
	}

	rows, err := mm.store.QueryContext(ctx,
		`SELECT content FROM working_memory WHERE session_id = ?
		 ORDER BY importance DESC, created_at DESC LIMIT 5`, sessionID)
	if err != nil {
		return
	}
	defer func() { _ = rows.Close() }()

	var parts []string
	totalLen := 0
	for rows.Next() {
		var content string
		if rows.Scan(&content) != nil {
			continue
		}
		if totalLen+len(content) > 500 {
			break
		}
		parts = append(parts, content)
		totalLen += len(content)
	}
	if len(parts) == 0 {
		return
	}

	summary := strings.Join(parts, "; ")
	mm.storeSemanticMemory(ctx, "session_summary", sessionID, summary)
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

	// Embedding-based classification upgrade: if keyword-based classification
	// returned a non-ToolUse type and an embed client is available, try
	// embedding-based classification for better accuracy.
	if turnType != TurnToolUse && mm.embedClient != nil {
		for i := len(messages) - 1; i >= 0; i-- {
			if messages[i].Role == "user" && messages[i].Content != "" {
				if embType, ok := classifyTurnWithEmbeddings(ctx, mm.embedClient, messages[i].Content); ok {
					turnType = embType
				}
				break
			}
		}
	}

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
				event := summarizeToolOutput(m.Name, m.Content) // Summarize JSON; plain-text truncated to 150 chars
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
		key := semanticKey(last.Content)
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
// storeEpisodicMemoryWithImportance writes with explicit importance (Rust parity).
func (mm *Manager) storeEpisodicMemoryWithImportance(ctx context.Context, classification, content string, importance int) {
	entryID := db.NewID()
	_, err := mm.store.ExecContext(ctx,
		`INSERT INTO episodic_memory (id, classification, content, importance)
		 VALUES (?, ?, ?, ?)`,
		entryID, classification, content, importance,
	)
	if err != nil {
		log.Warn().Err(err).Msg("failed to store episodic memory")
		return
	}
	mm.autoIndex(ctx, "episodic_memory", entryID, content)
	mm.embedAndStore(ctx, "episodic_memory", entryID, content)
}

// storeSemanticMemory writes to the semantic_memory table with UPSERT.
// When a key is superseded, the old entry is marked stale rather than deleted.
func (mm *Manager) storeSemanticMemory(ctx context.Context, category, key, value string) {
	entryID := db.NewID()
	_, err := mm.store.ExecContext(ctx,
		`INSERT INTO semantic_memory (id, category, key, value)
		 VALUES (?, ?, ?, ?)
		 ON CONFLICT(category, key) DO UPDATE SET
		     value = excluded.value,
		     updated_at = datetime('now'),
		     memory_state = 'active',
		     state_reason = NULL`,
		entryID, category, key, value,
	)
	if err != nil {
		log.Warn().Err(err).Msg("failed to store semantic memory")
		return
	}
	mm.autoIndex(ctx, "semantic_memory", entryID, key+": "+value)
	mm.embedAndStore(ctx, "semantic_memory", entryID, key+": "+value)
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
		if _, err := mm.store.ExecContext(ctx,
			`INSERT INTO procedural_memory (id, name, steps, success_count)
			 VALUES (?, ?, '', 1)
			 ON CONFLICT(name) DO UPDATE SET success_count = success_count + 1, updated_at = datetime('now')`,
			db.NewID(), toolName,
		); err != nil {
			mm.errBus.ReportIfErr(err, "memory", "record_tool_success", core.SevWarning)
		}
	} else {
		if _, err := mm.store.ExecContext(ctx,
			`INSERT INTO procedural_memory (id, name, steps, failure_count)
			 VALUES (?, ?, '', 1)
			 ON CONFLICT(name) DO UPDATE SET failure_count = failure_count + 1, updated_at = datetime('now')`,
			db.NewID(), toolName,
		); err != nil {
			mm.errBus.ReportIfErr(err, "memory", "record_tool_failure", core.SevWarning)
		}
	}
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
			if _, err := mm.store.ExecContext(ctx,
				`INSERT INTO relationship_memory (id, entity_id, entity_name, trust_score, interaction_count, last_interaction)
				 VALUES (?, ?, ?, ?, 1, datetime('now'))
				 ON CONFLICT(entity_id) DO UPDATE SET
				   trust_score = MAX(trust_score, ?),
				   interaction_count = interaction_count + 1,
				   last_interaction = datetime('now')`,
				db.NewID(), entity, entity, trustScore, trustScore,
			); err != nil {
				mm.errBus.ReportIfErr(err, "memory", "ingest_relationship", core.SevWarning)
			}
		}
	}
}

// entityExclusions are words that commonly start sentences or are not proper nouns.
var entityExclusions = map[string]bool{
	"the": true, "this": true, "that": true, "these": true, "those": true,
	"it": true, "its": true, "we": true, "they": true, "he": true, "she": true,
	"however": true, "therefore": true, "furthermore": true, "meanwhile": true,
	"also": true, "but": true, "and": true, "or": true, "so": true, "yet": true,
	"if": true, "when": true, "where": true, "while": true, "because": true,
	"after": true, "before": true, "since": true, "until": true,
	// Months.
	"january": true, "february": true, "march": true, "april": true,
	"may": true, "june": true, "july": true, "august": true,
	"september": true, "october": true, "november": true, "december": true,
	// Days.
	"monday": true, "tuesday": true, "wednesday": true, "thursday": true,
	"friday": true, "saturday": true, "sunday": true,
	// Common tech acronyms used as words.
	"api": true, "url": true, "http": true, "https": true, "sql": true,
	"css": true, "html": true, "json": true, "xml": true, "cli": true,
	// Common sentence starters.
	"yes": true, "no": true, "sure": true, "ok": true, "here": true,
	"there": true, "what": true, "how": true, "why": true, "who": true,
	"i": true, "my": true, "me": true, "your": true, "you": true,
}

// commonNonNameWords are words that frequently appear capitalized at sentence start
// but are almost never proper nouns. Used to distinguish "Alice fixed the bug" (name)
// from "After the deployment" (common word). If a sentence-start word is NOT in this
// set and NOT in entityExclusions, it's likely a proper noun.
var commonNonNameWords = map[string]bool{
	// Verbs commonly starting sentences
	"let": true, "make": true, "run": true, "try": true, "check": true,
	"set": true, "get": true, "use": true, "add": true, "fix": true,
	"see": true, "look": true, "just": true, "can": true, "should": true,
	"will": true, "would": true, "could": true, "do": true, "did": true,
	"have": true, "had": true, "has": true, "was": true, "were": true,
	"been": true, "being": true, "is": true, "are": true, "am": true,
	// Conjunctions and adverbs
	"then": true, "next": true, "now": true, "first": true, "last": true,
	"once": true, "still": true, "even": true, "each": true, "every": true,
	"both": true, "either": true, "neither": true, "some": true, "any": true,
	"all": true, "most": true, "many": true, "much": true, "more": true,
	"less": true, "few": true, "other": true, "another": true, "such": true,
	// Prepositions
	"in": true, "on": true, "at": true, "to": true, "for": true,
	"with": true, "from": true, "by": true, "about": true, "into": true,
	// Pronouns and determiners (supplements entityExclusions)
	"our": true, "their": true, "his": true, "her": true, "its": true,
	"one": true, "two": true, "three": true, "four": true, "five": true,
	// Common technical sentence starters
	"note": true, "error": true, "warning": true, "update": true,
	"found": true, "created": true, "deleted": true, "changed": true,
	"started": true, "stopped": true, "running": true, "loading": true,
	"tested": true, "deployed": true, "built": true, "installed": true,
	// Greetings and social words
	"hello": true, "hi": true, "hey": true, "thanks": true, "thank": true,
	"please": true, "sorry": true, "great": true, "good": true, "ok": true,
	"welcome": true, "bye": true, "goodbye": true,
}

// maxEntitiesPerMessage caps entity extraction to prevent noise from unusual text.
const maxEntitiesPerMessage = 5

// extractEntities finds potential entity references in text.
// Extracts @mentions and sequences of capitalized proper nouns (not at sentence start).
func extractEntities(text string) []string {
	var entities []string
	seen := make(map[string]bool)

	words := strings.Fields(text)
	for _, w := range words {
		// @mentions.
		if strings.HasPrefix(w, "@") && len(w) > 1 {
			name := strings.Trim(w[1:], ".,!?;:")
			if name != "" && !seen[strings.ToLower(name)] {
				entities = append(entities, name)
				seen[strings.ToLower(name)] = true
			}
		}
	}

	// Proper noun extraction: find sequences of capitalized words not at sentence start.
	// We skip words after sentence-ending punctuation (., !, ?) as they may just be
	// regular sentence starts.
	afterSentenceEnd := true // Start of text = start of sentence.
	var currentName []string

	for _, w := range words {
		clean := strings.Trim(w, ".,!?;:\"'()[]")

		// Check if this word ends a sentence.
		endsSentence := strings.HasSuffix(w, ".") || strings.HasSuffix(w, "!") || strings.HasSuffix(w, "?")

		// A capitalized word: first rune is uppercase, not all-uppercase (avoids ALL CAPS).
		isCapitalized := len(clean) >= 2 && isUpperRune(rune(clean[0])) && !isAllUpper(clean)

		// Allow capitalized words that are NOT at sentence start.
		// Also allow sentence-start words if they pass the name filter
		// (not a common non-name word, not in exclusions).
		isLikelyName := isCapitalized && !entityExclusions[strings.ToLower(clean)]
		if afterSentenceEnd && isLikelyName {
			// Sentence-start: only allow if not a common non-name word.
			isLikelyName = !commonNonNameWords[strings.ToLower(clean)]
		}
		if isLikelyName {
			currentName = append(currentName, clean)
		} else {
			// Flush any accumulated proper noun sequence.
			if len(currentName) >= 1 {
				name := strings.Join(currentName, " ")
				lower := strings.ToLower(name)
				if !seen[lower] && !entityExclusions[lower] {
					entities = append(entities, name)
					seen[lower] = true
				}
			}
			currentName = nil
		}

		afterSentenceEnd = endsSentence
		if len(entities) >= maxEntitiesPerMessage {
			break
		}
	}

	// Flush final sequence.
	if len(currentName) >= 1 && len(entities) < maxEntitiesPerMessage {
		name := strings.Join(currentName, " ")
		lower := strings.ToLower(name)
		if !seen[lower] && !entityExclusions[lower] {
			entities = append(entities, name)
		}
	}

	// Frequency pass: words that appear 2+ times as capitalized (any position,
	// including sentence start) are very likely proper nouns. This catches names
	// at sentence start that the position-based filter would otherwise skip.
	wordFreq := make(map[string]int)
	for _, w := range words {
		clean := strings.Trim(w, ".,!?;:\"'()[]")
		if len(clean) >= 2 && isUpperRune(rune(clean[0])) && !isAllUpper(clean) {
			wordFreq[strings.ToLower(clean)]++
		}
	}
	for word, count := range wordFreq {
		if count >= 2 && !entityExclusions[word] && !seen[word] && len(entities) < maxEntitiesPerMessage {
			// Title-case the word for consistency.
			titled := strings.ToUpper(word[:1]) + word[1:]
			entities = append(entities, titled)
			seen[word] = true
		}
	}

	return entities
}

func isUpperRune(r rune) bool { return r >= 'A' && r <= 'Z' }
func isAllUpper(s string) bool {
	for _, r := range s {
		if r >= 'a' && r <= 'z' {
			return false
		}
	}
	return true
}

// semanticKey produces a stable, human-readable key for semantic memory UPSERT.
// Format: first 60 chars of content + 8-char FNV hash suffix.
// This ensures exact-content matches dedup via UPSERT while rephrased content
// gets a different key (caught later by contradiction detection).
func semanticKey(content string) string {
	prefix := content
	if len(prefix) > 60 {
		prefix = prefix[:60]
	}
	h := fnv.New32a()
	h.Write([]byte(content))
	return fmt.Sprintf("%s_%08x", prefix, h.Sum32())
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

// classifyTurnPrototypes are prototype texts for each turn type, used for
// embedding-based classification when an embed client is available.
var classifyTurnPrototypes = map[TurnType]string{
	TurnFinancial: "financial transaction payment transfer balance wallet money send receive funds",
	TurnSocial:    "greeting hello thanks social conversation how are you good morning",
	TurnCreative:  "create write design compose generate build make produce draft",
	TurnReasoning: "analyze explain reason think evaluate compare understand research",
}

// classifyTurnWithEmbeddings uses cosine similarity against prototype embeddings.
// Returns (type, true) if classification succeeded, (TurnReasoning, false) if below threshold.
func classifyTurnWithEmbeddings(ctx context.Context, ec *llm.EmbeddingClient, text string) (TurnType, bool) {
	queryVec, err := ec.EmbedSingle(ctx, text)
	if err != nil || len(queryVec) == 0 {
		return TurnReasoning, false
	}

	bestType := TurnReasoning
	bestSim := float64(0)
	const threshold = 0.3

	for turnType, proto := range classifyTurnPrototypes {
		protoVec, err := ec.EmbedSingle(ctx, proto)
		if err != nil {
			continue
		}
		sim := llm.CosineSimilarity(queryVec, protoVec)
		if sim > bestSim {
			bestSim = sim
			bestType = turnType
		}
	}

	if bestSim >= threshold {
		return bestType, true
	}
	return TurnReasoning, false
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

// summarizeToolOutput produces a human-readable summary of a tool's output.
// For JSON content it extracts structure (array length, error/status fields, key names)
// so that episodic memory never contains truncated/malformed JSON fragments.
// Non-JSON content falls back to safeUTF8Truncate at 150 bytes.
func summarizeToolOutput(toolName, content string) string {
	trimmed := strings.TrimSpace(content)

	// Only attempt JSON summarisation when content looks like JSON.
	if len(trimmed) > 0 && (trimmed[0] == '{' || trimmed[0] == '[') {
		if s, ok := summarizeJSON(toolName, trimmed); ok {
			return s
		}
	}

	// Fallback: plain-text truncation (Rust parity: 150 chars).
	return toolName + ": " + safeUTF8Truncate(content, 150)
}

// summarizeJSON attempts to parse content as JSON and returns a concise summary.
// Returns ("", false) if content is not valid JSON.
func summarizeJSON(toolName, content string) (string, bool) {
	// Try array first.
	var arr []json.RawMessage
	if json.Unmarshal([]byte(content), &arr) == nil {
		return safeUTF8Truncate(fmt.Sprintf("%s: %d items returned", toolName, len(arr)), 150), true
	}

	// Try object.
	var obj map[string]json.RawMessage
	if json.Unmarshal([]byte(content), &obj) != nil {
		return "", false
	}

	// Error field takes priority.
	if raw, ok := obj["error"]; ok {
		var errMsg string
		if json.Unmarshal(raw, &errMsg) != nil {
			errMsg = strings.Trim(string(raw), `"`)
		}
		return safeUTF8Truncate(fmt.Sprintf("%s: error — %s", toolName, errMsg), 150), true
	}

	// Status field.
	if raw, ok := obj["status"]; ok {
		var status string
		if json.Unmarshal(raw, &status) != nil {
			status = strings.Trim(string(raw), `"`)
		}
		return safeUTF8Truncate(fmt.Sprintf("%s: status=%s", toolName, status), 150), true
	}

	// Generic: list the first few top-level keys.
	keys := make([]string, 0, len(obj))
	for k := range obj {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	keyList := strings.Join(keys, ", ")
	return safeUTF8Truncate(fmt.Sprintf("%s: {%s}", toolName, keyList), 150), true
}

// autoIndex creates a memory_index entry for a newly stored memory.
// Rust parity: auto_index() is called after every store_* function during ingestion.
// Uses INSERT OR IGNORE so consolidation backfill won't create duplicates.
func (mm *Manager) autoIndex(ctx context.Context, sourceTable, sourceID, content string) {
	summary := content
	if len(summary) > 200 {
		summary = summary[:200]
	}
	_, err := mm.store.ExecContext(ctx,
		`INSERT OR IGNORE INTO memory_index (id, source_table, source_id, summary)
		 VALUES (?, ?, ?, ?)`,
		db.NewID(), sourceTable, sourceID, summary)
	if err != nil {
		log.Debug().Err(err).Str("source", sourceTable).Msg("auto-index: insert failed")
	}
}
