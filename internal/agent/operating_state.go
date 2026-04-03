package agent

// OperatingState is a lightweight decision context for the action planner.
// It captures the minimal signals needed for pure-function action planning.
type OperatingState struct {
	// PendingApproval indicates a tool call is gated awaiting human approval.
	PendingApproval bool
	// PendingDelegation indicates a subagent result is awaited.
	PendingDelegation bool
	// MatchedSkill is non-empty when a skill trigger has been matched.
	MatchedSkill string
	// Confidence is the agent's self-assessed confidence (0.0–1.0).
	// Zero means unset.
	Confidence float64
	// CanEscalate indicates a higher-tier model is available for escalation.
	CanEscalate bool
	// NormalizationRetryStreak counts consecutive tool-call parse failures.
	NormalizationRetryStreak int
	// StructuralRepetition indicates the agent is producing identical responses.
	StructuralRepetition bool
	// EngagementDeclining indicates user messages are getting shorter.
	EngagementDeclining bool
}

// TaskStateInput gathers raw signals from all subsystems for state synthesis.
type TaskStateInput struct {
	UserContent string
	Intents     []string
	Authority   string

	// Retrieval metrics.
	RetrievalAvgSimilarity float64
	RetrievalCount         int
	RetrievalBudgetUsed    float64 // 0.0–1.0

	// Tool search stats.
	ToolCandidatesConsidered int
	ToolCandidatesSelected   int
	ToolTokenSavings         int
	MCPToolsAvailable        bool

	// Roster state.
	TaskableAgentCount int
	FitAgentCount      int
	FitAgentNames      []string

	// Skill state.
	EnabledSkillCount  int
	MatchingSkillCount int
	MissingSkills      []string

	// Runtime constraints.
	RemainingBudgetTokens int
	ProviderBreakerOpen   bool
	InferenceMode         string // "standard" or "streaming"

	// Delegation signals.
	ExplicitSpecialistWorkflow bool
	NamedToolMatch             bool

	// Behavioral history.
	RecentResponseSkeletons  []string
	RecentUserMessageLengths []int
	PreviousTurnHadProtocol  bool
	NormalizationRetryStreak int
}

// TaskOperatingState is the synthesized decision context for the action planner.
type TaskOperatingState struct {
	Classification    TaskClassification
	MemoryConfidence  MemoryConfidence
	RuntimeConstraint RuntimeConstraints
	ToolFit           ToolFit
	RosterFit         RosterFit
	SkillFit          SkillFit
	Behavioral        BehavioralHistory
}

// TaskClassification distinguishes conversational from task-oriented turns.
type TaskClassification int

const (
	ClassConversation TaskClassification = iota
	ClassTask
)

// MemoryConfidence summarizes retrieval system health.
type MemoryConfidence struct {
	AvgSimilarity     float64
	BudgetUtilization float64
	RetrievalCount    int
	RecallGap         bool
}

// RuntimeConstraints captures resource limits.
type RuntimeConstraints struct {
	RemainingBudget int
	BudgetPressured bool // true if < 2000 tokens
	BreakerOpen     bool
	InferenceMode   string
}

// ToolFit summarizes tool availability.
type ToolFit struct {
	AvailableCount     int
	HighRelevanceCount int
	TokenSavings       int
	MCPAvailable       bool
}

// RosterFit summarizes subagent availability.
type RosterFit struct {
	TaskableCount    int
	FitCount         int
	FitNames         []string
	ExplicitWorkflow bool
	NamedToolMatch   bool
}

// SkillFit summarizes skill availability.
type SkillFit struct {
	EnabledCount  int
	MatchingCount int
	MissingSkills []string
}

// BehavioralHistory tracks response patterns for repetition/engagement detection.
type BehavioralHistory struct {
	StructuralRepetition bool
	RepetitionStreak     int
	EngagementDeclining  bool
	ProtocolIssues       bool
	NormRetryStreak      int
}

var taskIntents = map[string]bool{
	"execution": true, "delegation": true, "cron": true,
	"tool_request": true, "current_events": true,
}

// SynthesizeState converts raw subsystem signals into a structured operating state.
func SynthesizeState(input TaskStateInput) *TaskOperatingState {
	state := &TaskOperatingState{}

	// Classification.
	state.Classification = ClassConversation
	if input.ExplicitSpecialistWorkflow {
		state.Classification = ClassTask
	} else {
		for _, intent := range input.Intents {
			if taskIntents[intent] {
				state.Classification = ClassTask
				break
			}
		}
	}

	// Memory confidence.
	state.MemoryConfidence = MemoryConfidence{
		AvgSimilarity:     input.RetrievalAvgSimilarity,
		BudgetUtilization: input.RetrievalBudgetUsed,
		RetrievalCount:    input.RetrievalCount,
		RecallGap:         input.RetrievalAvgSimilarity < 0.5 && input.RetrievalBudgetUsed < 0.8,
	}

	// Runtime constraints.
	state.RuntimeConstraint = RuntimeConstraints{
		RemainingBudget: input.RemainingBudgetTokens,
		BudgetPressured: input.RemainingBudgetTokens < 2000,
		BreakerOpen:     input.ProviderBreakerOpen,
		InferenceMode:   input.InferenceMode,
	}

	// Tool fit.
	highRel := min(input.ToolCandidatesSelected, max(input.ToolCandidatesConsidered/3, 1))
	state.ToolFit = ToolFit{
		AvailableCount:     input.ToolCandidatesConsidered,
		HighRelevanceCount: highRel,
		TokenSavings:       input.ToolTokenSavings,
		MCPAvailable:       input.MCPToolsAvailable,
	}

	// Roster fit.
	state.RosterFit = RosterFit{
		TaskableCount:    input.TaskableAgentCount,
		FitCount:         input.FitAgentCount,
		FitNames:         input.FitAgentNames,
		ExplicitWorkflow: input.ExplicitSpecialistWorkflow,
		NamedToolMatch:   input.NamedToolMatch,
	}

	// Skill fit.
	state.SkillFit = SkillFit{
		EnabledCount:  input.EnabledSkillCount,
		MatchingCount: input.MatchingSkillCount,
		MissingSkills: input.MissingSkills,
	}

	// Behavioral history.
	bh := BehavioralHistory{
		ProtocolIssues:  input.PreviousTurnHadProtocol,
		NormRetryStreak: input.NormalizationRetryStreak,
	}
	// Detect structural repetition (3+ identical consecutive skeletons).
	if len(input.RecentResponseSkeletons) >= 3 {
		last := input.RecentResponseSkeletons[len(input.RecentResponseSkeletons)-1]
		streak := 1
		for i := len(input.RecentResponseSkeletons) - 2; i >= 0; i-- {
			if input.RecentResponseSkeletons[i] == last {
				streak++
			} else {
				break
			}
		}
		if streak >= 3 {
			bh.StructuralRepetition = true
			bh.RepetitionStreak = streak
		}
	}
	// Detect engagement decline (3+ consecutive decreasing message lengths, latest < 30).
	if len(input.RecentUserMessageLengths) >= 3 {
		n := len(input.RecentUserMessageLengths)
		declining := true
		for i := n - 2; i >= max(0, n-3); i-- {
			if input.RecentUserMessageLengths[i] <= input.RecentUserMessageLengths[i+1] {
				declining = false
				break
			}
		}
		if declining && input.RecentUserMessageLengths[n-1] < 30 {
			bh.EngagementDeclining = true
		}
	}
	state.Behavioral = bh

	return state
}
