package pipeline

import (
	"context"

	"roboticus/internal/llm"
)

// InferenceResult holds the full result of an inference call, matching Rust's
// InferenceResult struct. Carries all metadata needed for cost recording,
// quality tracking, and pipeline observability.
type InferenceResult struct {
	Content      string  `json:"content"`
	Model        string  `json:"model"`
	Provider     string  `json:"provider"`
	TokensIn     int     `json:"tokens_in"`
	TokensOut    int     `json:"tokens_out"`
	Cost         float64 `json:"cost"`
	LatencyMs    int64   `json:"latency_ms"`
	QualityScore float64 `json:"quality_score"`
	Escalated    bool    `json:"escalated"`
	ToolCalls    []llm.ToolCall
}

// StreamResolvedInference holds a resolved streaming provider ready for SSE.
// Matches Rust's StreamResolvedInference.
type StreamResolvedInference struct {
	Chunks        <-chan llm.StreamChunk
	Errors        <-chan error
	SelectedModel string
	Provider      string
	CostIn        float64
	CostOut       float64
}

// InferenceRunner is the pipeline-facing inference contract.
// Matches Rust's InferenceRunner trait: model selection with audit,
// inference with fallback, and streaming resolution.
//
// The pipeline depends on this interface, not on the concrete LLM service.
// This enables testing, composition, and richer pipeline integration.
type InferenceRunner interface {
	// SelectAndAuditModel chooses the best model for a request, persists the
	// selection event to the database, and returns the model name.
	SelectAndAuditModel(ctx context.Context, userContent, sessionID, turnID, channel string) string

	// InferWithFallback runs inference with the full fallback chain.
	// Returns a rich InferenceResult with all metadata.
	InferWithFallback(ctx context.Context, req *llm.Request, initialModel string) (*InferenceResult, error)

	// InferStreamWithFallback resolves a streaming provider and returns
	// ready-to-consume SSE channels with provider metadata.
	InferStreamWithFallback(ctx context.Context, req *llm.Request, initialModel string) (*StreamResolvedInference, error)
}

// LLMServiceRunner adapts *llm.Service to the InferenceRunner interface.
// This is the production implementation used by the daemon.
type LLMServiceRunner struct {
	svc *llm.Service
}

// NewLLMServiceRunner wraps an LLM service as an InferenceRunner.
func NewLLMServiceRunner(svc *llm.Service) *LLMServiceRunner {
	return &LLMServiceRunner{svc: svc}
}

// SelectAndAuditModel selects the best model and records the selection event.
func (r *LLMServiceRunner) SelectAndAuditModel(ctx context.Context, userContent, sessionID, turnID, channel string) string {
	// Use the router to select a model.
	target := r.svc.Router().Select(&llm.Request{
		Messages: []llm.Message{{Role: "user", Content: userContent}},
	})
	model := llm.ModelSpecForTarget(target)
	if model == "" {
		model = r.svc.Primary()
	}

	// Record model selection event (Rust: record_model_selection_event).
	r.svc.RecordModelSelection(ctx, turnID, sessionID, "", channel, model, "routed", userContent)

	return model
}

// InferWithFallback runs inference with the full fallback chain.
func (r *LLMServiceRunner) InferWithFallback(ctx context.Context, req *llm.Request, initialModel string) (*InferenceResult, error) {
	if initialModel != "" {
		req.Model = initialModel
	}
	resp, err := r.svc.Complete(ctx, req)
	if err != nil {
		return nil, err
	}
	return &InferenceResult{
		Content:   resp.Content,
		Model:     resp.Model,
		TokensIn:  resp.Usage.InputTokens,
		TokensOut: resp.Usage.OutputTokens,
		ToolCalls: resp.ToolCalls,
	}, nil
}

// InferStreamWithFallback resolves a streaming provider.
func (r *LLMServiceRunner) InferStreamWithFallback(ctx context.Context, req *llm.Request, initialModel string) (*StreamResolvedInference, error) {
	if initialModel != "" {
		req.Model = initialModel
	}
	chunks, errs := r.svc.Stream(ctx, req)
	return &StreamResolvedInference{
		Chunks:        chunks,
		Errors:        errs,
		SelectedModel: req.Model,
	}, nil
}

// Note: modelSpecForTarget was previously duplicated here. Now uses
// llm.ModelSpecForTarget (the canonical version) to comply with Rule 6.3.
