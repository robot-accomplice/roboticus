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
	"roboticus/internal/core"
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
	MaxTurns        int           // Maximum thinking iterations before forced stop
	IdleThreshold   int           // Consecutive NoOps before entering Idle state
	LoopWindow      int           // Sliding window size for loop detection
	MaxLoopDuration time.Duration // Wall-clock deadline for entire ReAct loop
}

// DefaultLoopConfig returns sensible defaults.
func DefaultLoopConfig() LoopConfig {
	return LoopConfig{
		MaxTurns:        25,
		MaxLoopDuration: 300 * time.Second, // 5 min — local models need 60-80s per call
		IdleThreshold:   3,
		LoopWindow:      3,
	}
}

// recentCall tracks a tool invocation for loop detection.
type recentCall struct {
	tool   string
	params string
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

	turnCount    int
	noOpCount    int
	recentCalls  []recentCall
	failedCalls  map[string]int    // tool+params → failure count (error dedup)
	traceEntries []ReactTraceEntry // flight recorder
	doneReason   string

	// Dependencies injected at construction.
	llm       llm.Completer
	tools     *ToolRegistry
	policy    *policy.Engine
	injection *InjectionDetector
	memory    *memory.Manager
	context   *ContextBuilder
}

// LoopDeps bundles the dependencies for a Loop.
type LoopDeps struct {
	LLM       llm.Completer
	Tools     *ToolRegistry
	Policy    *policy.Engine
	Injection *InjectionDetector
	Memory    *memory.Manager
	Context   *ContextBuilder
}

// NewLoop creates a ReAct loop with the given config and dependencies.
func NewLoop(cfg LoopConfig, deps LoopDeps) *Loop {
	return &Loop{
		config:      cfg,
		state:       StateThinking,
		failedCalls: make(map[string]int),
		llm:         deps.LLM,
		tools:       deps.Tools,
		policy:      deps.Policy,
		injection:   deps.Injection,
		memory:      deps.Memory,
		context:     deps.Context,
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
	if placeholderOnly {
		log.Warn().
			Str("session", session.ID).
			Int("turn", turn).
			Int("tool_calls", len(resp.ToolCalls)).
			Str("finish_reason", resp.FinishReason).
			Msg("suppressing placeholder assistant content")
	}

	// Tool-call turns still need an assistant message in history for call
	// correlation, but placeholder scaffolding must not leak into the session.
	if len(resp.ToolCalls) > 0 {
		content := resp.Content
		if placeholderOnly {
			content = ""
		}
		session.AddAssistantMessage(content, resp.ToolCalls)
		return ActionAct, nil
	}

	// Final placeholder-only content is malformed model output; retry instead of
	// committing it to history or returning it to the user.
	if placeholderOnly {
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

		// Error dedup: suppress duplicate failed tool calls (Rust: should_suppress_duplicate).
		callKey := tc.Function.Name + ":" + tc.Function.Arguments
		l.mu.Lock()
		failCount := l.failedCalls[callKey]
		l.mu.Unlock()
		if failCount >= 2 {
			session.AddToolResult(tc.ID, tc.Function.Name,
				fmt.Sprintf("[Tool %s suppressed]: This call was already attempted and failed %d times", tc.Function.Name, failCount), true)
			log.Warn().Str("tool", tc.Function.Name).Int("failures", failCount).Msg("suppressing duplicate failed tool call")
			continue
		}

		// Policy check.
		if l.policy != nil {
			decision := l.policy.EvaluateWithTools(&policy.ToolCallRequest{
				ToolName:  tc.Function.Name,
				Arguments: tc.Function.Arguments,
				Authority: session.Authority,
			}, l.tools)
			if decision.Denied() {
				result := fmt.Sprintf("Policy denied: %s", decision.Reason)
				session.AddToolResult(tc.ID, tc.Function.Name, result, true)
				log.Warn().Str("tool", tc.Function.Name).Str("reason", decision.Reason).Str("session", session.ID).Msg("tool call denied by policy")
				continue
			}
		}

		// Execute the tool.
		tool := l.tools.Get(tc.Function.Name)
		if tool == nil {
			session.AddToolResult(tc.ID, tc.Function.Name, "unknown tool", true)
			continue
		}

		tctx := &ToolContext{
			SessionID:    session.ID,
			AgentID:      session.AgentID,
			AgentName:    session.AgentName,
			Workspace:    session.Workspace,
			AllowedPaths: session.AllowedPaths,
			Channel:      session.Channel,
		}

		toolStart := time.Now()
		result, err := tool.Execute(ctx, tc.Function.Arguments, tctx)
		toolDuration := time.Since(toolStart).Milliseconds()

		if err != nil {
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
			session.AddToolResult(tc.ID, tc.Function.Name, fmt.Sprintf("error: %v", err), true)
			continue
		}

		// Filter tool output (Rust: AnsiStripper + ProgressLineFilter + DuplicateLineDeduper + WhitespaceNormalizer).
		result.Output = FilterToolOutput(result.Output)

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

		session.AddToolResult(tc.ID, tc.Function.Name, result.Output, false)
	}

	return ActionObserve, nil
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
