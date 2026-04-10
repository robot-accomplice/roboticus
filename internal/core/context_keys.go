package core

import "context"

type ctxKey int

const ctxKeyModelOverride ctxKey = iota

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
