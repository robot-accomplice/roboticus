package pipeline

import (
	"context"
	"fmt"

	"roboticus/internal/core"
)

// RetryPolicy controls guard-triggered re-inference behavior.
type RetryPolicy struct {
	MaxRetries    int  // Maximum retry attempts (default 2)
	InjectReason  bool // Append guard rejection reason to next prompt
	PreserveChain bool // Carry forward rejected response as context
}

// DefaultRetryPolicy returns the standard retry policy.
func DefaultRetryPolicy() RetryPolicy {
	return RetryPolicy{
		MaxRetries:    2,
		InjectReason:  true,
		PreserveChain: true,
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
	totalTurns := 0
	var lastReason string

	for attempt := 0; attempt <= policy.MaxRetries; attempt++ {
		// Inject retry context if this is a retry.
		if attempt > 0 && policy.InjectReason && lastReason != "" {
			session.AddSystemMessage(fmt.Sprintf(
				"Your previous response was rejected: %s. Please revise your response.",
				lastReason,
			))
		}

		content, turns, err := executor.RunLoop(ctx, session)
		if err != nil {
			return "", totalTurns, err
		}
		totalTurns += turns

		// No guards or empty chain — return as-is.
		if guards == nil || guards.Len() == 0 {
			return content, totalTurns, nil
		}

		result := guards.ApplyFull(content)
		if !result.RetryRequested {
			return result.Content, totalTurns, nil
		}

		lastReason = result.RetryReason
	}

	return "", totalTurns, fmt.Errorf("%w: after %d attempts, last reason: %s",
		core.ErrGuardExhausted, policy.MaxRetries+1, lastReason)
}
