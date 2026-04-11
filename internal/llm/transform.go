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

// TransformOutput holds the result of a transform pipeline run with full metadata.
type TransformOutput struct {
	Content            string `json:"content"`
	ReasoningExtracted string `json:"reasoning_extracted,omitempty"`
	Modified           bool   `json:"modified"`
	Flagged            bool   `json:"flagged,omitempty"` // Rust parity: set when injection markers detected
}

// ApplyWithOutput runs all transforms and returns a TransformOutput with metadata
// about what changed. The ReasoningExtracted field captures any <think> blocks
// that were stripped by the ReasoningExtractor.
func (p *TransformPipeline) ApplyWithOutput(content string) TransformOutput {
	original := content
	var reasoning string
	var flagged bool

	for _, t := range p.transforms {
		if re, ok := t.(*ReasoningExtractor); ok {
			reasoning = re.ExtractReasoning(content)
		}
		if cg, ok := t.(*ContentGuard); ok {
			if cg.HasInjection(content) {
				flagged = true
			}
		}
		content = t.Transform(content)
	}

	return TransformOutput{
		Content:            content,
		ReasoningExtracted: reasoning,
		Modified:           content != original,
		Flagged:            flagged,
	}
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

// ExtractReasoning returns the concatenated content of all <think> blocks.
func (r *ReasoningExtractor) ExtractReasoning(content string) string {
	matches := thinkBlockRe.FindAllString(content, -1)
	if len(matches) == 0 {
		return ""
	}
	var parts []string
	for _, m := range matches {
		// Strip the <think> and </think> tags.
		inner := strings.TrimPrefix(m, "<think>")
		inner = strings.TrimSuffix(inner, "</think>")
		inner = strings.TrimSpace(inner)
		if inner != "" {
			parts = append(parts, inner)
		}
	}
	return strings.Join(parts, "\n\n")
}

// ContentGuard detects prompt injection markers in LLM output and replaces
// the entire response with a safety message.
// Rust parity: transform.rs INJECTION_MARKERS — 5 markers, full replacement, flagged=true.
type ContentGuard struct{}

// injectionMarkers matches Rust's INJECTION_MARKERS exactly (5 markers).
var injectionMarkers = []string{
	"[SYSTEM]",
	"[INST]",
	"<|im_start|>",
	"<s>",
	"</s>",
}

const filteredMessage = "[Content filtered for safety]"

// HasInjection returns true if any injection marker is present.
func (g *ContentGuard) HasInjection(content string) bool {
	for _, marker := range injectionMarkers {
		if strings.Contains(content, marker) {
			return true
		}
	}
	return false
}

// Transform replaces the entire content if injection markers are found.
// Rust parity: ContentGuard replaces with FILTERED_MESSAGE, does NOT strip markers.
func (g *ContentGuard) Transform(content string) string {
	if g.HasInjection(content) {
		return filteredMessage
	}
	return content
}

// FormatNormalizer cleans up common formatting artifacts in LLM responses:
// leading/trailing whitespace, broken code fences, and excessive blank lines.
type FormatNormalizer struct{}

var excessiveNewlines = regexp.MustCompile(`\n{3,}`)

func (f *FormatNormalizer) Transform(content string) string {
	content = strings.TrimSpace(content)
	content = excessiveNewlines.ReplaceAllString(content, "\n\n")
	return content
}
