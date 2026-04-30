package db

import (
	"context"
	"database/sql"
)

// --- Sessions ---

// ListSessions returns sessions with basic metadata.
func (rq *RouteQueries) ListSessions(ctx context.Context, limit int) (*sql.Rows, error) {
	return rq.q.QueryContext(ctx,
		`SELECT s.id, s.agent_id, s.scope_key, s.status, s.nickname, s.created_at, s.updated_at,
		        COALESCE(COUNT(t.id), 0) AS turn_count,
		        (SELECT COUNT(*) FROM session_messages sm WHERE sm.session_id = s.id) AS message_count,
		        (SELECT COUNT(*) FROM pipeline_traces pt WHERE pt.session_id = s.id) AS trace_count,
		        (SELECT COUNT(*)
		           FROM context_snapshots cs
		           JOIN turns st ON st.id = cs.turn_id
		          WHERE st.session_id = s.id) AS snapshot_count,
		        COALESCE(SUM(COALESCE(t.tokens_in, 0) + COALESCE(t.tokens_out, 0)), 0) AS total_tokens,
		        COALESCE(SUM(COALESCE(t.cost, 0)), 0) AS total_cost,
		        MAX(
		          COALESCE(s.updated_at, s.created_at),
		          COALESCE((SELECT MAX(sm.created_at) FROM session_messages sm WHERE sm.session_id = s.id), s.created_at),
		          COALESCE((SELECT MAX(st.created_at) FROM turns st WHERE st.session_id = s.id), s.created_at),
		          COALESCE((SELECT MAX(pt.created_at) FROM pipeline_traces pt WHERE pt.session_id = s.id), s.created_at)
		        ) AS last_activity_at
		   FROM sessions s
		   LEFT JOIN turns t ON t.session_id = s.id
		  GROUP BY s.id, s.agent_id, s.scope_key, s.status, s.nickname, s.created_at, s.updated_at
		  ORDER BY s.created_at DESC LIMIT ?`, limit)
}

// GetSession returns a single session by ID.
func (rq *RouteQueries) GetSession(ctx context.Context, id string) *sql.Row {
	return rq.q.QueryRowContext(ctx,
		`SELECT id, agent_id, scope_key, status, nickname, created_at, updated_at
		 FROM sessions WHERE id = ?`, id)
}

// CountActiveSessions returns the number of active sessions.
func (rq *RouteQueries) CountActiveSessions(ctx context.Context) (int, error) {
	var count int
	err := rq.q.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM sessions WHERE status = 'active'`).Scan(&count)
	return count, err
}

// HasRecentActivity checks if there's been a pipeline trace within the last N seconds,
// indicating the primary agent is actively processing. Used by workspace to derive
// agent activity status instead of hardcoding "idle".
func (rq *RouteQueries) HasRecentActivity(ctx context.Context, withinSeconds int) (bool, error) {
	var count int
	err := rq.q.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM pipeline_traces
		 WHERE created_at > datetime('now', '-' || ? || ' seconds')`,
		withinSeconds).Scan(&count)
	return count > 0, err
}

// LatestPipelineTraceTime returns the most recent pipeline trace timestamp.
// Used by workspace to populate last_event_at.
func (rq *RouteQueries) LatestPipelineTraceTime(ctx context.Context) (sql.NullString, error) {
	var ts sql.NullString
	err := rq.q.QueryRowContext(ctx,
		`SELECT MAX(created_at) FROM pipeline_traces`).Scan(&ts)
	return ts, err
}

// ActiveTaskSummary returns the current active task goal and completion percentage.
func (rq *RouteQueries) ActiveTaskSummary(ctx context.Context) (string, int, error) {
	var goal string
	var currentStep int
	var totalSteps int
	err := rq.q.QueryRowContext(ctx,
		`SELECT t.goal, t.current_step,
		        (SELECT COUNT(*) FROM task_steps WHERE task_id = t.id) AS total_steps
		 FROM agent_tasks t WHERE t.phase = 'active' ORDER BY t.updated_at DESC LIMIT 1`,
	).Scan(&goal, &currentStep, &totalSteps)
	if err != nil {
		return "", 0, err
	}
	pct := 0
	if totalSteps > 0 {
		pct = (currentStep * 100) / totalSteps
	}
	return goal, pct, nil
}

// SessionMessages returns messages for a session.
func (rq *RouteQueries) SessionMessages(ctx context.Context, sessionID string, limit int) (*sql.Rows, error) {
	return rq.q.QueryContext(ctx,
		`SELECT id, role, content, created_at FROM session_messages
		 WHERE session_id = ? ORDER BY created_at ASC LIMIT ?`, sessionID, limit)
}

// SessionMessageCount returns the message count for a session.
func (rq *RouteQueries) SessionMessageCount(ctx context.Context, sessionID string) (int, error) {
	var count int
	err := rq.q.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM session_messages WHERE session_id = ?`, sessionID).Scan(&count)
	return count, err
}

// --- Turns ---

// ListTurnsBySession returns turns for a session.
func (rq *RouteQueries) ListTurnsBySession(ctx context.Context, sessionID string, limit int) (*sql.Rows, error) {
	return rq.q.QueryContext(ctx,
		`SELECT id, session_id, thinking, tool_calls_json, tokens_in, tokens_out, cost, model, created_at
		 FROM turns WHERE session_id = ? ORDER BY created_at DESC LIMIT ?`, sessionID, limit)
}

// --- Session Analytics ---

// SessionsWithoutNicknames returns sessions lacking nicknames with their first user message.
func (rq *RouteQueries) SessionsWithoutNicknames(ctx context.Context) (*sql.Rows, error) {
	return rq.q.QueryContext(ctx,
		`SELECT s.id, (
			SELECT content FROM session_messages
			WHERE session_id = s.id AND role = 'user'
			ORDER BY created_at ASC LIMIT 1
		) AS first_msg
		FROM sessions s
		WHERE s.nickname IS NULL OR s.nickname = ''`)
}

// SessionExists checks if a session exists and returns its created_at.
func (rq *RouteQueries) SessionExists(ctx context.Context, id string) *sql.Row {
	return rq.q.QueryRowContext(ctx, `SELECT created_at FROM sessions WHERE id = ?`, id)
}

// ListTurnsForAnalysis returns turns with token/cost data for session analysis.
func (rq *RouteQueries) ListTurnsForAnalysis(ctx context.Context, sessionID string) (*sql.Rows, error) {
	return rq.q.QueryContext(ctx,
		`SELECT t.id, COALESCE(t.model, ''), COALESCE(t.tokens_in, 0), COALESCE(t.tokens_out, 0),
		        COALESCE(t.cost, 0), COALESCE(t.cached, 0)
		 FROM turns t WHERE t.session_id = ? ORDER BY t.created_at`, sessionID)
}

// ContextSnapshotForTurn returns context snapshot data for a turn.
func (rq *RouteQueries) ContextSnapshotForTurn(ctx context.Context, turnID string) *sql.Row {
	return rq.q.QueryRowContext(ctx,
		`SELECT COALESCE(token_budget, 0), COALESCE(system_prompt_tokens, 0), COALESCE(memory_tokens, 0),
		        COALESCE(history_tokens, 0), COALESCE(history_depth, 0)
		 FROM context_snapshots WHERE turn_id = ?`, turnID)
}

// ToolCallCountsForTurn returns tool call count and failure count for a turn.
func (rq *RouteQueries) ToolCallCountsForTurn(ctx context.Context, turnID string) *sql.Row {
	return rq.q.QueryRowContext(ctx,
		`SELECT COUNT(*), COALESCE(SUM(CASE WHEN status != 'success' THEN 1 ELSE 0 END), 0)
		 FROM tool_calls WHERE turn_id = ?`, turnID)
}

// SessionFeedbackGrades returns turn feedback grades for a session.
func (rq *RouteQueries) SessionFeedbackGrades(ctx context.Context, sessionID string) (*sql.Rows, error) {
	return rq.q.QueryContext(ctx,
		`SELECT turn_id, COALESCE(grade, 0) FROM turn_feedback WHERE session_id = ?`, sessionID)
}

// --- Session Detail ---

// SessionTurnsWithMessages returns session turns joined with messages.
func (rq *RouteQueries) SessionTurnsWithMessages(ctx context.Context, sessionID string) (*sql.Rows, error) {
	return rq.q.QueryContext(ctx,
		`SELECT t.id,
		        'turn' AS role,
		        COALESCE((
		          SELECT sm.content
		            FROM session_messages sm
		           WHERE sm.session_id = t.session_id
		             AND sm.role = 'user'
		             AND sm.created_at <= t.created_at
		           ORDER BY sm.created_at DESC
		           LIMIT 1
		        ), '') AS content,
		        t.created_at,
		        COALESCE(t.model, ''), COALESCE(t.cost, 0.0),
		        COALESCE(t.tokens_in, 0), COALESCE(t.tokens_out, 0)
		   FROM turns t
		  WHERE t.session_id = ?
		  ORDER BY t.created_at`, sessionID)
}

// SessionFeedback returns turn feedback for a session.
func (rq *RouteQueries) SessionFeedback(ctx context.Context, sessionID string) (*sql.Rows, error) {
	return rq.q.QueryContext(ctx,
		`SELECT tf.id, tf.turn_id, tf.grade, tf.source, tf.comment, tf.created_at
		 FROM turn_feedback tf
		 WHERE tf.session_id = ?
		 ORDER BY tf.created_at DESC`, sessionID)
}

// SessionTurnStats returns turn count, total tokens, and total cost for a session.
func (rq *RouteQueries) SessionTurnStats(ctx context.Context, sessionID string) *sql.Row {
	return rq.q.QueryRowContext(ctx,
		`SELECT COUNT(*), COALESCE(SUM(tokens_in + tokens_out), 0), COALESCE(SUM(cost), 0)
		 FROM turns WHERE session_id = ?`, sessionID)
}

// SessionToolCallCount returns the number of tool calls in a session.
func (rq *RouteQueries) SessionToolCallCount(ctx context.Context, sessionID string) *sql.Row {
	return rq.q.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM tool_calls tc
		 JOIN turns t ON t.id = tc.turn_id
		 WHERE t.session_id = ?`, sessionID)
}

// --- Turn Detail ---

// TurnMessages returns messages for a specific turn.
func (rq *RouteQueries) TurnMessages(ctx context.Context, turnID string) (*sql.Rows, error) {
	return rq.q.QueryContext(ctx,
		`SELECT id, role, content, created_at FROM session_messages
		 WHERE id = ? OR (session_id = (SELECT session_id FROM turns WHERE id = ?) AND created_at <= (SELECT created_at FROM turns WHERE id = ?))
		 ORDER BY created_at ASC`, turnID, turnID, turnID)
}

// TurnToolCalls returns tool calls for a turn.
func (rq *RouteQueries) TurnToolCalls(ctx context.Context, turnID string) (*sql.Rows, error) {
	return rq.q.QueryContext(ctx,
		`SELECT id, tool_name, arguments_json, output, status, duration_ms, created_at
		 FROM tool_calls WHERE turn_id = ? ORDER BY created_at ASC`, turnID)
}

// TurnContextSnapshot returns the context snapshot for a turn.
func (rq *RouteQueries) TurnContextSnapshot(ctx context.Context, turnID string) *sql.Row {
	return rq.q.QueryRowContext(ctx,
		`SELECT token_budget, system_prompt_tokens, memory_tokens, history_tokens, history_depth, 0
		 FROM context_snapshots WHERE turn_id = ?`, turnID)
}

// GetTurnContext returns context snapshot fields for a turn.
func (rq *RouteQueries) GetTurnContext(ctx context.Context, turnID string) *sql.Row {
	return rq.q.QueryRowContext(ctx,
		`SELECT complexity_level, token_budget, system_prompt_tokens, memory_tokens,
		        history_tokens, history_depth, model
		 FROM context_snapshots WHERE turn_id = ?`, turnID)
}

// GetTurnToolsDetailed returns tool calls for a turn including skill_name.
func (rq *RouteQueries) GetTurnToolsDetailed(ctx context.Context, turnID string) (*sql.Rows, error) {
	return rq.q.QueryContext(ctx,
		`SELECT id, tool_name, input, output, status, duration_ms, skill_name, created_at
		 FROM tool_calls WHERE turn_id = ? ORDER BY created_at`, turnID)
}

// GetTurnTokens returns token/cost data for a turn.
func (rq *RouteQueries) GetTurnTokens(ctx context.Context, turnID string) *sql.Row {
	return rq.q.QueryRowContext(ctx,
		`SELECT COALESCE(tokens_in, 0), COALESCE(tokens_out, 0), COALESCE(cost, 0)
		 FROM turns WHERE id = ?`, turnID)
}

// GetTurnModelSelection returns model selection details for a turn.
func (rq *RouteQueries) GetTurnModelSelection(ctx context.Context, turnID string) *sql.Row {
	return rq.q.QueryRowContext(ctx,
		`SELECT id, selected_model, strategy, primary_model, override_model,
		        complexity, candidates_json, attribution, created_at
		 FROM model_selection_events WHERE turn_id = ? LIMIT 1`, turnID)
}

// GetTurnDiagnostics returns the canonical diagnostics summary for a turn.
func (rq *RouteQueries) GetTurnDiagnostics(ctx context.Context, turnID string) *sql.Row {
	return rq.q.QueryRowContext(ctx,
		`SELECT id, turn_id, session_id, channel, status, COALESCE(final_model, ''),
		        COALESCE(final_provider, ''), total_ms, inference_attempts, fallback_count,
		        tool_call_count, guard_retry_count, verifier_retry_count, replay_suppression_count, request_messages,
		        request_tools, request_approx_tokens, COALESCE(context_pressure, ''),
		        COALESCE(resource_pressure, ''), COALESCE(resource_snapshot_json, ''), COALESCE(primary_diagnosis, ''),
		        diagnosis_confidence, COALESCE(user_narrative, ''), COALESCE(operator_narrative, ''),
		        COALESCE(recommendations_json, ''), created_at
		   FROM turn_diagnostics
		  WHERE turn_id = ?
		  LIMIT 1`, turnID)
}

// ListTurnDiagnosticEvents returns the ordered event stream for a turn.
func (rq *RouteQueries) ListTurnDiagnosticEvents(ctx context.Context, turnID string) (*sql.Rows, error) {
	return rq.q.QueryContext(ctx,
		`SELECT id, turn_id, seq, event_type, at_ms, duration_ms, COALESCE(parent_event_id, ''),
		        status, COALESCE(operator_summary, ''), COALESCE(user_summary, ''),
		        COALESCE(details_json, ''), created_at
		   FROM turn_diagnostic_events
		  WHERE turn_id = ?
		  ORDER BY seq ASC`, turnID)
}

// TurnCachedFlag returns the cached flag for a turn.
func (rq *RouteQueries) TurnCachedFlag(ctx context.Context, turnID string) *sql.Row {
	return rq.q.QueryRowContext(ctx,
		`SELECT COALESCE(cached, 0) FROM turns WHERE id = ?`, turnID)
}

// GetTurnForAnalysis returns model/token/cost data for turn analysis.
func (rq *RouteQueries) GetTurnForAnalysis(ctx context.Context, turnID string) *sql.Row {
	return rq.q.QueryRowContext(ctx,
		`SELECT model, COALESCE(tokens_in, 0), COALESCE(tokens_out, 0), COALESCE(cost, 0)
		 FROM turns WHERE id = ?`, turnID)
}

// GetContextSnapshotForAnalysis returns context snapshot data for turn analysis.
func (rq *RouteQueries) GetContextSnapshotForAnalysis(ctx context.Context, turnID string) *sql.Row {
	return rq.q.QueryRowContext(ctx,
		`SELECT COALESCE(token_budget, 0), COALESCE(system_prompt_tokens, 0), COALESCE(memory_tokens, 0),
		        COALESCE(history_tokens, 0), COALESCE(history_depth, 0), COALESCE(complexity_level, '')
		 FROM context_snapshots WHERE turn_id = ?`, turnID)
}

// GetTurnFeedbackByTurnID returns feedback for a specific turn.
func (rq *RouteQueries) GetTurnFeedbackByTurnID(ctx context.Context, turnID string) *sql.Row {
	return rq.q.QueryRowContext(ctx,
		`SELECT id, turn_id, session_id, grade, source, comment, created_at
		 FROM turn_feedback
		 WHERE turn_id = ?
		 ORDER BY created_at DESC
		 LIMIT 1`, turnID)
}

// GetSessionIDForTurn returns the session_id for a turn.
func (rq *RouteQueries) GetSessionIDForTurn(ctx context.Context, turnID string) *sql.Row {
	return rq.q.QueryRowContext(ctx,
		`SELECT session_id FROM turns WHERE id = ?`, turnID)
}

// --- Turn Feedback Mutations ---

// InsertTurnFeedback creates a new turn feedback entry.
func (rq *RouteQueries) InsertTurnFeedback(ctx context.Context, id, turnID, sessionID string, grade int, comment string) error {
	_, err := rq.q.ExecContext(ctx,
		`INSERT INTO turn_feedback (id, turn_id, session_id, grade, comment)
		 VALUES (?, ?, ?, ?, ?)`, id, turnID, sessionID, grade, comment)
	return err
}

// UpdateTurnFeedback updates an existing turn feedback entry by turn_id.
func (rq *RouteQueries) UpdateTurnFeedback(ctx context.Context, turnID string, grade int, comment string) (int64, error) {
	res, err := rq.q.ExecContext(ctx,
		`UPDATE turn_feedback SET grade = ?, comment = ? WHERE turn_id = ?`,
		grade, comment, turnID)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// ListTurnMessages returns messages by id or session_id for a turn.
func (rq *RouteQueries) ListTurnMessages(ctx context.Context, id string, limit int) (*sql.Rows, error) {
	return rq.q.QueryContext(ctx,
		`SELECT id, role, content, created_at FROM session_messages WHERE id = ? OR session_id = ? LIMIT ?`, id, id, limit)
}
