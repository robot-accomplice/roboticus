package agent

import (
	"fmt"
	"sort"
)

// PlannedAction is the action type the planner recommends.
type PlannedAction int

const (
	ActionAnswerDirectly PlannedAction = iota
	ActionContinueCentralized
	ActionInspectMemory
	ActionComposeSkill
	ActionComposeSubagent
	ActionDelegateToSpecialist
	ActionReturnBlocker
	ActionNormalizationRetry
)

func (a PlannedAction) String() string {
	switch a {
	case ActionAnswerDirectly:
		return "answer_directly"
	case ActionContinueCentralized:
		return "continue_centralized"
	case ActionInspectMemory:
		return "inspect_memory"
	case ActionComposeSkill:
		return "compose_skill"
	case ActionComposeSubagent:
		return "compose_subagent"
	case ActionDelegateToSpecialist:
		return "delegate_to_specialist"
	case ActionReturnBlocker:
		return "return_blocker"
	case ActionNormalizationRetry:
		return "normalization_retry"
	default:
		return "unknown"
	}
}

// ActionCandidate is a scored action proposal.
type ActionCandidate struct {
	Action     PlannedAction
	Confidence float64
	Rationale  string
}

// ActionPlan is the planner's output: ranked candidates and selected action.
type ActionPlan struct {
	Candidates []ActionCandidate
	Selected   PlannedAction
	Rationale  string
}

// ActionPlanner produces a deterministic action plan from the operating state.
// No LLM is involved — this is pure heuristic scoring with 10 rules.
type ActionPlanner struct {
	enabled bool
}

// NewActionPlanner creates a planner.
func NewActionPlanner(enabled bool) *ActionPlanner {
	return &ActionPlanner{enabled: enabled}
}

// Plan evaluates 10 deterministic rules and returns a ranked action plan.
func (p *ActionPlanner) Plan(state *TaskOperatingState) ActionPlan {
	if !p.enabled || state == nil {
		return ActionPlan{
			Selected:  ActionContinueCentralized,
			Rationale: "planner disabled",
		}
	}

	var candidates []ActionCandidate

	// Rule 1: Conversation → AnswerDirectly.
	if state.Classification == ClassConversation {
		candidates = append(candidates, ActionCandidate{
			Action: ActionAnswerDirectly, Confidence: 0.95,
			Rationale: "conversational turn, no task routing needed",
		})
	}

	// Rule 2: Provider breaker open → ReturnBlocker.
	if state.RuntimeConstraint.BreakerOpen {
		candidates = append(candidates, ActionCandidate{
			Action: ActionReturnBlocker, Confidence: 0.8,
			Rationale: "provider circuit breaker is open",
		})
	}

	// Rule 3: Explicit workflow + matching roster → DelegateToSpecialist.
	if state.RosterFit.ExplicitWorkflow && state.RosterFit.FitCount > 0 {
		candidates = append(candidates, ActionCandidate{
			Action: ActionDelegateToSpecialist, Confidence: 0.9,
			Rationale: fmt.Sprintf("explicit workflow matches %d specialist(s)", state.RosterFit.FitCount),
		})
	}

	// Rule 3b: Explicit workflow + named plugin tool match → ContinueCentralized.
	if state.RosterFit.ExplicitWorkflow && state.RosterFit.NamedToolMatch {
		candidates = append(candidates, ActionCandidate{
			Action: ActionContinueCentralized, Confidence: 0.88,
			Rationale: "explicit workflow resolved to named plugin tool",
		})
	}

	// Rule 4: Explicit workflow + empty roster + creator authority → ComposeSubagent.
	if state.RosterFit.ExplicitWorkflow && state.RosterFit.FitCount == 0 {
		candidates = append(candidates, ActionCandidate{
			Action: ActionComposeSubagent, Confidence: 0.85,
			Rationale: "explicit workflow but no matching specialist",
		})
	}

	// Rule 5: Delegation recommended + fit exists → DelegateToSpecialist.
	if state.RosterFit.FitCount > 0 && state.Classification == ClassTask {
		candidates = append(candidates, ActionCandidate{
			Action: ActionDelegateToSpecialist, Confidence: 0.75,
			Rationale: "task classification with available specialist",
		})
	}

	// Rule 6: Memory recall gap → InspectMemory.
	if state.MemoryConfidence.RecallGap && !state.RuntimeConstraint.BudgetPressured {
		candidates = append(candidates, ActionCandidate{
			Action: ActionInspectMemory, Confidence: 0.7,
			Rationale: "recall gap detected, deeper retrieval recommended",
		})
	}

	// Rule 7: Missing skills + task → ComposeSkill.
	if len(state.SkillFit.MissingSkills) > 0 && state.Classification == ClassTask {
		candidates = append(candidates, ActionCandidate{
			Action: ActionComposeSkill, Confidence: 0.65,
			Rationale: fmt.Sprintf("missing skills: %v", state.SkillFit.MissingSkills),
		})
	}

	// Rule 8: Previous turn had protocol issues → NormalizationRetry.
	if state.Behavioral.ProtocolIssues {
		conf := 0.75 + float64(state.Behavioral.NormRetryStreak)*0.02
		if conf > 0.85 {
			conf = 0.85
		}
		candidates = append(candidates, ActionCandidate{
			Action: ActionNormalizationRetry, Confidence: conf,
			Rationale: fmt.Sprintf("protocol issues detected, retry streak: %d", state.Behavioral.NormRetryStreak),
		})
	}

	// Rule 9: Structural repetition → ContinueCentralized with variation.
	if state.Behavioral.StructuralRepetition {
		candidates = append(candidates, ActionCandidate{
			Action: ActionContinueCentralized, Confidence: 0.55,
			Rationale: fmt.Sprintf("structural repetition detected (%d consecutive)", state.Behavioral.RepetitionStreak),
		})
	}

	// Rule 10: Engagement declining → ContinueCentralized.
	if state.Behavioral.EngagementDeclining {
		candidates = append(candidates, ActionCandidate{
			Action: ActionContinueCentralized, Confidence: 0.5,
			Rationale: "user engagement declining",
		})
	}

	// Fallback.
	if len(candidates) == 0 {
		candidates = append(candidates, ActionCandidate{
			Action: ActionContinueCentralized, Confidence: 0.6,
			Rationale: "no specific rule matched, proceeding with standard inference",
		})
	}

	// Sort by confidence descending.
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].Confidence > candidates[j].Confidence
	})

	return ActionPlan{
		Candidates: candidates,
		Selected:   candidates[0].Action,
		Rationale:  candidates[0].Rationale,
	}
}
