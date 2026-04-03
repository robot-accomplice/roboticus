package db

import (
	"context"
	"fmt"
	"time"
)

// PolicyDecision represents a row in policy_decisions.
type PolicyDecision struct {
	ID        string
	TurnID    string
	ToolName  string
	Decision  string
	RuleName  string
	Reason    string
	CreatedAt string
}

// PolicyRepository handles policy decision persistence.
type PolicyRepository struct {
	q Querier
}

// NewPolicyRepository creates a policy repository.
func NewPolicyRepository(q Querier) *PolicyRepository {
	return &PolicyRepository{q: q}
}

// RecordDecision inserts a policy decision.
func (r *PolicyRepository) RecordDecision(ctx context.Context, turnID, toolName, decision, rule, reason string) error {
	id := fmt.Sprintf("pd-%d", time.Now().UnixNano())
	_, err := r.q.ExecContext(ctx,
		`INSERT INTO policy_decisions (id, turn_id, tool_name, decision, rule_name, reason)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		id, turnID, toolName, decision, rule, reason,
	)
	return err
}

// ListByTurn returns all policy decisions for a given turn.
func (r *PolicyRepository) ListByTurn(ctx context.Context, turnID string) ([]PolicyDecision, error) {
	rows, err := r.q.QueryContext(ctx,
		`SELECT id, turn_id, tool_name, decision, COALESCE(rule_name,''), COALESCE(reason,''), created_at
		 FROM policy_decisions WHERE turn_id = ? ORDER BY created_at ASC`,
		turnID,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var result []PolicyDecision
	for rows.Next() {
		var d PolicyDecision
		if err := rows.Scan(&d.ID, &d.TurnID, &d.ToolName, &d.Decision, &d.RuleName, &d.Reason, &d.CreatedAt); err != nil {
			return nil, err
		}
		result = append(result, d)
	}
	return result, rows.Err()
}
