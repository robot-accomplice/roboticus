package llm

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"roboticus/internal/core"
)

// marshalRequest translates the provider-agnostic Request into the provider's wire format.
func (c *Client) marshalRequest(req *Request) ([]byte, error) {
	switch c.provider.Format {
	case FormatAnthropic:
		return c.marshalAnthropic(req)
	case FormatGoogle:
		return c.marshalGoogle(req)
	case FormatOllama:
		return c.marshalOllama(req)
	case FormatOpenAIResponses:
		return c.marshalOpenAIResponses(req)
	default:
		return c.marshalOpenAI(req)
	}
}

func (c *Client) marshalOpenAI(req *Request) ([]byte, error) {
	payload := map[string]any{
		"model":    req.Model,
		"messages": req.Messages,
		"stream":   req.Stream,
	}
	if req.MaxTokens > 0 {
		payload["max_tokens"] = req.MaxTokens
	}
	if req.Temperature != nil {
		payload["temperature"] = *req.Temperature
	}
	if len(req.Tools) > 0 {
		payload["tools"] = req.Tools
	}
	if len(req.Stop) > 0 {
		payload["stop"] = req.Stop
	}
	return json.Marshal(payload)
}

func (c *Client) marshalAnthropic(req *Request) ([]byte, error) {
	var system string
	var messages []map[string]any
	for _, m := range req.Messages {
		if m.Role == "system" {
			system = m.Content
			continue
		}
		messages = append(messages, map[string]any{"role": m.Role, "content": m.Content})
	}

	payload := map[string]any{
		"model":      req.Model,
		"messages":   messages,
		"max_tokens": max(req.MaxTokens, 4096),
		"stream":     req.Stream,
	}
	if system != "" {
		payload["system"] = system
	}
	if req.Temperature != nil {
		payload["temperature"] = *req.Temperature
	}
	if len(req.Tools) > 0 {
		var tools []map[string]any
		for _, t := range req.Tools {
			tools = append(tools, map[string]any{
				"name":         t.Function.Name,
				"description":  t.Function.Description,
				"input_schema": json.RawMessage(t.Function.Parameters),
			})
		}
		payload["tools"] = tools
	}
	return json.Marshal(payload)
}

func (c *Client) marshalOllama(req *Request) ([]byte, error) {
	payload := map[string]any{
		"model":    req.Model,
		"messages": req.Messages,
		"stream":   req.Stream,
	}
	if req.Temperature != nil {
		payload["options"] = map[string]any{"temperature": *req.Temperature}
	}
	return json.Marshal(payload)
}

func (c *Client) marshalGoogle(req *Request) ([]byte, error) {
	var contents []map[string]any
	for _, m := range req.Messages {
		role := m.Role
		if role == "assistant" {
			role = "model"
		}
		contents = append(contents, map[string]any{
			"role":  role,
			"parts": []map[string]any{{"text": m.Content}},
		})
	}
	payload := map[string]any{"contents": contents}
	if req.MaxTokens > 0 {
		payload["generationConfig"] = map[string]any{"maxOutputTokens": req.MaxTokens}
	}
	return json.Marshal(payload)
}

// unmarshalResponse translates the provider's response into our canonical Response.
func (c *Client) unmarshalResponse(body io.Reader) (*Response, error) {
	data, err := io.ReadAll(body)
	if err != nil {
		return nil, core.WrapError(core.ErrNetwork, "failed to read response", err)
	}

	switch c.provider.Format {
	case FormatAnthropic:
		return c.unmarshalAnthropicResponse(data)
	case FormatOllama:
		return c.unmarshalOllamaResponse(data)
	case FormatGoogle:
		return c.unmarshalGoogleResponse(data)
	case FormatOpenAIResponses:
		return c.unmarshalOpenAIResponsesResponse(data)
	default:
		return c.unmarshalOpenAIResponse(data)
	}
}

func (c *Client) unmarshalOpenAIResponse(data []byte) (*Response, error) {
	var raw struct {
		ID      string `json:"id"`
		Model   string `json:"model"`
		Choices []struct {
			Message      Message `json:"message"`
			FinishReason string  `json:"finish_reason"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, core.WrapError(core.ErrLLM, "failed to parse OpenAI response", err)
	}
	if len(raw.Choices) == 0 {
		return nil, core.NewError(core.ErrLLM, "empty choices in OpenAI response")
	}
	return &Response{
		ID:           raw.ID,
		Model:        raw.Model,
		Content:      raw.Choices[0].Message.Content,
		ToolCalls:    raw.Choices[0].Message.ToolCalls,
		FinishReason: raw.Choices[0].FinishReason,
		Usage:        Usage{InputTokens: raw.Usage.PromptTokens, OutputTokens: raw.Usage.CompletionTokens},
	}, nil
}

func (c *Client) unmarshalAnthropicResponse(data []byte) (*Response, error) {
	var raw struct {
		ID      string `json:"id"`
		Model   string `json:"model"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text,omitempty"`
		} `json:"content"`
		StopReason string `json:"stop_reason"`
		Usage      struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, core.WrapError(core.ErrLLM, "failed to parse Anthropic response", err)
	}
	var content string
	for _, block := range raw.Content {
		if block.Type == "text" {
			content += block.Text
		}
	}
	return &Response{
		ID:           raw.ID,
		Model:        raw.Model,
		Content:      content,
		FinishReason: raw.StopReason,
		Usage:        Usage{InputTokens: raw.Usage.InputTokens, OutputTokens: raw.Usage.OutputTokens},
	}, nil
}

func (c *Client) unmarshalOllamaResponse(data []byte) (*Response, error) {
	var raw struct {
		Model   string  `json:"model"`
		Message Message `json:"message"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, core.WrapError(core.ErrLLM, "failed to parse Ollama response", err)
	}
	return &Response{
		Model:        raw.Model,
		Content:      raw.Message.Content,
		FinishReason: "stop",
	}, nil
}

func (c *Client) unmarshalGoogleResponse(data []byte) (*Response, error) {
	var raw struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
			FinishReason string `json:"finishReason"`
		} `json:"candidates"`
		UsageMetadata struct {
			PromptTokenCount     int `json:"promptTokenCount"`
			CandidatesTokenCount int `json:"candidatesTokenCount"`
		} `json:"usageMetadata"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, core.WrapError(core.ErrLLM, "failed to parse Google response", err)
	}
	if len(raw.Candidates) == 0 {
		return nil, core.NewError(core.ErrLLM, "empty candidates in Google response")
	}
	var content string
	for _, part := range raw.Candidates[0].Content.Parts {
		content += part.Text
	}
	return &Response{
		Model:        "",
		Content:      content,
		FinishReason: raw.Candidates[0].FinishReason,
		Usage: Usage{
			InputTokens:  raw.UsageMetadata.PromptTokenCount,
			OutputTokens: raw.UsageMetadata.CandidatesTokenCount,
		},
	}, nil
}

// unmarshalStreamChunk parses a single SSE data line into a StreamChunk.
func (c *Client) unmarshalStreamChunk(data []byte) (*StreamChunk, error) {
	switch c.provider.Format {
	case FormatAnthropic:
		return c.unmarshalAnthropicChunk(data)
	default:
		return c.unmarshalOpenAIChunk(data)
	}
}

func (c *Client) unmarshalOpenAIChunk(data []byte) (*StreamChunk, error) {
	var raw struct {
		Choices []struct {
			Delta struct {
				Content   string     `json:"content"`
				ToolCalls []ToolCall `json:"tool_calls"`
			} `json:"delta"`
			FinishReason *string `json:"finish_reason"`
		} `json:"choices"`
		Usage *struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	chunk := &StreamChunk{}
	if len(raw.Choices) > 0 {
		chunk.Delta = raw.Choices[0].Delta.Content
		chunk.ToolCalls = raw.Choices[0].Delta.ToolCalls
		if raw.Choices[0].FinishReason != nil {
			chunk.FinishReason = *raw.Choices[0].FinishReason
		}
	}
	if raw.Usage != nil {
		chunk.Usage = &Usage{
			InputTokens:  raw.Usage.PromptTokens,
			OutputTokens: raw.Usage.CompletionTokens,
		}
	}
	return chunk, nil
}

func (c *Client) unmarshalAnthropicChunk(data []byte) (*StreamChunk, error) {
	var raw struct {
		Type  string `json:"type"`
		Delta struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"delta"`
		Usage *struct {
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	chunk := &StreamChunk{}
	if raw.Type == "content_block_delta" {
		chunk.Delta = raw.Delta.Text
	}
	if raw.Type == "message_stop" {
		chunk.FinishReason = "end_turn"
	}
	if raw.Usage != nil {
		chunk.Usage = &Usage{OutputTokens: raw.Usage.OutputTokens}
	}
	return chunk, nil
}

// marshalOpenAIResponses formats a request for the OpenAI Responses API
// (POST /v1/responses), which uses an `input` field instead of `messages`.
func (c *Client) marshalOpenAIResponses(req *Request) ([]byte, error) {
	// Convert messages to the Responses API input format.
	var input []map[string]any
	for _, m := range req.Messages {
		item := map[string]any{
			"role":    m.Role,
			"content": m.Content,
		}
		input = append(input, item)
	}

	payload := map[string]any{
		"model": req.Model,
		"input": input,
	}
	if req.MaxTokens > 0 {
		payload["max_output_tokens"] = req.MaxTokens
	}
	if req.Temperature != nil {
		payload["temperature"] = *req.Temperature
	}
	if len(req.Tools) > 0 {
		payload["tools"] = req.Tools
	}
	if req.Stream {
		payload["stream"] = true
	}
	return json.Marshal(payload)
}

// unmarshalOpenAIResponsesResponse parses an OpenAI Responses API response.
func (c *Client) unmarshalOpenAIResponsesResponse(data []byte) (*Response, error) {
	var raw struct {
		ID     string `json:"id"`
		Model  string `json:"model"`
		Status string `json:"status"`
		Output []struct {
			Type    string `json:"type"`
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content,omitempty"`
		} `json:"output"`
		Usage struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, core.WrapError(core.ErrLLM, "failed to parse OpenAI Responses response", err)
	}

	var content string
	for _, out := range raw.Output {
		if out.Type == "message" {
			for _, c := range out.Content {
				if c.Type == "output_text" {
					content += c.Text
				}
			}
		}
	}

	finishReason := "stop"
	if raw.Status == "incomplete" {
		finishReason = "length"
	}

	return &Response{
		ID:           raw.ID,
		Model:        raw.Model,
		Content:      content,
		FinishReason: finishReason,
		Usage: Usage{
			InputTokens:  raw.Usage.InputTokens,
			OutputTokens: raw.Usage.OutputTokens,
		},
	}, nil
}

// parseErrorResponse reads an error body and returns a categorized error.
func (c *Client) parseErrorResponse(resp *http.Response) error {
	body, _ := io.ReadAll(resp.Body)

	switch resp.StatusCode {
	case 429:
		return core.NewError(core.ErrRateLimited,
			fmt.Sprintf("provider %s: %s", c.provider.Name, string(body)))
	case 401, 403:
		return core.NewError(core.ErrUnauthorized,
			fmt.Sprintf("provider %s: %s", c.provider.Name, string(body)))
	case 402:
		return core.NewError(core.ErrCreditExhausted,
			fmt.Sprintf("provider %s: %s", c.provider.Name, string(body)))
	default:
		return core.NewError(core.ErrLLM,
			fmt.Sprintf("provider %s returned %d: %s", c.provider.Name, resp.StatusCode, string(body)))
	}
}
