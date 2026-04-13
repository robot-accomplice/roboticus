package pipeline

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/rs/zerolog/log"

	"roboticus/internal/db"
)

// ---------------------------------------------------------------------------
// Pipeline utility functions — mirrors Rust's crates/roboticus-pipeline/src/utils.rs
// ---------------------------------------------------------------------------

// IsModelProxyRole checks if a subagent role designates a model-proxy
// (not a taskable agent). Case-insensitive comparison.
func IsModelProxyRole(role string) bool {
	return strings.EqualFold(role, "model-proxy")
}

// SkillRegistryNamesFromDB collects all known skill names from the database.
// Returns a deduplicated set of skill names (lowercased) from enabled
// database-registered skills.
func SkillRegistryNamesFromDB(store *db.Store) map[string]struct{} {
	names := make(map[string]struct{})

	rows, err := store.QueryContext(context.Background(),
		`SELECT name FROM skills WHERE enabled = 1`)
	if err != nil {
		log.Warn().Err(err).Msg("pipeline: failed to query skill names from DB")
		return names
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			continue
		}
		names[strings.ToLower(name)] = struct{}{}
	}
	return names
}

// ParseSkillsJSON parses a JSON skills array (from a subagent's skills_json
// column). Returns an empty slice on nil input, invalid JSON, or non-array JSON.
func ParseSkillsJSON(raw *string) []string {
	if raw == nil || *raw == "" {
		return nil
	}

	var result []string
	if err := json.Unmarshal([]byte(*raw), &result); err != nil {
		log.Warn().Err(err).Str("raw", *raw).Msg("pipeline: failed to parse skills JSON")
		return nil
	}
	return result
}
