package db

import (
	"context"
	"database/sql"
)

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

// --- Flight Recorder ---

// ListSessionsWithFlightRecords returns sessions that have at least one react_traces record.
func (rq *RouteQueries) ListSessionsWithFlightRecords(ctx context.Context, limit int) (*sql.Rows, error) {
	return rq.q.QueryContext(ctx,
		`SELECT DISTINCT s.id, s.agent_id, s.scope_key, s.status, s.nickname, s.created_at, s.updated_at
		 FROM sessions s
		 WHERE EXISTS (
		     SELECT 1 FROM react_traces rt
		     JOIN pipeline_traces pt ON pt.id = rt.pipeline_trace_id
		     WHERE pt.session_id = s.id
		 )
		 ORDER BY s.created_at DESC LIMIT ?`, limit)
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
