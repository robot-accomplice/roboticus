// Hippocampus advanced operations — agent-owned table management,
// schema introspection, and context summaries.
//
// Ported from Rust: crates/roboticus-db/src/hippocampus.rs
//
// The basic HippocampusRegistry lives in repository.go; this file adds:
// - GetTable, ListAgentTables
// - CreateAgentTable, DropAgentTable (with SQL identifier validation)
// - SchemaSummary, CompactSummary (context injection)
// - BootstrapHippocampus (auto-discovery + consistency check)

package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/rs/zerolog/log"
)

// ColumnDef describes a column within a hippocampus schema entry.
type ColumnDef struct {
	Name        string `json:"name"`
	ColType     string `json:"col_type"`
	Nullable    bool   `json:"nullable"`
	Description string `json:"description,omitempty"`
}

// SchemaEntry is a fully hydrated hippocampus row with parsed columns.
type SchemaEntry struct {
	TableName   string
	Description string
	Columns     []ColumnDef
	CreatedBy   string
	AgentOwned  bool
	CreatedAt   string
	UpdatedAt   string
	AccessLevel string
	RowCount    int64
}

// GetTable looks up a single table's schema entry.
func (h *HippocampusRegistry) GetTable(ctx context.Context, tableName string) (*SchemaEntry, error) {
	row := h.q.QueryRowContext(ctx,
		`SELECT table_name, description, columns_json, created_by, agent_owned,
		        created_at, updated_at, access_level, row_count
		 FROM hippocampus WHERE table_name = ?`, tableName)
	return scanSchemaEntry(row)
}

// ListAllTables returns all registered schema entries (fully hydrated).
func (h *HippocampusRegistry) ListAllTables(ctx context.Context) ([]SchemaEntry, error) {
	rows, err := h.q.QueryContext(ctx,
		`SELECT table_name, description, columns_json, created_by, agent_owned,
		        created_at, updated_at, access_level, row_count
		 FROM hippocampus ORDER BY table_name`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	return collectSchemaEntries(rows)
}

// ListAgentTables returns only agent-owned tables for a specific agent.
func (h *HippocampusRegistry) ListAgentTables(ctx context.Context, agentID string) ([]SchemaEntry, error) {
	rows, err := h.q.QueryContext(ctx,
		`SELECT table_name, description, columns_json, created_by, agent_owned,
		        created_at, updated_at, access_level, row_count
		 FROM hippocampus
		 WHERE agent_owned = 1 AND created_by = ?
		 ORDER BY table_name`, agentID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	return collectSchemaEntries(rows)
}

// CreateAgentTable creates an agent-owned table with the given columns.
// Table names are prefixed with the agent ID for isolation.
// Returns the full table name (agentID_tableSuffix).
func (h *HippocampusRegistry) CreateAgentTable(
	ctx context.Context,
	agentID, tableSuffix, description string,
	columns []ColumnDef,
) (string, error) {
	tableName := agentID + "_" + tableSuffix

	if err := validateSQLIdentifier(tableName); err != nil {
		return "", err
	}
	for _, col := range columns {
		if err := validateSQLIdentifier(col.Name); err != nil {
			return "", fmt.Errorf("invalid column name: %w", err)
		}
		if err := validateSQLIdentifier(col.ColType); err != nil {
			return "", fmt.Errorf("invalid column type: %w", err)
		}
	}

	// Build CREATE TABLE DDL.
	var colDefs []string
	for _, c := range columns {
		null := ""
		if !c.Nullable {
			null = " NOT NULL"
		}
		colDefs = append(colDefs, fmt.Sprintf("%s %s%s", c.Name, c.ColType, null))
	}

	middle := ""
	if len(colDefs) > 0 {
		middle = ", " + strings.Join(colDefs, ", ")
	}

	createSQL := fmt.Sprintf(
		`CREATE TABLE IF NOT EXISTS "%s" (id TEXT PRIMARY KEY%s, created_at TEXT NOT NULL DEFAULT (datetime('now')))`,
		tableName, middle)

	if _, err := h.q.ExecContext(ctx, createSQL); err != nil {
		return "", fmt.Errorf("failed to create table %s: %w", tableName, err)
	}

	// Register in hippocampus.
	columnsJSON, _ := json.Marshal(columns)
	if _, err := h.q.ExecContext(ctx,
		`INSERT OR REPLACE INTO hippocampus
		 (table_name, description, columns_json, created_by, agent_owned, access_level, row_count, updated_at)
		 VALUES (?, ?, ?, ?, 1, 'readwrite', 0, datetime('now'))`,
		tableName, description, string(columnsJSON), agentID); err != nil {
		return "", err
	}

	return tableName, nil
}

// DropAgentTable drops an agent-owned table. Only tables created by the specified
// agent can be dropped. Uses a transactional pattern to prevent TOCTOU.
func (h *HippocampusRegistry) DropAgentTable(ctx context.Context, store *Store, agentID, tableName string) error {
	if err := validateSQLIdentifier(tableName); err != nil {
		return err
	}

	return store.InTx(ctx, func(tx *sql.Tx) error {
		// Verify ownership inside the transaction.
		var owned bool
		err := tx.QueryRowContext(ctx,
			`SELECT agent_owned = 1 AND created_by = ? FROM hippocampus WHERE table_name = ?`,
			agentID, tableName).Scan(&owned)
		if err == sql.ErrNoRows {
			return fmt.Errorf("table %s not found in hippocampus", tableName)
		}
		if err != nil {
			return err
		}
		if !owned {
			return fmt.Errorf("cannot drop: table %s not owned by agent %s", tableName, agentID)
		}

		if _, err := tx.ExecContext(ctx, fmt.Sprintf(`DROP TABLE IF EXISTS "%s"`, tableName)); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `DELETE FROM hippocampus WHERE table_name = ?`, tableName)
		return err
	})
}

// SchemaSummary generates a full markdown schema map for agent context injection.
func (h *HippocampusRegistry) SchemaSummary(ctx context.Context) (string, error) {
	tables, err := h.ListAllTables(ctx)
	if err != nil {
		return "", err
	}
	if len(tables) == 0 {
		return "No tables registered in hippocampus.", nil
	}

	var sb strings.Builder
	sb.WriteString("## Database Schema Map\n\n")
	for _, entry := range tables {
		owner := " (system)"
		if entry.AgentOwned {
			owner = fmt.Sprintf(" (owned by: %s)", entry.CreatedBy)
		}
		sb.WriteString(fmt.Sprintf("### %s%s [%s, %d rows]\n",
			entry.TableName, owner, entry.AccessLevel, entry.RowCount))
		sb.WriteString(entry.Description + "\n")
		for _, col := range entry.Columns {
			nullStr := ""
			if col.Nullable {
				nullStr = ", nullable"
			}
			desc := ""
			if col.Description != "" {
				desc = " — " + col.Description
			}
			sb.WriteString(fmt.Sprintf("- `%s` (%s%s)%s\n", col.Name, col.ColType, nullStr, desc))
		}
		sb.WriteString("\n")
	}
	return sb.String(), nil
}

// CompactSummary generates a concise hippocampus summary for context injection (~200 tokens).
// Prioritizes agent-owned tables (fully listed), then knowledge sources, then system table names.
func (h *HippocampusRegistry) CompactSummary(ctx context.Context) (string, error) {
	tables, err := h.ListAllTables(ctx)
	if err != nil {
		return "", err
	}
	if len(tables) == 0 {
		return "", nil
	}

	var systemNames []string
	var agentLines []string
	var knowledgeLines []string

	for _, entry := range tables {
		if entry.AgentOwned {
			agentLines = append(agentLines,
				fmt.Sprintf("- %s (%d rows) — %s", entry.TableName, entry.RowCount, entry.Description))
		} else if strings.HasPrefix(entry.TableName, "knowledge:") {
			knowledgeLines = append(knowledgeLines,
				fmt.Sprintf("- %s (%d chunks) — %s", entry.TableName, entry.RowCount, entry.Description))
		} else {
			systemNames = append(systemNames, entry.TableName)
		}
	}

	var sb strings.Builder
	sb.WriteString("[Database]\n")

	if len(agentLines) > 0 {
		sb.WriteString("Your tables:\n")
		for _, line := range agentLines {
			sb.WriteString(line + "\n")
		}
	}
	if len(knowledgeLines) > 0 {
		sb.WriteString("Knowledge sources:\n")
		for _, line := range knowledgeLines {
			sb.WriteString(line + "\n")
		}
	}
	if len(systemNames) > 0 {
		sb.WriteString(fmt.Sprintf("System tables (%d): %s\n",
			len(systemNames), strings.Join(systemNames, ", ")))
	}

	sb.WriteString("Use create_table/alter_table/drop_table tools to manage your tables. ")
	sb.WriteString("Use get_runtime_context for full schema details.")

	result := sb.String()
	if len(result) > 1000 {
		result = result[:1000]
		if idx := strings.LastIndex(result, "\n"); idx > 0 {
			result = result[:idx]
		}
		result += "\n...(use introspection tools for details)\n"
	}
	return result, nil
}

// RegisterTableFull registers a table with all hippocampus fields (Rust parity).
func (h *HippocampusRegistry) RegisterTableFull(
	ctx context.Context,
	tableName, description string,
	columns []ColumnDef,
	createdBy string,
	agentOwned bool,
	accessLevel string,
	rowCount int64,
) error {
	columnsJSON, _ := json.Marshal(columns)
	_, err := h.q.ExecContext(ctx,
		`INSERT OR REPLACE INTO hippocampus
		 (table_name, description, columns_json, created_by, agent_owned, access_level, row_count, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, datetime('now'))`,
		tableName, description, string(columnsJSON), createdBy, boolToInt(agentOwned), accessLevel, rowCount)
	return err
}

// BootstrapHippocampus auto-discovers all tables, introspects columns,
// and registers them. Also removes stale entries for dropped tables.
func (h *HippocampusRegistry) BootstrapHippocampus(ctx context.Context) error {
	// Phase 1: Discover all tables.
	rows, err := h.q.QueryContext(ctx,
		`SELECT name FROM sqlite_master
		 WHERE type = 'table' AND name NOT LIKE 'sqlite_%'
		 ORDER BY name`)
	if err != nil {
		return err
	}

	type tableData struct {
		name    string
		columns []ColumnDef
		count   int64
	}
	var discovered []tableData

	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			continue
		}
		names = append(names, name)
	}
	_ = rows.Close()

	// Introspect each table.
	for _, name := range names {
		columns := h.introspectColumns(ctx, name)
		var count int64
		_ = h.q.QueryRowContext(ctx,
			fmt.Sprintf(`SELECT COUNT(*) FROM "%s"`, name)).Scan(&count)
		discovered = append(discovered, tableData{name: name, columns: columns, count: count})
	}

	// Phase 2: Register each table.
	for _, td := range discovered {
		desc, accessLevel := systemTableMetadata(td.name)

		// Preserve existing agent-owned entries.
		existing, _ := h.GetTable(ctx, td.name)
		if existing != nil && existing.AgentOwned {
			if err := h.RegisterTableFull(ctx, td.name, existing.Description, td.columns,
				existing.CreatedBy, true, existing.AccessLevel, td.count); err != nil {
				return err
			}
			continue
		}

		if err := h.RegisterTableFull(ctx, td.name, desc, td.columns,
			"system", false, accessLevel, td.count); err != nil {
			return err
		}
	}

	// Phase 3: Consistency check — remove stale entries.
	existingSet := make(map[string]bool, len(discovered))
	for _, td := range discovered {
		existingSet[td.name] = true
	}

	registered, err := h.ListAllTables(ctx)
	if err != nil {
		return err
	}
	for _, entry := range registered {
		if !existingSet[entry.TableName] {
			log.Warn().Str("table", entry.TableName).Msg("hippocampus entry for missing table, removing")
			_, _ = h.q.ExecContext(ctx,
				`DELETE FROM hippocampus WHERE table_name = ?`, entry.TableName)
		}
	}

	log.Info().Int("tables", len(discovered)).Msg("hippocampus bootstrapped with schema map")
	return nil
}

// introspectColumns reads column definitions via PRAGMA table_info.
func (h *HippocampusRegistry) introspectColumns(ctx context.Context, tableName string) []ColumnDef {
	rows, err := h.q.QueryContext(ctx,
		fmt.Sprintf(`PRAGMA table_info("%s")`, tableName))
	if err != nil {
		return nil
	}
	defer func() { _ = rows.Close() }()

	var cols []ColumnDef
	for rows.Next() {
		var cid int
		var name, colType string
		var notnull int
		var dfltValue sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &colType, &notnull, &dfltValue, &pk); err != nil {
			continue
		}
		cols = append(cols, ColumnDef{
			Name:     name,
			ColType:  colType,
			Nullable: notnull == 0,
		})
	}
	return cols
}

// validateSQLIdentifier ensures a string is safe as a SQL identifier.
func validateSQLIdentifier(s string) error {
	if s == "" {
		return fmt.Errorf("invalid SQL identifier: empty string")
	}
	if s[0] >= '0' && s[0] <= '9' {
		return fmt.Errorf("invalid SQL identifier: %s (starts with digit)", s)
	}
	for _, c := range s {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_') {
			return fmt.Errorf("invalid SQL identifier: %s", s)
		}
	}
	return nil
}

// systemTableMetadata returns (description, access_level) for known system tables.
func systemTableMetadata(tableName string) (string, string) {
	switch tableName {
	case "schema_version":
		return "Schema migration version tracking", "internal"
	case "sessions":
		return "User conversation sessions", "read"
	case "session_messages":
		return "Messages within sessions", "read"
	case "turns":
		return "Conversation turn tracking", "internal"
	case "tool_calls":
		return "Tool invocation log", "read"
	case "policy_decisions":
		return "Policy evaluation results", "internal"
	case "working_memory":
		return "Session-scoped working memory", "read"
	case "episodic_memory":
		return "Long-term event memory", "read"
	case "semantic_memory":
		return "Factual knowledge store", "read"
	case "procedural_memory":
		return "Learned procedure memory", "read"
	case "relationship_memory":
		return "Entity relationship memory", "read"
	case "tasks":
		return "Task queue for agent work items", "read"
	case "cron_jobs":
		return "Scheduled cron jobs", "read"
	case "cron_runs":
		return "Cron job execution history", "read"
	case "transactions":
		return "Wallet transaction log", "internal"
	case "inference_costs":
		return "LLM inference cost tracking", "internal"
	case "semantic_cache":
		return "Semantic response cache", "internal"
	case "identity":
		return "Agent identity and credentials", "internal"
	case "os_personality_history":
		return "OS personality evolution log", "internal"
	case "metric_snapshots":
		return "System metric snapshots", "internal"
	case "discovered_agents":
		return "Discovered peer agents", "read"
	case "skills":
		return "Registered agent skills", "read"
	case "delivery_queue":
		return "Durable message delivery queue", "internal"
	case "approval_requests":
		return "Pending human approval requests", "read"
	case "plugins":
		return "Installed plugins", "read"
	case "embeddings":
		return "Vector embeddings store", "internal"
	case "sub_agents":
		return "Spawned sub-agent registry", "read"
	case "context_checkpoints":
		return "Context checkpoint snapshots", "internal"
	case "hippocampus":
		return "Schema map (this table)", "internal"
	case "learned_skills":
		return "Skills synthesized from successful tool sequences", "read"
	default:
		return "Agent-managed table", "readwrite"
	}
}

// scanSchemaEntry scans a single row into a SchemaEntry.
func scanSchemaEntry(row *sql.Row) (*SchemaEntry, error) {
	var e SchemaEntry
	var columnsJSON string
	var agentOwned int
	var accessLevel sql.NullString
	var rowCount sql.NullInt64

	err := row.Scan(&e.TableName, &e.Description, &columnsJSON, &e.CreatedBy,
		&agentOwned, &e.CreatedAt, &e.UpdatedAt, &accessLevel, &rowCount)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	e.AgentOwned = agentOwned != 0
	e.AccessLevel = "internal"
	if accessLevel.Valid {
		e.AccessLevel = accessLevel.String
	}
	if rowCount.Valid {
		e.RowCount = rowCount.Int64
	}

	if err := json.Unmarshal([]byte(columnsJSON), &e.Columns); err != nil {
		log.Warn().Err(err).Str("table", e.TableName).Msg("failed to deserialize column definitions")
		e.Columns = nil
	}
	return &e, nil
}

// collectSchemaEntries collects rows into SchemaEntry slice.
func collectSchemaEntries(rows *sql.Rows) ([]SchemaEntry, error) {
	var entries []SchemaEntry
	for rows.Next() {
		var e SchemaEntry
		var columnsJSON string
		var agentOwned int
		var accessLevel sql.NullString
		var rowCount sql.NullInt64

		if err := rows.Scan(&e.TableName, &e.Description, &columnsJSON, &e.CreatedBy,
			&agentOwned, &e.CreatedAt, &e.UpdatedAt, &accessLevel, &rowCount); err != nil {
			continue
		}

		e.AgentOwned = agentOwned != 0
		e.AccessLevel = "internal"
		if accessLevel.Valid {
			e.AccessLevel = accessLevel.String
		}
		if rowCount.Valid {
			e.RowCount = rowCount.Int64
		}

		if err := json.Unmarshal([]byte(columnsJSON), &e.Columns); err != nil {
			e.Columns = nil
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}
