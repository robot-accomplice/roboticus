package agent

import (
	"context"
	"fmt"
	"sync"

	"github.com/rs/zerolog/log"

	"roboticus/internal/agent/memory"
	"roboticus/internal/agent/policy"
	"roboticus/internal/core"
	"roboticus/internal/llm"
)

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
	MaxTurns      int // Maximum thinking iterations before forced stop
	IdleThreshold int // Consecutive NoOps before entering Idle state
	LoopWindow    int // Sliding window size for loop detection
}

// DefaultLoopConfig returns sensible defaults.
func DefaultLoopConfig() LoopConfig {
	return LoopConfig{
		MaxTurns:      25,
		IdleThreshold: 3,
		LoopWindow:    3,
	}
}

// recentCall tracks a tool invocation for loop detection.
type recentCall struct {
	tool   string
	params string
}

// Loop implements the ReAct state machine. It coordinates the agent's
// think → act → observe cycle with loop/idle detection and turn limits.
type Loop struct {
	mu     sync.Mutex
	config LoopConfig
	state  LoopState

	turnCount   int
	noOpCount   int
	recentCalls []recentCall
	doneReason  string

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
		config:    cfg,
		state:     StateThinking,
		llm:       deps.LLM,
		tools:     deps.Tools,
		policy:    deps.Policy,
		injection: deps.Injection,
		memory:    deps.Memory,
		context:   deps.Context,
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

// Run executes the ReAct loop until completion or context cancellation.
// It returns the final assistant response content and any error.
func (l *Loop) Run(ctx context.Context, session *Session) (string, error) {
	for {
		select {
		case <-ctx.Done():
			l.terminate("context cancelled")
			return "", ctx.Err()
		default:
		}

		l.mu.Lock()
		if l.state == StateDone {
			l.mu.Unlock()
			return session.LastAssistantContent(), nil
		}
		state := l.state
		l.mu.Unlock()

		switch state {
		case StateThinking:
			action, err := l.think(ctx, session)
			if err != nil {
				l.terminate(fmt.Sprintf("thinking error: %v", err))
				return "", err
			}
			l.transition(action)

		case StateActing:
			action, err := l.act(ctx, session)
			if err != nil {
				log.Warn().Err(err).Msg("tool execution failed")
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

// think calls the LLM and interprets the response.
func (l *Loop) think(ctx context.Context, session *Session) (Action, error) {
	l.mu.Lock()
	l.turnCount++
	turn := l.turnCount
	maxTurns := l.config.MaxTurns
	l.mu.Unlock()

	if turn > maxTurns {
		return ActionFinish, nil
	}

	// Build context-aware request.
	req := l.context.BuildRequest(session)

	resp, err := l.llm.Complete(ctx, req)
	if err != nil {
		return ActionFinish, core.WrapError(core.ErrLLM, "thinking failed", err)
	}

	// Scan output for injection attempts (L4 defense).
	if l.injection != nil {
		score := l.injection.ScanOutput(resp.Content)
		if score.IsBlocked() {
			log.Warn().Float64("score", float64(score)).Msg("injection detected in LLM output")
			resp.Content = "[Response filtered: potential injection detected]"
		}
	}

	// Add assistant response to session history.
	session.AddAssistantMessage(resp.Content, resp.ToolCalls)

	// If tool calls present, move to acting.
	if len(resp.ToolCalls) > 0 {
		return ActionAct, nil
	}

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
				log.Warn().Str("tool", tc.Function.Name).Str("reason", decision.Reason).Msg("tool call denied by policy")
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

		result, err := tool.Execute(ctx, tc.Function.Arguments, tctx)
		if err != nil {
			session.AddToolResult(tc.ID, tc.Function.Name, fmt.Sprintf("error: %v", err), true)
			continue
		}

		// Scan tool output for injection (L4).
		if l.injection != nil {
			score := l.injection.ScanOutput(result.Output)
			if score.IsBlocked() {
				log.Warn().Str("tool", tc.Function.Name).Float64("score", float64(score)).Msg("injection in tool output")
				result.Output = "[Tool output filtered: potential injection detected]"
			}
		}

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
