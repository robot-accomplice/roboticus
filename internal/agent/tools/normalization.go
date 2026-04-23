package tools

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
)

type NormalizationDisposition string

const (
	NormalizationNoTransformNeeded      NormalizationDisposition = "no_transform_needed"
	NormalizationQualifiedTransform     NormalizationDisposition = "qualified_transform_applied"
	NormalizationTransformFailed        NormalizationDisposition = "transform_failed"
	NormalizationNoQualifiedTransformer NormalizationDisposition = "no_qualified_transformer"
)

type NormalizationFidelity string

const (
	NormalizationExact    NormalizationFidelity = "exact"
	NormalizationRepaired NormalizationFidelity = "repaired"
)

type ToolCallNormalizationInput struct {
	ToolName      string
	RawArguments  string
	RequestModel  string
	ResponseModel string
	Provider      string
}

type ToolCallNormalizationResult struct {
	Arguments   string
	Transformer string
	Disposition NormalizationDisposition
	Fidelity    NormalizationFidelity
	Reason      string
}

type ToolResultNormalizationInput struct {
	ToolName      string
	Result        *Result
	RequestModel  string
	ResponseModel string
	Provider      string
}

type ToolResultNormalizationResult struct {
	Result      *Result
	Transformer string
	Disposition NormalizationDisposition
	Fidelity    NormalizationFidelity
	Reason      string
}

type ToolCallNormalizer interface {
	Name() string
	Qualifies(input ToolCallNormalizationInput) bool
	Normalize(input ToolCallNormalizationInput) (string, NormalizationFidelity, error)
}

type ToolResultNormalizer interface {
	Name() string
	Qualifies(input ToolResultNormalizationInput) bool
	Normalize(input ToolResultNormalizationInput) (*Result, NormalizationFidelity, error)
}

type NormalizationFactory struct {
	callNormalizers   []ToolCallNormalizer
	resultNormalizers []ToolResultNormalizer
}

func NewNormalizationFactory() *NormalizationFactory {
	return &NormalizationFactory{
		callNormalizers: []ToolCallNormalizer{
			identityStructuredArgsNormalizer{},
			quotedStructuredArgsNormalizer{},
			embeddedJSONObjectNormalizer{},
		},
		resultNormalizers: []ToolResultNormalizer{
			filteredTextResultNormalizer{},
		},
	}
}

func (f *NormalizationFactory) NormalizeToolCall(input ToolCallNormalizationInput) ToolCallNormalizationResult {
	if f == nil {
		f = NewNormalizationFactory()
	}
	for _, normalizer := range f.callNormalizers {
		if !normalizer.Qualifies(input) {
			continue
		}
		args, fidelity, err := normalizer.Normalize(input)
		if err != nil {
			return ToolCallNormalizationResult{
				Arguments:   input.RawArguments,
				Transformer: normalizer.Name(),
				Disposition: NormalizationTransformFailed,
				Fidelity:    fidelity,
				Reason:      err.Error(),
			}
		}
		disposition := NormalizationQualifiedTransform
		if args == input.RawArguments {
			disposition = NormalizationNoTransformNeeded
		}
		return ToolCallNormalizationResult{
			Arguments:   args,
			Transformer: normalizer.Name(),
			Disposition: disposition,
			Fidelity:    fidelity,
		}
	}
	return ToolCallNormalizationResult{
		Arguments:   input.RawArguments,
		Disposition: NormalizationNoQualifiedTransformer,
		Fidelity:    NormalizationRepaired,
		Reason:      "no qualified tool-call argument transformer for malformed structured input",
	}
}

func (f *NormalizationFactory) NormalizeToolResult(input ToolResultNormalizationInput) ToolResultNormalizationResult {
	if f == nil {
		f = NewNormalizationFactory()
	}
	for _, normalizer := range f.resultNormalizers {
		if !normalizer.Qualifies(input) {
			continue
		}
		result, fidelity, err := normalizer.Normalize(input)
		if err != nil {
			return ToolResultNormalizationResult{
				Result:      input.Result,
				Transformer: normalizer.Name(),
				Disposition: NormalizationTransformFailed,
				Fidelity:    fidelity,
				Reason:      err.Error(),
			}
		}
		disposition := NormalizationQualifiedTransform
		if sameResultPayload(result, input.Result) {
			disposition = NormalizationNoTransformNeeded
		}
		return ToolResultNormalizationResult{
			Result:      result,
			Transformer: normalizer.Name(),
			Disposition: disposition,
			Fidelity:    fidelity,
		}
	}
	return ToolResultNormalizationResult{
		Result:      input.Result,
		Disposition: NormalizationNoQualifiedTransformer,
		Fidelity:    NormalizationRepaired,
		Reason:      "no qualified tool-result transformer",
	}
}

type identityStructuredArgsNormalizer struct{}

func (identityStructuredArgsNormalizer) Name() string { return "identity_structured_args" }

func (identityStructuredArgsNormalizer) Qualifies(input ToolCallNormalizationInput) bool {
	return isStructuredJSON(input.RawArguments)
}

func (identityStructuredArgsNormalizer) Normalize(input ToolCallNormalizationInput) (string, NormalizationFidelity, error) {
	return strings.TrimSpace(input.RawArguments), NormalizationExact, nil
}

type quotedStructuredArgsNormalizer struct{}

func (quotedStructuredArgsNormalizer) Name() string { return "quoted_structured_args" }

func (quotedStructuredArgsNormalizer) Qualifies(input ToolCallNormalizationInput) bool {
	raw := strings.TrimSpace(input.RawArguments)
	if len(raw) < 2 || raw[0] != '"' {
		return false
	}
	var inner string
	if err := json.Unmarshal([]byte(raw), &inner); err != nil {
		return false
	}
	return isStructuredJSON(inner)
}

func (quotedStructuredArgsNormalizer) Normalize(input ToolCallNormalizationInput) (string, NormalizationFidelity, error) {
	var inner string
	if err := json.Unmarshal([]byte(strings.TrimSpace(input.RawArguments)), &inner); err != nil {
		return input.RawArguments, NormalizationRepaired, err
	}
	inner = strings.TrimSpace(inner)
	if !isStructuredJSON(inner) {
		return input.RawArguments, NormalizationRepaired, errors.New("quoted payload did not contain structured json")
	}
	return inner, NormalizationRepaired, nil
}

type embeddedJSONObjectNormalizer struct{}

func (embeddedJSONObjectNormalizer) Name() string { return "embedded_json_object" }

func (embeddedJSONObjectNormalizer) Qualifies(input ToolCallNormalizationInput) bool {
	raw := strings.TrimSpace(input.RawArguments)
	if raw == "" || isStructuredJSON(raw) {
		return false
	}
	_, ok := lastValidEmbeddedJSONObject(raw)
	return ok
}

func (embeddedJSONObjectNormalizer) Normalize(input ToolCallNormalizationInput) (string, NormalizationFidelity, error) {
	candidate, ok := lastValidEmbeddedJSONObject(strings.TrimSpace(input.RawArguments))
	if !ok {
		return input.RawArguments, NormalizationRepaired, errors.New("no embedded structured json object could be salvaged")
	}
	return candidate, NormalizationRepaired, nil
}

type filteredTextResultNormalizer struct{}

func (filteredTextResultNormalizer) Name() string { return "filtered_text_result" }

func (filteredTextResultNormalizer) Qualifies(input ToolResultNormalizationInput) bool {
	return input.Result != nil
}

func (filteredTextResultNormalizer) Normalize(input ToolResultNormalizationInput) (*Result, NormalizationFidelity, error) {
	if input.Result == nil {
		return nil, NormalizationExact, nil
	}
	out := *input.Result
	out.Output = FilterToolOutput(out.Output)
	if out.Source == "" {
		out.Source = input.Result.Source
	}
	fidelity := NormalizationExact
	if out.Output != input.Result.Output {
		fidelity = NormalizationRepaired
	}
	return &out, fidelity, nil
}

func isStructuredJSON(raw string) bool {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return false
	}
	switch raw[0] {
	case '{', '[':
	default:
		return false
	}
	return json.Valid([]byte(raw))
}

func lastValidEmbeddedJSONObject(raw string) (string, bool) {
	var best string
	for start := 0; start < len(raw); start++ {
		switch raw[start] {
		case '{', '[':
		default:
			continue
		}
		dec := json.NewDecoder(bytes.NewBufferString(raw[start:]))
		var payload any
		if err := dec.Decode(&payload); err != nil {
			continue
		}
		end := start + int(dec.InputOffset())
		if end <= start || end > len(raw) {
			continue
		}
		candidate := strings.TrimSpace(raw[start:end])
		if isStructuredJSON(candidate) {
			if len(candidate) >= len(best) {
				best = candidate
			}
		}
	}
	if strings.TrimSpace(best) == "" {
		return "", false
	}
	return best, true
}

func sameResultPayload(a, b *Result) bool {
	switch {
	case a == nil && b == nil:
		return true
	case a == nil || b == nil:
		return false
	}
	return a.Output == b.Output && string(a.Metadata) == string(b.Metadata) && a.Source == b.Source
}
