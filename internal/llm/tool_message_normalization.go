package llm

import (
	"encoding/json"
	"fmt"
	"strings"
)

type ToolMessageNormalizationDisposition string

const (
	ToolMessageNoTransformNeeded      ToolMessageNormalizationDisposition = "no_transform_needed"
	ToolMessageQualifiedTransform     ToolMessageNormalizationDisposition = "qualified_transform_applied"
	ToolMessageTransformFailed        ToolMessageNormalizationDisposition = "transform_failed"
	ToolMessageNoQualifiedTransformer ToolMessageNormalizationDisposition = "no_qualified_transformer"
)

type ToolMessageNormalizationFidelity string

const (
	ToolMessageExact    ToolMessageNormalizationFidelity = "exact"
	ToolMessageRepaired ToolMessageNormalizationFidelity = "repaired"
)

type ProviderMessageNormalizationInput struct {
	Format   APIFormat
	Messages []Message
}

type ProviderMessageNormalizationResult struct {
	Messages    []map[string]any
	Transformer string
	Disposition ToolMessageNormalizationDisposition
	Fidelity    ToolMessageNormalizationFidelity
	Reason      string
}

type ProviderMessageNormalizer interface {
	Name() string
	Qualifies(input ProviderMessageNormalizationInput) bool
	Normalize(input ProviderMessageNormalizationInput) ([]map[string]any, ToolMessageNormalizationFidelity, error)
}

type ToolMessageNormalizationFactory struct {
	normalizers []ProviderMessageNormalizer
}

func NewToolMessageNormalizationFactory() *ToolMessageNormalizationFactory {
	return &ToolMessageNormalizationFactory{
		normalizers: []ProviderMessageNormalizer{
			ollamaMessageNormalizer{},
			openAICompatibleMessageNormalizer{},
		},
	}
}

func (f *ToolMessageNormalizationFactory) NormalizeProviderMessages(input ProviderMessageNormalizationInput) ProviderMessageNormalizationResult {
	if f == nil {
		f = NewToolMessageNormalizationFactory()
	}
	for _, normalizer := range f.normalizers {
		if !normalizer.Qualifies(input) {
			continue
		}
		msgs, fidelity, err := normalizer.Normalize(input)
		if err != nil {
			return ProviderMessageNormalizationResult{
				Messages:    nil,
				Transformer: normalizer.Name(),
				Disposition: ToolMessageTransformFailed,
				Fidelity:    fidelity,
				Reason:      err.Error(),
			}
		}
		disposition := ToolMessageQualifiedTransform
		if fidelity == ToolMessageExact {
			disposition = ToolMessageNoTransformNeeded
		}
		return ProviderMessageNormalizationResult{
			Messages:    msgs,
			Transformer: normalizer.Name(),
			Disposition: disposition,
			Fidelity:    fidelity,
		}
	}
	return ProviderMessageNormalizationResult{
		Disposition: ToolMessageNoQualifiedTransformer,
		Fidelity:    ToolMessageRepaired,
		Reason:      fmt.Sprintf("no qualified provider-message normalizer for format %q", input.Format),
	}
}

type openAICompatibleMessageNormalizer struct{}

func (openAICompatibleMessageNormalizer) Name() string { return "openai_compatible_messages" }

func (openAICompatibleMessageNormalizer) Qualifies(input ProviderMessageNormalizationInput) bool {
	switch input.Format {
	case FormatOpenAI, FormatOpenAIResponses:
		return true
	default:
		return false
	}
}

func (openAICompatibleMessageNormalizer) Normalize(input ProviderMessageNormalizationInput) ([]map[string]any, ToolMessageNormalizationFidelity, error) {
	msgs := make([]map[string]any, 0, len(input.Messages))
	for _, m := range input.Messages {
		msg := map[string]any{"role": m.Role}

		if m.ToolCallID != "" {
			msg["tool_call_id"] = m.ToolCallID
			msg["content"] = m.Content
			if m.Name != "" {
				msg["name"] = m.Name
			}
			msgs = append(msgs, msg)
			continue
		}

		if m.Role == "assistant" && len(m.ToolCalls) > 0 {
			msg["content"] = m.Content
			msg["tool_calls"] = openAICompatibleToolCalls(m.ToolCalls)
			msgs = append(msgs, msg)
			continue
		}

		if m.Content != "" {
			msg["content"] = m.Content
		}
		if len(m.ToolCalls) > 0 {
			msg["tool_calls"] = openAICompatibleToolCalls(m.ToolCalls)
		}
		if m.Name != "" {
			msg["name"] = m.Name
		}
		msgs = append(msgs, msg)
	}
	return msgs, ToolMessageExact, nil
}

type ollamaMessageNormalizer struct{}

func (ollamaMessageNormalizer) Name() string { return "ollama_tool_messages" }

func (ollamaMessageNormalizer) Qualifies(input ProviderMessageNormalizationInput) bool {
	return input.Format == FormatOllama
}

func (ollamaMessageNormalizer) Normalize(input ProviderMessageNormalizationInput) ([]map[string]any, ToolMessageNormalizationFidelity, error) {
	msgs := make([]map[string]any, 0, len(input.Messages))
	fidelity := ToolMessageExact
	for _, m := range input.Messages {
		msg := map[string]any{"role": m.Role}

		if m.ToolCallID != "" {
			toolName := strings.TrimSpace(m.Name)
			if toolName == "" {
				return nil, ToolMessageRepaired, fmt.Errorf("ollama tool result message missing tool name for tool_call_id %q", m.ToolCallID)
			}
			msg["role"] = "tool"
			msg["tool_name"] = toolName
			msg["content"] = m.Content
			msgs = append(msgs, msg)
			fidelity = ToolMessageRepaired
			continue
		}

		if m.Role == "assistant" && len(m.ToolCalls) > 0 {
			msg["content"] = m.Content
			toolCalls, transformed, err := ollamaToolCalls(m.ToolCalls)
			if err != nil {
				return nil, ToolMessageRepaired, err
			}
			if transformed {
				fidelity = ToolMessageRepaired
			}
			msg["tool_calls"] = toolCalls
			msgs = append(msgs, msg)
			continue
		}

		if m.Content != "" {
			msg["content"] = m.Content
		}
		if len(m.ToolCalls) > 0 {
			toolCalls, transformed, err := ollamaToolCalls(m.ToolCalls)
			if err != nil {
				return nil, ToolMessageRepaired, err
			}
			if transformed {
				fidelity = ToolMessageRepaired
			}
			msg["tool_calls"] = toolCalls
		}
		if m.Name != "" {
			msg["name"] = m.Name
		}
		msgs = append(msgs, msg)
	}
	return msgs, fidelity, nil
}

func openAICompatibleToolCalls(toolCalls []ToolCall) []map[string]any {
	out := make([]map[string]any, 0, len(toolCalls))
	for _, tc := range toolCalls {
		toolCall := map[string]any{
			"id":   tc.ID,
			"type": defaultToolCallType(tc.Type),
			"function": map[string]any{
				"name":      tc.Function.Name,
				"arguments": tc.Function.Arguments,
			},
		}
		out = append(out, toolCall)
	}
	return out
}

func ollamaToolCalls(toolCalls []ToolCall) ([]map[string]any, bool, error) {
	out := make([]map[string]any, 0, len(toolCalls))
	transformed := false
	for i, tc := range toolCalls {
		args, err := ollamaArgumentsPayload(tc.Function.Arguments)
		if err != nil {
			return nil, transformed, fmt.Errorf("ollama tool call %q arguments were not valid structured json: %w", tc.Function.Name, err)
		}
		toolCall := map[string]any{
			"type": defaultToolCallType(tc.Type),
			"function": map[string]any{
				"index":     i,
				"name":      tc.Function.Name,
				"arguments": args,
			},
		}
		if tc.ID != "" {
			toolCall["id"] = tc.ID
		}
		out = append(out, toolCall)
		if strings.TrimSpace(tc.Function.Arguments) != "" {
			transformed = true
		}
	}
	return out, transformed, nil
}

func ollamaArgumentsPayload(raw string) (any, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return map[string]any{}, nil
	}
	var payload any
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return nil, err
	}
	return payload, nil
}

func defaultToolCallType(raw string) string {
	if strings.TrimSpace(raw) == "" {
		return "function"
	}
	return raw
}
