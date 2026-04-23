package db

import (
	"context"
	"database/sql"
	"strings"
)

// IsSubagentName reports whether agentID resolves to a registered subagent role.
// This is the authoritative runtime discriminator for prompt/profile policy and
// should be preferred over agent-id heuristics.
func IsSubagentName(ctx context.Context, store *Store, agentID string) (bool, error) {
	if store == nil || strings.TrimSpace(agentID) == "" {
		return false, nil
	}
	var one int
	err := store.QueryRowContext(ctx,
		`SELECT 1 FROM sub_agents WHERE name = ? AND role = 'subagent' LIMIT 1`,
		strings.TrimSpace(agentID),
	).Scan(&one)
	if err == nil {
		return true, nil
	}
	if err == sql.ErrNoRows {
		return false, nil
	}
	return false, err
}
