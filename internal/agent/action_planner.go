package agent

import (
	"strings"

	"goboticus/internal/llm"
)

// PlannedAction represents the agent's next action.
type PlannedAction int

const (
	ActionInfer        PlannedAction = iota // Standard LLM inference
	ActionDelegate                          // Spawn/resume subagent
	ActionSkillExec                         // Direct skill execution
	ActionRetrieve                          // Memory-only retrieval (no LLM)
	ActionEscalate                          // Escalate to higher-tier model
	ActionWait                              // Await async result
	ActionNormRetry                         // Correct malformed tool call
	ActionSurfaceBlock                      // Surface a genuine blocker
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
	case ActionNormRetry:
		return "normalization_retry"
	case ActionSurfaceBlock:
		return "return_blocker"
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
//  2. Normalization retry streak -> NormalizationRetry
//  3. Pending delegation -> Delegate
//  4. Matched skill -> SkillExec
//  5. Memory-only query -> Retrieve
//  6. Low confidence + can escalate -> Escalate
//  7. Structural repetition -> ReturnBlocker
//  8. Engagement declining -> Escalate (change approach)
//  9. Default -> Infer
func PlanNextAction(state *OperatingState, input string, _ []llm.Message) ActionPlan {
	if state.PendingApproval {
		return ActionPlan{
			Action:     ActionWait,
			Reason:     "awaiting human approval for gated tool call",
			Confidence: 1.0,
		}
	}

	// Detect repeated tool-call parse failures and inject a corrective retry.
	if state.NormalizationRetryStreak >= 2 {
		return ActionPlan{
			Action:     ActionNormRetry,
			Reason:     "consecutive tool-call parse failures, injecting corrective prompt",
			Confidence: 0.95,
			Context:    map[string]any{"retry_streak": state.NormalizationRetryStreak},
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

	// Detect agent producing identical response structures repeatedly.
	if state.StructuralRepetition {
		return ActionPlan{
			Action:     ActionSurfaceBlock,
			Reason:     "structural repetition detected, surfacing blocker to user",
			Confidence: 0.8,
		}
	}

	// Detect user disengagement — short, declining messages suggest the agent
	// is not being helpful. Escalate to change approach.
	if state.EngagementDeclining && state.CanEscalate {
		return ActionPlan{
			Action:     ActionEscalate,
			Reason:     "engagement declining, escalating to higher-tier model",
			Confidence: 0.65,
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
