package memory

import (
	"context"
	"fmt"
	"hash/fnv"
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

// Manager handles memory ingestion and retrieval across the layered stores.
type Manager struct {
	config      Config
	store       *db.Store
	errBus      *core.ErrorBus
	embedClient *llm.EmbeddingClient
	vectorIndex db.VectorIndex
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

// SetVectorIndex attaches a vector index for incremental updates during ingestion.
func (mm *Manager) SetVectorIndex(idx db.VectorIndex) { mm.vectorIndex = idx }

// embedAndStore generates an embedding for content and persists it to the
// embeddings table. If a vector index is attached, the entry is also added
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

	// Incremental vector index update for immediate retrieval availability.
	if mm.vectorIndex != nil {
		mm.vectorIndex.AddEntry(db.VectorEntry{
			SourceTable:    sourceTable,
			SourceID:       sourceID,
			ContentPreview: preview,
			Embedding:      vec,
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
//
// Milestone 3 (canonical knowledge layer):
//   - When an existing key's value changes, bump version and refresh
//     effective_date so retrieval can prefer the latest authoritative
//     revision.
//   - When the new value matches the existing value, leave version alone so
//     idempotent re-writes do not inflate the revision counter.
func (mm *Manager) storeSemanticMemory(ctx context.Context, category, key, value string) {
	entryID := db.NewID()
	_, err := mm.store.ExecContext(ctx,
		`INSERT INTO semantic_memory (id, category, key, value, version, effective_date)
		 VALUES (?, ?, ?, ?, 1, datetime('now'))
		 ON CONFLICT(category, key) DO UPDATE SET
		     value = excluded.value,
		     updated_at = datetime('now'),
		     memory_state = 'active',
		     state_reason = NULL,
		     superseded_by = NULL,
		     version = CASE
		         WHEN semantic_memory.value = excluded.value THEN semantic_memory.version
		         ELSE semantic_memory.version + 1
		     END,
		     effective_date = CASE
		         WHEN semantic_memory.value = excluded.value THEN semantic_memory.effective_date
		         ELSE datetime('now')
		     END`,
		entryID, category, key, value,
	)
	if err != nil {
		log.Warn().Err(err).Msg("failed to store semantic memory")
		return
	}
	actualID := entryID
	_ = mm.store.QueryRowContext(ctx,
		`SELECT id FROM semantic_memory WHERE category = ? AND key = ?`,
		category, key,
	).Scan(&actualID)
	mm.autoIndex(ctx, "semantic_memory", actualID, key+": "+value)
	mm.embedAndStore(ctx, "semantic_memory", actualID, key+": "+value)
	mm.extractKnowledgeFacts(ctx, "semantic_memory", actualID, key, value)
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
				   last_interaction = datetime('now'),
				   updated_at = datetime('now')`,
				db.NewID(), entity, entity, trustScore, trustScore,
			); err != nil {
				mm.errBus.ReportIfErr(err, "memory", "ingest_relationship", core.SevWarning)
			}
		}
	}
}

// Entity extraction moved to manager_entities.go.

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

type extractedFact struct {
	Subject  string
	Relation string
	Object   string
}

var graphRelationPatterns = []struct {
	relation string
	markers  []string
}{
	{relation: "depends_on", markers: []string{" depends on ", " dependent on "}},
	{relation: "owned_by", markers: []string{" owned by ", " owner is "}},
	{relation: "uses", markers: []string{" uses ", " use ", " powered by "}},
	{relation: "blocks", markers: []string{" blocks "}},
	{relation: "blocked_by", markers: []string{" blocked by "}},
	{relation: "causes", markers: []string{" causes "}},
	{relation: "caused_by", markers: []string{" caused by "}},
	{relation: "version_of", markers: []string{" version of "}},
}

func (mm *Manager) extractKnowledgeFacts(ctx context.Context, sourceTable, sourceID, key, value string) {
	if mm.store == nil {
		return
	}
	for _, fact := range extractKnowledgeFacts(key, value) {
		factID := knowledgeFactID(sourceTable, sourceID, fact.Subject, fact.Relation, fact.Object)
		if _, err := mm.store.ExecContext(ctx,
			`INSERT INTO knowledge_facts (id, subject, relation, object, source_table, source_id, confidence)
			 VALUES (?, ?, ?, ?, ?, ?, ?)
			 ON CONFLICT(id) DO UPDATE SET
			   subject = excluded.subject,
			   relation = excluded.relation,
			   object = excluded.object,
			   source_table = excluded.source_table,
			   source_id = excluded.source_id,
			   confidence = excluded.confidence,
			   updated_at = datetime('now')`,
			factID, fact.Subject, fact.Relation, fact.Object, sourceTable, sourceID, 0.75,
		); err != nil {
			mm.errBus.ReportIfErr(err, "memory", "store_knowledge_fact", core.SevWarning)
			continue
		}
		mm.autoIndex(ctx, "knowledge_facts", factID, fact.Subject+" "+fact.Relation+" "+fact.Object)
	}
}

func extractKnowledgeFacts(key, value string) []extractedFact {
	text := strings.TrimSpace(value)
	if text == "" {
		return nil
	}

	subjectHint := strings.TrimSpace(key)
	lower := strings.ToLower(text)
	seen := make(map[string]struct{})
	var facts []extractedFact

	for _, pattern := range graphRelationPatterns {
		for _, marker := range pattern.markers {
			idx := strings.Index(lower, marker)
			if idx < 0 {
				continue
			}
			left := strings.TrimSpace(text[:idx])
			right := strings.TrimSpace(text[idx+len(marker):])
			left = strings.Trim(left, " .,:;")
			right = strings.Trim(right, " .,:;")
			if subjectHint != "" && (left == "" || strings.EqualFold(left, "the system")) {
				left = subjectHint
			}
			if left == "" || right == "" {
				continue
			}
			signature := strings.ToLower(left + "|" + pattern.relation + "|" + right)
			if _, ok := seen[signature]; ok {
				continue
			}
			seen[signature] = struct{}{}
			facts = append(facts, extractedFact{
				Subject:  left,
				Relation: pattern.relation,
				Object:   right,
			})
		}
	}

	return facts
}

func knowledgeFactID(sourceTable, sourceID, subject, relation, object string) string {
	h := fnv.New32a()
	_, _ = h.Write([]byte(sourceTable + "|" + sourceID + "|" + strings.ToLower(subject) + "|" + relation + "|" + strings.ToLower(object)))
	return fmt.Sprintf("fact_%08x", h.Sum32())
}

// Turn classification and tool output helpers moved to manager_classify.go.\n\n

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
