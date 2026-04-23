package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/google/uuid"
)

const (
	OrchestrationPatternSequential = "sequential"
	OrchestrationPatternParallel   = "parallel"
	OrchestrationPatternFanOut     = "fan_out_fan_in"
	OrchestrationPatternHandoff    = "handoff"
)

type OrchestrationSubtaskSpec struct {
	Description       string   `json:"description"`
	RequiredSkills    []string `json:"required_skills,omitempty"`
	PreferredSubagent string   `json:"preferred_subagent,omitempty"`
	Model             string   `json:"model,omitempty"`
}

type OrchestrationPlanSpec struct {
	WorkflowName string                     `json:"workflow_name"`
	Pattern      string                     `json:"pattern"`
	RequestedBy  string                     `json:"requested_by,omitempty"`
	Subtasks     []OrchestrationSubtaskSpec `json:"subtasks"`
}

type OrchestrationAssignment struct {
	OutcomeID         string   `json:"outcome_id"`
	SubtaskID         string   `json:"subtask_id"`
	Description       string   `json:"description"`
	AssignedSubagent  string   `json:"assigned_subagent"`
	AssignmentReason  string   `json:"assignment_reason"`
	RequiredSkills    []string `json:"required_skills,omitempty"`
	PreferredSubagent string   `json:"preferred_subagent,omitempty"`
	Status            string   `json:"status"`
}

type OrchestrationWorkflow struct {
	WorkflowID   string                    `json:"workflow_id"`
	WorkflowName string                    `json:"workflow_name"`
	Pattern      string                    `json:"pattern"`
	Status       string                    `json:"status"`
	RequestedBy  string                    `json:"requested_by,omitempty"`
	Assignments  []OrchestrationAssignment `json:"assignments"`
}

type SubagentOrchestrationRepository struct {
	q        Querier
	events   *TaskEventsRepository
	outcomes *DelegationRepository
}

type orchestrationCandidate struct {
	Name        string
	Description string
	Model       string
	Skills      []string
}

func NewSubagentOrchestrationRepository(q Querier) *SubagentOrchestrationRepository {
	return &SubagentOrchestrationRepository{
		q:        q,
		events:   NewTaskEventsRepository(q),
		outcomes: NewDelegationRepository(q),
	}
}

func (r *SubagentOrchestrationRepository) CreateWorkflow(ctx context.Context, spec OrchestrationPlanSpec) (*OrchestrationWorkflow, error) {
	spec.WorkflowName = strings.TrimSpace(spec.WorkflowName)
	spec.Pattern = normalizeOrchestrationPattern(spec.Pattern)
	spec.RequestedBy = strings.TrimSpace(spec.RequestedBy)
	if spec.WorkflowName == "" {
		spec.WorkflowName = "Delegated workflow"
	}
	if len(spec.Subtasks) == 0 {
		return nil, fmt.Errorf("at least one subtask is required")
	}
	if len(spec.Subtasks) > 8 {
		return nil, fmt.Errorf("workflow exceeds max of 8 subtasks")
	}

	candidates, err := r.listEnabledSubagents(ctx)
	if err != nil {
		return nil, err
	}
	if len(candidates) == 0 {
		return nil, fmt.Errorf("no enabled subagents available for orchestration")
	}

	workflowID := uuid.NewString()
	if _, err := r.q.ExecContext(ctx,
		`INSERT INTO tasks (id, title, description, status, priority, source)
		 VALUES (?, ?, ?, 'pending', 0, 'orchestrate-subagents')`,
		workflowID, spec.WorkflowName, workflowDescription(spec.Subtasks),
	); err != nil {
		return nil, err
	}

	workflow := &OrchestrationWorkflow{
		WorkflowID:   workflowID,
		WorkflowName: spec.WorkflowName,
		Pattern:      spec.Pattern,
		Status:       "pending",
		RequestedBy:  spec.RequestedBy,
		Assignments:  make([]OrchestrationAssignment, 0, len(spec.Subtasks)),
	}

	if err := r.appendWorkflowEvent(ctx, workflowID, spec.RequestedBy, "workflow_created", map[string]any{
		"workflow_name": spec.WorkflowName,
		"pattern":       spec.Pattern,
		"subtask_count": len(spec.Subtasks),
	}); err != nil {
		return nil, err
	}

	assignedCounts := make(map[string]int)
	for i, subtask := range spec.Subtasks {
		assignment, err := chooseOrchestrationAssignment(subtask, candidates, assignedCounts, i)
		if err != nil {
			return nil, err
		}
		outcomeID := uuid.NewString()
		subtaskID := fmt.Sprintf("%s-s%02d", workflowID, i+1)
		if err := r.outcomes.Save(ctx, DelegationRow{
			ID:            outcomeID,
			ParentTaskID:  workflowID,
			SubagentID:    assignment.Name,
			Status:        "planned",
			ResultSummary: "",
			ErrorMessage:  "",
			DurationMs:    0,
		}); err != nil {
			return nil, err
		}
		payload := map[string]any{
			"subtask_id":         subtaskID,
			"description":        strings.TrimSpace(subtask.Description),
			"required_skills":    cleanStringList(subtask.RequiredSkills),
			"preferred_subagent": strings.TrimSpace(subtask.PreferredSubagent),
			"assigned_subagent":  assignment.Name,
			"assignment_reason":  assignmentReason(subtask, assignment),
			"assigned_model":     assignment.Model,
		}
		if err := r.appendWorkflowEvent(ctx, workflowID, assignment.Name, "subtask_planned", payload); err != nil {
			return nil, err
		}
		workflow.Assignments = append(workflow.Assignments, OrchestrationAssignment{
			OutcomeID:         outcomeID,
			SubtaskID:         subtaskID,
			Description:       strings.TrimSpace(subtask.Description),
			AssignedSubagent:  assignment.Name,
			AssignmentReason:  assignmentReason(subtask, assignment),
			RequiredSkills:    cleanStringList(subtask.RequiredSkills),
			PreferredSubagent: strings.TrimSpace(subtask.PreferredSubagent),
			Status:            "planned",
		})
	}

	return workflow, nil
}

func (r *SubagentOrchestrationRepository) MarkThreadedToInference(ctx context.Context, workflowID, summary string) error {
	if _, err := r.q.ExecContext(ctx,
		`UPDATE tasks SET status = 'in_progress', updated_at = datetime('now') WHERE id = ?`,
		workflowID,
	); err != nil {
		return err
	}
	return r.appendWorkflowEvent(ctx, workflowID, "", "workflow_threaded_to_inference", map[string]any{
		"summary": strings.TrimSpace(summary),
	})
}

func (r *SubagentOrchestrationRepository) MarkCompleted(ctx context.Context, workflowID, summary string) error {
	if _, err := r.q.ExecContext(ctx,
		`UPDATE tasks SET status = 'completed', updated_at = datetime('now') WHERE id = ?`,
		workflowID,
	); err != nil {
		return err
	}
	if err := r.updateOutcomeStatuses(ctx, workflowID, "completed", strings.TrimSpace(summary), ""); err != nil {
		return err
	}
	return r.appendWorkflowEvent(ctx, workflowID, "", "workflow_completed", map[string]any{
		"summary": strings.TrimSpace(summary),
	})
}

func (r *SubagentOrchestrationRepository) MarkFailed(ctx context.Context, workflowID, errMsg string) error {
	if _, err := r.q.ExecContext(ctx,
		`UPDATE tasks SET status = 'failed', updated_at = datetime('now') WHERE id = ?`,
		workflowID,
	); err != nil {
		return err
	}
	if err := r.updateOutcomeStatuses(ctx, workflowID, "failed", "", strings.TrimSpace(errMsg)); err != nil {
		return err
	}
	return r.appendWorkflowEvent(ctx, workflowID, "", "workflow_failed", map[string]any{
		"error": strings.TrimSpace(errMsg),
	})
}

func (r *SubagentOrchestrationRepository) appendWorkflowEvent(ctx context.Context, workflowID, assignedTo, eventType string, payload map[string]any) error {
	data, _ := json.Marshal(payload)
	return r.events.Append(ctx, TaskEventRow{
		ID:          uuid.NewString(),
		TaskID:      workflowID,
		AssignedTo:  strings.TrimSpace(assignedTo),
		EventType:   eventType,
		PayloadJSON: string(data),
	})
}

func (r *SubagentOrchestrationRepository) updateOutcomeStatuses(ctx context.Context, workflowID, status, summary, errMsg string) error {
	rows, err := r.outcomes.List(ctx, workflowID)
	if err != nil {
		return err
	}
	for _, row := range rows {
		if err := r.outcomes.UpdateOutcome(ctx, row.ID, status, summary, errMsg, row.DurationMs); err != nil {
			return err
		}
	}
	return nil
}

func (r *SubagentOrchestrationRepository) listEnabledSubagents(ctx context.Context) ([]orchestrationCandidate, error) {
	rows, err := r.q.QueryContext(ctx,
		`SELECT name, COALESCE(description, ''), COALESCE(model, ''), COALESCE(skills_json, '[]')
		   FROM sub_agents
		  WHERE enabled = 1 AND COALESCE(role, 'subagent') = 'subagent'
		  ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	candidates := make([]orchestrationCandidate, 0)
	for rows.Next() {
		var row orchestrationCandidate
		var skillsJSON string
		if err := rows.Scan(&row.Name, &row.Description, &row.Model, &skillsJSON); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(skillsJSON), &row.Skills)
		candidates = append(candidates, row)
	}
	return candidates, rows.Err()
}

func chooseOrchestrationAssignment(subtask OrchestrationSubtaskSpec, candidates []orchestrationCandidate, assignedCounts map[string]int, index int) (orchestrationCandidate, error) {
	preferred := strings.TrimSpace(subtask.PreferredSubagent)
	if preferred != "" {
		for _, candidate := range candidates {
			if candidate.Name == preferred {
				assignedCounts[candidate.Name]++
				return candidate, nil
			}
		}
		return orchestrationCandidate{}, fmt.Errorf("preferred subagent %q is not enabled", preferred)
	}

	type scored struct {
		candidate orchestrationCandidate
		score     int
		load      int
	}
	scores := make([]scored, 0, len(candidates))
	required := cleanStringList(subtask.RequiredSkills)
	for _, candidate := range candidates {
		score := 0
		if len(required) > 0 {
			candidateSkills := skillSet(candidate.Skills)
			for _, req := range required {
				if candidateSkills[strings.ToLower(req)] {
					score += 100
				}
			}
		}
		descLower := strings.ToLower(candidate.Description + " " + candidate.Name)
		for _, token := range tokenizeOrchestrationText(subtask.Description) {
			if strings.Contains(descLower, token) {
				score += 3
			}
		}
		scores = append(scores, scored{
			candidate: candidate,
			score:     score,
			load:      assignedCounts[candidate.Name],
		})
	}
	sort.SliceStable(scores, func(i, j int) bool {
		if scores[i].score != scores[j].score {
			return scores[i].score > scores[j].score
		}
		if scores[i].load != scores[j].load {
			return scores[i].load < scores[j].load
		}
		return scores[i].candidate.Name < scores[j].candidate.Name
	})
	if len(scores) == 0 {
		return orchestrationCandidate{}, sql.ErrNoRows
	}
	chosen := scores[0].candidate
	if scores[0].score == 0 && len(candidates) > 1 {
		chosen = candidates[index%len(candidates)]
	}
	assignedCounts[chosen.Name]++
	return chosen, nil
}

func assignmentReason(subtask OrchestrationSubtaskSpec, candidate orchestrationCandidate) string {
	if strings.TrimSpace(subtask.PreferredSubagent) != "" {
		return "preferred_subagent"
	}
	required := cleanStringList(subtask.RequiredSkills)
	if len(required) > 0 {
		return "required_skill_match"
	}
	return "balanced_round_robin"
}

func workflowDescription(subtasks []OrchestrationSubtaskSpec) string {
	parts := make([]string, 0, len(subtasks))
	for _, subtask := range subtasks {
		if text := strings.TrimSpace(subtask.Description); text != "" {
			parts = append(parts, text)
		}
	}
	return strings.Join(parts, "\n")
}

func normalizeOrchestrationPattern(pattern string) string {
	switch strings.ToLower(strings.TrimSpace(pattern)) {
	case OrchestrationPatternSequential, OrchestrationPatternParallel, OrchestrationPatternFanOut, OrchestrationPatternHandoff:
		return strings.ToLower(strings.TrimSpace(pattern))
	default:
		return OrchestrationPatternFanOut
	}
}

func cleanStringList(values []string) []string {
	out := make([]string, 0, len(values))
	seen := make(map[string]bool)
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		key := strings.ToLower(value)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, value)
	}
	return out
}

func skillSet(values []string) map[string]bool {
	out := make(map[string]bool, len(values))
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if value != "" {
			out[value] = true
		}
	}
	return out
}

func tokenizeOrchestrationText(text string) []string {
	parts := strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return (r < 'a' || r > 'z') && (r < '0' || r > '9')
	})
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if len(part) >= 4 {
			out = append(out, part)
		}
	}
	return out
}
