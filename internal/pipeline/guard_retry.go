package pipeline

import (
	"context"
	"fmt"
	"strings"

	"roboticus/internal/core"
)

// RetryPolicy controls guard-triggered re-inference behavior.
type RetryPolicy struct {
	MaxRetries     int  // Maximum retry attempts (default 2)
	InjectReason   bool // Append guard rejection reason to next prompt
	PreserveChain  bool // Carry forward rejected response as context
	ErrorOnExhaust bool // Return ErrGuardExhausted when last attempt still asks for retry
}

// DefaultRetryPolicy returns the standard retry policy.
func DefaultRetryPolicy() RetryPolicy {
	return RetryPolicy{
		MaxRetries:     2,
		InjectReason:   true,
		PreserveChain:  true,
		ErrorOnExhaust: true,
	}
}

// RetryDirective provides per-guard retry instructions (Wave 8, #73-74).
type RetryDirective struct {
	GuardName   string // Name of the guard that triggered the retry
	TokenBudget int    // Max tokens for the retry attempt (0 = use default)
	Instruction string // Specific instruction appended to the retry prompt
}

// guardRetryDirectives maps guard names to their specific retry directives.
var guardRetryDirectives = map[string]RetryDirective{
	"literary_quote_retry": {
		GuardName:   "literary_quote_retry",
		TokenBudget: 1024,
		Instruction: "Do not narrate or quote literary passages. Provide direct factual information instead.",
	},
	"perspective": {
		GuardName:   "perspective",
		TokenBudget: 0,
		Instruction: "Do not narrate user actions in first person. Respond from your own perspective only.",
	},
	"non_repetition_v2": {
		GuardName:   "non_repetition_v2",
		TokenBudget: 0,
		Instruction: "Your previous response was too similar to an earlier message. Provide fresh, original content.",
	},
	"execution_truth": {
		GuardName:   "execution_truth",
		TokenBudget: 0,
		Instruction: "Only claim tool execution if you actually called a tool. Check your tool results.",
	},
	"financial_action_truth": {
		GuardName:   "financial_action_truth",
		TokenBudget: 512,
		Instruction: "Do not claim financial actions unless a financial tool was executed and returned results.",
	},
	"action_verification": {
		GuardName:   "action_verification",
		TokenBudget: 512,
		Instruction: "Cross-reference your financial claims against actual tool execution results.",
	},
	"clarification_deflection": {
		GuardName:   "clarification_deflection",
		TokenBudget: 0,
		Instruction: "Do not ask the user to restate context that is already present in the conversation. Answer directly from the available context unless a truly missing detail blocks progress.",
	},
	"task_deferral": {
		GuardName:   "task_deferral",
		TokenBudget: 0,
		Instruction: "Perform the requested action with the selected tools now. If execution is genuinely blocked, report the exact tool, policy, sandbox, or provider result that blocked it. Do not ask for confirmation unless a required input is actually missing.",
	},
	"output_contract": {
		GuardName:   "output_contract",
		TokenBudget: 0,
		Instruction: "Return the requested output shape exactly. Do not add preface, explanation, bullets, or extra sentences unless the user requested them.",
	},
}

// GetRetryDirective returns the retry directive for a guard, or nil if none exists.
func GetRetryDirective(guardName string) *RetryDirective {
	if d, ok := guardRetryDirectives[guardName]; ok {
		return &d
	}
	return nil
}

// retryWithGuardsResume runs the guard chain starting at resumeIndex,
// skipping guards that have already been checked in a prior pass.
func retryWithGuardsResume(
	guards *GuardChain,
	content string,
	ctx *GuardContext,
	resumeIndex int,
) ApplyResult {
	result := ApplyResult{Content: content}
	for i := resumeIndex; i < len(guards.guards); i++ {
		g := guards.guards[i]
		var gr GuardResult
		if cg, ok := g.(ContextualGuard); ok && ctx != nil {
			gr = cg.CheckWithContext(result.Content, ctx)
		} else {
			gr = g.Check(result.Content)
		}
		if !gr.Passed {
			result.Violations = append(result.Violations, g.Name())
			result.ContractEvents = append(result.ContractEvents, buildGuardContractEvent(g.Name(), gr))
			if gr.Blocked || gr.Verdict == GuardBlocked {
				result.Blocked = true
				result.BlockReason = gr.Reason
				result.Content = ""
				return result
			}
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

// retryWithGuards runs inference through the executor and guard chain,
// retrying on guard rejection up to MaxRetries times.
func retryWithGuards(
	ctx context.Context,
	executor ToolExecutor,
	session *Session,
	guards *GuardChain,
	policy RetryPolicy,
) (string, int, error) {
	result, err := retryWithGuardsDetailed(ctx, executor, session, guards, policy, nil, nil)
	if err != nil {
		return "", result.Turns, err
	}
	return result.Content, result.Turns, nil
}

// GuardRetryRun is the full guard/inference retry outcome used by the live
// pipeline path. It preserves the exact initial and final guard results so the
// caller can annotate traces and persist the applied outcome without re-running
// the guard chain out of band.
type GuardRetryRun struct {
	Content             string
	Turns               int
	GuardRetried        bool
	RetrySuppressed     bool
	RetrySuppressReason string
	InitialGuardResult  ApplyResult
	FinalGuardResult    ApplyResult
}

// retryWithGuardsDetailed is the authoritative guard-triggered retry
// implementation for the live inference path.
func retryWithGuardsDetailed(
	ctx context.Context,
	executor ToolExecutor,
	session *Session,
	guards *GuardChain,
	policy RetryPolicy,
	buildGuardContext func() *GuardContext,
	decideRetry func(ApplyResult, *GuardContext) RetryDisposition,
) (GuardRetryRun, error) {
	run := GuardRetryRun{}
	var lastReason string
	var lastViolations []string

	for attempt := 0; attempt <= policy.MaxRetries; attempt++ {
		if attempt > 0 && policy.InjectReason && lastReason != "" {
			prefix := "Your previous response was rejected"
			if len(lastViolations) > 0 {
				prefix = fmt.Sprintf("%s by the %s guard", prefix, strings.Join(lastViolations, ", "))
			}
			instruction := retryInstructionForViolations(lastViolations)
			session.AddSystemMessage(fmt.Sprintf("%s: %s. %s", prefix, lastReason, instruction))
		}

		content, turns, err := executor.RunLoop(ctx, session)
		run.Turns += turns
		if err != nil {
			return run, err
		}

		var guardCtx *GuardContext
		if buildGuardContext != nil {
			guardCtx = buildGuardContext()
		}
		guardResult := applyGuardChainWithOptionalContext(guards, content, guardCtx)
		if attempt == 0 {
			run.InitialGuardResult = guardResult
		}
		run.FinalGuardResult = guardResult
		run.Content = guardResult.Content

		if guardResult.Blocked {
			return run, core.NewError(core.ErrPolicy, guardResult.BlockReason)
		}
		if guards == nil || guards.Len() == 0 || !guardResult.RetryRequested {
			return run, nil
		}
		if decideRetry != nil {
			disposition := decideRetry(guardResult, guardCtx)
			if !disposition.Allow {
				run.RetrySuppressed = true
				run.RetrySuppressReason = disposition.Reason
				return run, nil
			}
		}

		run.GuardRetried = true
		lastReason = guardResult.RetryReason
		lastViolations = append([]string(nil), guardResult.Violations...)
	}

	if !policy.ErrorOnExhaust {
		return run, nil
	}
	return run, fmt.Errorf("%w: after %d attempts, last reason: %s",
		core.ErrGuardExhausted, policy.MaxRetries+1, lastReason)
}

func retryInstructionForViolations(violations []string) string {
	for _, violation := range violations {
		name := strings.TrimSpace(strings.SplitN(violation, ":", 2)[0])
		if directive := GetRetryDirective(name); directive != nil && strings.TrimSpace(directive.Instruction) != "" {
			return directive.Instruction
		}
	}
	return "Please revise."
}

func applyGuardChainWithOptionalContext(guards *GuardChain, content string, guardCtx *GuardContext) ApplyResult {
	if guards == nil || guards.Len() == 0 {
		return ApplyResult{Content: content}
	}
	if guardCtx != nil {
		return guards.ApplyFullWithContext(content, guardCtx)
	}
	return guards.ApplyFull(content)
}
