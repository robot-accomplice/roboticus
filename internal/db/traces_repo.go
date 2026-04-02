package db

import (
	"context"
	"database/sql"
)

// PipelineTraceRow represents a stored pipeline trace.
type PipelineTraceRow struct {
	ID         string
	TurnID     string
	SessionID  string
	Channel    string
	TotalMs    int64
	StagesJSON string
	CreatedAt  string
}

// TraceFilter controls which traces to list.
type TraceFilter struct {
	Channel   string
	SessionID string
	Limit     int
}

// TraceRepository handles pipeline and react trace persistence.
// All queries go through the Querier interface (centralized connection pool).
type TraceRepository struct {
	q Querier
}

// NewTraceRepository creates a trace repository.
func NewTraceRepository(q Querier) *TraceRepository {
	return &TraceRepository{q: q}
}

// SavePipelineTrace inserts a pipeline trace row.
func (r *TraceRepository) SavePipelineTrace(ctx context.Context, row PipelineTraceRow) error {
	_, err := r.q.ExecContext(ctx,
		`INSERT INTO pipeline_traces (id, turn_id, session_id, channel, total_ms, stages_json)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		row.ID, row.TurnID, row.SessionID, row.Channel, row.TotalMs, row.StagesJSON,
	)
	return err
}

// SaveReactTrace inserts a react trace linked to a pipeline trace.
func (r *TraceRepository) SaveReactTrace(ctx context.Context, id, pipelineTraceID, reactJSON string) error {
	_, err := r.q.ExecContext(ctx,
		`INSERT INTO react_traces (id, pipeline_trace_id, react_json)
		 VALUES (?, ?, ?)`,
		id, pipelineTraceID, reactJSON,
	)
	return err
}

// GetByTurnID retrieves a pipeline trace by turn ID.
func (r *TraceRepository) GetByTurnID(ctx context.Context, turnID string) (*PipelineTraceRow, error) {
	row := r.q.QueryRowContext(ctx,
		`SELECT id, turn_id, session_id, channel, total_ms, stages_json, created_at
		 FROM pipeline_traces WHERE turn_id = ?`,
		turnID,
	)
	var tr PipelineTraceRow
	err := row.Scan(&tr.ID, &tr.TurnID, &tr.SessionID, &tr.Channel, &tr.TotalMs, &tr.StagesJSON, &tr.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &tr, nil
}

// ListTraces returns traces matching the filter.
func (r *TraceRepository) ListTraces(ctx context.Context, filter TraceFilter) ([]PipelineTraceRow, error) {
	query := `SELECT id, turn_id, session_id, channel, total_ms, stages_json, created_at FROM pipeline_traces`
	var args []any
	var where []string

	if filter.Channel != "" {
		where = append(where, "channel = ?")
		args = append(args, filter.Channel)
	}
	if filter.SessionID != "" {
		where = append(where, "session_id = ?")
		args = append(args, filter.SessionID)
	}
	if len(where) > 0 {
		query += " WHERE "
		for i, w := range where {
			if i > 0 {
				query += " AND "
			}
			query += w
		}
	}
	query += " ORDER BY created_at DESC"
	if filter.Limit > 0 {
		query += " LIMIT ?"
		args = append(args, filter.Limit)
	}

	rows, err := r.q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var result []PipelineTraceRow
	for rows.Next() {
		var tr PipelineTraceRow
		if err := rows.Scan(&tr.ID, &tr.TurnID, &tr.SessionID, &tr.Channel, &tr.TotalMs, &tr.StagesJSON, &tr.CreatedAt); err != nil {
			return nil, err
		}
		result = append(result, tr)
	}
	return result, rows.Err()
}
