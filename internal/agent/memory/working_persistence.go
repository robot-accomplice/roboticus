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
	MaxAge          time.Duration // discard entries older than this
	ExecutiveMaxAge time.Duration // same, but applied to executive-state entries
	MinImportance   int           // discard entries with importance <= this
	RetainTypes     []string      // always retain these entry types
	DiscardTypes    []string      // always discard these entry types
}

// DefaultVetConfig returns sensible defaults for working memory vetting.
// Executive-state entries (plan, assumption, unresolved_question,
// verified_conclusion, decision_checkpoint, stopping_criteria) are retained by
// default so long multi-step tasks resume coherently after restart.
func DefaultVetConfig() VetConfig {
	retain := []string{"goal", "decision", "observation"}
	retain = append(retain, ExecutiveEntryKinds...)
	return VetConfig{
		MaxAge:          24 * time.Hour,
		ExecutiveMaxAge: 7 * 24 * time.Hour,
		MinImportance:   3,
		RetainTypes:     retain,
		DiscardTypes:    []string{"turn_summary", "note"},
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
	// Executive-state entries use a longer cutoff so multi-step tasks persist
	// across restart. The standard cutoff still applies to everything else.
	cutoff := time.Now().Add(-cfg.MaxAge).Format("2006-01-02 15:04:05")
	executiveCutoff := cutoff
	if cfg.ExecutiveMaxAge > 0 {
		executiveCutoff = time.Now().Add(-cfg.ExecutiveMaxAge).Format("2006-01-02 15:04:05")
	}
	executivePlaceholders := make([]string, len(ExecutiveEntryKinds))
	executiveArgs := make([]any, 0, len(ExecutiveEntryKinds)+1)
	for i, kind := range ExecutiveEntryKinds {
		executivePlaceholders[i] = "?"
		executiveArgs = append(executiveArgs, kind)
	}
	executiveIn := strings.Join(executivePlaceholders, ", ")

	nonExecArgs := append([]any{cutoff}, executiveArgs...)
	res, err := mm.store.ExecContext(ctx,
		`DELETE FROM working_memory
		 WHERE persisted_at IS NOT NULL
		   AND created_at < ?
		   AND entry_type NOT IN (`+executiveIn+`)`,
		nonExecArgs...,
	)
	if err == nil {
		n, _ := res.RowsAffected()
		result.Discarded += int(n)
	}

	execArgs := append([]any{executiveCutoff}, executiveArgs...)
	res, err = mm.store.ExecContext(ctx,
		`DELETE FROM working_memory
		 WHERE persisted_at IS NOT NULL
		   AND created_at < ?
		   AND entry_type IN (`+executiveIn+`)`,
		execArgs...,
	)
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
