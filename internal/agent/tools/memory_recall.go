package tools

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/rs/zerolog/log"

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

	// Reinforce confidence — active recall prevents decay.
	// Beyond-parity: uses +0.1 (capped at 1.0) instead of resetting to 1.0.
	// This allows organic confidence growth rather than binary jumps that
	// cause all recalled entries to pile up at 1.0 indistinguishably.
	if _, err := t.store.ExecContext(ctx,
		`UPDATE memory_index SET confidence = MIN(1.0, confidence + 0.1), last_verified = datetime('now')
		 WHERE source_table = ? AND source_id = ?`, sourceTable, sourceID); err != nil {
		log.Warn().Err(err).Str("source", sourceTable).Str("id", sourceID).Msg("recall: confidence reinforce failed")
	}

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

// MemorySearchTool allows the agent to search memories by topic/keyword.
// This fills a design gap that exists in both Rust and Go: the agent previously
// had no way to find memories by topic — only by ID from the injected index.
//
// Search strategy: FTS5 MATCH on memory_fts, with LIKE fallback across
// episodic, semantic, and relationship tiers for non-FTS-indexed content.
type MemorySearchTool struct {
	store *db.Store
}

// NewMemorySearchTool creates a memory search tool.
func NewMemorySearchTool(store *db.Store) *MemorySearchTool {
	return &MemorySearchTool{store: store}
}

func (t *MemorySearchTool) Name() string { return "search_memories" }
func (t *MemorySearchTool) Description() string {
	return "Search memories by topic or keyword. Use when asked about past conversations, " +
		"specific people, events, or topics — especially when the Memory Index doesn't " +
		"contain what you need. Returns matching memories with IDs you can pass to recall_memory."
}
func (t *MemorySearchTool) Risk() RiskLevel { return RiskSafe }
func (t *MemorySearchTool) ParameterSchema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"query": {
				"type": "string",
				"description": "Topic, keyword, or phrase to search for (e.g., 'palm', 'job offer', 'deployment')"
			},
			"limit": {
				"type": "integer",
				"description": "Maximum results to return (default 10, max 25)"
			}
		},
		"required": ["query"]
	}`)
}

func (t *MemorySearchTool) Execute(ctx context.Context, argsJSON string, tctx *Context) (*Result, error) {
	if t.store == nil {
		return &Result{Output: "memory store not available"}, nil
	}

	var args struct {
		Query string `json:"query"`
		Limit int    `json:"limit"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return &Result{Output: "invalid arguments: " + err.Error()}, nil
	}
	if args.Query == "" {
		return &Result{Output: "query is required"}, nil
	}
	if args.Limit <= 0 || args.Limit > 25 {
		args.Limit = 10
	}

	type memResult struct {
		SourceTable string `json:"source_table"`
		SourceID    string `json:"source_id"`
		Content     string `json:"content"`
		Category    string `json:"category,omitempty"`
	}
	var results []memResult
	seen := make(map[string]struct{})

	// Leg 1: FTS5 MATCH on memory_fts.
	ftsQuery := db.SanitizeFTSQuery(args.Query)
	if ftsQuery != "" {
		rows, err := t.store.QueryContext(ctx,
			`SELECT content, source_table, source_id, category
			 FROM memory_fts WHERE memory_fts MATCH ? LIMIT ?`,
			ftsQuery, args.Limit*2)
		if err == nil {
			for rows.Next() {
				var r memResult
				if rows.Scan(&r.Content, &r.SourceTable, &r.SourceID, &r.Category) != nil {
					continue
				}
				key := r.SourceTable + "|" + r.SourceID
				if _, dup := seen[key]; dup {
					continue
				}
				seen[key] = struct{}{}
				// Truncate content preview.
				if len(r.Content) > 300 {
					r.Content = r.Content[:300] + "..."
				}
				results = append(results, r)
				if len(results) >= args.Limit {
					break
				}
			}
			_ = rows.Close()
		} else {
			log.Debug().Err(err).Str("query", ftsQuery).Msg("search_memories: FTS5 failed")
		}
	}

	// Leg 2: LIKE fallback for tiers not in FTS (procedural, relationship)
	// and as backup if FTS returned few results.
	if len(results) < args.Limit {
		remaining := args.Limit - len(results)
		likePattern := "%" + args.Query + "%"

		// Relationship memory (not in FTS).
		rows, err := t.store.QueryContext(ctx,
			`SELECT 'relationship_memory', id,
			   entity_name || ' (trust=' || CAST(trust_score AS TEXT) || ', interactions=' || CAST(interaction_count AS TEXT) || '): ' || COALESCE(interaction_summary, ''),
			   ''
			 FROM relationship_memory
			 WHERE entity_name LIKE ? OR interaction_summary LIKE ?
			 LIMIT ?`, likePattern, likePattern, remaining)
		if err == nil {
			for rows.Next() {
				var r memResult
				if rows.Scan(&r.SourceTable, &r.SourceID, &r.Content, &r.Category) != nil {
					continue
				}
				key := r.SourceTable + "|" + r.SourceID
				if _, dup := seen[key]; dup {
					continue
				}
				seen[key] = struct{}{}
				results = append(results, r)
			}
			_ = rows.Close()
		}

		// Procedural memory (not in FTS).
		if len(results) < args.Limit {
			remaining = args.Limit - len(results)
			rows, err = t.store.QueryContext(ctx,
				`SELECT 'procedural_memory', id,
				   name || ' (' || CAST(success_count AS TEXT) || ' success, ' || CAST(failure_count AS TEXT) || ' fail)',
				   ''
				 FROM procedural_memory WHERE name LIKE ? OR steps LIKE ?
				 LIMIT ?`, likePattern, likePattern, remaining)
			if err == nil {
				for rows.Next() {
					var r memResult
					if rows.Scan(&r.SourceTable, &r.SourceID, &r.Content, &r.Category) != nil {
						continue
					}
					key := r.SourceTable + "|" + r.SourceID
					if _, dup := seen[key]; dup {
						continue
					}
					seen[key] = struct{}{}
					results = append(results, r)
				}
				_ = rows.Close()
			}
		}
	}

	if len(results) == 0 {
		return &Result{
			Output: fmt.Sprintf("No memories found matching '%s'. This topic may not have been stored in memory.", args.Query),
			Source: "builtin",
		}, nil
	}

	// Reinforce confidence on matched entries (active recall prevents decay).
	for _, r := range results {
		_, _ = t.store.ExecContext(ctx,
			`UPDATE memory_index SET confidence = MIN(1.0, confidence + 0.1), last_verified = datetime('now')
			 WHERE source_table = ? AND source_id = ?`, r.SourceTable, r.SourceID)
	}

	b, _ := json.MarshalIndent(results, "", "  ")
	return &Result{
		Output: fmt.Sprintf("Found %d memories matching '%s':\n%s", len(results), args.Query, string(b)),
		Source: "builtin",
	}, nil
}

// BuildMemoryIndex generates the top-N memory index for system prompt injection.
// Returns a compact list of memory references the agent can use with recall_memory.
//
// When query is non-empty, the index is query-aware: FTS-matched entries are
// injected first (up to limit/3), then the remaining slots are filled with the
// tier-priority top-N. This ensures the model sees relevant entries for the
// current query, not just a static global top-N.
func BuildMemoryIndex(ctx context.Context, store *db.Store, limit int, query ...string) string {
	if store == nil || limit <= 0 {
		return ""
	}

	type indexEntry struct {
		id       string
		table    string
		summary  string
		category string
	}
	seen := make(map[string]struct{})
	var entries []indexEntry

	// Query-aware injection: if we have a query, FTS-match entries go first.
	q := ""
	if len(query) > 0 {
		q = query[0]
	}
	if q != "" {
		querySlots := limit / 3
		if querySlots < 3 {
			querySlots = 3
		}

		// Strategy 1: Search memory_index summaries directly (catches obsidian,
		// semantic, and any tier regardless of FTS coverage).
		likePattern := "%" + q + "%"
		rows, err := store.QueryContext(ctx,
			`SELECT id, source_table, summary, COALESCE(category, '')
			 FROM memory_index
			 WHERE confidence > 0.1
			   AND source_table != 'system'
			   AND summary LIKE ?`+toolNoiseFilter()+`
			 ORDER BY confidence DESC, created_at DESC
			 LIMIT ?`, likePattern, querySlots)
		if err == nil {
			for rows.Next() {
				var e indexEntry
				if rows.Scan(&e.id, &e.table, &e.summary, &e.category) != nil {
					continue
				}
				if _, dup := seen[e.id]; !dup {
					seen[e.id] = struct{}{}
					entries = append(entries, e)
				}
			}
			_ = rows.Close()
		}

		// Strategy 2: FTS5 MATCH on memory_fts -> JOIN to memory_index
		// (catches content matches not visible in the summary).
		if len(entries) < querySlots {
			ftsQuery := db.SanitizeFTSQuery(q)
			if ftsQuery != "" {
				remaining := querySlots - len(entries)
				rows, err = store.QueryContext(ctx,
					`SELECT DISTINCT mi.id, mi.source_table, mi.summary, COALESCE(mi.category, '')
					 FROM memory_fts fts
					 JOIN memory_index mi ON (
					   (mi.source_table = fts.source_table || '_memory' AND mi.source_id = fts.source_id)
					   OR (mi.source_table = fts.source_table AND mi.source_id = fts.source_id)
					 )
					 WHERE memory_fts MATCH ?
					   AND mi.confidence > 0.1
					   AND mi.source_table != 'system'`+toolNoiseFilter()+`
					 LIMIT ?`, ftsQuery, remaining)
				if err == nil {
					for rows.Next() {
						var e indexEntry
						if rows.Scan(&e.id, &e.table, &e.summary, &e.category) != nil {
							continue
						}
						if _, dup := seen[e.id]; !dup {
							seen[e.id] = struct{}{}
							entries = append(entries, e)
						}
					}
					_ = rows.Close()
				}
			}
		}
	}

	// Fill remaining slots with tier-priority top-N (Rust parity: top_entries).
	remaining := limit - len(entries)
	if remaining > 0 {
		rows, err := store.QueryContext(ctx,
			`SELECT id, source_table, summary, COALESCE(category, '') FROM memory_index
			 WHERE confidence > 0.1
			   AND source_table != 'system'`+toolNoiseFilter()+`
			 ORDER BY
			   CASE source_table
			     WHEN 'semantic_memory' THEN 1
			     WHEN 'semantic' THEN 1
			     WHEN 'learned_skills' THEN 2
			     WHEN 'relationship_memory' THEN 3
			     WHEN 'procedural_memory' THEN 4
			     WHEN 'obsidian' THEN 5
			     WHEN 'episodic_memory' THEN 6
			     WHEN 'episodic' THEN 6
			     ELSE 7
			   END,
			   confidence DESC, created_at DESC
			 LIMIT ?`, remaining+10) // fetch extra to account for dedup
		if err == nil {
			for rows.Next() {
				var e indexEntry
				if rows.Scan(&e.id, &e.table, &e.summary, &e.category) != nil {
					continue
				}
				if _, dup := seen[e.id]; !dup {
					seen[e.id] = struct{}{}
					entries = append(entries, e)
					if len(entries) >= limit {
						break
					}
				}
			}
			_ = rows.Close()
		}
	}

	if len(entries) == 0 {
		return ""
	}

	var lines []string
	for _, e := range entries {
		if e.category != "" {
			lines = append(lines, fmt.Sprintf("- [%s|%s] %s (recall: %s)", e.table, e.category, e.summary, e.id))
		} else {
			lines = append(lines, fmt.Sprintf("- [%s] %s (recall: %s)", e.table, e.summary, e.id))
		}
	}
	return "[Memory Index — call recall_memory(id) or search_memories(query) for details]\n" +
		strings.Join(lines, "\n") +
		"\n\nWhen asked about a specific topic, person, or past event: first scan this index " +
		"for relevant entries and call recall_memory(id). If nothing matches, call " +
		"search_memories(query) to search the full memory store. " +
		"NEVER fabricate, synthesize, or guess at memory content."
}

// toolNoiseFilter returns SQL AND clauses that exclude tool-output noise from the memory index.
// Extracted for reuse across index queries.
func toolNoiseFilter() string {
	return `
		   AND NOT (summary LIKE 'Executed %: {%' AND summary LIKE '%[]%')
		   AND NOT (summary LIKE 'Executed %: error:%')
		   AND NOT (summary LIKE 'Executed %: %"count": 0%')
		   AND NOT (summary LIKE 'Used tool %: Error:%')
		   AND NOT (summary LIKE '%: Error: Policy denied%')
		   AND NOT (summary LIKE 'search_files: no matches%')
		   AND NOT (summary LIKE 'bash: %')
		   AND NOT (summary LIKE 'get_runtime_context:%')
		   AND NOT (summary LIKE 'get_memory_stats:%')
		   AND NOT (summary LIKE 'introspect:%')
		   AND NOT (summary LIKE 'query_table:%')`
}
