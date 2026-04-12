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

// ListSkillsFull returns all skills with full detail for the skills page.
func (rq *RouteQueries) ListSkillsFull(ctx context.Context) (*sql.Rows, error) {
	return rq.q.QueryContext(ctx,
		`SELECT id, name, kind, description, enabled, version, risk_level, created_at
		 FROM skills ORDER BY name`)
}

// ListEnabledSkillNames returns names of enabled skills.
func (rq *RouteQueries) ListEnabledSkillNames(ctx context.Context, limit int) (*sql.Rows, error) {
	return rq.q.QueryContext(ctx,
		`SELECT name FROM skills WHERE enabled = 1 ORDER BY name LIMIT ?`, limit)
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

// ListSubAgentsAdmin returns subagents for the admin page (fewer columns).
func (rq *RouteQueries) ListSubAgentsAdmin(ctx context.Context) (*sql.Rows, error) {
	return rq.q.QueryContext(ctx,
		`SELECT id, name, display_name, model, role, description, enabled, created_at
		 FROM sub_agents ORDER BY created_at DESC`)
}

// ListRetirementCandidates returns sub-agents not used in 30+ days.
func (rq *RouteQueries) ListRetirementCandidates(ctx context.Context) (*sql.Rows, error) {
	return rq.q.QueryContext(ctx,
		`SELECT id, name, display_name, model, role, created_at
		 FROM sub_agents
		 WHERE created_at < datetime('now', '-30 days')
		 ORDER BY created_at ASC`)
}

// ListAgentsFull returns all agents for the agents list page.
func (rq *RouteQueries) ListAgentsFull(ctx context.Context) (*sql.Rows, error) {
	return rq.q.QueryContext(ctx,
		`SELECT id, name, display_name, model, role, description, enabled, created_at
		 FROM sub_agents ORDER BY created_at DESC`)
}

// --- Workspace ---

// ListSubAgentNamesModels returns subagent name/model/enabled for workspace state.
func (rq *RouteQueries) ListSubAgentNamesModels(ctx context.Context) (*sql.Rows, error) {
	return rq.q.QueryContext(ctx,
		`SELECT name, model, enabled FROM sub_agents ORDER BY name`)
}

// ListSubAgentRoster returns subagent roster data.
func (rq *RouteQueries) ListSubAgentRoster(ctx context.Context) (*sql.Rows, error) {
	return rq.q.QueryContext(ctx,
		`SELECT name, COALESCE(display_name, ''), model, enabled, role, COALESCE(description, '')
		 FROM sub_agents ORDER BY name`)
}

// ListSubAgentRosterEnriched returns enriched subagent data for the roster endpoint.
// Includes fallback models, skills, session counts, and status for Rust parity.
func (rq *RouteQueries) ListSubAgentRosterEnriched(ctx context.Context) (*sql.Rows, error) {
	return rq.q.QueryContext(ctx,
		`SELECT name, COALESCE(display_name, name) AS display_name, model, enabled,
		        COALESCE(role, 'specialist') AS role, COALESCE(description, '') AS description,
		        COALESCE(fallback_models_json, '[]') AS fallback_models_json,
		        COALESCE(skills_json, '[]') AS skills_json,
		        session_count, last_used_at, status
		 FROM sub_agents ORDER BY name`)
}

// ListSkillNamesAndKinds returns skill name, kind, and enabled for roster breakdown.
func (rq *RouteQueries) ListSkillNamesAndKinds(ctx context.Context) (*sql.Rows, error) {
	return rq.q.QueryContext(ctx,
		`SELECT name, kind, enabled FROM skills ORDER BY name`)
}

// CountSubAgents returns total and enabled sub-agent counts.
func (rq *RouteQueries) CountSubAgents(ctx context.Context) (total, enabled int, err error) {
	err = rq.q.QueryRowContext(ctx, `SELECT COUNT(*) FROM sub_agents`).Scan(&total)
	if err != nil {
		return
	}
	err = rq.q.QueryRowContext(ctx, `SELECT COUNT(*) FROM sub_agents WHERE enabled = 1`).Scan(&enabled)
	return
}

// CountRunningSubAgents returns the number of sub-agents with status 'running'.
func (rq *RouteQueries) CountRunningSubAgents(ctx context.Context) (int, error) {
	var count int
	err := rq.q.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM sub_agents WHERE status = 'running' OR (enabled = 1 AND status = 'registered')`).Scan(&count)
	return count, err
}

// --- Themes ---

// ListThemes returns installed themes.
func (rq *RouteQueries) ListThemes(ctx context.Context) (*sql.Rows, error) {
	return rq.q.QueryContext(ctx,
		`SELECT id, name, source, version, active, created_at FROM installed_themes ORDER BY name ASC`)
}

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

// --- Runtime Settings ---

// GetRuntimeSetting returns a runtime setting value by key.
func (rq *RouteQueries) GetRuntimeSetting(ctx context.Context, key string) *sql.Row {
	return rq.q.QueryRowContext(ctx, `SELECT value FROM runtime_settings WHERE key = ?`, key)
}

// GetIdentityValue returns an identity table value by key.
func (rq *RouteQueries) GetIdentityValue(ctx context.Context, key string) *sql.Row {
	return rq.q.QueryRowContext(ctx, `SELECT value FROM identity WHERE key = ?`, key)
}

// UpsertRuntimeSetting inserts or updates a runtime setting.
func (rq *RouteQueries) UpsertRuntimeSetting(ctx context.Context, key, value string) error {
	_, err := rq.q.ExecContext(ctx,
		`INSERT INTO runtime_settings (key, value, updated_at) VALUES (?, ?, datetime('now'))
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = datetime('now')`,
		key, value)
	return err
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
