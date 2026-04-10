// InferenceParams records model selection and resource consumption for a
// single inference call. Persisted as JSON in pipeline_traces.inference_params_json.
//
// Rust parity: crates/roboticus-pipeline/src/context/inference.rs records
// model_selected, model_actual, temperature, tokens_in, tokens_out, cost,
// and escalation metadata per inference call.

package pipeline

import "encoding/json"

// InferenceParams captures inference metadata for trace persistence.
type InferenceParams struct {
	// Model routing.
	ModelRequested string `json:"model_requested,omitempty"` // What the router selected
	ModelActual    string `json:"model_actual,omitempty"`    // What actually ran (may differ after fallback)
	Provider       string `json:"provider,omitempty"`        // Provider name (e.g., "openai", "ollama")
	Escalated      bool   `json:"escalated,omitempty"`       // Whether model was escalated from initial selection

	// Resource consumption.
	TokensIn  int `json:"tokens_in,omitempty"`
	TokensOut int `json:"tokens_out,omitempty"`

	// ReAct loop metadata.
	ReactTurns int  `json:"react_turns,omitempty"` // Number of ReAct loop iterations
	FromCache  bool `json:"from_cache,omitempty"`  // Response came from semantic cache

	// Guard chain results.
	GuardViolations []string `json:"guard_violations,omitempty"` // Guards that flagged the response
	GuardRetried    bool     `json:"guard_retried,omitempty"`    // Whether a guard-triggered retry occurred
}

// JSON returns the InferenceParams as a JSON string for DB persistence.
func (ip *InferenceParams) JSON() string {
	if ip == nil {
		return ""
	}
	b, _ := json.Marshal(ip)
	return string(b)
}

// ParseInferenceParams reconstructs InferenceParams from stored JSON.
func ParseInferenceParams(raw string) (*InferenceParams, error) {
	if raw == "" {
		return nil, nil
	}
	var ip InferenceParams
	if err := json.Unmarshal([]byte(raw), &ip); err != nil {
		return nil, err
	}
	return &ip, nil
}
