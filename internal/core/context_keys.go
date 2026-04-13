package core

import "context"

type ctxKey int

const (
	ctxKeyModelOverride ctxKey = iota
	ctxKeySessionID
)

// WithModelOverride attaches a model override to the context.
// The LLM service reads this to force a specific model, bypassing the router.
func WithModelOverride(ctx context.Context, model string) context.Context {
	return context.WithValue(ctx, ctxKeyModelOverride, model)
}

// ModelOverrideFromCtx extracts the model override from the context (if set).
func ModelOverrideFromCtx(ctx context.Context) string {
	if v, ok := ctx.Value(ctxKeyModelOverride).(string); ok {
		return v
	}
	return ""
}

// WithSessionID attaches a session ID to the context for per-session tracking.
func WithSessionID(ctx context.Context, sessionID string) context.Context {
	return context.WithValue(ctx, ctxKeySessionID, sessionID)
}

// SessionIDFromCtx extracts the session ID from the context (if set).
func SessionIDFromCtx(ctx context.Context) string {
	if v, ok := ctx.Value(ctxKeySessionID).(string); ok {
		return v
	}
	return ""
}
