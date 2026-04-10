package db

import (
	"context"
	"database/sql"
)

// RouteQueries provides read-only query methods that route handlers need.
// All domain lifecycle mutations (INSERT/UPDATE/DELETE) live in domain-specific
// repos. These queries are pure reads for response formatting.
//
// This consolidation exists to move SQL out of route files per architecture_rules.md
// while keeping the transition incremental. Each method here should eventually
// migrate to its domain-specific repo as those repos grow.
type RouteQueries struct {
	q Querier
}

// NewRouteQueries creates a route query helper.
func NewRouteQueries(q Querier) *RouteQueries {
	return &RouteQueries{q: q}
}

// --- Sessions ---

// ListSessions returns sessions with basic metadata.
func (rq *RouteQueries) ListSessions(ctx context.Context, limit int) (*sql.Rows, error) {
	return rq.q.QueryContext(ctx,
		`SELECT id, agent_id, scope_key, status, nickname, created_at, updated_at
		 FROM sessions ORDER BY created_at DESC LIMIT ?`, limit)
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

// --- Skills ---

// ListSkillsAll returns all skills for catalog display.
func (rq *RouteQueries) ListSkillsAll(ctx context.Context) (*sql.Rows, error) {
	return rq.q.QueryContext(ctx,
		`SELECT id, name, kind, description, version, risk_level, enabled, created_at
		 FROM skills ORDER BY name`)
}

// GetSkillByID returns a skill by ID.
func (rq *RouteQueries) GetSkillByID(ctx context.Context, id string) *sql.Row {
	return rq.q.QueryRowContext(ctx,
		`SELECT id, name, kind, description, enabled, version, risk_level, created_at
		 FROM skills WHERE id = ?`, id)
}

// CountSkills returns total and enabled skill counts.
func (rq *RouteQueries) CountSkills(ctx context.Context) (total, enabled int, err error) {
	err = rq.q.QueryRowContext(ctx, `SELECT COUNT(*) FROM skills`).Scan(&total)
	if err != nil {
		return
	}
	err = rq.q.QueryRowContext(ctx, `SELECT COUNT(*) FROM skills WHERE enabled = 1`).Scan(&enabled)
	return
}

// --- Sub-agents ---

// ListSubAgents returns all sub-agents with full detail.
func (rq *RouteQueries) ListSubAgents(ctx context.Context) (*sql.Rows, error) {
	return rq.q.QueryContext(ctx,
		`SELECT id, name, display_name, model, role, description, skills_json, enabled, session_count, last_used_at, created_at
		 FROM sub_agents ORDER BY name ASC`)
}

// GetSubAgentByName returns a sub-agent by name.
func (rq *RouteQueries) GetSubAgentByName(ctx context.Context, name string) *sql.Row {
	return rq.q.QueryRowContext(ctx,
		`SELECT id, name, display_name, model, role, description, skills_json, enabled, session_count, last_used_at, created_at
		 FROM sub_agents WHERE name = ?`, name)
}

// --- Turns ---

// ListTurnsBySession returns turns for a session.
func (rq *RouteQueries) ListTurnsBySession(ctx context.Context, sessionID string, limit int) (*sql.Rows, error) {
	return rq.q.QueryContext(ctx,
		`SELECT id, session_id, thinking, tool_calls_json, tokens_in, tokens_out, cost, model, created_at
		 FROM turns WHERE session_id = ? ORDER BY created_at DESC LIMIT ?`, sessionID, limit)
}

// --- Pipeline Traces ---

// ListPipelineTraces returns recent pipeline traces.
func (rq *RouteQueries) ListPipelineTraces(ctx context.Context, limit int) (*sql.Rows, error) {
	return rq.q.QueryContext(ctx,
		`SELECT id, turn_id, session_id, channel, total_ms, stages_json, created_at
		 FROM pipeline_traces ORDER BY created_at DESC LIMIT ?`, limit)
}

// GetPipelineTrace returns a trace by ID.
func (rq *RouteQueries) GetPipelineTrace(ctx context.Context, id string) *sql.Row {
	return rq.q.QueryRowContext(ctx,
		`SELECT id, turn_id, session_id, channel, total_ms, stages_json, created_at
		 FROM pipeline_traces WHERE id = ?`, id)
}

// --- Inference Costs ---

// ListInferenceCosts returns recent inference costs.
func (rq *RouteQueries) ListInferenceCosts(ctx context.Context, hours, limit int) (*sql.Rows, error) {
	return rq.q.QueryContext(ctx,
		`SELECT id, model, provider, tokens_in, tokens_out, cost, latency_ms, quality_score, escalation, turn_id, cached, created_at
		 FROM inference_costs WHERE created_at >= datetime('now', '-' || ? || ' hours')
		 ORDER BY created_at DESC LIMIT ?`, hours, limit)
}

// TotalCostSince returns the total inference cost since the given hours ago.
func (rq *RouteQueries) TotalCostSince(ctx context.Context, hours int) (float64, error) {
	var total float64
	err := rq.q.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(cost), 0) FROM inference_costs
		 WHERE created_at >= datetime('now', '-' || ? || ' hours')`, hours).Scan(&total)
	return total, err
}

// --- Tool Calls ---

// ListToolCallsByTurn returns tool calls for a turn.
func (rq *RouteQueries) ListToolCallsByTurn(ctx context.Context, turnID string) (*sql.Rows, error) {
	return rq.q.QueryContext(ctx,
		`SELECT id, turn_id, tool_name, arguments_json, output, status, duration_ms, created_at
		 FROM tool_calls WHERE turn_id = ? ORDER BY created_at ASC`, turnID)
}

// --- Cron Jobs ---

// ListCronJobs returns all cron jobs.
func (rq *RouteQueries) ListCronJobs(ctx context.Context) (*sql.Rows, error) {
	return rq.q.QueryContext(ctx,
		`SELECT id, name, description, enabled, schedule_kind, schedule_expr,
		        schedule_every_ms, agent_id, payload_json, last_run_at, last_status, next_run_at
		 FROM cron_jobs ORDER BY name`)
}

// GetCronJob returns a cron job by ID.
func (rq *RouteQueries) GetCronJob(ctx context.Context, id string) *sql.Row {
	return rq.q.QueryRowContext(ctx,
		`SELECT id, name, description, enabled, schedule_kind, schedule_expr, schedule_every_ms,
		        agent_id, payload_json, last_run_at, last_status, next_run_at
		 FROM cron_jobs WHERE id = ?`, id)
}

// GetCronJobPayload returns just the payload_json for a cron job.
func (rq *RouteQueries) GetCronJobPayload(ctx context.Context, id string) *sql.Row {
	return rq.q.QueryRowContext(ctx,
		`SELECT payload_json FROM cron_jobs WHERE id = ? AND enabled = 1`, id)
}

// ListCronRuns returns recent cron runs.
func (rq *RouteQueries) ListCronRuns(ctx context.Context, limit int) (*sql.Rows, error) {
	return rq.q.QueryContext(ctx,
		`SELECT id, job_id, status, duration_ms, error_msg, '', timestamp
		 FROM cron_runs ORDER BY timestamp DESC LIMIT ?`, limit)
}

// --- Working Memory ---

// ListWorkingMemory returns working memory entries, optionally filtered by session.
func (rq *RouteQueries) ListWorkingMemory(ctx context.Context, sessionID string, limit int) (*sql.Rows, error) {
	if sessionID != "" {
		return rq.q.QueryContext(ctx,
			`SELECT id, session_id, entry_type, content, importance, created_at
			 FROM working_memory WHERE session_id = ? ORDER BY created_at DESC LIMIT ?`, sessionID, limit)
	}
	return rq.q.QueryContext(ctx,
		`SELECT id, session_id, entry_type, content, importance, created_at
		 FROM working_memory ORDER BY created_at DESC LIMIT ?`, limit)
}

// --- Episodic Memory ---

// ListEpisodicMemory returns recent episodic memories.
func (rq *RouteQueries) ListEpisodicMemory(ctx context.Context, limit int) (*sql.Rows, error) {
	return rq.q.QueryContext(ctx,
		`SELECT id, classification, content, importance, created_at
		 FROM episodic_memory ORDER BY created_at DESC LIMIT ?`, limit)
}

// --- Semantic Memory ---

// ListSemanticMemory returns semantic memory entries.
func (rq *RouteQueries) ListSemanticMemory(ctx context.Context, category string, limit int) (*sql.Rows, error) {
	if category != "" {
		return rq.q.QueryContext(ctx,
			`SELECT id, category, key, value, confidence
			 FROM semantic_memory WHERE category = ? ORDER BY confidence DESC LIMIT ?`, category, limit)
	}
	return rq.q.QueryContext(ctx,
		`SELECT id, category, key, value, confidence, created_at
		 FROM semantic_memory ORDER BY category, key LIMIT ?`, limit)
}

// --- Context Snapshots ---

// ListContextSnapshots returns context snapshots for a session.
func (rq *RouteQueries) ListContextSnapshots(ctx context.Context, sessionID string, limit int) (*sql.Rows, error) {
	return rq.q.QueryContext(ctx,
		`SELECT id, session_id, system_prompt_hash, memory_summary, conversation_digest, turn_count, created_at
		 FROM context_checkpoints WHERE session_id = ? ORDER BY created_at DESC LIMIT ?`, sessionID, limit)
}

// --- Delegation Outcomes ---

// ListDelegationOutcomes returns recent delegation outcomes.
func (rq *RouteQueries) ListDelegationOutcomes(ctx context.Context, limit int) (*sql.Rows, error) {
	return rq.q.QueryContext(ctx,
		`SELECT id, session_id, turn_id, task_description, assigned_agents_json,
		        pattern, duration_ms, success, quality_score, created_at
		 FROM delegation_outcomes ORDER BY created_at DESC LIMIT ?`, limit)
}

// --- Model Selection Events ---

// ListModelSelections returns recent model selection events.
func (rq *RouteQueries) ListModelSelections(ctx context.Context, limit int) (*sql.Rows, error) {
	return rq.q.QueryContext(ctx,
		`SELECT id, turn_id, session_id, agent_id, channel, selected_model, strategy, primary_model, user_excerpt, created_at
		 FROM model_selection_events ORDER BY created_at DESC LIMIT ?`, limit)
}

// --- Abuse Events ---

// ListAbuseEvents returns recent abuse events.
func (rq *RouteQueries) ListAbuseEvents(ctx context.Context, limit int) (*sql.Rows, error) {
	return rq.q.QueryContext(ctx,
		`SELECT id, event_type, severity, source, details, created_at
		 FROM abuse_events ORDER BY created_at DESC LIMIT ?`, limit)
}

// --- Turn Feedback ---

// ListTurnFeedback returns recent turn feedback.
func (rq *RouteQueries) ListTurnFeedback(ctx context.Context, limit int) (*sql.Rows, error) {
	return rq.q.QueryContext(ctx,
		`SELECT id, turn_id, session_id, grade, comment, created_at
		 FROM turn_feedback ORDER BY created_at DESC LIMIT ?`, limit)
}

// --- Themes ---

// ListThemes returns installed themes.
func (rq *RouteQueries) ListThemes(ctx context.Context) (*sql.Rows, error) {
	return rq.q.QueryContext(ctx,
		`SELECT id, name, source, version, active, created_at FROM installed_themes ORDER BY name ASC`)
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
		`SELECT COALESCE(max_tokens, 0), COALESCE(system_tokens, 0), COALESCE(memory_tokens, 0),
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
		`SELECT sm.id, sm.role, sm.content, sm.created_at,
		        COALESCE(t.model, ''), COALESCE(t.cost, 0.0),
		        COALESCE(t.tokens_in, 0), COALESCE(t.tokens_out, 0)
		 FROM session_messages sm
		 LEFT JOIN turns t ON t.id = sm.id AND t.session_id = sm.session_id
		 WHERE sm.session_id = ? ORDER BY sm.created_at`, sessionID)
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

// SemanticCategories returns semantic memory categories with counts.
func (rq *RouteQueries) SemanticCategories(ctx context.Context) (*sql.Rows, error) {
	return rq.q.QueryContext(ctx,
		`SELECT category, COUNT(*) as cnt FROM semantic_memory GROUP BY category ORDER BY cnt DESC`)
}

// --- Memory Search ---

// SearchWorkingMemory searches working memory by content pattern.
func (rq *RouteQueries) SearchWorkingMemory(ctx context.Context, pattern string, limit int) (*sql.Rows, error) {
	return rq.q.QueryContext(ctx,
		`SELECT id, 'working' as tier, entry_type, content, created_at
		 FROM working_memory WHERE content LIKE ? LIMIT ?`, pattern, limit)
}

// SearchEpisodicMemory searches episodic memory by content pattern.
func (rq *RouteQueries) SearchEpisodicMemory(ctx context.Context, pattern string, limit int) (*sql.Rows, error) {
	return rq.q.QueryContext(ctx,
		`SELECT id, 'episodic' as tier, classification, content, created_at
		 FROM episodic_memory WHERE content LIKE ? LIMIT ?`, pattern, limit)
}

// SearchSemanticMemory searches semantic memory by value or key pattern.
func (rq *RouteQueries) SearchSemanticMemory(ctx context.Context, pattern string, limit int) (*sql.Rows, error) {
	return rq.q.QueryContext(ctx,
		`SELECT id, 'semantic' as tier, category, value, created_at
		 FROM semantic_memory WHERE value LIKE ? OR key LIKE ? LIMIT ?`, pattern, pattern, limit)
}

// --- Stats ---

// CostsByHour returns cost aggregation by hour.
func (rq *RouteQueries) CostsByHour(ctx context.Context, hours int) (*sql.Rows, error) {
	return rq.q.QueryContext(ctx,
		`SELECT strftime('%Y-%m-%d %H:00', created_at) as hour,
		        SUM(cost) as total_cost, SUM(tokens_in) as total_in, SUM(tokens_out) as total_out, COUNT(*) as calls
		 FROM inference_costs
		 WHERE created_at >= datetime('now', '-' || ? || ' hours')
		 GROUP BY hour ORDER BY hour`, hours)
}

// CostsByModel returns cost aggregation by model.
func (rq *RouteQueries) CostsByModel(ctx context.Context, hours int) (*sql.Rows, error) {
	return rq.q.QueryContext(ctx,
		`SELECT model, SUM(cost) as total_cost, SUM(tokens_in) as total_in, SUM(tokens_out) as total_out, COUNT(*) as calls
		 FROM inference_costs
		 WHERE created_at >= datetime('now', '-' || ? || ' hours')
		 GROUP BY model ORDER BY total_cost DESC`, hours)
}

// CountRow returns a single integer count for a query.
func (rq *RouteQueries) CountRow(ctx context.Context, query string, args ...any) (int64, error) {
	var count int64
	err := rq.q.QueryRowContext(ctx, query, args...).Scan(&count)
	return count, err
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
		`SELECT max_tokens, system_tokens, memory_tokens, history_tokens, history_depth, tools_count
		 FROM context_snapshots WHERE turn_id = ?`, turnID)
}

// --- Observability ---

// ListRecentTransactions returns recent inference costs for the transaction log.
func (rq *RouteQueries) ListRecentTransactions(ctx context.Context, hours, limit int) (*sql.Rows, error) {
	return rq.q.QueryContext(ctx,
		`SELECT id, model, provider, tokens_in, tokens_out, cost, latency_ms, quality_score, escalation, turn_id, cached, created_at
		 FROM inference_costs
		 WHERE created_at >= datetime('now', '-' || ? || ' hours')
		 ORDER BY created_at DESC LIMIT ?`, hours, limit)
}

// --- Throttle ---

// ProviderCapacity returns capacity metrics per provider.
func (rq *RouteQueries) ProviderCapacity(ctx context.Context, hours int) (*sql.Rows, error) {
	return rq.q.QueryContext(ctx,
		`SELECT provider, COUNT(*) as calls, SUM(tokens_in + tokens_out) as total_tokens,
		        AVG(latency_ms) as avg_latency, SUM(cost) as total_cost
		 FROM inference_costs
		 WHERE created_at >= datetime('now', '-' || ? || ' hours')
		 GROUP BY provider ORDER BY calls DESC`, hours)
}

// --- Discovery / Runtime ---

// ListDiscoveredAgents returns discovered agents.
func (rq *RouteQueries) ListDiscoveredAgents(ctx context.Context, limit int) (*sql.Rows, error) {
	return rq.q.QueryContext(ctx,
		`SELECT id, did, agent_card_json, capabilities, endpoint_url, trust_score, last_verified_at, created_at
		 FROM discovered_agents ORDER BY created_at DESC LIMIT ?`, limit)
}

// ListPairedDevices returns paired devices.
func (rq *RouteQueries) ListPairedDevices(ctx context.Context) (*sql.Rows, error) {
	return rq.q.QueryContext(ctx,
		`SELECT id, device_name, state, paired_at, verified_at, last_seen
		 FROM paired_devices ORDER BY paired_at DESC`)
}

// --- Sub-agent Retirement ---

// ListRetirementCandidates returns sub-agents not used in 30+ days.
func (rq *RouteQueries) ListRetirementCandidates(ctx context.Context) (*sql.Rows, error) {
	return rq.q.QueryContext(ctx,
		`SELECT id, name, display_name, model, role, created_at
		 FROM sub_agents
		 WHERE created_at < datetime('now', '-30 days')
		 ORDER BY created_at ASC`)
}

// --- Delivery Queue ---

// ListDeadLetters returns dead-lettered delivery queue items.
func (rq *RouteQueries) ListDeadLetters(ctx context.Context, limit int) (*sql.Rows, error) {
	return rq.q.QueryContext(ctx,
		`SELECT id, channel, recipient_id, content, last_error, created_at
		 FROM delivery_queue WHERE status = 'dead_letter' ORDER BY created_at DESC LIMIT ?`, limit)
}

// --- Wallet ---

// ListWalletBalances returns cached on-chain balances.
func (rq *RouteQueries) ListWalletBalances(ctx context.Context) (*sql.Rows, error) {
	return rq.q.QueryContext(ctx,
		`SELECT symbol, name, balance, contract, decimals, is_native, updated_at
		 FROM wallet_balances ORDER BY symbol`)
}

// --- Policy / Audit ---

// ListPolicyDecisions returns policy decisions for a turn.
func (rq *RouteQueries) ListPolicyDecisions(ctx context.Context, turnID string) (*sql.Rows, error) {
	return rq.q.QueryContext(ctx,
		`SELECT id, turn_id, tool_name, decision, rule_name, reason, created_at
		 FROM policy_decisions WHERE turn_id = ? ORDER BY created_at`, turnID)
}

// ListToolCallsForAudit returns tool calls for audit display.
func (rq *RouteQueries) ListToolCallsForAudit(ctx context.Context, turnID string) (*sql.Rows, error) {
	return rq.q.QueryContext(ctx,
		`SELECT id, tool_name, input, output, status, duration_ms, created_at
		 FROM tool_calls WHERE turn_id = ? ORDER BY created_at`, turnID)
}

// --- Runtime Settings ---

// GetRuntimeSetting returns a runtime setting value by key.
func (rq *RouteQueries) GetRuntimeSetting(ctx context.Context, key string) *sql.Row {
	return rq.q.QueryRowContext(ctx, `SELECT value FROM runtime_settings WHERE key = ?`, key)
}

// GetIdentityValue returns an identity table value by key.
func (rq *RouteQueries) GetIdentityValue(ctx context.Context, key string) *sql.Row {
	return rq.q.QueryRowContext(ctx, `SELECT value FROM identity WHERE key = ?`, key)
}

// --- Skill Counts ---

// CountEnabledSkills returns the number of enabled skills.
func (rq *RouteQueries) CountEnabledSkills(ctx context.Context) (int64, error) {
	var count int64
	err := rq.q.QueryRowContext(ctx, `SELECT COUNT(*) FROM skills WHERE enabled = 1`).Scan(&count)
	return count, err
}

// CountDisabledSkills returns the number of disabled skills.
func (rq *RouteQueries) CountDisabledSkills(ctx context.Context) (int64, error) {
	var count int64
	err := rq.q.QueryRowContext(ctx, `SELECT COUNT(*) FROM skills WHERE enabled = 0`).Scan(&count)
	return count, err
}

// CountAllSkills returns the total number of skills.
func (rq *RouteQueries) CountAllSkills(ctx context.Context) (int64, error) {
	var count int64
	err := rq.q.QueryRowContext(ctx, `SELECT COUNT(*) FROM skills`).Scan(&count)
	return count, err
}

// LatestSkillTimestamp returns the most recent created_at from skills.
func (rq *RouteQueries) LatestSkillTimestamp(ctx context.Context) (*string, error) {
	var ts *string
	err := rq.q.QueryRowContext(ctx, `SELECT MAX(created_at) FROM skills`).Scan(&ts)
	return ts, err
}

// --- Memory Analytics ---

// RetrievalQualityAvg returns total turns and turns-with-memory counts.
func (rq *RouteQueries) RetrievalQualityAvg(ctx context.Context, offset string) (totalTurns, turnsWithMemory int64, err error) {
	err = rq.q.QueryRowContext(ctx,
		`SELECT COUNT(*),
		        COALESCE(SUM(CASE WHEN COALESCE(memory_tokens, 0) > 0 THEN 1 ELSE 0 END), 0)
		 FROM context_snapshots
		 WHERE created_at >= datetime('now', ? || ' hours')`, offset).Scan(&totalTurns, &turnsWithMemory)
	return
}

// CachePerformance returns average budget utilization.
func (rq *RouteQueries) CachePerformance(ctx context.Context, offset string) (float64, error) {
	var avg float64
	err := rq.q.QueryRowContext(ctx,
		`SELECT COALESCE(
		   AVG(CAST(COALESCE(memory_tokens, 0) + COALESCE(system_prompt_tokens, 0) + COALESCE(history_tokens, 0) AS REAL)
		       / NULLIF(token_budget, 0)),
		 0)
		 FROM context_snapshots
		 WHERE created_at >= datetime('now', ? || ' hours')
		   AND token_budget > 0`, offset).Scan(&avg)
	return avg, err
}

// ComplexityDistribution returns rows of (complexity_level, count).
func (rq *RouteQueries) ComplexityDistribution(ctx context.Context, offset string) (*sql.Rows, error) {
	return rq.q.QueryContext(ctx,
		`SELECT complexity_level, COUNT(*)
		 FROM context_snapshots
		 WHERE created_at >= datetime('now', ? || ' hours')
		 GROUP BY complexity_level ORDER BY complexity_level`, offset)
}

// MemoryROIWithMemory returns average feedback grade for turns that used memory.
func (rq *RouteQueries) MemoryROIWithMemory(ctx context.Context, offset string) (float64, error) {
	var avg float64
	err := rq.q.QueryRowContext(ctx,
		`SELECT COALESCE(AVG(tf.grade), 0)
		 FROM turn_feedback tf
		 JOIN context_snapshots cs ON cs.turn_id = tf.turn_id
		 WHERE cs.created_at >= datetime('now', ? || ' hours')
		   AND COALESCE(cs.memory_tokens, 0) > 0`, offset).Scan(&avg)
	return avg, err
}

// MemoryROIWithoutMemory returns average feedback grade for turns that did not use memory.
func (rq *RouteQueries) MemoryROIWithoutMemory(ctx context.Context, offset string) (float64, error) {
	var avg float64
	err := rq.q.QueryRowContext(ctx,
		`SELECT COALESCE(AVG(tf.grade), 0)
		 FROM turn_feedback tf
		 JOIN context_snapshots cs ON cs.turn_id = tf.turn_id
		 WHERE cs.created_at >= datetime('now', ? || ' hours')
		   AND COALESCE(cs.memory_tokens, 0) = 0`, offset).Scan(&avg)
	return avg, err
}

// CountWorkingMemory returns the number of working memory entries.
func (rq *RouteQueries) CountWorkingMemory(ctx context.Context) (int64, error) {
	var count int64
	err := rq.q.QueryRowContext(ctx, `SELECT COUNT(*) FROM working_memory`).Scan(&count)
	return count, err
}

// CountEpisodicMemory returns the number of episodic memory entries.
func (rq *RouteQueries) CountEpisodicMemory(ctx context.Context) (int64, error) {
	var count int64
	err := rq.q.QueryRowContext(ctx, `SELECT COUNT(*) FROM episodic_memory`).Scan(&count)
	return count, err
}

// CountSemanticMemory returns the number of semantic memory entries.
func (rq *RouteQueries) CountSemanticMemory(ctx context.Context) (int64, error) {
	var count int64
	err := rq.q.QueryRowContext(ctx, `SELECT COUNT(*) FROM semantic_memory`).Scan(&count)
	return count, err
}

// CountProceduralMemory returns the number of procedural memory entries.
func (rq *RouteQueries) CountProceduralMemory(ctx context.Context) (int64, error) {
	var count int64
	err := rq.q.QueryRowContext(ctx, `SELECT COUNT(*) FROM procedural_memory`).Scan(&count)
	return count, err
}

// CountRelationshipMemory returns the number of relationship memory entries.
func (rq *RouteQueries) CountRelationshipMemory(ctx context.Context) (int64, error) {
	var count int64
	err := rq.q.QueryRowContext(ctx, `SELECT COUNT(*) FROM relationship_memory`).Scan(&count)
	return count, err
}

// CountWorkingMemoryStale returns working memory entries older than 24 hours.
func (rq *RouteQueries) CountWorkingMemoryStale(ctx context.Context) (int64, error) {
	var count int64
	err := rq.q.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM working_memory WHERE created_at < datetime('now', '-24 hours')`).Scan(&count)
	return count, err
}

// CountEpisodicMemoryStale returns episodic memory entries older than 7 days.
func (rq *RouteQueries) CountEpisodicMemoryStale(ctx context.Context) (int64, error) {
	var count int64
	err := rq.q.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM episodic_memory WHERE created_at < datetime('now', '-7 days')`).Scan(&count)
	return count, err
}

// --- Stats / Costs ---

// ListCostRows returns recent inference cost rows for the dashboard.
func (rq *RouteQueries) ListCostRows(ctx context.Context, limit int) (*sql.Rows, error) {
	return rq.q.QueryContext(ctx,
		`SELECT id, model, provider, cost, tokens_in, tokens_out, created_at, cached, COALESCE(latency_ms, 0)
		 FROM inference_costs ORDER BY created_at DESC LIMIT ?`, limit)
}

// CacheStats returns cache entry count, total inference count, and cached inference count.
func (rq *RouteQueries) CacheStats(ctx context.Context) (cacheCount, totalInferences, cachedInferences int64, err error) {
	err = rq.q.QueryRowContext(ctx, `SELECT COUNT(*) FROM semantic_cache`).Scan(&cacheCount)
	if err != nil {
		return
	}
	err = rq.q.QueryRowContext(ctx,
		`SELECT COUNT(*), COALESCE(SUM(CASE WHEN cached = 1 THEN 1 ELSE 0 END), 0) FROM inference_costs`).
		Scan(&totalInferences, &cachedInferences)
	return
}

// ListFinancialTransactions returns recent financial transactions.
func (rq *RouteQueries) ListFinancialTransactions(ctx context.Context, limit int) (*sql.Rows, error) {
	return rq.q.QueryContext(ctx,
		`SELECT id, tx_type, amount, currency, counterparty, tx_hash, created_at
		 FROM transactions ORDER BY created_at DESC LIMIT ?`, limit)
}

// EfficiencyMetrics returns aggregate inference metrics for a time window.
func (rq *RouteQueries) EfficiencyMetrics(ctx context.Context, offset string) (totalTokens, count, cachedCount int64, totalCost, avgLatency float64, err error) {
	err = rq.q.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(tokens_in + tokens_out), 0),
		        COALESCE(SUM(cost), 0),
		        COALESCE(AVG(latency_ms), 0),
		        COUNT(*),
		        COALESCE(SUM(CASE WHEN cached = 1 THEN 1 ELSE 0 END), 0)
		 FROM inference_costs
		 WHERE created_at >= datetime('now', ? || ' hours')`, offset).
		Scan(&totalTokens, &totalCost, &avgLatency, &count, &cachedCount)
	return
}

// ModelCostBreakdown returns per-model cost breakdown for a time window.
func (rq *RouteQueries) ModelCostBreakdown(ctx context.Context, offset string) (*sql.Rows, error) {
	return rq.q.QueryContext(ctx,
		`SELECT model, COUNT(*), COALESCE(SUM(cost), 0), COALESCE(SUM(tokens_in), 0), COALESCE(SUM(tokens_out), 0)
		 FROM inference_costs
		 WHERE created_at >= datetime('now', ? || ' hours')
		 GROUP BY model ORDER BY COUNT(*) DESC`, offset)
}

// ListModelSelectionEvents returns recent model selection events with candidates.
func (rq *RouteQueries) ListModelSelectionEvents(ctx context.Context, limit int) (*sql.Rows, error) {
	return rq.q.QueryContext(ctx,
		`SELECT id, turn_id, session_id, selected_model, strategy, complexity, candidates_json, created_at
		 FROM model_selection_events ORDER BY created_at DESC LIMIT ?`, limit)
}

// RecommendationMetrics returns aggregate metrics for generating recommendations.
func (rq *RouteQueries) RecommendationMetrics(ctx context.Context, offset string) (requests, cachedCount, totalTokens int64, totalCost, avgLatency float64, err error) {
	err = rq.q.QueryRowContext(ctx,
		`SELECT COUNT(*),
		        COALESCE(SUM(cost), 0),
		        COALESCE(AVG(latency_ms), 0),
		        COALESCE(SUM(CASE WHEN cached = 1 THEN 1 ELSE 0 END), 0),
		        COALESCE(SUM(tokens_in + tokens_out), 0)
		 FROM inference_costs
		 WHERE created_at >= datetime('now', ? || ' hours')`, offset).
		Scan(&requests, &totalCost, &avgLatency, &cachedCount, &totalTokens)
	return
}

// CostTimeseries returns hourly cost/token/request buckets.
func (rq *RouteQueries) CostTimeseries(ctx context.Context, days int) (*sql.Rows, error) {
	return rq.q.QueryContext(ctx,
		`SELECT strftime('%Y-%m-%dT%H:00:00', created_at) as bucket,
		        COUNT(*) as requests, COALESCE(SUM(cost), 0) as cost,
		        COALESCE(SUM(tokens_in + tokens_out), 0) as tokens
		 FROM inference_costs
		 WHERE created_at >= datetime('now', ? || ' days')
		 GROUP BY bucket ORDER BY bucket`, -days)
}

// --- Turn Detail ---

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
		`SELECT COALESCE(max_tokens, 0), COALESCE(system_tokens, 0), COALESCE(memory_tokens, 0),
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

// --- Admin / Subagents ---

// ListSubAgentsAdmin returns subagents for the admin page (fewer columns).
func (rq *RouteQueries) ListSubAgentsAdmin(ctx context.Context) (*sql.Rows, error) {
	return rq.q.QueryContext(ctx,
		`SELECT id, name, display_name, model, role, description, enabled, created_at
		 FROM sub_agents ORDER BY created_at DESC`)
}

// --- Traces ---

// ListTracesSimple returns traces with fewer columns for the trace list.
func (rq *RouteQueries) ListTracesSimple(ctx context.Context, limit int) (*sql.Rows, error) {
	return rq.q.QueryContext(ctx,
		`SELECT id, turn_id, channel, total_ms, created_at
		 FROM pipeline_traces ORDER BY created_at DESC LIMIT ?`, limit)
}

// GetTraceByTurnID returns a pipeline trace by turn_id.
func (rq *RouteQueries) GetTraceByTurnID(ctx context.Context, turnID string) *sql.Row {
	return rq.q.QueryRowContext(ctx,
		`SELECT id, turn_id, channel, total_ms, stages_json, created_at
		 FROM pipeline_traces WHERE turn_id = ? LIMIT 1`, turnID)
}

// GetReactTraceByTurnID returns a ReAct trace by joining through pipeline_traces.
func (rq *RouteQueries) GetReactTraceByTurnID(ctx context.Context, turnID string) *sql.Row {
	return rq.q.QueryRowContext(ctx,
		`SELECT rt.id, rt.pipeline_trace_id, rt.react_json, rt.created_at
		 FROM react_traces rt
		 JOIN pipeline_traces pt ON pt.id = rt.pipeline_trace_id
		 WHERE pt.turn_id = ? LIMIT 1`, turnID)
}

// GetTraceByIDOrTurnID returns a pipeline trace by either id or turn_id.
func (rq *RouteQueries) GetTraceByIDOrTurnID(ctx context.Context, id string) *sql.Row {
	return rq.q.QueryRowContext(ctx,
		`SELECT id, turn_id, channel, total_ms, stages_json, created_at
		 FROM pipeline_traces WHERE id = ? OR turn_id = ? LIMIT 1`, id, id)
}

// --- Observability ---

// ListObservabilityTracesPage returns traces with full detail and pagination.
func (rq *RouteQueries) ListObservabilityTracesPage(ctx context.Context, limit, offset int) (*sql.Rows, error) {
	return rq.q.QueryContext(ctx,
		`SELECT id, turn_id, session_id, channel, total_ms, stages_json, created_at
		 FROM pipeline_traces ORDER BY created_at DESC LIMIT ? OFFSET ?`, limit, offset)
}

// CountPipelineTraces returns the total number of pipeline traces.
func (rq *RouteQueries) CountPipelineTraces(ctx context.Context) (int64, error) {
	var count int64
	err := rq.q.QueryRowContext(ctx, `SELECT COUNT(*) FROM pipeline_traces`).Scan(&count)
	return count, err
}

// ListDelegationOutcomesDetailed returns delegation outcomes with subtask_count.
func (rq *RouteQueries) ListDelegationOutcomesDetailed(ctx context.Context, limit int) (*sql.Rows, error) {
	return rq.q.QueryContext(ctx,
		`SELECT id, turn_id, session_id, task_description, subtask_count,
		        pattern, assigned_agents_json, duration_ms, success, quality_score, created_at
		 FROM delegation_outcomes ORDER BY created_at DESC LIMIT ?`, limit)
}

// DelegationTotals returns aggregate delegation stats.
func (rq *RouteQueries) DelegationTotals(ctx context.Context) (total, successful int64, avgDuration float64, err error) {
	err = rq.q.QueryRowContext(ctx,
		`SELECT COUNT(*), COALESCE(SUM(success), 0), COALESCE(AVG(duration_ms), 0)
		 FROM delegation_outcomes`).Scan(&total, &successful, &avgDuration)
	return
}

// DelegationAvgQuality returns average quality score from delegation outcomes.
func (rq *RouteQueries) DelegationAvgQuality(ctx context.Context) (float64, error) {
	var avg float64
	err := rq.q.QueryRowContext(ctx,
		`SELECT COALESCE(AVG(quality_score), 0) FROM delegation_outcomes WHERE quality_score IS NOT NULL`).Scan(&avg)
	return avg, err
}

// AgentDelegationStats holds per-agent delegation success rates.
type AgentDelegationStats struct {
	AgentName   string
	Total       int64
	Successful  int64
	SuccessRate float64
	AvgDuration float64
}

// PerAgentDelegationStats unpacks assigned_agents_json with json_each()
// to compute per-agent success rates from delegation_outcomes.
func (rq *RouteQueries) PerAgentDelegationStats(ctx context.Context) ([]AgentDelegationStats, error) {
	rows, err := rq.q.QueryContext(ctx,
		`SELECT
			j.value AS agent_name,
			COUNT(*) AS total,
			COALESCE(SUM(d.success), 0) AS successful,
			CASE WHEN COUNT(*) > 0 THEN CAST(SUM(d.success) AS REAL) / COUNT(*) ELSE 0 END AS success_rate,
			COALESCE(AVG(d.duration_ms), 0) AS avg_duration
		 FROM delegation_outcomes d, json_each(d.assigned_agents_json) j
		 GROUP BY j.value
		 ORDER BY total DESC`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var result []AgentDelegationStats
	for rows.Next() {
		var s AgentDelegationStats
		if err := rows.Scan(&s.AgentName, &s.Total, &s.Successful, &s.SuccessRate, &s.AvgDuration); err != nil {
			return nil, err
		}
		result = append(result, s)
	}
	return result, rows.Err()
}

// --- Workspace ---

// ListSubAgentNamesModels returns subagent name/model/enabled for workspace state.
func (rq *RouteQueries) ListSubAgentNamesModels(ctx context.Context) (*sql.Rows, error) {
	return rq.q.QueryContext(ctx,
		`SELECT name, model, enabled FROM sub_agents ORDER BY name`)
}

// ListEnabledSkillNames returns names of enabled skills.
func (rq *RouteQueries) ListEnabledSkillNames(ctx context.Context, limit int) (*sql.Rows, error) {
	return rq.q.QueryContext(ctx,
		`SELECT name FROM skills WHERE enabled = 1 ORDER BY name LIMIT ?`, limit)
}

// ListSubAgentRoster returns subagent roster data.
func (rq *RouteQueries) ListSubAgentRoster(ctx context.Context) (*sql.Rows, error) {
	return rq.q.QueryContext(ctx,
		`SELECT name, COALESCE(display_name, ''), model, enabled, role, COALESCE(description, '')
		 FROM sub_agents ORDER BY name`)
}

// --- Turns / Skills ---

// ListTurnMessages returns messages by id or session_id for a turn.
func (rq *RouteQueries) ListTurnMessages(ctx context.Context, id string, limit int) (*sql.Rows, error) {
	return rq.q.QueryContext(ctx,
		`SELECT id, role, content, created_at FROM session_messages WHERE id = ? OR session_id = ? LIMIT ?`, id, id, limit)
}

// ListSkillsFull returns all skills with full detail for the skills page.
func (rq *RouteQueries) ListSkillsFull(ctx context.Context) (*sql.Rows, error) {
	return rq.q.QueryContext(ctx,
		`SELECT id, name, kind, description, enabled, version, risk_level, created_at
		 FROM skills ORDER BY name`)
}

// --- Throttle ---

// AbuseSummary returns total abuse event count in a time window.
func (rq *RouteQueries) AbuseSummary(ctx context.Context, offset string) (int64, error) {
	var count int64
	err := rq.q.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM abuse_events
		 WHERE created_at >= datetime('now', ? || ' hours')`, offset).Scan(&count)
	return count, err
}

// AbuseByType returns abuse events grouped by signal type.
func (rq *RouteQueries) AbuseByType(ctx context.Context, offset string) (*sql.Rows, error) {
	return rq.q.QueryContext(ctx,
		`SELECT signal_type, COUNT(*), COALESCE(AVG(score), 0)
		 FROM abuse_events
		 WHERE created_at >= datetime('now', ? || ' hours')
		 GROUP BY signal_type ORDER BY COUNT(*) DESC`, offset)
}

// AbuseByActor returns abuse events grouped by actor.
func (rq *RouteQueries) AbuseByActor(ctx context.Context, offset string) (*sql.Rows, error) {
	return rq.q.QueryContext(ctx,
		`SELECT actor_id, COUNT(*), COALESCE(SUM(score), 0), MAX(action_taken)
		 FROM abuse_events
		 WHERE created_at >= datetime('now', ? || ' hours')
		 GROUP BY actor_id ORDER BY SUM(score) DESC LIMIT 10`, offset)
}

// RateLimitCurrent returns active slowdown and quarantine penalty counts.
func (rq *RouteQueries) RateLimitCurrent(ctx context.Context) (slowdown, quarantine int64, err error) {
	err = rq.q.QueryRowContext(ctx,
		`SELECT
		   COALESCE(SUM(CASE WHEN action_taken = 'slowdown' THEN 1 ELSE 0 END), 0),
		   COALESCE(SUM(CASE WHEN action_taken = 'quarantine' THEN 1 ELSE 0 END), 0)
		 FROM abuse_events
		 WHERE created_at >= datetime('now', '-5 minutes')`).Scan(&slowdown, &quarantine)
	return
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

// --- Theme Mutations ---

// InstallTheme inserts or replaces an installed theme.
func (rq *RouteQueries) InstallTheme(ctx context.Context, id, name, content string) error {
	_, err := rq.q.ExecContext(ctx,
		`INSERT OR REPLACE INTO installed_themes (id, name, source, version, active, content) VALUES (?, ?, 'catalog', '1.0.0', 0, ?)`,
		id, name, content)
	return err
}

// SetActiveThemeID updates the active theme in the identity table.
func (rq *RouteQueries) SetActiveThemeID(ctx context.Context, themeID string) error {
	_, err := rq.q.ExecContext(ctx,
		`INSERT OR REPLACE INTO identity (key, value) VALUES ('active_theme', ?)`, themeID)
	return err
}

// ListInstalledThemeIDs returns IDs of installed themes.
func (rq *RouteQueries) ListInstalledThemeIDs(ctx context.Context) (*sql.Rows, error) {
	return rq.q.QueryContext(ctx, `SELECT id FROM installed_themes`)
}

// --- Runtime Settings Mutations ---

// UpsertRuntimeSetting inserts or updates a runtime setting.
func (rq *RouteQueries) UpsertRuntimeSetting(ctx context.Context, key, value string) error {
	_, err := rq.q.ExecContext(ctx,
		`INSERT INTO runtime_settings (key, value, updated_at) VALUES (?, ?, datetime('now'))
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = datetime('now')`,
		key, value)
	return err
}

// --- Remaining Read Queries ---

// ListAgentsFull returns all agents for the agents list page.
func (rq *RouteQueries) ListAgentsFull(ctx context.Context) (*sql.Rows, error) {
	return rq.q.QueryContext(ctx,
		`SELECT id, name, display_name, model, role, description, enabled, created_at
		 FROM sub_agents ORDER BY created_at DESC`)
}

// ListDiscoveredAgentsFull returns discovered agents with full detail.
func (rq *RouteQueries) ListDiscoveredAgentsFull(ctx context.Context, limit int) (*sql.Rows, error) {
	return rq.q.QueryContext(ctx,
		`SELECT id, did, agent_card_json, capabilities, endpoint_url, trust_score, last_verified_at, created_at
		 FROM discovered_agents ORDER BY created_at DESC LIMIT ?`, limit)
}

// ListPairedDevicesFull returns paired devices with full detail.
func (rq *RouteQueries) ListPairedDevicesFull(ctx context.Context) (*sql.Rows, error) {
	return rq.q.QueryContext(ctx,
		`SELECT id, device_name, state, paired_at, verified_at, last_seen
		 FROM paired_devices ORDER BY paired_at DESC`)
}

// --- Generic fallbacks (to be eliminated) ---

// QueryRow executes a single-row query. Use for simple COUNT/SUM aggregations
// that don't yet have a dedicated method.
func (rq *RouteQueries) QueryRow(ctx context.Context, query string, args ...any) *sql.Row {
	return rq.q.QueryRowContext(ctx, query, args...)
}

// Query executes a multi-row query. Use sparingly — prefer dedicated methods.
func (rq *RouteQueries) Query(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return rq.q.QueryContext(ctx, query, args...)
}
