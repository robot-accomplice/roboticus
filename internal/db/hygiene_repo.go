package db

import (
	"context"
	"fmt"
	"time"
)

// HygieneSweepRow represents a row in hygiene_log.
type HygieneSweepRow struct {
	ID                          string
	SweepAt                     string
	StaleProceduralDays         int
	DeadSkillPriorityThreshold  int
	ProcTotal                   int
	ProcStale                   int
	ProcPruned                  int
	SkillsTotal                 int
	SkillsDead                  int
	SkillsPruned                int
	AvgSkillPriority            float64
}

// HygieneRepository handles hygiene sweep log persistence.
type HygieneRepository struct {
	q Querier
}

// NewHygieneRepository creates a hygiene repository.
func NewHygieneRepository(q Querier) *HygieneRepository {
	return &HygieneRepository{q: q}
}

// RecordSweep inserts a hygiene sweep record.
func (r *HygieneRepository) RecordSweep(ctx context.Context, row HygieneSweepRow) error {
	if row.ID == "" {
		row.ID = fmt.Sprintf("hyg-%d", time.Now().UnixNano())
	}
	_, err := r.q.ExecContext(ctx,
		`INSERT INTO hygiene_log (id, stale_procedural_days, dead_skill_priority_threshold,
		 proc_total, proc_stale, proc_pruned, skills_total, skills_dead, skills_pruned, avg_skill_priority)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		row.ID, row.StaleProceduralDays, row.DeadSkillPriorityThreshold,
		row.ProcTotal, row.ProcStale, row.ProcPruned,
		row.SkillsTotal, row.SkillsDead, row.SkillsPruned, row.AvgSkillPriority,
	)
	return err
}

// ListSweeps returns the most recent hygiene sweeps.
func (r *HygieneRepository) ListSweeps(ctx context.Context, limit int) ([]HygieneSweepRow, error) {
	rows, err := r.q.QueryContext(ctx,
		`SELECT id, sweep_at, stale_procedural_days, dead_skill_priority_threshold,
		 proc_total, proc_stale, proc_pruned, skills_total, skills_dead, skills_pruned, avg_skill_priority
		 FROM hygiene_log ORDER BY sweep_at DESC LIMIT ?`,
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var result []HygieneSweepRow
	for rows.Next() {
		var s HygieneSweepRow
		if err := rows.Scan(&s.ID, &s.SweepAt, &s.StaleProceduralDays, &s.DeadSkillPriorityThreshold,
			&s.ProcTotal, &s.ProcStale, &s.ProcPruned,
			&s.SkillsTotal, &s.SkillsDead, &s.SkillsPruned, &s.AvgSkillPriority); err != nil {
			return nil, err
		}
		result = append(result, s)
	}
	return result, rows.Err()
}
