// semantic_supersession.go implements the canonical-knowledge-layer helpers
// for semantic memory (Milestone 3).
//
// The manager's upsert path already bumps version and refreshes
// effective_date when a key's value changes; the consolidation pipeline
// already sets memory_state='stale' and superseded_by when contradictions
// are detected. This file adds the read-side helpers the pipeline and tools
// need:
//
//   - CurrentSemanticValue walks the supersession chain from any semantic
//     entry (including stale ones) and returns the currently authoritative
//     revision plus the depth of the chain traversed.
//   - MarkSemanticSuperseded manually supersedes an entry when a caller
//     knows a specific newer row should replace it, without waiting for
//     consolidation's embedding-based detection to catch up.

package memory

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

// SemanticRevision captures the authoritative revision of a semantic fact
// along with the supersession chain that led here.
type SemanticRevision struct {
	ID            string
	Category      string
	Key           string
	Value         string
	Version       int
	Confidence    float64
	MemoryState   string
	EffectiveDate time.Time
	UpdatedAt     time.Time
	// ChainDepth is the number of supersession hops walked to reach the
	// current revision. A depth of 0 means the starting entry was already
	// authoritative.
	ChainDepth int
}

// ErrSemanticChainCycle is returned when the supersession chain forms a cycle.
// The chain is truncated at the first repeat and the revision closest to the
// starting ID is returned alongside the error so callers can still recover
// the most-current value they can trust.
var ErrSemanticChainCycle = errors.New("semantic memory: supersession chain cycle")

// ErrSemanticChainTooLong is returned when the supersession chain exceeds
// the max hop count. The most recent revision reached is still returned.
var ErrSemanticChainTooLong = errors.New("semantic memory: supersession chain too long")

// maxSupersessionHops caps chain traversal so a misbehaving consolidation
// run cannot produce an unbounded walk.
const maxSupersessionHops = 16

// CurrentSemanticValue follows the superseded_by chain from startID and
// returns the authoritative revision (memory_state='active' and no
// superseded_by pointer). Stale entries without a superseded_by pointer are
// returned as-is so callers can detect orphaned stale rows.
func (mm *Manager) CurrentSemanticValue(ctx context.Context, startID string) (*SemanticRevision, error) {
	if mm.store == nil || strings.TrimSpace(startID) == "" {
		return nil, nil
	}

	visited := make(map[string]struct{})
	currentID := startID
	var (
		last      *SemanticRevision
		hops      int
		cycleErr  error
		lengthErr error
	)

	for {
		if _, seen := visited[currentID]; seen {
			cycleErr = ErrSemanticChainCycle
			break
		}
		visited[currentID] = struct{}{}

		var (
			row         SemanticRevision
			effective   sql.NullString
			updated     sql.NullString
			superseded  sql.NullString
			memoryState sql.NullString
		)
		err := mm.store.QueryRowContext(ctx,
			`SELECT id, category, key, value, version, confidence,
			        memory_state, effective_date, updated_at, superseded_by
			   FROM semantic_memory
			  WHERE id = ?`,
			currentID,
		).Scan(
			&row.ID, &row.Category, &row.Key, &row.Value, &row.Version, &row.Confidence,
			&memoryState, &effective, &updated, &superseded,
		)
		if err != nil {
			if err == sql.ErrNoRows {
				if last != nil {
					return last, nil
				}
				return nil, nil
			}
			return nil, err
		}
		if memoryState.Valid {
			row.MemoryState = memoryState.String
		}
		row.EffectiveDate = parseSemanticTime(effective.String)
		row.UpdatedAt = parseSemanticTime(updated.String)
		row.ChainDepth = hops

		last = &row

		// Terminal state: active entry with no pointer forward.
		if row.MemoryState == "active" && !superseded.Valid {
			break
		}
		// Or: any entry with no pointer forward — caller should inspect
		// MemoryState to decide whether to trust it.
		if !superseded.Valid || superseded.String == "" {
			break
		}

		currentID = superseded.String
		hops++
		if hops >= maxSupersessionHops {
			lengthErr = ErrSemanticChainTooLong
			break
		}
	}

	if cycleErr != nil {
		return last, cycleErr
	}
	if lengthErr != nil {
		return last, lengthErr
	}
	return last, nil
}

// MarkSemanticSuperseded flips an active entry to stale and points its
// superseded_by at replacementID. The replacement must already exist.
// Returns true when exactly one row was flipped.
func (mm *Manager) MarkSemanticSuperseded(ctx context.Context, originalID, replacementID, reason string) (bool, error) {
	if mm.store == nil {
		return false, nil
	}
	if originalID == "" || replacementID == "" {
		return false, errors.New("mark semantic superseded: both IDs are required")
	}
	if originalID == replacementID {
		return false, errors.New("mark semantic superseded: original and replacement cannot be the same row")
	}

	// Verify replacement exists and is active.
	var replacementState sql.NullString
	err := mm.store.QueryRowContext(ctx,
		`SELECT memory_state FROM semantic_memory WHERE id = ?`, replacementID,
	).Scan(&replacementState)
	if err != nil {
		return false, err
	}
	if replacementState.Valid && replacementState.String != "active" {
		return false, errors.New("mark semantic superseded: replacement entry is not active")
	}

	if strings.TrimSpace(reason) == "" {
		reason = "manually superseded"
	}

	res, err := mm.store.ExecContext(ctx,
		`UPDATE semantic_memory
		    SET memory_state = 'stale',
		        state_reason = ?,
		        superseded_by = ?,
		        updated_at = datetime('now')
		  WHERE id = ?`,
		reason, replacementID, originalID,
	)
	if err != nil {
		return false, err
	}
	affected, _ := res.RowsAffected()
	return affected == 1, nil
}

// parseSemanticTime is permissive across the two formats SQLite produces in
// this schema (standard datetime and RFC3339).
func parseSemanticTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	for _, layout := range []string{"2006-01-02 15:04:05", time.RFC3339} {
		if t, err := time.Parse(layout, s); err == nil {
			return t
		}
	}
	return time.Time{}
}
