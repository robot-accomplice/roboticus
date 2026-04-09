package llm

import (
	"context"
	"encoding/json"
)

// Provider represents an LLM API endpoint (OpenAI, Anthropic, Ollama, Google, etc).
type Provider struct {
	Name             string            `json:"name"`
	URL              string            `json:"url"`
	Format           APIFormat         `json:"format"`
	APIKeyEnv        string            `json:"api_key_env,omitempty"`
	ChatPath         string            `json:"chat_path,omitempty"`
	EmbeddingPath    string            `json:"embedding_path,omitempty"`
	EmbeddingModel   string            `json:"embedding_model,omitempty"`
	IsLocal          bool              `json:"is_local,omitempty"`
	CostPerInputTok  float64           `json:"cost_per_input_token,omitempty"`
	CostPerOutputTok float64           `json:"cost_per_output_token,omitempty"`
	AuthHeader       string            `json:"auth_header,omitempty"`
	AuthMode         string            `json:"auth_mode,omitempty"`       // "bearer" (default), "query", "oauth"
	APIKeyRef        string            `json:"api_key_ref,omitempty"`     // reference to keystore secret name
	OAuthClientID    string            `json:"oauth_client_id,omitempty"` // for OAuth-based providers
	ExtraHeaders     map[string]string `json:"extra_headers,omitempty"`
	TPMLimit         uint64            `json:"tpm_limit,omitempty"`
	RPMLimit         uint64            `json:"rpm_limit,omitempty"`
	TimeoutSecs      int               `json:"timeout_seconds,omitempty"`
}

// APIFormat identifies which wire format a provider speaks.
type APIFormat string

const (
	FormatOpenAI          APIFormat = "openai"
	FormatOpenAIResponses APIFormat = "openai-responses"
	FormatAnthropic       APIFormat = "anthropic"
	FormatOllama          APIFormat = "ollama"
	FormatGoogle          APIFormat = "google"
)

// Message is a single chat message in the conversation.
type Message struct {
	Role         string          `json:"role"`
	Content      string          `json:"content,omitempty"`
	ContentParts []ContentPart   `json:"content_parts,omitempty"` // multimodal; takes precedence over Content
	ToolCalls    []ToolCall      `json:"tool_calls,omitempty"`
	ToolCallID   string          `json:"tool_call_id,omitempty"`
	Name         string          `json:"name,omitempty"`
	Metadata     json.RawMessage `json:"metadata,omitempty"`
}

// ToolCall represents a tool invocation requested by the model.
type ToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function ToolCallFunc `json:"function"`
}

// ToolCallFunc holds the function name and arguments for a tool call.
type ToolCallFunc struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// ToolDef describes a tool available to the model.
type ToolDef struct {
	Type     string      `json:"type"`
	Function ToolFuncDef `json:"function"`
}

// ToolFuncDef is the function schema for a tool definition.
type ToolFuncDef struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

// Request is a provider-agnostic inference request.
type Request struct {
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	Tools       []ToolDef `json:"tools,omitempty"`
	Temperature *float64  `json:"temperature,omitempty"`
	MaxTokens   int       `json:"max_tokens,omitempty"`
	Stream      bool      `json:"stream,omitempty"`
	Stop        []string  `json:"stop,omitempty"`
	// IntentClass carries the classified intent for per-(model, intent) quality
	// tracking. Set by the pipeline before inference. Not sent to the provider.
	IntentClass string `json:"-"`
	// NoEscalate disables confidence escalation, cache reads, and cache writes.
	// Set during exercise/baseline runs where we need to measure a specific
	// model's raw capability — no cache hits, no fallback contamination, no
	// polluting the cache with synthetic prompts.
	NoEscalate bool `json:"-"`
}

// Response is a provider-agnostic inference response.
type Response struct {
	ID           string     `json:"id"`
	Model        string     `json:"model"`
	Content      string     `json:"content"`
	ToolCalls    []ToolCall `json:"tool_calls,omitempty"`
	FinishReason string     `json:"finish_reason"`
	Usage        Usage      `json:"usage"`
	// Metadata set by the service — not from the provider wire format.
	Provider  string `json:"-"` // provider name that produced this response
	IsLocal   bool   `json:"-"` // whether the provider is local
	LatencyMs int64  `json:"-"` // inference latency in milliseconds
}

// Usage tracks token consumption for cost accounting.
type Usage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

// Cost returns the estimated cost based on provider pricing.
func (u Usage) Cost(p *Provider) float64 {
	return float64(u.InputTokens)*p.CostPerInputTok + float64(u.OutputTokens)*p.CostPerOutputTok
}

// StreamChunk is a single piece of a streaming response.
type StreamChunk struct {
	Delta        string     `json:"delta"`
	ToolCalls    []ToolCall `json:"tool_calls,omitempty"`
	FinishReason string     `json:"finish_reason,omitempty"`
	Usage        *Usage     `json:"usage,omitempty"`
}

// Completer is the core abstraction: send a request, get a response.
// Implementations handle format translation internally.
type Completer interface {
	Complete(ctx context.Context, req *Request) (*Response, error)
	Stream(ctx context.Context, req *Request) (<-chan StreamChunk, <-chan error)
}
