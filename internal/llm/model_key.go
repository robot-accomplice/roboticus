package llm

import "strings"

// canonicalModelKey normalizes provider-qualified model specs to the bare model
// identifier used by RouteTarget.Model. This keeps routing telemetry,
// historical performance, and live selections on the same key space.
func canonicalModelKey(model string) string {
	model = strings.TrimSpace(model)
	if model == "" {
		return ""
	}
	// Only strip the first segment when the model is clearly provider-qualified,
	// e.g. "openrouter/openai/gpt-4o-mini". Single-slash model namespaces such as
	// "openai/gpt-4o-mini" are part of the model identity and must be preserved.
	if strings.Count(model, "/") >= 2 {
		if provider, bare := splitModelSpec(model); bare != "" && provider != "" {
			return bare
		}
	}
	return model
}

func historyModelKey(provider, model string) string {
	model = canonicalModelKey(model)
	provider = strings.TrimSpace(provider)
	if provider == "" || model == "" {
		return model
	}
	prefix := provider + "/"
	if strings.HasPrefix(model, prefix) {
		return strings.TrimPrefix(model, prefix)
	}
	return model
}
