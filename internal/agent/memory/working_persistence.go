// working_persistence.go implements shutdown/startup continuity for working memory.
//
// On shutdown: all active working memory entries are marked with persisted_at
// so the startup vet knows they survived a clean shutdown.
//
// On startup: persisted entries are vetted — stale/low-importance entries are
// discarded, goals and active decisions are retained. This makes the agent
// resume like a human after sleep: you remember what you were working on,
// but not every fleeting thought.
//
// Working memory entries that survive multiple shutdown/startup cycles
// become candidates for episodic promotion in the consolidation pipeline —
// the "I keep thinking about this" signal.

package memory

import (
	"context"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
)

// VetConfig controls which working memory entries survive startup vetting.
type VetConfig struct {
	MaxAge       time.Duration // discard entries older than this
	MinImportance int          // discard entries with importance <= this
	RetainTypes  []string      // always retain these entry types
	DiscardTypes []string      // always discard these entry types
}

// DefaultVetConfig returns sensible defaults for working memory vetting.
func DefaultVetConfig() VetConfig {
	return VetConfig{
		MaxAge:        24 * time.Hour,
		MinImportance: 3,
		RetainTypes:   []string{"goal", "decision", "observation"},
		DiscardTypes:  []string{"turn_summary", "note"},
	}
}

// VetResult reports what happened during working memory vetting.
type VetResult struct {
	Retained  int
	Discarded int
}

// PersistWorkingMemory marks all active working memory entries as persisted.
// Called during graceful shutdown to enable startup vetting.
func (mm *Manager) PersistWorkingMemory(ctx context.Context) {
	if mm.store == nil {
		return
	}

	result, err := mm.store.ExecContext(ctx,
		`UPDATE working_memory SET persisted_at = datetime('now')
		 WHERE persisted_at IS NULL`)
	if err != nil {
		log.Error().Err(err).Msg("working memory: failed to persist on shutdown")
		return
	}

	affected, _ := result.RowsAffected()
	log.Info().Int64("entries", affected).Msg("working memory: persisted on shutdown")
}

// VetWorkingMemory reviews persisted working memory entries on startup,
// discarding stale and low-value entries while retaining goals and decisions.
func (mm *Manager) VetWorkingMemory(ctx context.Context, cfg VetConfig) VetResult {
	if mm.store == nil {
		return VetResult{}
	}

	var result VetResult

	// Count total persisted entries.
	var total int
	_ = mm.store.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM working_memory WHERE persisted_at IS NOT NULL`).Scan(&total)

	if total == 0 {
		log.Info().Msg("working memory: no persisted entries to vet")
		return result
	}

	// Phase 1: Discard by age.
	cutoff := time.Now().Add(-cfg.MaxAge).Format("2006-01-02 15:04:05")
	res, err := mm.store.ExecContext(ctx,
		`DELETE FROM working_memory
		 WHERE persisted_at IS NOT NULL AND created_at < ?`, cutoff)
	if err == nil {
		n, _ := res.RowsAffected()
		result.Discarded += int(n)
	}

	// Phase 2: Discard by importance (but not retained types).
	if cfg.MinImportance > 0 && len(cfg.RetainTypes) > 0 {
		// Build parameterized NOT IN clause from config.
		placeholders := make([]string, len(cfg.RetainTypes))
		args := []any{cfg.MinImportance}
		for i, rt := range cfg.RetainTypes {
			placeholders[i] = "?"
			args = append(args, rt)
		}
		notIn := strings.Join(placeholders, ", ")
		res, err = mm.store.ExecContext(ctx,
			`DELETE FROM working_memory
			 WHERE persisted_at IS NOT NULL
			   AND importance <= ?
			   AND entry_type NOT IN (`+notIn+`)`,
			args...)
		if err == nil {
			n, _ := res.RowsAffected()
			result.Discarded += int(n)
		}
	}

	// Phase 3: Discard explicitly unwanted types.
	for _, dt := range cfg.DiscardTypes {
		res, err = mm.store.ExecContext(ctx,
			`DELETE FROM working_memory
			 WHERE persisted_at IS NOT NULL AND entry_type = ?`, dt)
		if err == nil {
			n, _ := res.RowsAffected()
			result.Discarded += int(n)
		}
	}

	// Count remaining.
	_ = mm.store.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM working_memory WHERE persisted_at IS NOT NULL`).Scan(&result.Retained)

	// Clear persisted_at on survivors so they behave as fresh working memory.
	_, _ = mm.store.ExecContext(ctx,
		`UPDATE working_memory SET persisted_at = NULL WHERE persisted_at IS NOT NULL`)

	log.Info().
		Int("retained", result.Retained).
		Int("discarded", result.Discarded).
		Int("total_before", total).
		Msg("working memory: vetted on startup")

	return result
}
