package memory

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// retrieveSemanticMemory fetches from the semantic_memory table.
func (mr *Retriever) retrieveSemanticMemory(ctx context.Context, query string, budgetTokens int) string {
	maxChars := budgetTokens * mr.charsPerToken

	var rows *sql.Rows
	var err error
	if query != "" {
		rows, err = mr.store.QueryContext(ctx,
			`SELECT category, key, value FROM semantic_memory
			 WHERE memory_state = 'active' AND (value LIKE ? OR key LIKE ?)
			 ORDER BY confidence DESC, updated_at DESC LIMIT 20`,
			"%"+query+"%", "%"+query+"%")
	}
	if err != nil || rows == nil {
		rows, err = mr.store.QueryContext(ctx,
			`SELECT category, key, value FROM semantic_memory
			 WHERE memory_state = 'active'
			 ORDER BY confidence DESC, updated_at DESC LIMIT 20`)
	}
	if err != nil {
		return ""
	}
	defer func() { _ = rows.Close() }()

	var b strings.Builder
	used := 0
	for rows.Next() {
		var category, key, value string
		if rows.Scan(&category, &key, &value) != nil {
			continue
		}
		line := fmt.Sprintf("[%s] %s: %s", category, key, value)
		if used+len(line) > maxChars {
			break
		}
		b.WriteString("- ")
		b.WriteString(line)
		b.WriteString("\n")
		used += len(line)
	}
	return b.String()
}

// retrieveProceduralMemory formats tool success/failure statistics from procedural_memory.
func (mr *Retriever) retrieveProceduralMemory(ctx context.Context, budgetTokens int) string {
	rows, err := mr.store.QueryContext(ctx,
		`SELECT name, success_count, failure_count FROM procedural_memory
		 ORDER BY (success_count + failure_count) DESC LIMIT 20`)
	if err != nil {
		return ""
	}
	defer func() { _ = rows.Close() }()

	var b strings.Builder
	for rows.Next() {
		var name string
		var successCount, failureCount int
		if rows.Scan(&name, &successCount, &failureCount) != nil {
			continue
		}
		total := successCount + failureCount
		if total == 0 {
			continue
		}
		pct := float64(successCount) / float64(total) * 100
		fmt.Fprintf(&b, "- %s: %d/%d (%.0f%% success)\n", name, successCount, total, pct)
	}
	return b.String()
}

// retrieveRelationshipMemory formats relationship data.
func (mr *Retriever) retrieveRelationshipMemory(ctx context.Context, budgetTokens int) string {
	maxChars := budgetTokens * mr.charsPerToken

	rows, err := mr.store.QueryContext(ctx,
		`SELECT entity_name, trust_score, interaction_count, last_interaction
		 FROM relationship_memory
		 ORDER BY interaction_count DESC LIMIT 20`)
	if err != nil {
		return ""
	}
	defer func() { _ = rows.Close() }()

	var b strings.Builder
	used := 0
	for rows.Next() {
		var entityName string
		var trustScore float64
		var interactionCount int
		var lastInteraction *string
		if rows.Scan(&entityName, &trustScore, &interactionCount, &lastInteraction) != nil {
			continue
		}
		line := fmt.Sprintf("%s: trust=%.1f, interactions=%d", entityName, trustScore, interactionCount)
		if lastInteraction != nil {
			line += ", last=" + *lastInteraction
		}
		if used+len(line) > maxChars {
			break
		}
		b.WriteString("- ")
		b.WriteString(line)
		b.WriteString("\n")
		used += len(line)
	}
	return b.String()
}
