package llm

import (
	"strings"
	"time"

	"roboticus/internal/core"
)

const (
	exerciseCloudTimeout = 120 * time.Second
	exerciseLocalTimeout = 300 * time.Second
)

// ExerciseModelIsLocal resolves whether exercise/baseline should treat a model
// as local-hosted for warm-up and timeout policy. Unknown providers default to
// local-style handling so baseline runs do not silently skip warm-up on models
// that are likely running on the operator's machine.
func ExerciseModelIsLocal(cfg *core.Config, model string) bool {
	if cfg == nil {
		return true
	}
	provider := exerciseModelProvider(model)
	if provider == "" {
		return true
	}
	prov, ok := cfg.Providers[provider]
	if !ok {
		return true
	}
	return prov.IsLocal
}

// ExerciseModelTimeout resolves the per-call timeout for exercise/baseline
// runs. Model-specific overrides win; otherwise local models get the longer
// cold-start-tolerant timeout and cloud models get the shorter default.
func ExerciseModelTimeout(cfg *core.Config, model string) time.Duration {
	if cfg != nil {
		if override, ok := cfg.Models.ModelOverrides[model]; ok && override.TimeoutSecs > 0 {
			return time.Duration(override.TimeoutSecs) * time.Second
		}
		if timeout := cfg.Models.ResolveModelTimeout(model); timeout > 0 {
			return time.Duration(timeout) * time.Second
		}
	}
	if ExerciseModelIsLocal(cfg, model) {
		return exerciseLocalTimeout
	}
	return exerciseCloudTimeout
}

// ExercisePromptTimeout resolves the authoritative execution budget for one
// exercise row. The per-model timeout remains the base policy, but complex
// TEOR rows need a longer server-side budget than trivial/direct rows.
func ExercisePromptTimeout(base time.Duration, prompt ExercisePrompt) time.Duration {
	if base <= 0 {
		base = exerciseCloudTimeout
	}

	switch prompt.Complexity {
	case ComplexityTrivial:
		if base < 90*time.Second {
			return base
		}
		return 90 * time.Second
	case ComplexitySimple:
		return base
	case ComplexityModerate:
		return scaleDuration(base, 3, 2)
	case ComplexityComplex:
		return scaleDuration(base, 2, 1)
	case ComplexityExpert:
		return scaleDuration(base, 5, 2)
	default:
		return base
	}
}

// ExerciseTurnTimeout is the finite whole-row ceiling for exploratory
// baselines. It is intentionally separate from the per-call model timeout:
// multi-step R-TEOR-R rows may spend more total wall-clock than any single
// model invocation while still remaining bounded.
func ExerciseTurnTimeout(modelCallTimeout time.Duration) time.Duration {
	if modelCallTimeout <= 0 {
		modelCallTimeout = exerciseCloudTimeout
	}
	turnTimeout := scaleDuration(modelCallTimeout, 3, 1)
	const maxBaselineTurnTimeout = 15 * time.Minute
	if turnTimeout > maxBaselineTurnTimeout {
		return maxBaselineTurnTimeout
	}
	return turnTimeout
}

func scaleDuration(base time.Duration, num, denom int64) time.Duration {
	if base <= 0 || num <= 0 || denom <= 0 {
		return base
	}
	return time.Duration((int64(base) * num) / denom)
}

func exerciseModelProvider(model string) string {
	provider, _, ok := strings.Cut(model, "/")
	if !ok {
		return ""
	}
	return provider
}
