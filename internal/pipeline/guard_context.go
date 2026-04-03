package pipeline

// GuardContext provides rich context for guards that need more than just the
// response text. Guards implementing ContextualGuard receive this context;
// simple text-only guards implement the base Guard interface.
type GuardContext struct {
	// UserPrompt is the original user message that triggered this inference.
	UserPrompt string

	// Intents are the classified intent labels for the user message.
	// Populated by the IntentClassifier (Phase 3). Empty until classifier is wired.
	Intents []string

	// ToolResults are (tool_name, output) pairs from tool calls in this turn.
	ToolResults []ToolResultEntry

	// AgentName is the configured agent display name.
	AgentName string

	// ResolvedModel is the LLM model used for this inference.
	ResolvedModel string

	// PreviousAssistant is the last assistant message before this turn.
	PreviousAssistant string

	// PriorAssistantMessages contains all prior assistant messages in the session.
	PriorAssistantMessages []string

	// SubagentNames are the lowercase names of all configured subagents.
	SubagentNames []string

	// SemanticScores are pre-computed classifier scores keyed by category name.
	// Values are (score, trust_level) pairs. Populated by semantic classifier.
	SemanticScores map[string]float64

	// DelegationProvenance tracks subagent lifecycle events in this turn.
	DelegationProvenance DelegationProvenance
}

// ToolResultEntry pairs a tool name with its output.
type ToolResultEntry struct {
	ToolName string
	Output   string
}

// DelegationProvenance tracks whether subagent delegation steps completed.
type DelegationProvenance struct {
	SubagentTaskStarted    bool
	SubagentTaskCompleted  bool
	SubagentResultAttached bool
}

// HasIntent returns true if any classified intent matches the given label.
func (gc *GuardContext) HasIntent(label string) bool {
	for _, intent := range gc.Intents {
		if intent == label {
			return true
		}
	}
	return false
}

// HasToolResult returns true if any tool with the given name was called.
func (gc *GuardContext) HasToolResult(toolName string) bool {
	for _, tr := range gc.ToolResults {
		if tr.ToolName == toolName {
			return true
		}
	}
	return false
}

// ContextualGuard extends Guard with access to rich context.
// Guards that need user prompt, intents, tool results, or subagent names
// should implement this interface. The GuardChain auto-detects and passes
// the context when available.
type ContextualGuard interface {
	Guard
	CheckWithContext(content string, ctx *GuardContext) GuardResult
}

// ApplyFullWithContext runs all guards with the given context.
// Contextual guards receive the context; basic guards receive only content.
func (gc *GuardChain) ApplyFullWithContext(content string, ctx *GuardContext) ApplyResult {
	result := ApplyResult{Content: content}
	for _, g := range gc.guards {
		var gr GuardResult
		if cg, ok := g.(ContextualGuard); ok && ctx != nil {
			gr = cg.CheckWithContext(result.Content, ctx)
		} else {
			gr = g.Check(result.Content)
		}
		if !gr.Passed {
			result.Violations = append(result.Violations, g.Name())
			if gr.Retry {
				result.RetryRequested = true
				result.RetryReason = gr.Reason
				return result
			}
			if gr.Content != "" {
				result.Content = gr.Content
			}
		}
	}
	return result
}
