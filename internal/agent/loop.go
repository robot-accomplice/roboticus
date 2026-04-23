package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog/log"

	"roboticus/internal/agent/memory"
	"roboticus/internal/agent/policy"
	agenttools "roboticus/internal/agent/tools"
	"roboticus/internal/core"
	"roboticus/internal/db"
	"roboticus/internal/llm"
)

// ErrMaxTurns is returned when the ReAct loop exceeds the configured turn limit.
var ErrMaxTurns = errors.New("max turns exceeded")

// LoopState represents the current phase of the ReAct state machine.
type LoopState int

const (
	StateThinking   LoopState = iota // LLM is generating a response
	StateActing                      // Executing a tool call
	StateObserving                   // Processing tool results
	StatePersisting                  // Saving state (memory, context)
	StateIdle                        // No progress detected
	StateDone                        // Terminal state
)

func (s LoopState) String() string {
	switch s {
	case StateThinking:
		return "thinking"
	case StateActing:
		return "acting"
	case StateObserving:
		return "observing"
	case StatePersisting:
		return "persisting"
	case StateIdle:
		return "idle"
	case StateDone:
		return "done"
	default:
		return fmt.Sprintf("unknown(%d)", int(s))
	}
}

// Action represents what the agent decided to do in this iteration.
type Action int

const (
	ActionThink   Action = iota // Continue reasoning
	ActionAct                   // Invoke a tool
	ActionObserve               // Process observation
	ActionPersist               // Save state
	ActionNoOp                  // Nothing useful happened
	ActionFinish                // Terminate loop
)

// LoopConfig controls the ReAct loop behavior.
type LoopConfig struct {
	MaxTurns               int           // Maximum thinking iterations before forced stop
	IdleThreshold          int           // Consecutive NoOps before entering Idle state
	LoopWindow             int           // Sliding window size for loop detection
	MaxSameRouteNoProgress int           // Same-route repeated no-progress completions before termination
	MaxReadOnlyExploration int           // Successful read-only exploration steps on execute_directly turns before termination
	MaxLoopDuration        time.Duration // Wall-clock deadline for entire ReAct loop
}

// DefaultLoopConfig returns sensible defaults.
func DefaultLoopConfig() LoopConfig {
	return LoopConfig{
		MaxTurns:               25,
		MaxLoopDuration:        300 * time.Second, // 5 min — local models need 60-80s per call
		IdleThreshold:          3,
		LoopWindow:             3,
		MaxSameRouteNoProgress: 2,
		MaxReadOnlyExploration: 4,
	}
}

// recentCall tracks a tool invocation for loop detection.
type recentCall struct {
	tool   string
	params string
}

type recentInferenceOutcome struct {
	route           string
	skeleton        string
	placeholderOnly bool
	toolResultsSeen int
	streak          int
}

// lastRoleSnippet returns a truncated content snippet from the most
// recent message with the given role in the request's messages
// slice. Used by turn-by-turn instrumentation to surface what
// actually reached the LLM. Returns "" when no message of that role
// is present (a meaningful diagnostic in itself — e.g., empty
// last_user means the LLM is being called with no user request,
// which is the v1.0.6 empty-prompt bug pattern).
func lastRoleSnippet(messages []llm.Message, role string, maxLen int) string {
	if maxLen <= 0 {
		maxLen = 200
	}
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == role {
			content := messages[i].Content
			if len(content) > maxLen {
				return content[:maxLen] + "…"
			}
			return content
		}
	}
	return ""
}

// ReactTraceEntry records a single tool call for the flight recorder.
// Matches Rust's react trace: tool_name, duration_ms, success, result_summary.
type ReactTraceEntry struct {
	ToolName      string `json:"tool_name"`
	DurationMs    int64  `json:"duration_ms"`
	Success       bool   `json:"success"`
	ResultSummary string `json:"result_summary"`
	Source        string `json:"source"` // "builtin", "plugin", "mcp"
}

// Loop implements the ReAct state machine. It coordinates the agent's
// think → act → observe cycle with loop/idle detection and turn limits.
type Loop struct {
	mu     sync.Mutex
	config LoopConfig
	state  LoopState

	turnCount                 int
	noOpCount                 int
	recentCalls               []recentCall
	lastNoProgress            recentInferenceOutcome
	readOnlyExplorationStreak int
	failedCalls               map[string]int    // tool+params → failure count (error dedup)
	successfulCalls           map[string]int    // protected resource/effect fingerprint → prior successes in this turn
	traceEntries              []ReactTraceEntry // flight recorder
	doneReason                string

	// Dependencies injected at construction.
	llm        llm.Completer
	tools      *ToolRegistry
	recorder   ToolCallRecorder
	policy     *policy.Engine
	injection  *InjectionDetector
	memory     *memory.Manager
	context    *ContextBuilder
	normalizer *agenttools.NormalizationFactory
	store      *db.Store
}

// LoopDeps bundles the dependencies for a Loop.
type LoopDeps struct {
	LLM         llm.Completer
	Tools       *ToolRegistry
	Recorder    ToolCallRecorder
	Policy      *policy.Engine
	Injection   *InjectionDetector
	Memory      *memory.Manager
	Context     *ContextBuilder
	Normalizers *agenttools.NormalizationFactory
	Store       *db.Store
}

type ToolExecutionRecord struct {
	TurnID     string
	ToolCallID string
	ToolName   string
	Input      string
	Output     string
	Status     string
	DurationMs int64
}

type ToolCallRecorder interface {
	RecordToolExecution(ctx context.Context, rec ToolExecutionRecord) error
}

// NewLoop creates a ReAct loop with the given config and dependencies.
func NewLoop(cfg LoopConfig, deps LoopDeps) *Loop {
	return &Loop{
		config:          cfg,
		state:           StateThinking,
		failedCalls:     make(map[string]int),
		successfulCalls: make(map[string]int),
		llm:             deps.LLM,
		tools:           deps.Tools,
		recorder:        deps.Recorder,
		policy:          deps.Policy,
		injection:       deps.Injection,
		memory:          deps.Memory,
		context:         deps.Context,
		normalizer:      deps.Normalizers,
		store:           deps.Store,
	}
}

// State returns the current loop state.
func (l *Loop) State() LoopState {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.state
}

// DoneReason returns why the loop terminated (empty if still running).
func (l *Loop) DoneReason() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.doneReason
}

// TurnCount returns the number of thinking turns elapsed.
func (l *Loop) TurnCount() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.turnCount
}

// maxLoopDurationFallback is used only when LoopConfig.MaxLoopDuration is not set.
const maxLoopDurationFallback = 300 * time.Second

// Run executes the ReAct loop until completion or context cancellation.
// It returns the final assistant response content and any error.
func (l *Loop) Run(ctx context.Context, session *Session) (string, error) {
	loopDuration := l.config.MaxLoopDuration
	if loopDuration <= 0 {
		loopDuration = maxLoopDurationFallback
	}
	deadline := time.Now().Add(loopDuration)
	log.Debug().Str("session", session.ID).Int("max_turns", l.config.MaxTurns).Msg("ReAct loop started")

	for {
		// Wall-clock deadline check (Rust: react_deadline).
		if time.Now().After(deadline) {
			l.terminate("wall-clock deadline exceeded")
			log.Warn().Str("session", session.ID).Dur("limit", loopDuration).Msg("ReAct loop hit wall-clock deadline")
			content := session.LastAssistantContent()
			if content == "" {
				content = "I stopped this turn after reaching the autonomy duration limit. Here's what I accomplished so far."
			}
			return content, nil
		}

		select {
		case <-ctx.Done():
			l.terminate("context cancelled")
			return "", ctx.Err()
		default:
		}

		l.mu.Lock()
		if l.state == StateDone {
			l.mu.Unlock()
			content := session.LastAssistantContent()
			// Failure synthesis: if LLM returned empty after tool execution,
			// synthesize a summary from tool results (Rust parity).
			if content == "" && l.turnCount > 1 {
				content = l.synthesizeFromToolResults(session)
			}
			return content, nil
		}
		state := l.state
		l.mu.Unlock()

		switch state {
		case StateThinking:
			// Instruction anti-fade: inject compact reminder after 8+ turns
			// to prevent system prompt instruction drift (Rust parity).
			if l.turnCount >= 8 && l.turnCount%4 == 0 {
				l.injectInstructionReminder(session)
			}

			action, err := l.think(ctx, session)
			if err != nil {
				// Hard max turn enforcement: return partial content + error.
				if errors.Is(err, ErrMaxTurns) {
					l.terminate("max turns exceeded")
					content := session.LastAssistantContent()
					if content == "" {
						content = "I stopped after reaching the maximum number of turns."
					}
					return content, err
				}
				l.terminate(fmt.Sprintf("thinking error: %v", err))
				return "", err
			}
			l.transition(action)

		case StateActing:
			action, err := l.act(ctx, session)
			if err != nil {
				log.Warn().Err(err).Str("session", session.ID).Msg("tool execution failed")
				// Tool failures are observed, not fatal.
				l.transition(ActionObserve)
				continue
			}
			l.transition(action)

		case StateObserving:
			l.transition(ActionPersist)

		case StatePersisting:
			l.persist(ctx, session)
			l.transition(ActionThink)

		case StateIdle:
			l.terminate("idle: no progress")
			return session.LastAssistantContent(), nil
		}
	}
}

// synthesizeFromToolResults creates a response summary when the LLM returns
// empty after tool execution. Matches Rust's fallback synthesis.
func (l *Loop) synthesizeFromToolResults(session *Session) string {
	msgs := session.Messages()
	var toolResults []string
	for i := len(msgs) - 1; i >= 0 && len(toolResults) < 5; i-- {
		if msgs[i].Role == "tool" {
			summary := msgs[i].Content
			if len(summary) > 200 {
				summary = summary[:200] + "..."
			}
			toolResults = append(toolResults, summary)
		}
	}
	if len(toolResults) == 0 {
		return "I completed the requested actions but wasn't able to generate a summary."
	}
	var sb strings.Builder
	sb.WriteString("Here's what I found:\n\n")
	for _, r := range toolResults {
		sb.WriteString("- " + r + "\n")
	}
	return sb.String()
}

// injectInstructionReminder adds a compact directive summary to prevent
// system prompt instruction fade in long conversations (Rust: build_instruction_reminder).
func (l *Loop) injectInstructionReminder(session *Session) {
	reminder := "[Instruction reminder: Stay focused on the user's request. " +
		"Use tools when needed. Be concise and direct. " +
		"If a task is complete, say so clearly.]"
	session.AddSystemMessage(reminder)
}

// think calls the LLM and interprets the response.
func (l *Loop) think(ctx context.Context, session *Session) (Action, error) {
	l.mu.Lock()
	prevState := l.state
	// Only increment turnCount on actual transitions TO thinking state,
	// not on every think() call — prevents 2-3x inflation (#41).
	if prevState == StateThinking {
		l.turnCount++
	}
	turn := l.turnCount
	maxTurns := l.config.MaxTurns
	l.mu.Unlock()

	// Hard max turn enforcement (#42): return error instead of just logging.
	if turn > maxTurns {
		log.Warn().Str("session", session.ID).Int("max_turns", maxTurns).Msg("ReAct loop hit max turn limit")
		return ActionFinish, core.NewError(ErrMaxTurns, fmt.Sprintf("exceeded max turns (%d)", maxTurns))
	}

	// Build context-aware request.
	req := l.context.BuildRequest(session)

	// v1.0.6: turn-by-turn instrumentation. Pre-v1.0.6 these log
	// lines existed at DEBUG level without content snippets, so an
	// operator watching live `roboticus serve` couldn't see WHICH
	// specific user message reached the model on a given turn.
	// That made the empty-prompt bug (where the user's request was
	// being silently dropped from the LLM context due to budget
	// underflow) invisible from the operator's perspective until the
	// behavioral soak surfaced it after months. Promoting these to
	// INFO with truncated content snippets means an operator
	// running serve can now grep "agent loop: send/recv" and
	// reconstruct the exact request/response of every turn — which
	// would have caught the empty-prompt bug on first reproduction.
	//
	// Snippet length cap: 200 chars on each end of the
	// representative content. Keeps log volume sane while
	// preserving enough text for operators to recognize the
	// scenario.
	lastUserSnippet := lastRoleSnippet(req.Messages, "user", 200)
	lastSystemSnippet := lastRoleSnippet(req.Messages, "system", 100)
	log.Info().
		Int("turn", turn).
		Str("session", session.ID).
		Int("tools_in_request", len(req.Tools)).
		Int("messages", len(req.Messages)).
		Str("model", req.Model).
		Str("last_user", lastUserSnippet).
		Str("last_system_prefix", lastSystemSnippet).
		Msg("agent loop: sending LLM request")

	resp, err := l.llm.Complete(ctx, req)
	if err != nil {
		return ActionFinish, core.WrapError(core.ErrLLM, "thinking failed", err)
	}

	// Sanitize model output: HMAC boundary verification + L4 injection scan.
	// Matches Rust's sanitize_model_output pipeline.
	resp.Content = SanitizeModelOutput(resp.Content, nil, l.injection)

	respSnippet := resp.Content
	if len(respSnippet) > 200 {
		respSnippet = respSnippet[:200] + "…"
	}
	log.Info().
		Int("turn", turn).
		Str("session", session.ID).
		Int("tool_calls", len(resp.ToolCalls)).
		Int("content_len", len(resp.Content)).
		Str("finish_reason", resp.FinishReason).
		Str("model", resp.Model).
		Str("content_preview", respSnippet).
		Msg("agent loop: LLM response received")

	placeholderOnly := isPlaceholderAssistantContent(resp.Content)
	promissoryOnly := isPromissoryAssistantContent(resp.Content)
	nonTerminalFiller := placeholderOnly || (promissoryOnly && isDirectExecutionTurn(session))
	route := routeForInferenceOutcome(req.Model, resp.Provider, resp.Model)
	toolResultsSeen := toolResultCount(session)
	if l.detectSameRouteNoProgress(route, resp, nonTerminalFiller, toolResultsSeen) {
		if obs := llm.InferenceObserverFromContext(ctx); obs != nil {
			obs.RecordEvent("loop_terminated", "error",
				"terminated same-route no-progress churn",
				"The framework stopped repeated same-route responses because they were not making progress.",
				map[string]any{
					"reason_code":       "same_route_no_progress",
					"route":             route,
					"response_skeleton": responseSkeleton(resp.Content, placeholderOnly),
					"tool_results_seen": toolResultsSeen,
					"repeat_threshold":  max(1, l.config.MaxSameRouteNoProgress),
				},
			)
			obs.SetSummaryField("termination_cause", "same_route_no_progress")
			obs.SetSummaryField("primary_diagnosis", "same_route_no_progress_churn")
		}
		l.terminate("loop terminated: same-route no-progress churn")
		if nonTerminalFiller {
			return ActionFinish, nil
		}
		session.AddAssistantMessage(resp.Content, nil)
		return ActionFinish, nil
	}
	if nonTerminalFiller {
		log.Warn().
			Str("session", session.ID).
			Int("turn", turn).
			Int("tool_calls", len(resp.ToolCalls)).
			Str("finish_reason", resp.FinishReason).
			Msg("suppressing non-terminal filler assistant content")
	}

	// Tool-call turns still need an assistant message in history for call
	// correlation, but placeholder scaffolding must not leak into the session.
	if len(resp.ToolCalls) > 0 {
		content := resp.Content
		if nonTerminalFiller {
			content = ""
		}
		session.AddAssistantMessage(content, resp.ToolCalls)
		return ActionAct, nil
	}

	// Final placeholder-only content is malformed model output; retry instead of
	// committing it to history or returning it to the user.
	if nonTerminalFiller {
		return ActionNoOp, nil
	}

	// Add assistant response to session history.
	session.AddAssistantMessage(resp.Content, nil)

	// No tool calls — this is a final response.
	return ActionFinish, nil
}

// act executes pending tool calls from the last LLM response.
func (l *Loop) act(ctx context.Context, session *Session) (Action, error) {
	pending := session.PendingToolCalls()
	if len(pending) == 0 {
		return ActionNoOp, nil
	}

	for _, tc := range pending {
		// Check loop detection.
		if l.detectLoop(tc.Function.Name, tc.Function.Arguments) {
			l.terminate("loop detected: repeated tool calls")
			return ActionFinish, nil
		}
		if l.shouldTerminateReadOnlyExploration(session, tc.Function.Name) {
			streak := l.currentReadOnlyExplorationStreak()
			if obs := llm.InferenceObserverFromContext(ctx); obs != nil {
				obs.RecordEvent("loop_terminated", "error",
					"terminated exploratory read-only tool churn",
					"The framework stopped repeated read-only exploration because the turn was not making execution progress.",
					map[string]any{
						"reason_code":         "exploratory_tool_churn",
						"tool_name":           tc.Function.Name,
						"operation_class":     string(agenttools.OperationClassForName(tc.Function.Name)),
						"exploration_streak":  streak,
						"repeat_threshold":    max(1, l.config.MaxReadOnlyExploration),
						"task_intent":         session.TaskIntent(),
						"task_planned_action": session.TaskPlannedAction(),
					},
				)
				obs.SetSummaryField("termination_cause", "exploratory_tool_churn")
				obs.SetSummaryField("primary_diagnosis", "exploratory_tool_churn")
			}
			l.terminate("loop terminated: exploratory read-only tool churn")
			session.AddAssistantMessage(
				"I stopped because this direct execution turn kept gathering context without taking action. The framework treated that as exploratory churn instead of continuing to spend tool calls.",
				nil,
			)
			return ActionFinish, nil
		}

		// Error dedup: suppress duplicate failed tool calls (Rust: should_suppress_duplicate).
		callKey := tc.Function.Name + ":" + tc.Function.Arguments
		l.mu.Lock()
		failCount := l.failedCalls[callKey]
		l.mu.Unlock()
		if failCount >= 2 {
			l.resetReadOnlyExploration()
			output := fmt.Sprintf("[Tool %s suppressed]: This call was already attempted and failed %d times", tc.Function.Name, failCount)
			session.AddToolResult(tc.ID, tc.Function.Name, output, true)
			l.recordToolExecution(ctx, ToolExecutionRecord{
				TurnID:     core.TurnIDFromCtx(ctx),
				ToolCallID: tc.ID,
				ToolName:   tc.Function.Name,
				Input:      tc.Function.Arguments,
				Output:     output,
				Status:     "suppressed",
			})
			log.Warn().Str("tool", tc.Function.Name).Int("failures", failCount).Msg("suppressing duplicate failed tool call")
			continue
		}
		if !toolAllowedForTurnSurface(session, tc.Function.Name) {
			l.resetReadOnlyExploration()
			output := fmt.Sprintf("[Tool %s rejected]: this tool was not on the selected tool surface for the current turn", tc.Function.Name)
			session.AddToolResult(tc.ID, tc.Function.Name, output, true)
			if obs := llm.InferenceObserverFromContext(ctx); obs != nil {
				obs.RecordEvent("tool_call_blocked", "error",
					"tool call blocked outside the selected tool surface",
					"The framework rejected a tool call because the model asked for a tool that was not on the selected tool surface for this turn.",
					map[string]any{
						"tool_call_id": tc.ID,
						"tool_name":    tc.Function.Name,
						"reason_code":  "tool_not_selected",
					},
				)
			}
			l.recordToolExecution(ctx, ToolExecutionRecord{
				TurnID:     core.TurnIDFromCtx(ctx),
				ToolCallID: tc.ID,
				ToolName:   tc.Function.Name,
				Input:      tc.Function.Arguments,
				Output:     output,
				Status:     "out_of_surface",
			})
			continue
		}
		// Execute the tool.
		tool := l.tools.Get(tc.Function.Name)
		if tool == nil {
			l.resetReadOnlyExploration()
			session.AddToolResult(tc.ID, tc.Function.Name, "unknown tool", true)
			l.recordToolExecution(ctx, ToolExecutionRecord{
				TurnID:     core.TurnIDFromCtx(ctx),
				ToolCallID: tc.ID,
				ToolName:   tc.Function.Name,
				Input:      tc.Function.Arguments,
				Output:     "unknown tool",
				Status:     "unknown_tool",
			})
			continue
		}

		tctx := &ToolContext{
			SessionID:              session.ID,
			AgentID:                session.AgentID,
			AgentName:              session.AgentName,
			Workspace:              session.Workspace,
			AllowedPaths:           session.AllowedPaths,
			ProtectedReadOnlyPaths: session.SourceArtifacts(),
			Channel:                session.Channel,
			Store:                  l.store,
		}

		normFactory := l.normalizer
		if normFactory == nil {
			normFactory = agenttools.NewNormalizationFactory()
		}
		callNorm := normFactory.NormalizeToolCall(agenttools.ToolCallNormalizationInput{
			ToolName:      tc.Function.Name,
			RawArguments:  tc.Function.Arguments,
			RequestModel:  "",
			ResponseModel: "",
			Provider:      "",
		})
		switch callNorm.Disposition {
		case agenttools.NormalizationQualifiedTransform:
			if obs := llm.InferenceObserverFromContext(ctx); obs != nil {
				obs.RecordEvent("tool_call_normalized", "warning",
					"tool-call arguments normalized before execution",
					"The framework repaired malformed tool-call arguments before executing the tool.",
					map[string]any{
						"tool_call_id":  tc.ID,
						"tool_name":     tc.Function.Name,
						"transformer":   callNorm.Transformer,
						"fidelity":      string(callNorm.Fidelity),
						"disposition":   string(callNorm.Disposition),
						"original_args": tc.Function.Arguments,
					},
				)
			}
		case agenttools.NormalizationTransformFailed, agenttools.NormalizationNoQualifiedTransformer:
			l.resetReadOnlyExploration()
			output := "[Tool " + tc.Function.Name + " rejected]: malformed structured arguments could not be normalized before execution"
			if callNorm.Reason != "" {
				output += " (" + callNorm.Reason + ")"
			}
			session.AddToolResult(tc.ID, tc.Function.Name, output, true)
			if obs := llm.InferenceObserverFromContext(ctx); obs != nil {
				obs.RecordEvent("tool_call_normalization_failed", "error",
					"tool-call arguments were rejected before execution",
					"The framework blocked a malformed tool call because it could not normalize the structured arguments safely.",
					map[string]any{
						"tool_call_id": tc.ID,
						"tool_name":    tc.Function.Name,
						"transformer":  callNorm.Transformer,
						"fidelity":     string(callNorm.Fidelity),
						"disposition":  string(callNorm.Disposition),
						"reason":       callNorm.Reason,
						"raw_args":     tc.Function.Arguments,
					},
				)
			}
			l.recordToolExecution(ctx, ToolExecutionRecord{
				TurnID:     core.TurnIDFromCtx(ctx),
				ToolCallID: tc.ID,
				ToolName:   tc.Function.Name,
				Input:      tc.Function.Arguments,
				Output:     output,
				Status:     "invalid_arguments",
			})
			continue
		}

		execArgs := callNorm.Arguments
		if execArgs == "" {
			execArgs = tc.Function.Arguments
		}
		replayFingerprint := agenttools.ReplayFingerprintForCall(tc.Function.Name, execArgs)
		l.mu.Lock()
		successCount := l.successfulCalls[replayFingerprint.Key]
		l.mu.Unlock()
		if historyCount := successfulReplayCount(session, tc.Function.Name, replayFingerprint); historyCount > successCount {
			successCount = historyCount
		}
		if successCount > 0 && agenttools.RequiresReplayProtection(tc.Function.Name) {
			output := fmt.Sprintf("[Tool %s suppressed]: duplicate replay of a successful side-effecting call was blocked to avoid repeating non-idempotent work", tc.Function.Name)
			session.AddToolResult(tc.ID, tc.Function.Name, output, false)
			replayReason := "a prior successful execution made the duplicate call replay-risky"
			if replayFingerprint.Resource != "" {
				replayReason = fmt.Sprintf("a prior successful execution already mutated %s in this turn", replayFingerprint.Resource)
			}
			if obs := llm.InferenceObserverFromContext(ctx); obs != nil {
				obs.IncrementSummaryCounter("replay_suppression_count", 1)
				obs.RecordEvent("tool_call_replay_suppressed", "warning",
					"duplicate side-effecting tool replay suppressed",
					"The framework blocked a repeated side-effecting tool call after an earlier success to avoid replaying non-idempotent work.",
					map[string]any{
						"tool_call_id":         tc.ID,
						"tool_name":            tc.Function.Name,
						"prior_success_count":  successCount,
						"requires_protection":  true,
						"replay_policy_source": "tool_semantics",
						"protected_resource":   replayFingerprint.Resource,
						"replay_fingerprint":   replayFingerprint.Key,
						"reason":               replayReason,
					},
				)
			}
			l.recordToolExecution(ctx, ToolExecutionRecord{
				TurnID:     core.TurnIDFromCtx(ctx),
				ToolCallID: tc.ID,
				ToolName:   tc.Function.Name,
				Input:      execArgs,
				Output:     output,
				Status:     "suppressed_replay",
			})
			log.Warn().
				Str("tool", tc.Function.Name).
				Str("protected_resource", replayFingerprint.Resource).
				Int("prior_successes", successCount).
				Msg("suppressing duplicate successful side-effecting tool call")
			continue
		}

		// Policy check.
		if l.policy != nil {
			decision := l.policy.EvaluateWithTools(&policy.ToolCallRequest{
				ToolName:  tc.Function.Name,
				Arguments: execArgs,
				Authority: session.Authority,
			}, l.tools)
			if decision.Denied() {
				l.resetReadOnlyExploration()
				result := fmt.Sprintf("Policy denied: %s", decision.Reason)
				session.AddToolResult(tc.ID, tc.Function.Name, result, true)
				l.recordToolExecution(ctx, ToolExecutionRecord{
					TurnID:     core.TurnIDFromCtx(ctx),
					ToolCallID: tc.ID,
					ToolName:   tc.Function.Name,
					Input:      execArgs,
					Output:     result,
					Status:     "denied",
				})
				log.Warn().Str("tool", tc.Function.Name).Str("reason", decision.Reason).Str("session", session.ID).Msg("tool call denied by policy")
				continue
			}
		}

		toolStart := time.Now()
		result, err := tool.Execute(ctx, execArgs, tctx)
		toolDuration := time.Since(toolStart).Milliseconds()

		if err != nil {
			l.resetReadOnlyExploration()
			// Record failure for error dedup.
			l.mu.Lock()
			l.failedCalls[callKey]++
			l.traceEntries = append(l.traceEntries, ReactTraceEntry{
				ToolName:      tc.Function.Name,
				DurationMs:    toolDuration,
				Success:       false,
				ResultSummary: truncate(err.Error(), 120),
				Source:        "builtin",
			})
			l.mu.Unlock()
			output := fmt.Sprintf("error: %v", err)
			session.AddToolResult(tc.ID, tc.Function.Name, output, true)
			l.recordToolExecution(ctx, ToolExecutionRecord{
				TurnID:     core.TurnIDFromCtx(ctx),
				ToolCallID: tc.ID,
				ToolName:   tc.Function.Name,
				Input:      execArgs,
				Output:     output,
				Status:     "error",
				DurationMs: toolDuration,
			})
			continue
		}

		resultNorm := normFactory.NormalizeToolResult(agenttools.ToolResultNormalizationInput{
			ToolName:      tc.Function.Name,
			Result:        result,
			RequestModel:  "",
			ResponseModel: "",
			Provider:      "",
		})
		if resultNorm.Disposition == agenttools.NormalizationQualifiedTransform {
			if obs := llm.InferenceObserverFromContext(ctx); obs != nil {
				obs.RecordEvent("tool_result_normalized", "warning",
					"tool result normalized before observation",
					"The framework normalized tool output before it was fed back into the model context.",
					map[string]any{
						"tool_call_id": tc.ID,
						"tool_name":    tc.Function.Name,
						"transformer":  resultNorm.Transformer,
						"fidelity":     string(resultNorm.Fidelity),
						"disposition":  string(resultNorm.Disposition),
					},
				)
			}
		}
		if resultNorm.Result != nil {
			result = resultNorm.Result
		}

		// Scan tool output for injection (L4).
		if l.injection != nil {
			score := l.injection.ScanOutput(result.Output)
			if score.IsBlocked() {
				log.Warn().Str("tool", tc.Function.Name).Float64("score", float64(score)).Msg("injection in tool output")
				result.Output = "[Tool output filtered: potential injection detected]"
			}
		}

		// Record in flight recorder.
		l.mu.Lock()
		l.traceEntries = append(l.traceEntries, ReactTraceEntry{
			ToolName:      tc.Function.Name,
			DurationMs:    toolDuration,
			Success:       true,
			ResultSummary: truncate(result.Output, 120),
			Source:        result.Source,
		})
		l.mu.Unlock()

		session.AddToolResultWithMetadata(tc.ID, tc.Function.Name, result.Output, result.Metadata, false)
		if agenttools.RequiresReplayProtection(tc.Function.Name) {
			successFingerprint := agenttools.ReplayFingerprintForResult(tc.Function.Name, execArgs, result.Metadata)
			key := successFingerprint.Key
			if key == "" {
				key = replayFingerprint.Key
			}
			l.mu.Lock()
			l.successfulCalls[key]++
			l.mu.Unlock()
		}
		l.noteToolOutcome(session, tc.Function.Name, result)
		l.recordToolExecution(ctx, ToolExecutionRecord{
			TurnID:     core.TurnIDFromCtx(ctx),
			ToolCallID: tc.ID,
			ToolName:   tc.Function.Name,
			Input:      execArgs,
			Output:     result.Output,
			Status:     "success",
			DurationMs: toolDuration,
		})
	}

	return ActionObserve, nil
}

func toolAllowedForTurnSurface(session *Session, toolName string) bool {
	if session == nil {
		return true
	}
	selected := session.SelectedToolDefs()
	if selected == nil {
		return true
	}
	normalized := strings.TrimSpace(toolName)
	if normalized == "" {
		return false
	}
	for _, def := range selected {
		if strings.EqualFold(strings.TrimSpace(def.Function.Name), normalized) {
			return true
		}
	}
	return false
}

// persist saves state after an observation cycle.
func (l *Loop) persist(ctx context.Context, session *Session) {
	if l.memory != nil {
		l.memory.IngestTurn(ctx, session)
	}
}

// transition moves the state machine based on the action taken.
func (l *Loop) transition(action Action) {
	l.mu.Lock()
	defer l.mu.Unlock()

	switch action {
	case ActionThink:
		l.noOpCount = 0
		l.state = StateThinking
	case ActionAct:
		l.noOpCount = 0
		l.state = StateActing
	case ActionObserve:
		l.noOpCount = 0
		l.state = StateObserving
	case ActionPersist:
		l.state = StatePersisting
	case ActionNoOp:
		l.noOpCount++
		if l.noOpCount >= l.config.IdleThreshold {
			l.state = StateIdle
		} else {
			l.state = StateThinking
		}
	case ActionFinish:
		l.state = StateDone
	}
}

// terminate forces the loop to Done with a reason.
func (l *Loop) terminate(reason string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.state = StateDone
	l.doneReason = reason
}

// ReactTrace returns the collected tool execution trace entries (flight recorder).
func (l *Loop) ReactTrace() []ReactTraceEntry {
	l.mu.Lock()
	defer l.mu.Unlock()
	entries := make([]ReactTraceEntry, len(l.traceEntries))
	copy(entries, l.traceEntries)
	return entries
}

// truncate limits a string to maxLen characters.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

func successfulReplayCount(session *Session, toolName string, fingerprint agenttools.ReplayFingerprint) int {
	if session == nil || fingerprint.Key == "" {
		return 0
	}
	count := 0
	for _, msg := range session.Messages() {
		if msg.Role != "tool" || msg.Name != toolName {
			continue
		}
		if strings.HasPrefix(msg.Content, "Error:") {
			continue
		}
		if proofFingerprint := agenttools.ReplayFingerprintForResult(toolName, "", msg.Metadata); proofFingerprint.Key == fingerprint.Key {
			count++
		}
	}
	return count
}

func isPlaceholderAssistantContent(content string) bool {
	trimmed := strings.ToLower(strings.TrimSpace(content))
	switch trimmed {
	case "[assistant message]", "assistant message", "[assistant]", "assistant",
		"[agent message]", "agent message", "[agent]", "agent":
		return true
	default:
		return false
	}
}

func isPromissoryAssistantContent(content string) bool {
	trimmed := strings.ToLower(strings.TrimSpace(content))
	switch {
	case trimmed == "":
		return false
	case strings.HasPrefix(trimmed, "let me check"),
		strings.HasPrefix(trimmed, "let me inspect"),
		strings.HasPrefix(trimmed, "let me look"),
		strings.HasPrefix(trimmed, "let me take a look"),
		strings.HasPrefix(trimmed, "i'll check"),
		strings.HasPrefix(trimmed, "i will check"),
		strings.HasPrefix(trimmed, "i'll inspect"),
		strings.HasPrefix(trimmed, "i will inspect"),
		strings.HasPrefix(trimmed, "i'll look"),
		strings.HasPrefix(trimmed, "i will look"),
		strings.HasPrefix(trimmed, "i'll take a look"),
		strings.HasPrefix(trimmed, "i will take a look"):
		return true
	default:
		return false
	}
}

func routeForInferenceOutcome(requestModel, provider, responseModel string) string {
	model := strings.TrimSpace(responseModel)
	if model == "" {
		model = strings.TrimSpace(requestModel)
	}
	provider = strings.TrimSpace(provider)
	switch {
	case provider != "" && model != "":
		return provider + "/" + model
	case model != "":
		return model
	default:
		return "unknown"
	}
}

func toolResultCount(session *Session) int {
	if session == nil {
		return 0
	}
	count := 0
	for _, msg := range session.Messages() {
		if msg.Role == "tool" {
			count++
		}
	}
	return count
}

func responseSkeleton(content string, placeholderOnly bool) string {
	if placeholderOnly {
		return "placeholder"
	}
	trimmed := strings.ToLower(strings.TrimSpace(content))
	if trimmed == "" {
		return "empty"
	}
	var b strings.Builder
	lastSpace := false
	for _, r := range trimmed {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			b.WriteRune(r)
			lastSpace = false
		case r == ' ' || r == '\n' || r == '\t' || r == '\r':
			if !lastSpace {
				b.WriteByte(' ')
				lastSpace = true
			}
		}
		if b.Len() >= 160 {
			break
		}
	}
	skeleton := strings.TrimSpace(b.String())
	if skeleton == "" {
		return "empty"
	}
	return skeleton
}

func (l *Loop) detectSameRouteNoProgress(route string, resp *llm.Response, placeholderOnly bool, toolResultsSeen int) bool {
	threshold := l.config.MaxSameRouteNoProgress
	if threshold <= 0 {
		return false
	}
	if resp == nil || len(resp.ToolCalls) > 0 {
		l.mu.Lock()
		l.lastNoProgress = recentInferenceOutcome{}
		l.mu.Unlock()
		return false
	}
	current := recentInferenceOutcome{
		route:           route,
		skeleton:        responseSkeleton(resp.Content, placeholderOnly),
		placeholderOnly: placeholderOnly,
		toolResultsSeen: toolResultsSeen,
		streak:          1,
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.lastNoProgress.route == current.route &&
		l.lastNoProgress.skeleton == current.skeleton &&
		l.lastNoProgress.toolResultsSeen == current.toolResultsSeen {
		current.streak = l.lastNoProgress.streak + 1
	}
	l.lastNoProgress = current
	return current.streak >= threshold
}

func (l *Loop) shouldTerminateReadOnlyExploration(session *Session, toolName string) bool {
	if !isDirectExecutionTurn(session) {
		return false
	}
	if !agenttools.IsReadOnlyExploration(toolName) {
		return false
	}
	threshold := l.config.MaxReadOnlyExploration
	if threshold <= 0 {
		return false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.readOnlyExplorationStreak >= threshold
}

func (l *Loop) noteToolOutcome(session *Session, toolName string, result *agenttools.Result) {
	l.mu.Lock()
	defer l.mu.Unlock()
	switch {
	case agenttools.MakesExecutionProgress(toolName):
		l.readOnlyExplorationStreak = 0
	case inspectionEvidenceCountsAsProgress(session, toolName, result):
		l.readOnlyExplorationStreak = 0
	case agenttools.IsReadOnlyExploration(toolName):
		l.readOnlyExplorationStreak++
	default:
		l.readOnlyExplorationStreak = 0
	}
}

func inspectionEvidenceCountsAsProgress(session *Session, toolName string, result *agenttools.Result) bool {
	if session == nil || result == nil {
		return false
	}
	if strings.TrimSpace(session.TurnToolProfile()) != "focused_inspection" {
		return false
	}
	switch agenttools.OperationClassForName(toolName) {
	case agenttools.OperationWorkspaceInspect:
		if proof, ok := agenttools.ParseInspectionProof(result.Metadata); ok {
			return proof.Count > 0 && !proof.Empty
		}
	case agenttools.OperationArtifactRead:
		if proof, ok := agenttools.ParseArtifactReadProof(result.Metadata); ok {
			return strings.TrimSpace(proof.Content) != ""
		}
	}
	return false
}

func (l *Loop) resetReadOnlyExploration() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.readOnlyExplorationStreak = 0
}

func (l *Loop) currentReadOnlyExplorationStreak() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.readOnlyExplorationStreak
}

func isDirectExecutionTurn(session *Session) bool {
	if session == nil {
		return false
	}
	if strings.TrimSpace(session.TaskPlannedAction()) != "execute_directly" {
		return false
	}
	switch strings.TrimSpace(session.TaskIntent()) {
	case "task", "code":
		return true
	default:
		return false
	}
}

// detectLoop checks if the same tool+params has been called repeatedly.
func (l *Loop) detectLoop(toolName, params string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	call := recentCall{tool: toolName, params: params}
	l.recentCalls = append(l.recentCalls, call)

	// Keep only the window.
	if len(l.recentCalls) > l.config.LoopWindow {
		l.recentCalls = l.recentCalls[len(l.recentCalls)-l.config.LoopWindow:]
	}

	// All entries in window must match for loop detection.
	if len(l.recentCalls) < l.config.LoopWindow {
		return false
	}

	first := l.recentCalls[0]
	for _, c := range l.recentCalls[1:] {
		if c.tool != first.tool || c.params != first.params {
			return false
		}
	}
	return true
}

func (l *Loop) recordToolExecution(ctx context.Context, rec ToolExecutionRecord) {
	if rec.ToolName == "" {
		return
	}
	if obs := llm.InferenceObserverFromContext(ctx); obs != nil {
		obs.IncrementSummaryCounter("tool_call_count", 1)
		obs.RecordEvent("tool_call_finished", rec.Status,
			"tool execution recorded",
			"A tool call finished for this turn.",
			map[string]any{
				"tool_call_id": rec.ToolCallID,
				"tool_name":    rec.ToolName,
				"status":       rec.Status,
				"duration_ms":  rec.DurationMs,
			},
		)
	}
	if l.recorder == nil {
		return
	}
	if err := l.recorder.RecordToolExecution(ctx, rec); err != nil {
		log.Warn().Err(err).Str("tool", rec.ToolName).Str("turn", rec.TurnID).Msg("failed to persist tool execution record")
	}
}
