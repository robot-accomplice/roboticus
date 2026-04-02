package pipeline

import (
	"context"
	"fmt"

	"goboticus/internal/core"
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
