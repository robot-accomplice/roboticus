package db

import (
	"context"
	"database/sql"

	"github.com/rs/zerolog/log"
)

// Querier is the minimal interface for database read/write operations.
// Components should depend on this interface rather than *Store directly,
// enabling testing with in-memory stubs and enforcing the dependency rule.
type Querier interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// Ensure *Store satisfies Querier at compile time.
var _ Querier = (*Store)(nil)

// SessionRepository handles session and message persistence.
type SessionRepository struct {
	q Querier
}

// NewSessionRepository creates a session repository.
func NewSessionRepository(q Querier) *SessionRepository {
	return &SessionRepository{q: q}
}

// CreateSession inserts a new session and returns its ID.
func (r *SessionRepository) CreateSession(ctx context.Context, id, agentID, scopeKey string) error {
	_, err := r.q.ExecContext(ctx,
		`INSERT INTO sessions (id, agent_id, scope_key) VALUES (?, ?, ?)`,
		id, agentID, scopeKey)
	return err
}

// ArchiveSession sets a session's status to 'archived'.
func (r *SessionRepository) ArchiveSession(ctx context.Context, sessionID string) error {
	_, err := r.q.ExecContext(ctx,
		`UPDATE sessions SET status = 'archived' WHERE id = ?`, sessionID)
	return err
}

// DeleteSession removes a session and all dependent records.
// Must delete child rows in dependency order before the parent session,
// because SQLite enforces FK constraints when foreign_keys=ON.
func (r *SessionRepository) DeleteSession(ctx context.Context, sessionID string) error {
	// Dependency graph (children → parent):
	//   tool_calls → turns → sessions
	//   turn_feedback → turns, sessions
	//   context_snapshots → turns
	//   delegation_outcomes → turns, sessions
	//   consent_requests → sessions
	//   context_checkpoints → sessions
	//   pipeline_traces → sessions (+ react_traces → pipeline_traces)
	//   session_messages → sessions

	// 1. Get turn IDs for this session (needed for grandchild cleanup).
	turnIDs, err := r.collectTurnIDs(ctx, sessionID)
	if err != nil {
		log.Warn().Err(err).Str("session", sessionID).Msg("db: failed to collect turn IDs for cascade delete")
	}

	// 2. Delete grandchild records (reference turns).
	for _, turnID := range turnIDs {
		_, _ = r.q.ExecContext(ctx, `DELETE FROM tool_calls WHERE turn_id = ?`, turnID)
		_, _ = r.q.ExecContext(ctx, `DELETE FROM context_snapshots WHERE turn_id = ?`, turnID)
	}

	// 3. Delete child records referencing turns or sessions.
	_, _ = r.q.ExecContext(ctx, `DELETE FROM turn_feedback WHERE session_id = ?`, sessionID)
	_, _ = r.q.ExecContext(ctx, `DELETE FROM delegation_outcomes WHERE session_id = ?`, sessionID)
	_, _ = r.q.ExecContext(ctx, `DELETE FROM turns WHERE session_id = ?`, sessionID)

	// 4. Delete child records referencing only sessions.
	_, _ = r.q.ExecContext(ctx, `DELETE FROM session_messages WHERE session_id = ?`, sessionID)
	_, _ = r.q.ExecContext(ctx, `DELETE FROM context_checkpoints WHERE session_id = ?`, sessionID)
	_, _ = r.q.ExecContext(ctx, `DELETE FROM consent_requests WHERE session_id = ?`, sessionID)

	// 5. Delete pipeline traces and their react_traces.
	_, _ = r.q.ExecContext(ctx,
		`DELETE FROM react_traces WHERE trace_id IN (SELECT id FROM pipeline_traces WHERE session_id = ?)`,
		sessionID)
	_, _ = r.q.ExecContext(ctx, `DELETE FROM pipeline_traces WHERE session_id = ?`, sessionID)

	// 6. Finally delete the session itself.
	_, err = r.q.ExecContext(ctx, `DELETE FROM sessions WHERE id = ?`, sessionID)
	return err
}

// collectTurnIDs returns all turn IDs for a session (for grandchild cleanup).
func (r *SessionRepository) collectTurnIDs(ctx context.Context, sessionID string) ([]string, error) {
	rows, err := r.q.QueryContext(ctx, `SELECT id FROM turns WHERE session_id = ?`, sessionID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			continue
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// SetNickname updates a session's nickname.
func (r *SessionRepository) SetNickname(ctx context.Context, sessionID, nickname string) error {
	_, err := r.q.ExecContext(ctx,
		`UPDATE sessions SET nickname = ? WHERE id = ?`,
		nickname, sessionID)
	return err
}

// FindActiveSession looks up an active session by agent and scope key.
func (r *SessionRepository) FindActiveSession(ctx context.Context, agentID, scopeKey string) (string, error) {
	var id string
	err := r.q.QueryRowContext(ctx,
		`SELECT id FROM sessions WHERE agent_id = ? AND scope_key = ? AND status = 'active'
		 ORDER BY created_at DESC LIMIT 1`,
		agentID, scopeKey).Scan(&id)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return id, err
}

// StoreMessage inserts a message into session_messages.
func (r *SessionRepository) StoreMessage(ctx context.Context, id, sessionID, role, content string) error {
	_, err := r.q.ExecContext(ctx,
		`INSERT INTO session_messages (id, session_id, role, content) VALUES (?, ?, ?, ?)`,
		id, sessionID, role, content)
	return err
}

// LoadMessages loads recent messages for a session.
func (r *SessionRepository) LoadMessages(ctx context.Context, sessionID string, limit int) ([]SessionMessage, error) {
	rows, err := r.q.QueryContext(ctx,
		`SELECT id, role, content, created_at FROM session_messages
		 WHERE session_id = ? ORDER BY created_at ASC LIMIT ?`,
		sessionID, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var msgs []SessionMessage
	for rows.Next() {
		var m SessionMessage
		if err := rows.Scan(&m.ID, &m.Role, &m.Content, &m.CreatedAt); err != nil {
			continue
		}
		msgs = append(msgs, m)
	}
	return msgs, nil
}

// SessionMessage represents a stored message.
type SessionMessage struct {
	ID        string
	Role      string
	Content   string
	CreatedAt string
}

// CacheStore is the persistence interface for the LLM semantic cache.
type CacheStore interface {
	GetCachedResponse(ctx context.Context, key string) (string, bool, error)
	PutCachedResponse(ctx context.Context, key, model, response string, tokensIn, tokensOut int) error
}

// RecordInferenceCost stores inference cost data.
func (r *SessionRepository) RecordInferenceCost(ctx context.Context, id, model, provider string, tokensIn, tokensOut int, cost float64) error {
	_, err := r.q.ExecContext(ctx,
		`INSERT INTO inference_costs (id, model, provider, tokens_in, tokens_out, cost)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		id, model, provider, tokensIn, tokensOut, cost)
	return err
}

// HippocampusRegistry manages the schema-on-write table registry.
type HippocampusRegistry struct {
	q Querier
}

// NewHippocampusRegistry creates a hippocampus registry.
func NewHippocampusRegistry(q Querier) *HippocampusRegistry {
	return &HippocampusRegistry{q: q}
}

// RegisterTable records a table's existence and schema in the hippocampus.
func (h *HippocampusRegistry) RegisterTable(ctx context.Context, tableName, description, columnsJSON string) error {
	_, err := h.q.ExecContext(ctx,
		`INSERT INTO hippocampus (table_name, description, columns_json)
		 VALUES (?, ?, ?)
		 ON CONFLICT(table_name) DO UPDATE SET
		   description = excluded.description,
		   columns_json = excluded.columns_json,
		   updated_at = datetime('now')`,
		tableName, description, columnsJSON)
	return err
}

// ListTables returns all registered tables.
func (h *HippocampusRegistry) ListTables(ctx context.Context) ([]TableEntry, error) {
	rows, err := h.q.QueryContext(ctx,
		`SELECT table_name, description, columns_json, agent_owned FROM hippocampus ORDER BY table_name`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var entries []TableEntry
	for rows.Next() {
		var e TableEntry
		if err := rows.Scan(&e.Name, &e.Description, &e.ColumnsJSON, &e.AgentOwned); err != nil {
			continue
		}
		entries = append(entries, e)
	}
	return entries, nil
}

// SyncBuiltinTables registers all known schema tables in the hippocampus.
func (h *HippocampusRegistry) SyncBuiltinTables(ctx context.Context) error {
	tables := []TableEntry{
		{"sessions", "Active and archived agent sessions", `["id","agent_id","scope_key","status","model","nickname","created_at","updated_at","metadata","cross_channel_consent"]`, false},
		{"session_messages", "Messages within sessions", `["id","session_id","parent_id","role","content","usage_json","created_at"]`, false},
		{"turns", "Agent reasoning turns", `["id","session_id","thinking","input_tokens","output_tokens","model","latency_ms","created_at"]`, false},
		{"working_memory", "Active session context", `["id","session_id","entry_type","content","importance","created_at"]`, false},
		{"episodic_memory", "Past events with temporal decay", `["id","classification","content","importance","created_at"]`, false},
		{"semantic_memory", "Structured knowledge", `["id","category","key","value","confidence","created_at","updated_at"]`, false},
		{"procedural_memory", "Tool usage statistics", `["id","name","steps","success_count","failure_count","created_at","updated_at"]`, false},
		{"relationship_memory", "Entity interaction tracking", `["id","entity_id","entity_name","trust_score","interaction_summary","interaction_count","last_interaction","created_at"]`, false},
		{"cron_jobs", "Scheduled jobs", `["id","name","description","enabled","schedule_kind","schedule_expr","schedule_every_ms","schedule_tz","agent_id","session_target","payload_json","delivery_mode","delivery_channel","last_run_at","last_status","next_run_at"]`, false},
		{"cron_runs", "Job execution history", `["id","job_id","status","duration_ms","error","output_text","created_at"]`, false},
		{"inference_costs", "LLM inference cost tracking", `["id","model","provider","tokens_in","tokens_out","cost","tier","cached","created_at"]`, false},
		{"semantic_cache", "LLM response cache", `["id","prompt_hash","embedding","response","model","tokens_saved","hit_count","created_at","updated_at"]`, false},
		{"delivery_queue", "Outbound message delivery", `["id","channel","recipient_id","content","idempotency_key","status","attempts","max_attempts","next_retry_at","last_error","created_at"]`, false},
		{"sub_agents", "Registered sub-agents", `["id","name","display_name","model","fallback_models_json","role","description","skills_json","enabled","created_at"]`, false},
		{"embeddings", "Vector embeddings", `["id","source_table","source_id","content_preview","embedding_blob","dimensions","created_at"]`, false},
		{"turn_feedback", "User feedback on turns", `["id","turn_id","session_id","grade","source","comment","created_at"]`, false},
		{"hippocampus", "Schema registry (this table)", `["table_name","description","columns_json","created_by","agent_owned","created_at","updated_at"]`, false},
	}

	for _, t := range tables {
		if err := h.RegisterTable(ctx, t.Name, t.Description, t.ColumnsJSON); err != nil {
			return err
		}
	}
	return nil
}

// TableEntry represents a registered table.
type TableEntry struct {
	Name        string
	Description string
	ColumnsJSON string
	AgentOwned  bool
}
