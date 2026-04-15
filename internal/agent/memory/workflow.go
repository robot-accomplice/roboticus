// workflow.go implements Milestone 4: turn procedural memory into real
// workflow memory.
//
// A Workflow is a named, versioned, reusable action sequence with
// preconditions, error modes, context tags, and tracked success/failure
// evidence — not just a tool counter. Workflows are stored in the
// procedural_memory table with category='workflow' to discriminate them
// from bare tool statistics already recorded by recordToolStat.
//
// Retrieval prefers workflows when the query overlaps their metadata; the
// tool-stat rollup is retained as a low-priority fallback so past
// procedural retrieval behaviour does not regress.

package memory

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/rs/zerolog/log"

	"roboticus/internal/db"
)

// WorkflowCategory discriminates reusable workflows from bare tool stats.
const (
	WorkflowCategoryTool     = "tool"
	WorkflowCategoryWorkflow = "workflow"
)

// Workflow represents a reusable procedural record in working memory.
type Workflow struct {
	ID             string
	Name           string
	Steps          []string
	Preconditions  []string
	ErrorModes     []string
	ContextTags    []string
	Category       string
	Version        int
	Confidence     float64
	MemoryState    string
	SuccessCount   int
	FailureCount   int
	AvgDurationMs  int
	LastUsedAt     time.Time
	CreatedAt      time.Time
	UpdatedAt      time.Time
	SuccessEvidence []string
	FailureEvidence []string
}

// SuccessRate returns success_count / (success_count + failure_count), or
// 0 when the workflow has no recorded outcomes yet.
func (w Workflow) SuccessRate() float64 {
	total := w.SuccessCount + w.FailureCount
	if total == 0 {
		return 0
	}
	return float64(w.SuccessCount) / float64(total)
}

// RecordWorkflow inserts or updates a reusable workflow in procedural_memory.
// When a row with the same name already exists, the update only replaces the
// structural fields (steps, preconditions, error_modes, context_tags) and
// bumps the version — it does NOT overwrite success/failure counters or
// evidence so the track record is preserved across revisions.
func (mm *Manager) RecordWorkflow(ctx context.Context, workflow Workflow) error {
	if mm.store == nil {
		return nil
	}
	if strings.TrimSpace(workflow.Name) == "" {
		return fmt.Errorf("record workflow: name is required")
	}
	if len(workflow.Steps) == 0 {
		return fmt.Errorf("record workflow: steps are required")
	}

	category := workflow.Category
	if category == "" {
		category = WorkflowCategoryWorkflow
	}
	memoryState := workflow.MemoryState
	if memoryState == "" {
		memoryState = "active"
	}

	stepsJSON, err := json.Marshal(workflow.Steps)
	if err != nil {
		return fmt.Errorf("record workflow: marshal steps: %w", err)
	}
	preconditionsJSON, err := marshalStringSliceForStorage(workflow.Preconditions)
	if err != nil {
		return fmt.Errorf("record workflow: marshal preconditions: %w", err)
	}
	errorModesJSON, err := marshalStringSliceForStorage(workflow.ErrorModes)
	if err != nil {
		return fmt.Errorf("record workflow: marshal error_modes: %w", err)
	}
	contextTagsJSON, err := marshalStringSliceForStorage(workflow.ContextTags)
	if err != nil {
		return fmt.Errorf("record workflow: marshal context_tags: %w", err)
	}

	existingID, err := mm.lookupProceduralID(ctx, workflow.Name)
	if err != nil {
		return err
	}
	if existingID != "" {
		_, err := mm.store.ExecContext(ctx,
			`UPDATE procedural_memory
			    SET steps = ?,
			        preconditions = ?,
			        error_modes = ?,
			        context_tags = ?,
			        category = ?,
			        version = version + 1,
			        memory_state = ?,
			        updated_at = datetime('now')
			  WHERE id = ?`,
			string(stepsJSON), preconditionsJSON, errorModesJSON, contextTagsJSON,
			category, memoryState, existingID,
		)
		if err != nil {
			return fmt.Errorf("record workflow: update: %w", err)
		}
		return nil
	}

	id := db.NewID()
	confidence := workflow.Confidence
	if confidence == 0 {
		confidence = 1.0
	}
	_, err = mm.store.ExecContext(ctx,
		`INSERT INTO procedural_memory (
		   id, name, steps,
		   preconditions, error_modes, context_tags,
		   category, version, confidence, memory_state,
		   success_count, failure_count,
		   success_evidence, failure_evidence
		 )
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0, 0, '[]', '[]')`,
		id, workflow.Name, string(stepsJSON),
		preconditionsJSON, errorModesJSON, contextTagsJSON,
		category, 1, confidence, memoryState,
	)
	if err != nil {
		return fmt.Errorf("record workflow: insert: %w", err)
	}
	return nil
}

// RecordWorkflowSuccess increments the success counter for a workflow and
// appends supporting evidence. Evidence is a short identifier (session ID,
// turn ID, or free-text label) so operators can audit why the workflow is
// trusted. last_used_at is bumped so recency-aware retrieval can surface it.
func (mm *Manager) RecordWorkflowSuccess(ctx context.Context, name, evidence string) error {
	return mm.recordWorkflowOutcome(ctx, name, evidence, true)
}

// RecordWorkflowFailure increments the failure counter and appends evidence.
func (mm *Manager) RecordWorkflowFailure(ctx context.Context, name, evidence string) error {
	return mm.recordWorkflowOutcome(ctx, name, evidence, false)
}

// findWorkflowsHybrid is the M3.2 HybridSearch-first variant of FindWorkflows.
// It matches workflow rows in procedural_memory via memory_fts (BM25 +
// vector cosine), filters to category='workflow', and hydrates the full
// Workflow records by ID. When both legs return zero workflow candidates it
// falls back to the existing LIKE-based FindWorkflows so retrieval never
// disappears for queries with no embedding or no FTS coverage.
//
// The retrieval-path annotation is emitted under
// "retrieval.path.workflow" with the same value vocabulary as the other
// tiers (fts | vector | hybrid | like_fallback | empty) so M3.3 can
// retire LIKE only after evidence shows it is dormant for workflow
// queries too.
//
// query="" is treated as a non-search browse and bypasses the hybrid leg,
// matching the LIKE-version's "list active workflows by confidence" behavior
// — no annotation is emitted for that case (consistent with the other tiers).
func (mm *Manager) findWorkflowsHybrid(
	ctx context.Context,
	query string,
	queryEmbed []float32,
	vectorIndex db.VectorIndex,
	hybridWeight float64,
	limit int,
) ([]Workflow, error) {
	if mm.store == nil {
		return nil, nil
	}
	if limit <= 0 {
		limit = 5
	}
	trimmed := strings.TrimSpace(query)
	if trimmed == "" {
		// No-query browse — go straight to LIKE-version's empty-query
		// behavior (sorted by confidence/recency).
		return mm.FindWorkflows(ctx, "", limit)
	}

	// HybridSearch is over the full memory_fts index. We pull a wider
	// candidate set (limit*4) so the workflow-only filter has enough
	// headroom to find category='workflow' rows even when other tiers
	// also match the query.
	results := db.HybridSearch(ctx, mm.store, trimmed, queryEmbed, limit*4, hybridWeight, vectorIndex)

	// Per-leg hit counts feed the path classification. Only count rows
	// that actually correspond to a workflow procedural row so the
	// annotation reflects what the workflow tier observed, not what the
	// other tiers did.
	var ftsHits, vecHits int
	var workflowIDs []string
	seen := make(map[string]struct{})
	for _, hr := range results {
		if hr.SourceTable != "procedural_memory" {
			continue
		}
		if _, dup := seen[hr.SourceID]; dup {
			continue
		}
		seen[hr.SourceID] = struct{}{}
		workflowIDs = append(workflowIDs, hr.SourceID)
		if hr.FTSScore > 0 {
			ftsHits++
		}
		if hr.VectorScore > 0 {
			vecHits++
		}
	}

	if len(workflowIDs) > 0 {
		hydrated, err := mm.loadWorkflowsByIDs(ctx, workflowIDs, limit)
		if err == nil && len(hydrated) > 0 {
			annotateRetrievalPath(ctx, RetrievalTierWorkflow, classifyHybridPath(ftsHits, vecHits))
			return hydrated, nil
		}
		// Fall through to LIKE if hydration filtered everything out
		// (e.g. all matched rows were tool-stats, not workflows).
	}

	// LIKE safety net.
	likeResults, err := mm.FindWorkflows(ctx, trimmed, limit)
	if err != nil {
		annotateRetrievalPath(ctx, RetrievalTierWorkflow, RetrievalPathEmpty)
		return nil, err
	}
	if len(likeResults) > 0 {
		annotateRetrievalPath(ctx, RetrievalTierWorkflow, RetrievalPathLikeFallback)
	} else {
		annotateRetrievalPath(ctx, RetrievalTierWorkflow, RetrievalPathEmpty)
	}
	return likeResults, nil
}

// loadWorkflowsByIDs hydrates Workflow records for a set of procedural_memory
// IDs, preserving the order in which IDs were supplied (which is the order
// HybridSearch ranked them) and dropping any IDs that don't correspond to
// category='workflow' rows. The returned slice is capped at limit.
//
// We hydrate one row at a time rather than via SQL IN (...) so the workflow
// scan helper (scanWorkflowRow) can be reused unchanged. That keeps the JSON
// decoding of steps/preconditions/error_modes/context_tags/evidence in one
// place; building a parameterised IN query just to share it would duplicate
// scanning logic for marginal speedup at this corpus size.
func (mm *Manager) loadWorkflowsByIDs(ctx context.Context, ids []string, limit int) ([]Workflow, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	if limit <= 0 {
		limit = len(ids)
	}
	out := make([]Workflow, 0, limit)
	for _, id := range ids {
		if len(out) >= limit {
			break
		}
		rows, err := mm.store.QueryContext(ctx,
			`SELECT id, name, steps, preconditions, error_modes, context_tags,
			        category, version, confidence, memory_state,
			        success_count, failure_count, avg_duration_ms,
			        last_used_at, created_at, updated_at,
			        success_evidence, failure_evidence
			   FROM procedural_memory
			  WHERE id = ?
			    AND category = ?
			    AND memory_state = 'active'
			  LIMIT 1`,
			id, WorkflowCategoryWorkflow,
		)
		if err != nil {
			continue
		}
		if rows.Next() {
			workflow, scanErr := scanWorkflowRow(rows)
			_ = rows.Close()
			if scanErr != nil {
				log.Debug().Err(scanErr).Str("id", id).Msg("workflow: hydrate scan failed")
				continue
			}
			out = append(out, workflow)
			continue
		}
		_ = rows.Close()
	}
	return out, nil
}

// FindWorkflows returns workflows whose metadata overlaps the query. Matches
// against name, steps, preconditions, error_modes, and context_tags so a
// query like "rollout" surfaces workflows tagged "release" or with a
// precondition that mentions rollout staging. Results are ordered by
// confidence * success_rate, then by recency.
//
// M3.2 note: this LIKE-based path remains the safety-net fallback used by
// findWorkflowsHybrid and is also the direct entry point for the
// WorkflowSearchTool (internal/agent/tools/workflow_search.go), which
// re-ranks candidates in memory and doesn't carry a query embedding. The
// LIKE behavior here is deliberately preserved unchanged so external
// callers don't regress while M3.2 promotes HybridSearch to primary.
func (mm *Manager) FindWorkflows(ctx context.Context, query string, limit int) ([]Workflow, error) {
	if mm.store == nil {
		return nil, nil
	}
	if limit <= 0 {
		limit = 5
	}
	trimmed := strings.TrimSpace(query)

	var rows *sql.Rows
	var err error
	if trimmed == "" {
		rows, err = mm.store.QueryContext(ctx,
			`SELECT id, name, steps, preconditions, error_modes, context_tags,
			        category, version, confidence, memory_state,
			        success_count, failure_count, avg_duration_ms,
			        last_used_at, created_at, updated_at,
			        success_evidence, failure_evidence
			   FROM procedural_memory
			  WHERE category = ? AND memory_state = 'active'
			  ORDER BY confidence DESC, updated_at DESC
			  LIMIT ?`,
			WorkflowCategoryWorkflow, limit,
		)
	} else {
		like := "%" + trimmed + "%"
		rows, err = mm.store.QueryContext(ctx,
			`SELECT id, name, steps, preconditions, error_modes, context_tags,
			        category, version, confidence, memory_state,
			        success_count, failure_count, avg_duration_ms,
			        last_used_at, created_at, updated_at,
			        success_evidence, failure_evidence
			   FROM procedural_memory
			  WHERE category = ?
			    AND memory_state = 'active'
			    AND (name LIKE ? OR steps LIKE ? OR preconditions LIKE ?
			         OR error_modes LIKE ? OR context_tags LIKE ?)
			  ORDER BY confidence DESC, updated_at DESC
			  LIMIT ?`,
			WorkflowCategoryWorkflow, like, like, like, like, like, limit,
		)
	}
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var out []Workflow
	for rows.Next() {
		workflow, err := scanWorkflowRow(rows)
		if err != nil {
			log.Debug().Err(err).Msg("workflow: scan failed")
			continue
		}
		out = append(out, workflow)
	}
	return out, rows.Err()
}

// GetWorkflow returns the single workflow matching name (case-insensitive),
// or nil if none exists.
func (mm *Manager) GetWorkflow(ctx context.Context, name string) (*Workflow, error) {
	if mm.store == nil {
		return nil, nil
	}
	rows, err := mm.store.QueryContext(ctx,
		`SELECT id, name, steps, preconditions, error_modes, context_tags,
		        category, version, confidence, memory_state,
		        success_count, failure_count, avg_duration_ms,
		        last_used_at, created_at, updated_at,
		        success_evidence, failure_evidence
		   FROM procedural_memory
		  WHERE LOWER(name) = LOWER(?)
		  LIMIT 1`,
		name,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		return nil, nil
	}
	workflow, err := scanWorkflowRow(rows)
	if err != nil {
		return nil, err
	}
	return &workflow, nil
}

func (mm *Manager) lookupProceduralID(ctx context.Context, name string) (string, error) {
	var id sql.NullString
	row := mm.store.QueryRowContext(ctx,
		`SELECT id FROM procedural_memory WHERE name = ? LIMIT 1`, name)
	if err := row.Scan(&id); err != nil {
		if err == sql.ErrNoRows {
			return "", nil
		}
		return "", err
	}
	if id.Valid {
		return id.String, nil
	}
	return "", nil
}

func (mm *Manager) recordWorkflowOutcome(ctx context.Context, name, evidence string, success bool) error {
	if mm.store == nil {
		return nil
	}
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("record workflow outcome: name is required")
	}
	id, err := mm.lookupProceduralID(ctx, name)
	if err != nil {
		return err
	}
	if id == "" {
		return fmt.Errorf("record workflow outcome: workflow %q not found", name)
	}

	column := "success_evidence"
	counter := "success_count"
	if !success {
		column = "failure_evidence"
		counter = "failure_count"
	}

	var raw sql.NullString
	if err := mm.store.QueryRowContext(ctx,
		"SELECT "+column+" FROM procedural_memory WHERE id = ?", id,
	).Scan(&raw); err != nil {
		return fmt.Errorf("record workflow outcome: read %s: %w", column, err)
	}

	evidenceEntry := strings.TrimSpace(evidence)
	if evidenceEntry == "" {
		evidenceEntry = time.Now().UTC().Format(time.RFC3339)
	}
	list := decodeEvidenceList(raw.String)
	list = appendUniqueEvidence(list, evidenceEntry, 16)
	encoded, err := json.Marshal(list)
	if err != nil {
		return fmt.Errorf("record workflow outcome: marshal evidence: %w", err)
	}

	_, err = mm.store.ExecContext(ctx,
		"UPDATE procedural_memory SET "+
			counter+" = "+counter+" + 1, "+
			column+" = ?, "+
			"last_used_at = datetime('now'), "+
			"updated_at = datetime('now') "+
			"WHERE id = ?",
		string(encoded), id,
	)
	if err != nil {
		return fmt.Errorf("record workflow outcome: update: %w", err)
	}
	return nil
}

// scanWorkflowRow pulls a Workflow from the current row of a procedural_memory
// query. The function is the single place that decodes the JSON columns so
// the parsing behaviour stays consistent across Get / Find / List paths.
func scanWorkflowRow(rows *sql.Rows) (Workflow, error) {
	var (
		w                Workflow
		stepsRaw         sql.NullString
		preconditionsRaw sql.NullString
		errorModesRaw    sql.NullString
		contextTagsRaw   sql.NullString
		memoryState      sql.NullString
		lastUsedAt       sql.NullString
		createdAt        string
		updatedAt        sql.NullString
		successEvidence  sql.NullString
		failureEvidence  sql.NullString
	)
	if err := rows.Scan(
		&w.ID, &w.Name, &stepsRaw,
		&preconditionsRaw, &errorModesRaw, &contextTagsRaw,
		&w.Category, &w.Version, &w.Confidence, &memoryState,
		&w.SuccessCount, &w.FailureCount, &w.AvgDurationMs,
		&lastUsedAt, &createdAt, &updatedAt,
		&successEvidence, &failureEvidence,
	); err != nil {
		return w, err
	}
	w.Steps = decodeEvidenceList(stepsRaw.String)
	w.Preconditions = decodeEvidenceList(preconditionsRaw.String)
	w.ErrorModes = decodeEvidenceList(errorModesRaw.String)
	w.ContextTags = decodeEvidenceList(contextTagsRaw.String)
	if memoryState.Valid {
		w.MemoryState = memoryState.String
	} else {
		w.MemoryState = "active"
	}
	w.CreatedAt = parseProceduralTime(createdAt)
	if updatedAt.Valid {
		w.UpdatedAt = parseProceduralTime(updatedAt.String)
	}
	if lastUsedAt.Valid {
		w.LastUsedAt = parseProceduralTime(lastUsedAt.String)
	}
	w.SuccessEvidence = decodeEvidenceList(successEvidence.String)
	w.FailureEvidence = decodeEvidenceList(failureEvidence.String)
	return w, nil
}

func parseProceduralTime(s string) time.Time {
	for _, layout := range []string{"2006-01-02 15:04:05", time.RFC3339} {
		if t, err := time.Parse(layout, s); err == nil {
			return t
		}
	}
	return time.Time{}
}

// decodeEvidenceList safely parses a JSON array string into a slice of
// strings. Non-JSON or empty values decode to nil.
func decodeEvidenceList(raw string) []string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || trimmed == "null" {
		return nil
	}
	var out []string
	if err := json.Unmarshal([]byte(trimmed), &out); err != nil {
		// Tolerate raw free-text (older rows) by wrapping it as a single
		// entry rather than dropping the column on the floor.
		return []string{trimmed}
	}
	return out
}

// appendUniqueEvidence appends entry to list if it is not already present,
// trimming the list to the most recent maxEntries.
func appendUniqueEvidence(list []string, entry string, maxEntries int) []string {
	if entry == "" {
		return list
	}
	for _, item := range list {
		if item == entry {
			return list
		}
	}
	list = append(list, entry)
	if maxEntries > 0 && len(list) > maxEntries {
		list = list[len(list)-maxEntries:]
	}
	return list
}

func marshalStringSliceForStorage(items []string) (string, error) {
	if len(items) == 0 {
		return "[]", nil
	}
	buf, err := json.Marshal(items)
	if err != nil {
		return "", err
	}
	return string(buf), nil
}

// workflowQueryHint picks a concise keyword for the workflow query. Retrieval
// filters are wildcard-based, and a very long query string rarely matches
// anything useful; taking the last two meaningful tokens mirrors how
// retrievalKeywords.go extracts keywords elsewhere.
func workflowQueryHint(query string, filtered bool) string {
	if !filtered {
		return ""
	}
	trimmed := strings.TrimSpace(query)
	if trimmed == "" {
		return ""
	}
	if len(trimmed) > 64 {
		trimmed = trimmed[:64]
	}
	return trimmed
}

// writeWorkflowSummary formats a workflow for inclusion in the procedural
// retrieval block. Output is concise (one heading line + one detail line) so
// multiple workflows fit within the retrieval token budget.
func writeWorkflowSummary(b *strings.Builder, wf Workflow) {
	success := wf.SuccessCount
	failure := wf.FailureCount
	total := success + failure
	pct := 0
	if total > 0 {
		pct = int((float64(success) / float64(total)) * 100)
	}
	fmt.Fprintf(b, "• workflow %q (v%d, %d/%d run(s), %d%% success, confidence=%.2f)\n",
		wf.Name, wf.Version, success, total, pct, wf.Confidence)
	if len(wf.Steps) > 0 {
		stepPreview := wf.Steps
		if len(stepPreview) > 4 {
			stepPreview = append([]string(nil), stepPreview[:4]...)
			stepPreview = append(stepPreview, "…")
		}
		fmt.Fprintf(b, "   steps: %s\n", strings.Join(stepPreview, " → "))
	}
	if len(wf.Preconditions) > 0 {
		fmt.Fprintf(b, "   preconditions: %s\n", strings.Join(wf.Preconditions, "; "))
	}
	if len(wf.ContextTags) > 0 {
		fmt.Fprintf(b, "   tags: %s\n", strings.Join(wf.ContextTags, ", "))
	}
}
