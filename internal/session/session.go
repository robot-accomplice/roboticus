// Package session provides the unified conversation session type shared by
// pipeline and agent layers. It is a leaf package with no internal imports
// beyond core and llm, enabling both layers to use it without circular deps.
package session

import (
	"encoding/json"

	"roboticus/internal/core"
	"roboticus/internal/llm"
)

// Session holds the state of an ongoing conversation.
type Session struct {
	ID            string
	AgentID       string
	AgentName     string
	Authority     core.AuthorityLevel
	SecurityClaim *core.SecurityClaim // Full claim for audit — set by pipeline stage 8.
	Workspace     string
	AllowedPaths  []string
	Channel       string
	ScopeKey      string // "platform:chatid" — used for cross-channel consent

	messages          []llm.Message
	pendingCalls      []llm.ToolCall
	memoryContext     string // Pre-retrieved memory block for cognitive scaffold (ARCHITECTURE.md §4).
	memoryIndex       string // Pre-built memory index block for recall/search tool guidance.
	taskIntent        string
	taskComplexity    string
	taskPlannedAction string
	taskSubgoals      []string

	// Perception artifact (Milestone 2): unified decision record produced
	// after task synthesis. Later stages read these fields instead of
	// re-classifying intent / risk / source-of-truth independently.
	taskRisk          string
	taskSourceOfTruth string
	taskRequiredTiers []string
	taskFreshness     bool
	agentRole         string
	turnWeight        string
	turnToolProfile   string
	turnPolicyReason  string
	sourceArtifacts   []string
	inspectionTarget  string
	destinationTarget string

	// v1.0.6 typed evidence artifact (see verification_evidence.go).
	// Populated by the pipeline after retrieval; consumed by the
	// verifier instead of re-parsing the rendered memoryContext text.
	verificationEvidence *VerificationEvidence
	// verificationEvidenceDerived tracks whether the current artifact
	// was synthesized from rendered memory text for compatibility
	// callers rather than supplied explicitly by the pipeline.
	verificationEvidenceDerived bool

	// v1.0.6 selected tool set for the current turn.
	// Populated by the pipeline's tool-pruning stage (query-time
	// semantic ranking + budget enforcement; see
	// internal/pipeline/pipeline_run_stages.go::stageToolPruning and
	// internal/agent/tools/tool_search.go). Consumed by the
	// agent-context builder so the ContextBuilder attaches exactly the
	// tools the pipeline selected instead of bulk-injecting everything
	// at loop time.
	//
	// nil means "no pipeline stage ran" (typical for non-pipeline
	// callers such as isolated executor-adapter tests). An empty
	// non-nil slice means "pipeline ran but produced no tools," which
	// the consumer MAY treat as authoritative or MAY fall back — the
	// authoritative behavior is owned by the consumer.
	selectedToolDefs   []llm.ToolDef
	lastAssistantPhase string
	continuation       *ContinuationArtifact

	// v1.0.6 hippocampus table summary for the current turn.
	// Populated by the pipeline's hippocampus stage (see
	// internal/pipeline/pipeline_run_stages.go::stageHippocampusSummary).
	// Consumed by buildAgentContext, which appends the summary as a
	// system message after the memory block so the model has ambient
	// awareness of the database surface (agent-owned tables, knowledge
	// sources, and system table count). Matches Rust's
	// crates/roboticus-pipeline/src/core/context_builder.rs:356-369
	// which calls roboticus_db::hippocampus::compact_summary at the
	// same position.
	//
	// Empty string means either (a) the pipeline stage didn't run, or
	// (b) the registry is empty. Consumers MUST NOT inject an empty
	// hippocampus message.
	hippocampusSummary string
}

// New creates a session with the given identity.
func New(id, agentID, agentName string) *Session {
	return &Session{
		ID:        id,
		AgentID:   agentID,
		AgentName: agentName,
		Authority: core.AuthorityExternal,
	}
}

// Messages returns the full message history.
func (s *Session) Messages() []llm.Message { return s.messages }

// ContinuationArtifact returns the one-shot provider-agnostic continuation
// payload prepared by the reflective R, if any.
func (s *Session) ContinuationArtifact() *ContinuationArtifact { return s.continuation }

// SetContinuationArtifact stores the one-shot continuation payload that the
// next think request should consume instead of raw session replay.
func (s *Session) SetContinuationArtifact(artifact *ContinuationArtifact) {
	s.continuation = artifact
}

// ConsumeContinuationArtifact returns the prepared continuation payload and
// clears it from the session.
func (s *Session) ConsumeContinuationArtifact() *ContinuationArtifact {
	artifact := s.continuation
	s.continuation = nil
	return artifact
}

// SetMessages replaces the full message history.
// The caller owns the replacement slice and should treat it as immutable after
// handing it to the session.
func (s *Session) SetMessages(messages []llm.Message) {
	s.messages = messages
}

// AddUserMessage appends a user message.
func (s *Session) AddUserMessage(content string) {
	s.messages = append(s.messages, llm.Message{Role: "user", Content: content})
}

// AddSystemMessage appends a system message.
func (s *Session) AddSystemMessage(content string) {
	s.messages = append(s.messages, llm.Message{Role: "system", Content: content})
}

// SetMemoryContext stores pre-retrieved memory for cognitive scaffold injection.
// Called by the pipeline before delegation/skill-first so early-exit paths
// still have full cognitive context (ARCHITECTURE.md §4).
func (s *Session) SetMemoryContext(block string) {
	s.memoryContext = block
	// Compatibility bridge: callers that only set the rendered memory
	// block still get a typed verification artifact at the session
	// boundary. Downstream stages consume typed data only.
	if s.verificationEvidence == nil || s.verificationEvidenceDerived {
		s.verificationEvidence = deriveVerificationEvidenceFromMemoryContext(block)
		s.verificationEvidenceDerived = true
	}
}

// MemoryContext returns the pre-retrieved memory block, if any.
func (s *Session) MemoryContext() string { return s.memoryContext }

// SetMemoryIndex stores the pre-built memory index for prompt injection.
func (s *Session) SetMemoryIndex(block string) { s.memoryIndex = block }

// MemoryIndex returns the pre-built memory index block, if any.
func (s *Session) MemoryIndex() string { return s.memoryIndex }

// SetTaskVerificationHints stores pipeline-computed task state so later stages
// can verify responses against structured intent/subgoals instead of re-deriving
// everything from raw prompt text.
func (s *Session) SetTaskVerificationHints(intent, complexity, plannedAction string, subgoals []string) {
	s.taskIntent = intent
	s.taskComplexity = complexity
	s.taskPlannedAction = plannedAction
	s.taskSubgoals = append([]string(nil), subgoals...)
}

// TaskIntent returns the pipeline-computed intent label, if any.
func (s *Session) TaskIntent() string { return s.taskIntent }

// TaskComplexity returns the pipeline-computed complexity label, if any.
func (s *Session) TaskComplexity() string { return s.taskComplexity }

// TaskPlannedAction returns the pipeline-computed planned action, if any.
func (s *Session) TaskPlannedAction() string { return s.taskPlannedAction }

// TaskSubgoals returns verifier-oriented subgoals computed by the pipeline.
func (s *Session) TaskSubgoals() []string { return append([]string(nil), s.taskSubgoals...) }

// SetPerception stores the pipeline-computed perception artifact so later
// stages can consume risk, source-of-truth, required tiers, and freshness
// without re-classifying.
func (s *Session) SetPerception(risk, sourceOfTruth string, requiredTiers []string, freshness bool) {
	s.taskRisk = risk
	s.taskSourceOfTruth = sourceOfTruth
	s.taskRequiredTiers = append([]string(nil), requiredTiers...)
	s.taskFreshness = freshness
}

// TaskRisk returns the perception risk label (low / medium / high).
func (s *Session) TaskRisk() string { return s.taskRisk }

// TaskSourceOfTruth returns the perception source-of-truth label.
func (s *Session) TaskSourceOfTruth() string { return s.taskSourceOfTruth }

// TaskRequiredTiers returns the memory tiers retrieval must consult.
func (s *Session) TaskRequiredTiers() []string {
	return append([]string(nil), s.taskRequiredTiers...)
}

// TaskFreshness returns whether the answer depends on current state.
func (s *Session) TaskFreshness() bool { return s.taskFreshness }

// SetAgentRole records whether this session is operating as the operator-facing
// orchestrator or as a bounded subagent worker.
func (s *Session) SetAgentRole(role string) { s.agentRole = role }

// AgentRole returns the role assigned to the active session.
func (s *Session) AgentRole() string { return s.agentRole }

// SetTurnEnvelopePolicy records the pipeline-selected turn weight, focused tool
// profile, and reason so downstream request assembly and routing consume the
// same classification.
func (s *Session) SetTurnEnvelopePolicy(weight, toolProfile, reason string) {
	s.turnWeight = weight
	s.turnToolProfile = toolProfile
	s.turnPolicyReason = reason
}

// TurnWeight returns the pipeline-selected turn weight for the active turn.
func (s *Session) TurnWeight() string { return s.turnWeight }

// TurnToolProfile returns the pipeline-selected tool profile for the active
// turn.
func (s *Session) TurnToolProfile() string { return s.turnToolProfile }

// TurnPolicyReason returns the rationale for the current turn weight.
func (s *Session) TurnPolicyReason() string { return s.turnPolicyReason }

// SetSourceArtifacts records prompt-declared source artifacts that the current
// turn is expected to read from rather than mutate.
func (s *Session) SetSourceArtifacts(paths []string) {
	s.sourceArtifacts = append([]string(nil), paths...)
}

// SourceArtifacts returns the prompt-declared source artifacts for the current
// turn. Callers must treat the result as immutable.
func (s *Session) SourceArtifacts() []string { return append([]string(nil), s.sourceArtifacts...) }

// SetInspectionTargetSummary records the authoritative inspection-target hint
// for the current turn. Empty string means no focused inspection target could
// be resolved.
func (s *Session) SetInspectionTargetSummary(summary string) { s.inspectionTarget = summary }

// InspectionTargetSummary returns the authoritative inspection-target hint for
// the current turn, if any.
func (s *Session) InspectionTargetSummary() string { return s.inspectionTarget }

// SetDestinationTargetSummary records the authoritative destination hint for
// the current turn's filesystem authoring, if any.
func (s *Session) SetDestinationTargetSummary(summary string) { s.destinationTarget = summary }

// DestinationTargetSummary returns the authoritative destination hint for the
// current turn's filesystem authoring, if any.
func (s *Session) DestinationTargetSummary() string { return s.destinationTarget }

// SetSelectedToolDefs records the tool set the pipeline selected for this
// turn (after query-time semantic ranking + token-budget enforcement).
// Callers should always pass a newly-allocated slice so later mutations
// don't leak through shared backing arrays; this setter stores the
// reference as-is and does not copy.
func (s *Session) SetSelectedToolDefs(defs []llm.ToolDef) { s.selectedToolDefs = defs }

// SelectedToolDefs returns the pipeline-selected tool set for this turn,
// or nil if no pruning stage ran. Returns the underlying slice by
// reference — callers must not mutate. Consumers that need to append
// should copy first.
func (s *Session) SelectedToolDefs() []llm.ToolDef { return s.selectedToolDefs }

// SetHippocampusSummary records the ambient database/table summary
// produced by the pipeline's hippocampus stage. Empty string is valid
// and is the signal "skip injection" — consumers must not append an
// empty summary message.
func (s *Session) SetHippocampusSummary(summary string) { s.hippocampusSummary = summary }

// HippocampusSummary returns the ambient database/table summary, or ""
// if the pipeline stage didn't run or produced an empty summary.
func (s *Session) HippocampusSummary() string { return s.hippocampusSummary }

// AddAssistantMessage appends an assistant message with optional tool calls.
func (s *Session) AddAssistantMessage(content string, toolCalls []llm.ToolCall) {
	s.AddAssistantMessageWithPhase(content, toolCalls, "")
}

// AddAssistantMessageWithPhase appends an assistant message and records which
// execution phase produced it when the caller knows that provenance.
func (s *Session) AddAssistantMessageWithPhase(content string, toolCalls []llm.ToolCall, phase string) {
	historyToolCalls := append([]llm.ToolCall(nil), toolCalls...)
	pendingToolCalls := append([]llm.ToolCall(nil), toolCalls...)
	s.messages = append(s.messages, llm.Message{
		Role: "assistant", Content: content, ToolCalls: historyToolCalls,
	})
	s.pendingCalls = pendingToolCalls
	s.lastAssistantPhase = phase
}

// AddToolResult appends a tool result message.
func (s *Session) AddToolResult(callID, toolName, output string, isError bool) {
	s.AddToolResultWithMetadata(callID, toolName, output, nil, isError)
}

// AddToolResultWithMetadata appends a tool result message with optional typed
// metadata preserved for later verification/RCA consumers.
func (s *Session) AddToolResultWithMetadata(callID, toolName, output string, metadata json.RawMessage, isError bool) {
	content := output
	if isError {
		content = "Error: " + output
	}
	s.messages = append(s.messages, llm.Message{
		Role: "tool", Content: content, ToolCallID: callID, Name: toolName, Metadata: metadata,
	})
	remaining := s.pendingCalls[:0]
	for _, tc := range s.pendingCalls {
		if tc.ID != callID {
			remaining = append(remaining, tc)
		}
	}
	s.pendingCalls = remaining
}

// PendingToolCalls returns tool calls not yet resolved.
func (s *Session) PendingToolCalls() []llm.ToolCall {
	return append([]llm.ToolCall(nil), s.pendingCalls...)
}

// LastAssistantContent returns the most recent assistant message content.
func (s *Session) LastAssistantContent() string {
	for i := len(s.messages) - 1; i >= 0; i-- {
		if s.messages[i].Role == "assistant" {
			return s.messages[i].Content
		}
	}
	return ""
}

// LastAssistantPhase returns the execution phase that produced the most recent
// assistant message when known. Empty string means the provenance was not
// recorded by the caller.
func (s *Session) LastAssistantPhase() string { return s.lastAssistantPhase }

// TurnCount returns the number of user messages (conversation turns).
func (s *Session) TurnCount() int {
	count := 0
	for _, m := range s.messages {
		if m.Role == "user" {
			count++
		}
	}
	return count
}

// MessageCount returns total messages in history.
func (s *Session) MessageCount() int { return len(s.messages) }
