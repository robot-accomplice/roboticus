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

func exerciseModelProvider(model string) string {
	provider, _, ok := strings.Cut(model, "/")
	if !ok {
		return ""
	}
	return provider
}
