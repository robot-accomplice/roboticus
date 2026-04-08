package tools

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"roboticus/internal/db"
)

// MemoryRecallTool allows the agent to look up full content from the memory index.
// The system prompt injects a summary index of top memories; the agent can call
// this tool with an ID to retrieve the complete content.
//
// Matches Rust's memory index injection + recall_memory on-demand pattern.
type MemoryRecallTool struct {
	store *db.Store
}

// NewMemoryRecallTool creates a memory recall tool.
func NewMemoryRecallTool(store *db.Store) *MemoryRecallTool {
	return &MemoryRecallTool{store: store}
}

func (t *MemoryRecallTool) Name() string { return "recall_memory" }
func (t *MemoryRecallTool) Description() string {
	return "Retrieve full content of a memory entry by ID. Use when the memory index summary is insufficient and you need complete details."
}
func (t *MemoryRecallTool) Risk() RiskLevel { return RiskSafe }
func (t *MemoryRecallTool) ParameterSchema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"memory_id": {
				"type": "string",
				"description": "The ID of the memory entry to recall (from the memory index)"
			},
			"source_table": {
				"type": "string",
				"description": "The source table: episodic_memory, semantic_memory, working_memory, procedural_memory",
				"enum": ["episodic_memory", "semantic_memory", "working_memory", "procedural_memory", "relationship_memory"]
			}
		},
		"required": ["memory_id"]
	}`)
}

func (t *MemoryRecallTool) Execute(ctx context.Context, argsJSON string, tctx *Context) (*Result, error) {
	if t.store == nil {
		return &Result{Output: "memory store not available"}, nil
	}

	var args struct {
		MemoryID    string `json:"memory_id"`
		SourceTable string `json:"source_table"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return &Result{Output: "invalid arguments: " + err.Error()}, nil
	}
	if args.MemoryID == "" {
		return &Result{Output: "memory_id is required"}, nil
	}

	// If source_table not specified, look up in the memory_index first.
	sourceTable := args.SourceTable
	sourceID := args.MemoryID
	if sourceTable == "" {
		row := t.store.QueryRowContext(ctx,
			`SELECT source_table, source_id FROM memory_index WHERE id = ?`, args.MemoryID)
		if err := row.Scan(&sourceTable, &sourceID); err != nil {
			// Fall back: try each table directly.
			sourceTable = ""
		}
	}

	// Search across tables if no specific source.
	if sourceTable == "" {
		tables := []struct {
			name    string
			columns string
		}{
			{"episodic_memory", "classification, content, importance, created_at"},
			{"semantic_memory", "category, key, value, confidence, created_at"},
			{"working_memory", "entry_type, content, importance, created_at"},
			{"procedural_memory", "name, steps, success_count, failure_count, created_at"},
			{"relationship_memory", "entity_name, trust_score, interaction_summary, interaction_count, last_interaction"},
		}
		for _, tbl := range tables {
			row := t.store.QueryRowContext(ctx,
				fmt.Sprintf("SELECT %s FROM %s WHERE id = ?", tbl.columns, tbl.name), sourceID)
			content, err := scanToMap(row, strings.Split(tbl.columns, ", "))
			if err == nil {
				content["source_table"] = tbl.name
				content["id"] = sourceID
				b, _ := json.MarshalIndent(content, "", "  ")
				return &Result{Output: string(b), Source: "builtin"}, nil
			}
		}
		return &Result{Output: "memory entry not found: " + args.MemoryID}, nil
	}

	// Direct table lookup.
	var columns string
	switch sourceTable {
	case "episodic_memory":
		columns = "classification, content, importance, created_at"
	case "semantic_memory":
		columns = "category, key, value, confidence, created_at"
	case "working_memory":
		columns = "entry_type, content, importance, created_at"
	case "procedural_memory":
		columns = "name, steps, success_count, failure_count, created_at"
	case "relationship_memory":
		columns = "entity_name, trust_score, interaction_summary, interaction_count, last_interaction"
	default:
		return &Result{Output: "unknown source table: " + sourceTable}, nil
	}

	row := t.store.QueryRowContext(ctx,
		fmt.Sprintf("SELECT %s FROM %s WHERE id = ?", columns, sourceTable), sourceID)
	content, err := scanToMap(row, strings.Split(columns, ", "))
	if err != nil {
		return &Result{Output: "memory entry not found"}, nil
	}
	content["source_table"] = sourceTable
	content["id"] = sourceID

	// Reinforce confidence — active recall prevents decay (Rust parity).
	_, _ = t.store.ExecContext(ctx,
		`UPDATE memory_index SET confidence = 1.0, last_verified = datetime('now')
		 WHERE source_table = ? AND source_id = ?`, sourceTable, sourceID)

	b, _ := json.MarshalIndent(content, "", "  ")
	return &Result{Output: string(b), Source: "builtin"}, nil
}

// scanToMap scans a single row into a string map using the given column names.
func scanToMap(row *sql.Row, columns []string) (map[string]any, error) {
	values := make([]any, len(columns))
	ptrs := make([]any, len(columns))
	for i := range values {
		ptrs[i] = &values[i]
	}
	if err := row.Scan(ptrs...); err != nil {
		return nil, err
	}
	result := make(map[string]any, len(columns))
	for i, col := range columns {
		result[strings.TrimSpace(col)] = values[i]
	}
	return result, nil
}

// BuildMemoryIndex generates the top-N memory index for system prompt injection.
// Returns a compact list of memory references the agent can use with recall_memory.
func BuildMemoryIndex(ctx context.Context, store *db.Store, limit int) string {
	if store == nil || limit <= 0 {
		return ""
	}

	rows, err := store.QueryContext(ctx,
		`SELECT id, source_table, summary, category FROM memory_index
		 ORDER BY confidence DESC, created_at DESC LIMIT ?`, limit)
	if err != nil {
		return ""
	}
	defer func() { _ = rows.Close() }()

	var lines []string
	for rows.Next() {
		var id, table, summary, category string
		if err := rows.Scan(&id, &table, &summary, &category); err != nil {
			continue
		}
		if category != "" {
			lines = append(lines, fmt.Sprintf("- [%s|%s] %s (recall: %s)", table, category, summary, id))
		} else {
			lines = append(lines, fmt.Sprintf("- [%s] %s (recall: %s)", table, summary, id))
		}
	}
	if len(lines) == 0 {
		return ""
	}
	return "[Memory Index]\n" + strings.Join(lines, "\n") +
		"\n\nUse recall_memory(memory_id) to retrieve full content of any entry."
}
