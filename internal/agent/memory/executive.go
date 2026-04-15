// executive.go implements the executive-state layer on top of working memory.
//
// Milestone 7 of the agentic memory roadmap requires working memory to act as
// the agent's short-term executive state: the current plan, assumptions,
// unresolved questions, verified conclusions, decision checkpoints, and
// stopping criteria must all survive across turns and across clean restarts.
//
// Design:
//   - Every executive entry is a row in working_memory with a structured
//     entry_type and a JSON payload.
//   - Entries belong to a task (task_id). Multiple tasks can coexist per
//     session, and each entry type can have multiple active rows for the same
//     task (e.g., several assumptions).
//   - Retrieval returns ExecutiveState grouped by task_id so the context
//     assembler and verifier can reason about the currently active task.
//
// This file only depends on the db store, so it remains testable without the
// full embedding/retrieval stack.

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

// Executive entry types. These values match the CHECK constraint in migration
// 044_working_memory_executive_state.sql.
const (
	EntryPlan               = "plan"
	EntryAssumption         = "assumption"
	EntryUnresolvedQuestion = "unresolved_question"
	EntryVerifiedConclusion = "verified_conclusion"
	EntryDecisionCheckpoint = "decision_checkpoint"
	EntryStoppingCriteria   = "stopping_criteria"
)

// ExecutiveEntryKinds lists every executive entry type. It is used by the vet
// and context-assembly layers to enumerate executive state without hard-coding
// the list in several places.
var ExecutiveEntryKinds = []string{
	EntryPlan,
	EntryAssumption,
	EntryUnresolvedQuestion,
	EntryVerifiedConclusion,
	EntryDecisionCheckpoint,
	EntryStoppingCriteria,
}

// IsExecutiveEntryType returns true when the given entry_type belongs to the
// executive-state set (as opposed to the legacy working-memory types like
// turn_summary or note).
func IsExecutiveEntryType(entryType string) bool {
	for _, kind := range ExecutiveEntryKinds {
		if entryType == kind {
			return true
		}
	}
	return false
}

// ExecutiveEntry is a single row in working_memory carrying structured state.
type ExecutiveEntry struct {
	ID         string
	SessionID  string
	TaskID     string
	EntryType  string
	Content    string
	Importance int
	Payload    map[string]any
	CreatedAt  time.Time
}

// ExecutiveState groups the currently active executive entries for a task.
type ExecutiveState struct {
	TaskID               string
	Plans                []ExecutiveEntry
	Assumptions          []ExecutiveEntry
	UnresolvedQuestions  []ExecutiveEntry
	VerifiedConclusions  []ExecutiveEntry
	DecisionCheckpoints  []ExecutiveEntry
	StoppingCriteria     []ExecutiveEntry
}

// IsEmpty reports whether the state carries no executive entries at all.
func (s *ExecutiveState) IsEmpty() bool {
	if s == nil {
		return true
	}
	return len(s.Plans) == 0 &&
		len(s.Assumptions) == 0 &&
		len(s.UnresolvedQuestions) == 0 &&
		len(s.VerifiedConclusions) == 0 &&
		len(s.DecisionCheckpoints) == 0 &&
		len(s.StoppingCriteria) == 0
}

// PlanPayload describes the structured content behind a plan entry.
type PlanPayload struct {
	Subgoals     []string `json:"subgoals,omitempty"`
	Steps        []string `json:"steps,omitempty"`
	Dependencies []string `json:"dependencies,omitempty"`
	Intent       string   `json:"intent,omitempty"`
	Complexity   string   `json:"complexity,omitempty"`
}

// AssumptionPayload captures what an assumption is based on.
type AssumptionPayload struct {
	Source     string  `json:"source,omitempty"`
	Confidence float64 `json:"confidence,omitempty"`
}

// UnresolvedQuestionPayload captures what a question is blocking.
type UnresolvedQuestionPayload struct {
	BlockingSubgoal string   `json:"blocking_subgoal,omitempty"`
	Related         []string `json:"related,omitempty"`
}

// VerifiedConclusionPayload captures why a conclusion was considered verified.
type VerifiedConclusionPayload struct {
	SupportingEvidence []string `json:"supporting_evidence,omitempty"`
	VerifiedAt         string   `json:"verified_at,omitempty"`
}

// DecisionCheckpointPayload captures the alternatives considered for a choice.
type DecisionCheckpointPayload struct {
	Chosen     string   `json:"chosen,omitempty"`
	Considered []string `json:"considered,omitempty"`
	Rationale  string   `json:"rationale,omitempty"`
}

// StoppingCriteriaPayload captures the condition that ends the task.
type StoppingCriteriaPayload struct {
	Condition string  `json:"condition,omitempty"`
	Threshold float64 `json:"threshold,omitempty"`
}

// RecordPlan persists a plan entry. Overwrites any existing plan for the task
// so the current plan is always the authoritative one per task.
func (mm *Manager) RecordPlan(ctx context.Context, sessionID, taskID, content string, payload PlanPayload) error {
	return mm.recordExecutiveEntry(ctx, sessionID, taskID, EntryPlan, content, 9, payload, true)
}

// RecordAssumption persists a new assumption entry. Multiple assumptions can
// coexist per task.
func (mm *Manager) RecordAssumption(ctx context.Context, sessionID, taskID, content string, payload AssumptionPayload) error {
	return mm.recordExecutiveEntry(ctx, sessionID, taskID, EntryAssumption, content, 6, payload, false)
}

// RecordUnresolvedQuestion persists a question that the agent could not answer
// yet but must preserve so later turns can return to it.
func (mm *Manager) RecordUnresolvedQuestion(ctx context.Context, sessionID, taskID, content string, payload UnresolvedQuestionPayload) error {
	return mm.recordExecutiveEntry(ctx, sessionID, taskID, EntryUnresolvedQuestion, content, 8, payload, false)
}

// RecordVerifiedConclusion persists a conclusion that the verifier signed off
// on. Multiple verified conclusions can coexist per task.
func (mm *Manager) RecordVerifiedConclusion(ctx context.Context, sessionID, taskID, content string, payload VerifiedConclusionPayload) error {
	return mm.recordExecutiveEntry(ctx, sessionID, taskID, EntryVerifiedConclusion, content, 8, payload, false)
}

// RecordDecisionCheckpoint persists a decision event with the alternatives that
// were considered and the rationale for the chosen path.
func (mm *Manager) RecordDecisionCheckpoint(ctx context.Context, sessionID, taskID, content string, payload DecisionCheckpointPayload) error {
	return mm.recordExecutiveEntry(ctx, sessionID, taskID, EntryDecisionCheckpoint, content, 7, payload, false)
}

// RecordStoppingCriteria persists the condition that ends the task. Only one
// stopping criterion is preserved per task; the latest call wins.
func (mm *Manager) RecordStoppingCriteria(ctx context.Context, sessionID, taskID, content string, payload StoppingCriteriaPayload) error {
	return mm.recordExecutiveEntry(ctx, sessionID, taskID, EntryStoppingCriteria, content, 7, payload, true)
}

// ResolveQuestion marks an unresolved question as resolved by deleting the row.
// If id is empty, the question is matched by fuzzy content prefix within the task.
func (mm *Manager) ResolveQuestion(ctx context.Context, sessionID, taskID, id string) error {
	if mm.store == nil {
		return nil
	}
	if id != "" {
		_, err := mm.store.ExecContext(ctx,
			`DELETE FROM working_memory WHERE id = ? AND entry_type = ?`,
			id, EntryUnresolvedQuestion,
		)
		return err
	}
	if sessionID == "" || taskID == "" {
		return fmt.Errorf("resolve question: need either id, or session_id and task_id")
	}
	_, err := mm.store.ExecContext(ctx,
		`DELETE FROM working_memory
		  WHERE session_id = ? AND task_id = ? AND entry_type = ?`,
		sessionID, taskID, EntryUnresolvedQuestion,
	)
	return err
}

// LoadExecutiveState reads the executive state for (session, task). Pass an
// empty taskID to load the most recent task's state for the session.
func (mm *Manager) LoadExecutiveState(ctx context.Context, sessionID, taskID string) (*ExecutiveState, error) {
	if mm.store == nil || sessionID == "" {
		return &ExecutiveState{TaskID: taskID}, nil
	}

	if taskID == "" {
		latest, err := mm.latestTaskID(ctx, sessionID)
		if err != nil {
			return nil, err
		}
		taskID = latest
	}

	state := &ExecutiveState{TaskID: taskID}
	if taskID == "" {
		return state, nil
	}

	rows, err := mm.store.QueryContext(ctx,
		`SELECT id, session_id, task_id, entry_type, content, importance, payload, created_at
		   FROM working_memory
		  WHERE session_id = ? AND task_id = ? AND entry_type IN (?, ?, ?, ?, ?, ?)
		  ORDER BY created_at ASC`,
		sessionID, taskID,
		EntryPlan, EntryAssumption, EntryUnresolvedQuestion,
		EntryVerifiedConclusion, EntryDecisionCheckpoint, EntryStoppingCriteria,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		entry, err := scanExecutiveEntry(rows)
		if err != nil {
			return nil, err
		}
		appendExecutiveEntry(state, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return state, nil
}

// LoadAllExecutiveState returns executive state grouped by task_id for a
// session, useful for context assembly when the current task is ambiguous.
func (mm *Manager) LoadAllExecutiveState(ctx context.Context, sessionID string) ([]*ExecutiveState, error) {
	if mm.store == nil || sessionID == "" {
		return nil, nil
	}

	rows, err := mm.store.QueryContext(ctx,
		`SELECT id, session_id, task_id, entry_type, content, importance, payload, created_at
		   FROM working_memory
		  WHERE session_id = ? AND task_id IS NOT NULL AND entry_type IN (?, ?, ?, ?, ?, ?)
		  ORDER BY task_id ASC, created_at ASC`,
		sessionID,
		EntryPlan, EntryAssumption, EntryUnresolvedQuestion,
		EntryVerifiedConclusion, EntryDecisionCheckpoint, EntryStoppingCriteria,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	byTask := make(map[string]*ExecutiveState)
	var order []string
	for rows.Next() {
		entry, err := scanExecutiveEntry(rows)
		if err != nil {
			return nil, err
		}
		state, ok := byTask[entry.TaskID]
		if !ok {
			state = &ExecutiveState{TaskID: entry.TaskID}
			byTask[entry.TaskID] = state
			order = append(order, entry.TaskID)
		}
		appendExecutiveEntry(state, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]*ExecutiveState, 0, len(order))
	for _, taskID := range order {
		out = append(out, byTask[taskID])
	}
	return out, nil
}

func (mm *Manager) latestTaskID(ctx context.Context, sessionID string) (string, error) {
	var taskID sql.NullString
	row := mm.store.QueryRowContext(ctx,
		`SELECT task_id FROM working_memory
		  WHERE session_id = ? AND task_id IS NOT NULL AND entry_type IN (?, ?, ?, ?, ?, ?)
		  ORDER BY created_at DESC LIMIT 1`,
		sessionID,
		EntryPlan, EntryAssumption, EntryUnresolvedQuestion,
		EntryVerifiedConclusion, EntryDecisionCheckpoint, EntryStoppingCriteria,
	)
	if err := row.Scan(&taskID); err != nil {
		if err == sql.ErrNoRows {
			return "", nil
		}
		return "", err
	}
	if taskID.Valid {
		return taskID.String, nil
	}
	return "", nil
}

// recordExecutiveEntry writes a structured executive-state entry. When
// uniquePerTask is true, any prior entries of the same type for the same
// (session, task) are replaced so the latest call is authoritative.
func (mm *Manager) recordExecutiveEntry(
	ctx context.Context,
	sessionID, taskID, entryType, content string,
	importance int,
	payload any,
	uniquePerTask bool,
) error {
	if mm.store == nil {
		return nil
	}
	if sessionID == "" {
		return fmt.Errorf("record executive entry: session_id is required")
	}
	if taskID == "" {
		return fmt.Errorf("record executive entry: task_id is required")
	}
	if strings.TrimSpace(content) == "" {
		return fmt.Errorf("record executive entry: content is required")
	}

	var payloadJSON sql.NullString
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("record executive entry: marshal payload: %w", err)
		}
		// An empty JSON object payload is not useful; skip it.
		if string(data) != "null" && string(data) != "{}" {
			payloadJSON = sql.NullString{String: string(data), Valid: true}
		}
	}

	if uniquePerTask {
		if _, err := mm.store.ExecContext(ctx,
			`DELETE FROM working_memory
			  WHERE session_id = ? AND task_id = ? AND entry_type = ?`,
			sessionID, taskID, entryType,
		); err != nil {
			log.Warn().Err(err).Msg("executive: failed to delete prior entry")
		}
	}

	_, err := mm.store.ExecContext(ctx,
		`INSERT INTO working_memory (id, session_id, entry_type, content, importance, task_id, payload)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		db.NewID(), sessionID, entryType, content, importance, taskID, payloadJSON,
	)
	if err != nil {
		return fmt.Errorf("record executive entry: insert: %w", err)
	}
	return nil
}

func scanExecutiveEntry(rows *sql.Rows) (ExecutiveEntry, error) {
	var (
		entry     ExecutiveEntry
		taskID    sql.NullString
		payload   sql.NullString
		createdAt string
	)
	if err := rows.Scan(
		&entry.ID, &entry.SessionID, &taskID, &entry.EntryType,
		&entry.Content, &entry.Importance, &payload, &createdAt,
	); err != nil {
		return entry, err
	}
	if taskID.Valid {
		entry.TaskID = taskID.String
	}
	if payload.Valid && payload.String != "" {
		var generic map[string]any
		if err := json.Unmarshal([]byte(payload.String), &generic); err == nil {
			entry.Payload = generic
		}
	}
	if t, err := time.Parse("2006-01-02 15:04:05", createdAt); err == nil {
		entry.CreatedAt = t
	} else if t, err := time.Parse(time.RFC3339, createdAt); err == nil {
		entry.CreatedAt = t
	}
	return entry, nil
}

func appendExecutiveEntry(state *ExecutiveState, entry ExecutiveEntry) {
	switch entry.EntryType {
	case EntryPlan:
		state.Plans = append(state.Plans, entry)
	case EntryAssumption:
		state.Assumptions = append(state.Assumptions, entry)
	case EntryUnresolvedQuestion:
		state.UnresolvedQuestions = append(state.UnresolvedQuestions, entry)
	case EntryVerifiedConclusion:
		state.VerifiedConclusions = append(state.VerifiedConclusions, entry)
	case EntryDecisionCheckpoint:
		state.DecisionCheckpoints = append(state.DecisionCheckpoints, entry)
	case EntryStoppingCriteria:
		state.StoppingCriteria = append(state.StoppingCriteria, entry)
	}
}

// FormatForContext renders the executive state as a compact text block that
// can be injected into the [Working State] section of the assembled context.
// The renderer is deliberately short so it does not starve evidence/gaps
// sections of the working-memory token budget.
func (s *ExecutiveState) FormatForContext() string {
	if s == nil || s.IsEmpty() {
		return ""
	}
	var b strings.Builder
	if s.TaskID != "" {
		b.WriteString("Task: " + s.TaskID + "\n")
	}
	writeSection(&b, "Plan", s.Plans)
	writeSection(&b, "Assumptions", s.Assumptions)
	writeSection(&b, "Unresolved questions", s.UnresolvedQuestions)
	writeSection(&b, "Verified conclusions", s.VerifiedConclusions)
	writeSection(&b, "Decision checkpoints", s.DecisionCheckpoints)
	writeSection(&b, "Stopping criteria", s.StoppingCriteria)
	return strings.TrimRight(b.String(), "\n")
}

func writeSection(b *strings.Builder, label string, entries []ExecutiveEntry) {
	if len(entries) == 0 {
		return
	}
	b.WriteString(label + ":\n")
	for _, entry := range entries {
		b.WriteString("- " + entry.Content)
		if detail := formatPayloadShort(entry); detail != "" {
			b.WriteString(" (" + detail + ")")
		}
		b.WriteString("\n")
	}
}

func formatPayloadShort(entry ExecutiveEntry) string {
	if entry.Payload == nil {
		return ""
	}
	switch entry.EntryType {
	case EntryPlan:
		if steps, ok := entry.Payload["steps"].([]any); ok && len(steps) > 0 {
			var labels []string
			for _, step := range steps {
				if s, ok := step.(string); ok && s != "" {
					labels = append(labels, s)
				}
			}
			if len(labels) > 0 {
				return "steps=" + strings.Join(labels, " → ")
			}
		}
	case EntryDecisionCheckpoint:
		chosen, _ := entry.Payload["chosen"].(string)
		if chosen != "" {
			return "chose " + chosen
		}
	case EntryStoppingCriteria:
		if cond, ok := entry.Payload["condition"].(string); ok && cond != "" {
			return cond
		}
	case EntryAssumption:
		if conf, ok := entry.Payload["confidence"].(float64); ok && conf > 0 {
			return fmt.Sprintf("confidence=%.2f", conf)
		}
	}
	return ""
}
