package core

import "context"

type ctxKey int

const (
	ctxKeyModelOverride ctxKey = iota
	ctxKeySessionID
	ctxKeyTurnID
	ctxKeyChannelLabel
	ctxKeyNoEscalate
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

// WithTurnID attaches a turn ID to the context for per-turn observability.
func WithTurnID(ctx context.Context, turnID string) context.Context {
	return context.WithValue(ctx, ctxKeyTurnID, turnID)
}

// TurnIDFromCtx extracts the turn ID from the context (if set).
func TurnIDFromCtx(ctx context.Context) string {
	if v, ok := ctx.Value(ctxKeyTurnID).(string); ok {
		return v
	}
	return ""
}

// WithChannelLabel attaches the channel label to the context for routing/event persistence.
func WithChannelLabel(ctx context.Context, channel string) context.Context {
	return context.WithValue(ctx, ctxKeyChannelLabel, channel)
}

// ChannelLabelFromCtx extracts the channel label from the context (if set).
func ChannelLabelFromCtx(ctx context.Context) string {
	if v, ok := ctx.Value(ctxKeyChannelLabel).(string); ok {
		return v
	}
	return ""
}

// WithNoEscalate marks the request context as a no-escalate/no-fallback path.
func WithNoEscalate(ctx context.Context) context.Context {
	return context.WithValue(ctx, ctxKeyNoEscalate, true)
}

// NoEscalateFromCtx reports whether the context carries no-escalate semantics.
func NoEscalateFromCtx(ctx context.Context) bool {
	if v, ok := ctx.Value(ctxKeyNoEscalate).(bool); ok {
		return v
	}
	return false
}
