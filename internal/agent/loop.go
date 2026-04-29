package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

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
	StateReflecting                  // Interpreting observed results without reopening tools
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
	case StateReflecting:
		return "reflecting"
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
	ActionReflect               // Interpret observed results without tools
	ActionNoOp                  // Nothing useful happened
	ActionFinish                // Terminate loop
)

const (
	reflectContinuePrefix = "CONTINUE_EXECUTION"
)

const reflectInstruction = "Post-observation reflection mode: tools are disabled. You are receiving a canonical TOTOF artifact: task, authoritative observed results, key tool outcomes, open issues, and a bounded finalization instruction. Interpret the artifact, finalize directly when the observed results are sufficient, and only if more execution is strictly required start the first line with CONTINUE_EXECUTION and then briefly explain what remains to be done."
const continuationInstruction = "Post-observation continuation mode: you are resuming execution from a canonical continuation artifact rather than raw session replay. Use the authoritative observations, tool outcomes, and explicit remaining-work summary to decide the next bounded execution step. Continue execution only as far as needed to close the named remaining work, then return to observation/reflection."

// continueSentinelSpan finds a discrete CONTINUE_EXECUTION control token in
// model output. The token may appear at the start of a line or inline after
// prose; either way it is framework control text, not user-visible prose.
func continueSentinelSpan(content string) (start int, end int, ok bool) {
	offset := 0
	for {
		idx := strings.Index(content[offset:], reflectContinuePrefix)
		if idx < 0 {
			return 0, 0, false
		}
		start = offset + idx
		end = start + len(reflectContinuePrefix)
		beforeOK := start == 0
		if !beforeOK {
			r, _ := utf8.DecodeLastRuneInString(content[:start])
			beforeOK = unicode.IsSpace(r) || unicode.IsPunct(r)
		}
		afterOK := end == len(content)
		if !afterOK {
			r, _ := utf8.DecodeRuneInString(content[end:])
			afterOK = unicode.IsSpace(r) || unicode.IsPunct(r)
		}
		if beforeOK && afterOK {
			return start, end, true
		}
		offset = end
	}
}

// detectContinueExecution scans response content for a discrete
// CONTINUE_EXECUTION control sentinel. When found, the function returns
// taken=true and the response remainder after the sentinel marker.
//
// Leading prose and the sentinel token are consumed by the framework as
// control text and must NEVER be persisted via session.AddAssistantMessage*.
// See R-AGENT-197 / R-AGENT-198 for the regression contract.
func detectContinueExecution(content string) (taken bool, remainder string) {
	if content == "" {
		return false, ""
	}
	_, end, ok := continueSentinelSpan(content)
	if !ok {
		return false, ""
	}
	return true, strings.TrimLeftFunc(content[end:], func(r rune) bool {
		return unicode.IsSpace(r) || unicode.IsPunct(r)
	})
}

// scrubControlSentinels removes any stray CONTINUE_EXECUTION sentinel and
// the line it appears on from content destined for user-visible
// persistence. This is a belt-and-braces guard so a future model behavior
// change cannot leak the framework control token into chat — even if the
// upstream line-aware detector is bypassed by a code path that does not
// route through reflect.
//
// Returning an empty string is acceptable: callers downstream of this
// function are expected to treat empty content as "nothing to persist"
// rather than "persist a blank assistant turn."
func scrubControlSentinels(content string) string {
	if !strings.Contains(content, reflectContinuePrefix) {
		return content
	}
	start, _, ok := continueSentinelSpan(content)
	if !ok {
		return content
	}
	log.Warn().
		Str("sentinel", reflectContinuePrefix).
		Int("content_len", len(content)).
		Msg("scrubbing CONTINUE_EXECUTION sentinel from user-visible assistant content; the control-token detector should have caught this earlier — investigate the upstream call path")
	return strings.TrimSpace(content[:start])
}

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

// Loop implements the live TEOR core inside the broader R-TEOR-R model:
// retrieval memory happens before the loop is entered, this loop owns
// Think -> Execute -> Observe -> Reflect, and retention memory happens after
// reflection decides what the turn actually proved.
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
	approvals  *policy.ApprovalManager
	injection  *InjectionDetector
	memory     *memory.Manager
	context    *ContextBuilder
	normalizer *agenttools.NormalizationFactory
	store      *db.Store
}

// LoopDeps bundles the dependencies for a Loop.
type LoopDeps struct {
	LLM      llm.Completer
	Tools    *ToolRegistry
	Recorder ToolCallRecorder
	Policy   *policy.Engine
	// Approvals classifies tool names against the operator-configured
	// blocked/gated lists before dispatch. nil means approval gating is
	// disabled (the loop falls through to the policy engine and the
	// registry as before). Wired in v1.0.8 to close the dead-code gap
	// where `policy.ApprovalManager.ClassifyTool` had no production
	// callers despite being instantiated by the daemon.
	Approvals   *policy.ApprovalManager
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
		approvals:       deps.Approvals,
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
			l.transition(ActionReflect)

		case StateReflecting:
			action, err := l.reflect(ctx, session)
			if err != nil {
				if errors.Is(err, ErrMaxTurns) {
					l.terminate("max turns exceeded")
					content := session.LastAssistantContent()
					return content, err
				}
				l.terminate(fmt.Sprintf("reflection error: %v", err))
				return "", err
			}
			l.transition(action)

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
		return session.LastAssistantContent()
	}
	var sb strings.Builder
	for _, r := range toolResults {
		if sb.Len() > 0 {
			sb.WriteString("\n")
		}
		sb.WriteString(r)
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
	if prevState == StateThinking || prevState == StateReflecting {
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
	//
	// Working-memory / trailing-overlay: when a continuation artifact is present,
	// we layer the continuation brief as a TRAILING SYSTEM OVERLAY on top
	// of the full session history rather than replacing history with a
	// synthetic 2-message scaffold. Previously the continuation path
	// called BuildRequestWithMessages(session, continuation.Messages()),
	// which dropped every prior user/assistant/tool turn for the
	// duration of the continuation think — the model lost the original
	// user task and was answering a synthetic re-extraction of it
	// instead of the real conversation.
	//
	// Architecture-rule anchor: docs/architecture-rules-diagrams.md
	// §6.7 "ReAct Reflection Memory Layering". TOTOF / continuation are
	// a tighter action-request lens layered on top of conversation
	// memory, never a replacement.
	continuation := session.ConsumeContinuationArtifact()
	var req *llm.Request
	if continuation != nil {
		overlay := []string{continuationInstruction, continuation.Render()}
		req = l.context.BuildRequestWithTrailingSystemOverlay(session, overlay)
	} else {
		req = l.context.BuildRequest(session)
	}

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
		session.AddAssistantMessageWithPhase(scrubControlSentinels(resp.Content), nil, "think")
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
		session.AddAssistantMessageWithPhase(scrubControlSentinels(content), resp.ToolCalls, "think")
		return ActionAct, nil
	}

	if promissoryOnly && toolResultsSeen > 0 {
		artifact := session.BuildContinuationArtifact(strings.TrimSpace(resp.Content), continuationInstruction)
		session.SetContinuationArtifact(&artifact)
		if obs := llm.InferenceObserverFromContext(ctx); obs != nil {
			obs.RecordEvent("think_continues_execution", "warning",
				"post-tool think returned promissory execution text",
				"The normal think phase described a future execution step after tool evidence instead of finalizing, so the framework treated it as continuation state.",
				map[string]any{
					"source": "promissory_think",
					"reason": strings.TrimSpace(resp.Content),
				},
			)
		}
		return ActionThink, nil
	}

	// Final placeholder-only content is malformed model output; retry instead of
	// committing it to history or returning it to the user.
	if nonTerminalFiller {
		return ActionNoOp, nil
	}

	// Add assistant response to session history.
	//
	// Defensive scrub: the line-aware detectContinueExecution lives in
	// the reflect phase, not here. If the model nevertheless emits the
	// sentinel during a normal think turn (e.g. a regression in prompt
	// shaping), scrubControlSentinels strips it before persistence so
	// the literal control token never reaches user-visible chat
	// (R-AGENT-198).
	session.AddAssistantMessageWithPhase(scrubControlSentinels(resp.Content), nil, "think")

	// No tool calls — this is a final response.
	return ActionFinish, nil
}

func (l *Loop) reflect(ctx context.Context, session *Session) (Action, error) {
	l.mu.Lock()
	prevState := l.state
	if prevState == StateReflecting {
		l.turnCount++
	}
	turn := l.turnCount
	maxTurns := l.config.MaxTurns
	l.mu.Unlock()

	if turn > maxTurns {
		log.Warn().Str("session", session.ID).Int("max_turns", maxTurns).Msg("ReAct loop hit max turn limit during reflection")
		return ActionFinish, core.NewError(ErrMaxTurns, fmt.Sprintf("exceeded max turns (%d)", maxTurns))
	}

	// Build the reflect request from full session history with the TOTOF
	// brief as a trailing system overlay. Previously reflect built a
	// synthetic 2-message scaffold from
	// totof.Messages(), which replaced history entirely and severed
	// conversation continuity for the duration of the reflect call.
	//
	// We clear req.Tools after construction because reflectInstruction
	// declares "tools are disabled" — the request must not advertise
	// tool definitions or the model can ignore that directive. This
	// matches prior reflect behavior (no Tools on the synthetic reflect
	// request) while preserving full conversation memory.
	//
	// Architecture-rule anchor: docs/architecture-rules-diagrams.md
	// §6.7 "ReAct Reflection Memory Layering".
	totof := session.BuildTOTOF(reflectInstruction)
	overlay := []string{reflectInstruction, totof.Render()}
	req := l.context.BuildRequestWithTrailingSystemOverlay(session, overlay)
	req.Tools = nil

	lastUserSnippet := lastRoleSnippet(req.Messages, "user", 200)
	log.Info().
		Int("turn", turn).
		Str("session", session.ID).
		Int("messages", len(req.Messages)).
		Int("totof_observations", len(totof.AuthoritativeObservedResult)).
		Int("totof_open_issues", len(totof.OpenIssues)).
		Str("model", req.Model).
		Str("last_user", lastUserSnippet).
		Msg("agent loop: sending reflection request")

	resp, err := l.llm.Complete(ctx, req)
	if err != nil {
		return ActionFinish, core.WrapError(core.ErrLLM, "reflection failed", err)
	}

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
		Msg("agent loop: reflection response received")

	trimmed := strings.TrimSpace(resp.Content)

	// Line-aware sentinel detection. The legacy detector used
	// strings.HasPrefix on the entire trimmed response, which failed when
	// the model emitted reasoning before the sentinel and leaked the
	// entire response (including the literal CONTINUE_EXECUTION token)
	// to user-visible chat. detectContinueExecution scans every line,
	// so prose-then-sentinel emissions are still classified correctly.
	//
	// When taken=true, the leading prose AND the sentinel itself are
	// consumed by the framework as control text and MUST NOT be persisted
	// via session.AddAssistantMessage*. Only the remainder (the model's
	// stated reason for continuing) is forwarded — and that, too, lives
	// only inside the continuation artifact, never the chat transcript.
	if taken, remainder := detectContinueExecution(resp.Content); taken {
		artifact := session.BuildContinuationArtifact(remainder, continuationInstruction)
		session.SetContinuationArtifact(&artifact)
		if obs := llm.InferenceObserverFromContext(ctx); obs != nil {
			obs.RecordEvent("reflection_continues_execution", "warning",
				"post-observation reflection requested more execution",
				"The framework finished one execution cycle, reflected on the observed results, and explicitly decided that more execution was still required.",
				map[string]any{
					"reason": remainder,
				},
			)
		}
		return ActionThink, nil
	}

	if len(resp.ToolCalls) > 0 {
		remainder := trimmed
		artifact := session.BuildContinuationArtifact(remainder, continuationInstruction)
		session.SetContinuationArtifact(&artifact)
		if obs := llm.InferenceObserverFromContext(ctx); obs != nil {
			obs.RecordEvent("reflection_continues_execution", "warning",
				"post-observation reflection requested more execution",
				"The post-observation reflection phase returned tool calls instead of a final answer, so the framework treated the response as an explicit request to continue execution.",
				map[string]any{
					"tool_call_count": len(resp.ToolCalls),
					"source":          "tool_calls",
					"reason":          remainder,
				},
			)
		}
		// Defensive scrub: the line-aware detector above already returned
		// false here, so the sentinel should not be present — but we
		// scrub regardless so a future model behavior change cannot leak
		// the framework control token into chat (R-AGENT-198).
		session.AddAssistantMessageWithPhase(scrubControlSentinels(resp.Content), resp.ToolCalls, "reflect")
		return ActionAct, nil
	}

	if isPromissoryAssistantContent(trimmed) && toolResultCount(session) > 0 {
		artifact := session.BuildContinuationArtifact(trimmed, continuationInstruction)
		session.SetContinuationArtifact(&artifact)
		if obs := llm.InferenceObserverFromContext(ctx); obs != nil {
			obs.RecordEvent("reflection_continues_execution", "warning",
				"post-observation reflection returned promissory execution text",
				"The post-observation reflection phase described a future execution step instead of finalizing from evidence, so the framework treated it as continuation state.",
				map[string]any{
					"source": "promissory_reflection",
					"reason": trimmed,
				},
			)
		}
		return ActionThink, nil
	}

	if trimmed == "" {
		if toolResultCount(session) > 0 {
			content := strings.TrimSpace(l.synthesizeFromToolResults(session))
			if content != "" {
				session.AddAssistantMessageWithPhase(content, nil, "reflect")
			}
		}
		return ActionFinish, nil
	}

	session.AddAssistantMessageWithPhase(scrubControlSentinels(resp.Content), nil, "reflect")
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
			PathAnchor:             singleInspectionRoot(session),
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

		// Approval gating. Blocked tools are rejected outright; gated
		// tools currently log a warning and proceed (the human-approval
		// flow is owned by the API/UX layer in pipeline). This is the
		// single authoritative call site for `policy.ApprovalManager.
		// ClassifyTool` — the loop must consult the manager before the
		// policy engine so blocked tools never reach the registry, and
		// before recording a "denied" outcome that mixes blocked with
		// policy-denied semantics.
		if l.approvals != nil {
			switch l.approvals.ClassifyTool(tc.Function.Name) {
			case policy.ToolBlocked:
				l.resetReadOnlyExploration()
				output := policy.FormatBlockedToolResult(tc.Function.Name)
				session.AddToolResult(tc.ID, tc.Function.Name, output, true)
				if obs := llm.InferenceObserverFromContext(ctx); obs != nil {
					obs.RecordEvent("tool_call_blocked_by_approval", "error",
						"tool call blocked by approval policy",
						"The framework rejected a tool call because the operator listed the tool under approvals.blocked_tools.",
						map[string]any{
							"tool_call_id": tc.ID,
							"tool_name":    tc.Function.Name,
							"reason_code":  "tool_blocked",
						},
					)
				}
				l.recordToolExecution(ctx, ToolExecutionRecord{
					TurnID:     core.TurnIDFromCtx(ctx),
					ToolCallID: tc.ID,
					ToolName:   tc.Function.Name,
					Input:      execArgs,
					Output:     output,
					Status:     "blocked",
				})
				log.Warn().Str("tool", tc.Function.Name).Str("session", session.ID).Msg("tool call blocked by approval classification")
				continue
			case policy.ToolGated:
				if obs := llm.InferenceObserverFromContext(ctx); obs != nil {
					obs.RecordEvent("tool_call_gated", "warning",
						"gated tool executed without explicit approval",
						"The framework executed a tool that was configured as gated. Until human-in-the-loop approvals are wired through the API layer, gated tools proceed with a warning so the operator sees the call in audit logs.",
						map[string]any{
							"tool_call_id": tc.ID,
							"tool_name":    tc.Function.Name,
							"reason_code":  "tool_gated",
						},
					)
				}
				log.Warn().Str("tool", tc.Function.Name).Str("session", session.ID).Msg("gated tool executed without explicit approval flow")
			}
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
				result := policy.FormatDeniedToolResult(tc.Function.Name, decision, session.Authority, session.SecurityClaim)
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

func singleInspectionRoot(session *Session) string {
	if session == nil {
		return ""
	}
	roots := session.InspectionTargetRoots()
	if len(roots) != 1 {
		return ""
	}
	return strings.TrimSpace(roots[0])
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
	case ActionReflect:
		l.noOpCount = 0
		l.state = StateReflecting
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
		strings.HasPrefix(trimmed, "next, i will"),
		strings.HasPrefix(trimmed, "next i will"),
		strings.HasPrefix(trimmed, "i will now"),
		strings.HasPrefix(trimmed, "i'll now"),
		strings.HasPrefix(trimmed, "i will proceed"),
		strings.HasPrefix(trimmed, "i'll proceed"),
		strings.HasPrefix(trimmed, "i will continue"),
		strings.HasPrefix(trimmed, "i'll continue"),
		strings.HasPrefix(trimmed, "i'll check"),
		strings.HasPrefix(trimmed, "i will check"),
		strings.HasPrefix(trimmed, "i'll inspect"),
		strings.HasPrefix(trimmed, "i will inspect"),
		strings.HasPrefix(trimmed, "i'll look"),
		strings.HasPrefix(trimmed, "i will look"),
		strings.HasPrefix(trimmed, "i'll take a look"),
		strings.HasPrefix(trimmed, "i will take a look"),
		strings.Contains(trimmed, "\nnext, i will"),
		strings.Contains(trimmed, "\nnext i will"),
		strings.Contains(trimmed, "\ni will continue"),
		strings.Contains(trimmed, "\ni'll continue"),
		strings.Contains(trimmed, "\ni will proceed"),
		strings.Contains(trimmed, "\ni'll proceed"),
		strings.Contains(trimmed, " i will proceed"),
		strings.Contains(trimmed, " i'll proceed"),
		strings.Contains(trimmed, " i will now proceed"),
		strings.Contains(trimmed, " i'll now proceed"),
		strings.Contains(trimmed, " i will now use"),
		strings.Contains(trimmed, " i'll now use"),
		strings.Contains(trimmed, " i will now continue"),
		strings.Contains(trimmed, " i'll now continue"),
		strings.Contains(trimmed, "continuing execution"),
		strings.Contains(trimmed, "executing the next step"):
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
	if !turnProfileCountsReadOnlyEvidenceAsProgress(session.TurnToolProfile()) {
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

func turnProfileCountsReadOnlyEvidenceAsProgress(profile string) bool {
	switch strings.TrimSpace(profile) {
	case "focused_inspection", "focused_analysis_authoring", "focused_source_code":
		return true
	default:
		return false
	}
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
