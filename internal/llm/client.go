package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/rs/zerolog/log"

	"goboticus/internal/core"
)

// Client is a single-provider HTTP client that speaks the provider's native
// format. It implements Completer.
type Client struct {
	provider   *Provider
	httpClient *http.Client
	apiKey     string
}

// NewClient creates a Client for the given provider. It resolves the API key
// from the environment at construction time, returning an error if the key
// is required but missing.
func NewClient(p *Provider) (*Client, error) {
	var apiKey string
	if p.APIKeyEnv != "" && !p.IsLocal {
		apiKey = os.Getenv(p.APIKeyEnv)
		if apiKey == "" {
			return nil, core.NewError(core.ErrConfig,
				fmt.Sprintf("env var %s not set for provider %s", p.APIKeyEnv, p.Name))
		}
	}

	return &Client{
		provider: p,
		httpClient: &http.Client{
			Timeout: 120 * time.Second,
			Transport: &http.Transport{
				MaxIdleConnsPerHost: 10,
				IdleConnTimeout:    90 * time.Second,
			},
		},
		apiKey: apiKey,
	}, nil
}

// Complete sends a non-streaming request and returns the full response.
func (c *Client) Complete(ctx context.Context, req *Request) (*Response, error) {
	req.Stream = false
	body, err := c.marshalRequest(req)
	if err != nil {
		return nil, err
	}

	url := c.chatURL()
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, core.WrapError(core.ErrNetwork, "failed to create request", err)
	}
	c.setHeaders(httpReq)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, core.WrapError(core.ErrNetwork, "request failed", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, c.parseErrorResponse(resp)
	}

	return c.unmarshalResponse(resp.Body)
}

// Stream sends a streaming request and returns channels for chunks and errors.
// The chunk channel closes when the stream ends. The error channel receives
// at most one error.
func (c *Client) Stream(ctx context.Context, req *Request) (<-chan StreamChunk, <-chan error) {
	chunks := make(chan StreamChunk, 32)
	errs := make(chan error, 1)

	go func() {
		defer close(chunks)
		defer close(errs)

		req.Stream = true
		body, err := c.marshalRequest(req)
		if err != nil {
			errs <- err
			return
		}

		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.chatURL(), bytes.NewReader(body))
		if err != nil {
			errs <- core.WrapError(core.ErrNetwork, "failed to create request", err)
			return
		}
		c.setHeaders(httpReq)

		resp, err := c.httpClient.Do(httpReq)
		if err != nil {
			errs <- core.WrapError(core.ErrNetwork, "stream request failed", err)
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			errs <- c.parseErrorResponse(resp)
			return
		}

		c.readSSE(ctx, resp.Body, chunks, errs)
	}()

	return chunks, errs
}

// readSSE parses an SSE stream into StreamChunks.
func (c *Client) readSSE(ctx context.Context, body io.Reader, chunks chan<- StreamChunk, errs chan<- error) {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return
		default:
		}

		line := scanner.Text()

		if !strings.HasPrefix(line, "data: ") {
			continue
		}

		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			return
		}

		chunk, err := c.unmarshalStreamChunk([]byte(data))
		if err != nil {
			log.Warn().Err(err).Str("provider", c.provider.Name).Msg("failed to parse SSE chunk")
			continue
		}

		select {
		case chunks <- *chunk:
		case <-ctx.Done():
			return
		}
	}

	if err := scanner.Err(); err != nil {
		errs <- core.WrapError(core.ErrNetwork, "SSE stream read error", err)
	}
}

// chatURL returns the full URL for the chat completions endpoint.
func (c *Client) chatURL() string {
	path := c.provider.ChatPath
	if path == "" {
		switch c.provider.Format {
		case FormatAnthropic:
			path = "/v1/messages"
		case FormatGoogle:
			path = "/v1/models/" // needs model appended
		case FormatOllama:
			path = "/api/chat"
		default:
			path = "/v1/chat/completions"
		}
	}
	return strings.TrimRight(c.provider.URL, "/") + path
}

// setHeaders adds authentication and content-type headers.
func (c *Client) setHeaders(req *http.Request) {
	req.Header.Set("Content-Type", "application/json")

	if c.apiKey != "" {
		authHeader := c.provider.AuthHeader
		if authHeader == "" {
			switch c.provider.Format {
			case FormatAnthropic:
				authHeader = "x-api-key"
			default:
				authHeader = "Authorization"
			}
		}
		if authHeader == "Authorization" {
			req.Header.Set(authHeader, "Bearer "+c.apiKey)
		} else {
			req.Header.Set(authHeader, c.apiKey)
		}
	}

	// Anthropic-specific headers.
	if c.provider.Format == FormatAnthropic {
		req.Header.Set("anthropic-version", "2023-06-01")
	}

	for k, v := range c.provider.ExtraHeaders {
		req.Header.Set(k, v)
	}
}

// marshalRequest translates the provider-agnostic Request into the provider's
// wire format.
func (c *Client) marshalRequest(req *Request) ([]byte, error) {
	switch c.provider.Format {
	case FormatAnthropic:
		return c.marshalAnthropic(req)
	case FormatGoogle:
		return c.marshalGoogle(req)
	case FormatOllama:
		return c.marshalOllama(req)
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
	// Anthropic uses a different message format: system is separate.
	var system string
	var messages []map[string]any
	for _, m := range req.Messages {
		if m.Role == "system" {
			system = m.Content
			continue
		}
		msg := map[string]any{"role": m.Role, "content": m.Content}
		messages = append(messages, msg)
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
		// Anthropic uses a different tool format.
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
	// Google Generative AI uses "contents" with "parts".
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

// parseErrorResponse reads an error body and returns a categorized error.
func (c *Client) parseErrorResponse(resp *http.Response) error {
	body, _ := io.ReadAll(resp.Body)

	switch {
	case resp.StatusCode == 429:
		return core.NewError(core.ErrRateLimited,
			fmt.Sprintf("provider %s: %s", c.provider.Name, string(body)))
	case resp.StatusCode == 401 || resp.StatusCode == 403:
		return core.NewError(core.ErrUnauthorized,
			fmt.Sprintf("provider %s: %s", c.provider.Name, string(body)))
	case resp.StatusCode == 402:
		return core.NewError(core.ErrCreditExhausted,
			fmt.Sprintf("provider %s: %s", c.provider.Name, string(body)))
	default:
		return core.NewError(core.ErrLLM,
			fmt.Sprintf("provider %s returned %d: %s", c.provider.Name, resp.StatusCode, string(body)))
	}
}
