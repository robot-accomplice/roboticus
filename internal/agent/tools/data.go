package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"roboticus/internal/db"
)

// validIdentifier matches safe SQL identifiers (alphanumeric + underscore).
var validIdentifier = regexp.MustCompile(`^[a-zA-Z0-9_]+$`)

// allowedColumnTypes lists the SQLite types agents may use.
var allowedColumnTypes = map[string]bool{
	"TEXT":    true,
	"INTEGER": true,
	"REAL":    true,
	"BLOB":    true,
}

const maxAgentTables = 50
const maxColumns = 64

// agentTableName builds the fully-qualified agent table name.
func agentTableName(agentID, name string) string {
	return fmt.Sprintf("agent_%s_%s", agentID, name)
}

// --- CreateTableTool ---

// CreateTableTool lets the agent create its own relational tables.
type CreateTableTool struct{}

func (t *CreateTableTool) Name() string        { return "create_table" }
func (t *CreateTableTool) Description() string { return "Create a new agent-owned relational table." }
func (t *CreateTableTool) Risk() RiskLevel     { return RiskCaution }
func (t *CreateTableTool) ParameterSchema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"name":        {"type": "string", "description": "Table name (alphanumeric and underscores only)"},
			"description": {"type": "string", "description": "What this table stores"},
			"columns": {
				"type": "array",
				"items": {
					"type": "object",
					"properties": {
						"name": {"type": "string", "description": "Column name"},
						"type": {"type": "string", "enum": ["TEXT", "INTEGER", "REAL", "BLOB"], "description": "Column type"}
					},
					"required": ["name", "type"]
				},
				"description": "Column definitions"
			}
		},
		"required": ["name", "description", "columns"]
	}`)
}

func (t *CreateTableTool) Execute(ctx context.Context, params string, tctx *Context) (*Result, error) {
	var args struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Columns     []struct {
			Name string `json:"name"`
			Type string `json:"type"`
		} `json:"columns"`
	}
	if err := json.Unmarshal([]byte(params), &args); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}
	if tctx.Store == nil {
		return nil, fmt.Errorf("database store not available")
	}

	// Validate table name.
	if !validIdentifier.MatchString(args.Name) {
		return nil, fmt.Errorf("table name must be alphanumeric and underscores only")
	}
	if strings.TrimSpace(args.Description) == "" {
		return nil, fmt.Errorf("description is required")
	}

	// Validate columns.
	if len(args.Columns) == 0 {
		return nil, fmt.Errorf("at least one column is required")
	}
	if len(args.Columns) > maxColumns {
		return nil, fmt.Errorf("maximum %d columns allowed", maxColumns)
	}
	for _, col := range args.Columns {
		if !validIdentifier.MatchString(col.Name) {
			return nil, fmt.Errorf("column name %q must be alphanumeric and underscores only", col.Name)
		}
		if !allowedColumnTypes[strings.ToUpper(col.Type)] {
			return nil, fmt.Errorf("column type %q not allowed; use TEXT, INTEGER, REAL, or BLOB", col.Type)
		}
	}

	// Check table count limit.
	var count int
	err := tctx.Store.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM hippocampus WHERE created_by = ? AND agent_owned = 1`,
		tctx.AgentID).Scan(&count)
	if err != nil {
		return nil, fmt.Errorf("count agent tables: %w", err)
	}
	if count >= maxAgentTables {
		return nil, fmt.Errorf("agent table limit reached (%d/%d)", count, maxAgentTables)
	}

	tableName := agentTableName(tctx.AgentID, args.Name)

	// Build CREATE TABLE statement.
	var colDefs []string
	for _, col := range args.Columns {
		colDefs = append(colDefs, fmt.Sprintf("%s %s", col.Name, strings.ToUpper(col.Type)))
	}
	createSQL := fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s (id TEXT PRIMARY KEY, %s)",
		tableName, strings.Join(colDefs, ", "))

	if _, err := tctx.Store.ExecContext(ctx, createSQL); err != nil {
		return nil, fmt.Errorf("create table: %w", err)
	}

	// Register in hippocampus.
	colsJSON, err := json.Marshal(args.Columns)
	if err != nil {
		return nil, fmt.Errorf("marshal columns: %w", err)
	}
	_, err = tctx.Store.ExecContext(ctx,
		`INSERT OR REPLACE INTO hippocampus (table_name, description, columns_json, created_by, agent_owned)
		 VALUES (?, ?, ?, ?, 1)`,
		tableName, args.Description, string(colsJSON), tctx.AgentID)
	if err != nil {
		return nil, fmt.Errorf("register in hippocampus: %w", err)
	}

	return &Result{Output: fmt.Sprintf("Created table %q with %d columns", tableName, len(args.Columns))}, nil
}

// --- QueryTableTool ---

// QueryTableTool lets the agent query its own tables.
type QueryTableTool struct{}

func (t *QueryTableTool) Name() string        { return "query_table" }
func (t *QueryTableTool) Description() string { return "Query rows from an agent-owned table." }
func (t *QueryTableTool) Risk() RiskLevel     { return RiskCaution }
func (t *QueryTableTool) ParameterSchema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"table": {"type": "string", "description": "Table name (without agent prefix)"},
			"query": {"type": "string", "description": "Optional WHERE clause (without WHERE keyword)"},
			"limit": {"type": "integer", "description": "Max rows to return (default 50)", "default": 50}
		},
		"required": ["table"]
	}`)
}

func (t *QueryTableTool) Execute(ctx context.Context, params string, tctx *Context) (*Result, error) {
	var args struct {
		Table string `json:"table"`
		Query string `json:"query"`
		Limit int    `json:"limit"`
	}
	args.Limit = 50
	if err := json.Unmarshal([]byte(params), &args); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}
	if tctx.Store == nil {
		return nil, fmt.Errorf("database store not available")
	}

	if !validIdentifier.MatchString(args.Table) {
		return nil, fmt.Errorf("table name must be alphanumeric and underscores only")
	}
	if args.Limit <= 0 || args.Limit > 500 {
		args.Limit = 50
	}

	tableName := agentTableName(tctx.AgentID, args.Table)

	// Validate table exists in hippocampus registry.
	var exists int
	err := tctx.Store.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM hippocampus WHERE table_name = ? AND agent_owned = 1`,
		tableName).Scan(&exists)
	if err != nil {
		return nil, fmt.Errorf("check hippocampus: %w", err)
	}
	if exists == 0 {
		return nil, fmt.Errorf("table %q not found in hippocampus registry", args.Table)
	}

	// Build query. The WHERE clause is user-provided text that runs against
	// an agent-owned table the agent itself created, so it is intentionally
	// flexible. The table name is validated above.
	querySQL := fmt.Sprintf("SELECT * FROM %s", tableName)
	if strings.TrimSpace(args.Query) != "" {
		querySQL += " WHERE " + args.Query
	}
	querySQL += fmt.Sprintf(" LIMIT %d", args.Limit)

	rows, err := tctx.Store.QueryContext(ctx, querySQL)
	if err != nil {
		return nil, fmt.Errorf("query table: %w", err)
	}
	defer func() { _ = rows.Close() }()

	columns, err := rows.Columns()
	if err != nil {
		return nil, fmt.Errorf("get columns: %w", err)
	}

	var results []map[string]any
	for rows.Next() {
		values := make([]any, len(columns))
		ptrs := make([]any, len(columns))
		for i := range values {
			ptrs[i] = &values[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, fmt.Errorf("scan row: %w", err)
		}
		row := make(map[string]any)
		for i, col := range columns {
			row[col] = values[i]
		}
		results = append(results, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate rows: %w", err)
	}

	if len(results) == 0 {
		return &Result{Output: "[]"}, nil
	}

	data, err := json.Marshal(results)
	if err != nil {
		return nil, fmt.Errorf("marshal results: %w", err)
	}
	return &Result{Output: string(data)}, nil
}

// --- InsertRowTool ---

// InsertRowTool lets the agent insert rows into its own tables.
type InsertRowTool struct{}

func (t *InsertRowTool) Name() string        { return "insert_row" }
func (t *InsertRowTool) Description() string { return "Insert a row into an agent-owned table." }
func (t *InsertRowTool) Risk() RiskLevel     { return RiskCaution }
func (t *InsertRowTool) ParameterSchema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"table": {"type": "string", "description": "Table name (without agent prefix)"},
			"data":  {"type": "object", "description": "Column name to value mapping"}
		},
		"required": ["table", "data"]
	}`)
}

func (t *InsertRowTool) Execute(ctx context.Context, params string, tctx *Context) (*Result, error) {
	var args struct {
		Table string         `json:"table"`
		Data  map[string]any `json:"data"`
	}
	if err := json.Unmarshal([]byte(params), &args); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}
	if tctx.Store == nil {
		return nil, fmt.Errorf("database store not available")
	}

	if !validIdentifier.MatchString(args.Table) {
		return nil, fmt.Errorf("table name must be alphanumeric and underscores only")
	}
	if len(args.Data) == 0 {
		return nil, fmt.Errorf("data must contain at least one column")
	}

	tableName := agentTableName(tctx.AgentID, args.Table)

	// Validate table exists and get registered columns.
	var colsJSON string
	err := tctx.Store.QueryRowContext(ctx,
		`SELECT columns_json FROM hippocampus WHERE table_name = ? AND agent_owned = 1`,
		tableName).Scan(&colsJSON)
	if err != nil {
		return nil, fmt.Errorf("table %q not found in hippocampus registry", args.Table)
	}

	var registeredCols []struct {
		Name string `json:"name"`
		Type string `json:"type"`
	}
	if err := json.Unmarshal([]byte(colsJSON), &registeredCols); err != nil {
		return nil, fmt.Errorf("parse registered columns: %w", err)
	}

	validCols := make(map[string]bool)
	for _, c := range registeredCols {
		validCols[c.Name] = true
	}

	// Validate all provided column names.
	for col := range args.Data {
		if !validIdentifier.MatchString(col) {
			return nil, fmt.Errorf("column name %q must be alphanumeric and underscores only", col)
		}
		if !validCols[col] {
			return nil, fmt.Errorf("column %q is not registered for table %q", col, args.Table)
		}
	}

	// Build INSERT statement with auto-generated id.
	id := db.NewID()
	colNames := []string{"id"}
	placeholders := []string{"?"}
	values := []any{id}

	for col, val := range args.Data {
		colNames = append(colNames, col)
		placeholders = append(placeholders, "?")
		values = append(values, val)
	}

	insertSQL := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)",
		tableName,
		strings.Join(colNames, ", "),
		strings.Join(placeholders, ", "))

	if _, err := tctx.Store.ExecContext(ctx, insertSQL, values...); err != nil {
		return nil, fmt.Errorf("insert row: %w", err)
	}

	return &Result{Output: fmt.Sprintf("Inserted row (id=%s) into %s", id, tableName)}, nil
}
