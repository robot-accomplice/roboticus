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

// executionModelSpec joins the selected outer execution provider with the
// downstream model namespace exactly once. This keeps routed execution,
// tracing, and request formatting on the same provider/model identity even
// when the downstream model name is itself provider-qualified, e.g.
// "openrouter/openai/gpt-4o-mini".
func executionModelSpec(provider, model string) string {
	provider = strings.TrimSpace(provider)
	model = strings.TrimSpace(model)
	if provider == "" {
		return model
	}
	if model == "" {
		return provider
	}
	prefix := provider + "/"
	if strings.HasPrefix(model, prefix) {
		return model
	}
	return provider + "/" + model
}

// modelIdentityKeys returns the ordered alias set that may refer to the same
// exercised model across routing, explicit benchmark specs, and direct-provider
// execution. This lets the evidence layer reconcile:
// - bare routed names like "gemma4"
// - direct provider-qualified specs like "openai/gpt-4o-mini"
// - nested execution-provider specs like "openrouter/openai/gpt-4o-mini"
//
// The first entries are the most routing-native identities; later entries are
// compatibility aliases for persisted evidence recorded before or outside the
// routed bare-name space.
func modelIdentityKeys(provider, model string) []string {
	add := func(items []string, value string) []string {
		value = strings.TrimSpace(value)
		if value == "" {
			return items
		}
		for _, existing := range items {
			if existing == value {
				return items
			}
		}
		return append(items, value)
	}

	var keys []string
	rawModel := strings.TrimSpace(model)
	rawSpec := strings.TrimSpace(executionModelSpec(provider, model))

	keys = add(keys, canonicalModelKey(rawModel))
	keys = add(keys, canonicalModelKey(rawSpec))
	keys = add(keys, rawModel)
	keys = add(keys, rawSpec)
	return keys
}
