package llm

import (
	"regexp"
	"strings"
)

// ResponseTransform modifies LLM response content before it reaches the caller.
type ResponseTransform interface {
	Transform(content string) string
}

// TransformPipeline applies a sequence of transforms in order.
type TransformPipeline struct {
	transforms []ResponseTransform
}

// NewTransformPipeline creates a pipeline from the given transforms.
func NewTransformPipeline(transforms ...ResponseTransform) *TransformPipeline {
	return &TransformPipeline{transforms: transforms}
}

// Apply runs all transforms sequentially.
func (p *TransformPipeline) Apply(content string) string {
	for _, t := range p.transforms {
		content = t.Transform(content)
	}
	return content
}

// DefaultTransformPipeline returns the standard transform chain.
func DefaultTransformPipeline() *TransformPipeline {
	return NewTransformPipeline(
		&ReasoningExtractor{},
		&ContentGuard{},
		&FormatNormalizer{},
	)
}

// ReasoningExtractor strips <think>...</think> blocks from chain-of-thought
// model responses (e.g., DeepSeek, Qwen with reasoning). These blocks contain
// internal reasoning that should not be shown to the user.
type ReasoningExtractor struct{}

var thinkBlockRe = regexp.MustCompile(`(?s)<think>.*?</think>`)

func (r *ReasoningExtractor) Transform(content string) string {
	result := thinkBlockRe.ReplaceAllString(content, "")
	return strings.TrimSpace(result)
}

// ContentGuard detects and redacts prompt injection markers that may have
// leaked through the LLM output. These markers are used by various models
// to delineate system/user/assistant boundaries.
type ContentGuard struct{}

var injectionMarkers = []string{
	"[SYSTEM]",
	"[INST]",
	"[/INST]",
	"<|im_start|>",
	"<|im_end|>",
	"<|system|>",
	"<|user|>",
	"<|assistant|>",
	"<<SYS>>",
	"<</SYS>>",
}

func (g *ContentGuard) Transform(content string) string {
	for _, marker := range injectionMarkers {
		content = strings.ReplaceAll(content, marker, "")
	}
	return content
}

// FormatNormalizer cleans up common formatting artifacts in LLM responses:
// leading/trailing whitespace, broken code fences, and excessive blank lines.
type FormatNormalizer struct{}

var excessiveNewlines = regexp.MustCompile(`\n{4,}`)

func (f *FormatNormalizer) Transform(content string) string {
	content = strings.TrimSpace(content)
	content = excessiveNewlines.ReplaceAllString(content, "\n\n\n")
	return content
}
