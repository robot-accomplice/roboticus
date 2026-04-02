package agent

import (
	"strings"

	"goboticus/internal/llm"
)

// PlannedAction represents the agent's next action.
type PlannedAction int

const (
	ActionInfer     PlannedAction = iota // Standard LLM inference
	ActionDelegate                       // Spawn/resume subagent
	ActionSkillExec                      // Direct skill execution
	ActionRetrieve                       // Memory-only retrieval (no LLM)
	ActionEscalate                       // Escalate to higher-tier model
	ActionWait                           // Await async result
)

func (a PlannedAction) String() string {
	switch a {
	case ActionInfer:
		return "infer"
	case ActionDelegate:
		return "delegate"
	case ActionSkillExec:
		return "skill_exec"
	case ActionRetrieve:
		return "retrieve"
	case ActionEscalate:
		return "escalate"
	case ActionWait:
		return "wait"
	default:
		return "unknown"
	}
}

// ActionPlan is the result of action planning.
type ActionPlan struct {
	Action     PlannedAction
	Reason     string
	Confidence float64
	Context    map[string]any
}

// PlanNextAction evaluates operating state and produces a plan.
// This is a pure function — no I/O, no side effects.
//
// Priority order (first match wins):
//  1. Pending approval -> Wait
//  2. Pending delegation -> Delegate
//  3. Matched skill -> SkillExec
//  4. Memory-only query -> Retrieve
//  5. Low confidence + can escalate -> Escalate
//  6. Default -> Infer
func PlanNextAction(state *OperatingState, input string, history []llm.Message) ActionPlan {
	if state.PendingApproval {
		return ActionPlan{
			Action:     ActionWait,
			Reason:     "awaiting human approval for gated tool call",
			Confidence: 1.0,
		}
	}

	if state.PendingDelegation {
		return ActionPlan{
			Action:     ActionDelegate,
			Reason:     "delegation results pending from subagent",
			Confidence: 0.9,
		}
	}

	if state.MatchedSkill != "" {
		return ActionPlan{
			Action:     ActionSkillExec,
			Reason:     "skill trigger matched: " + state.MatchedSkill,
			Confidence: 0.85,
			Context:    map[string]any{"skill": state.MatchedSkill},
		}
	}

	if isMemoryOnlyQuery(input) {
		return ActionPlan{
			Action:     ActionRetrieve,
			Reason:     "query appears to be about past context",
			Confidence: 0.7,
		}
	}

	if state.Confidence > 0 && state.Confidence < 0.4 && state.CanEscalate {
		return ActionPlan{
			Action:     ActionEscalate,
			Reason:     "low confidence, escalating to higher-tier model",
			Confidence: 0.6,
		}
	}

	return ActionPlan{
		Action:     ActionInfer,
		Reason:     "standard inference",
		Confidence: 0.8,
	}
}

// isMemoryOnlyQuery checks if the input is asking about past conversations.
func isMemoryOnlyQuery(input string) bool {
	lower := strings.ToLower(input)
	memorySignals := []string{
		"what did we discuss",
		"what did we talk about",
		"do you remember",
		"last time we",
		"previously we",
		"you mentioned earlier",
		"our earlier conversation",
	}
	for _, signal := range memorySignals {
		if strings.Contains(lower, signal) {
			return true
		}
	}
	return false
}
