package mcp

import "encoding/json"

// McpServerConfig defines an MCP server connection.
type McpServerConfig struct {
	Name          string            `json:"name" mapstructure:"name"`
	Transport     string            `json:"transport" mapstructure:"transport"` // "stdio" or "sse"
	Command       string            `json:"command" mapstructure:"command"`     // for stdio
	Args          []string          `json:"args" mapstructure:"args"`           // for stdio
	URL           string            `json:"url" mapstructure:"url"`             // for sse
	Env           map[string]string `json:"env" mapstructure:"env"`
	Headers       map[string]string `json:"headers,omitempty" mapstructure:"headers"`
	Enabled       bool              `json:"enabled" mapstructure:"enabled"`
	AuthTokenEnv  string            `json:"auth_token_env,omitempty" mapstructure:"auth_token_env"`
	ToolAllowlist []string          `json:"tool_allowlist,omitempty" mapstructure:"tool_allowlist"`
}

// SSEValidationEvidence captures a reproducible validation artifact for a named
// SSE MCP target.
type SSEValidationEvidence struct {
	Name             string                `json:"name"`
	URL              string                `json:"url"`
	ResolvedPostURL  string                `json:"resolved_post_url,omitempty"`
	ServerName       string                `json:"server_name,omitempty"`
	ServerVersion    string                `json:"server_version,omitempty"`
	ToolCount        int                   `json:"tool_count"`
	FirstTool        string                `json:"first_tool,omitempty"`
	InitializeOK     bool                  `json:"initialize_ok"`
	ToolListOK       bool                  `json:"tool_list_ok"`
	AuthConfigured   bool                  `json:"auth_configured"`
	ToolCall         SSEValidationToolCall `json:"tool_call"`
	FatalError       string                `json:"fatal_error,omitempty"`
	ExpectedEvidence map[string]string     `json:"expected_evidence,omitempty"`
}

type SSEValidationToolCall struct {
	Attempted      bool   `json:"attempted"`
	Interpretable  bool   `json:"interpretable"`
	Tool           string `json:"tool,omitempty"`
	ContentPreview string `json:"content_preview,omitempty"`
	Error          string `json:"error,omitempty"`
}

// ToolDescriptor describes a tool exposed by an MCP server.
type ToolDescriptor struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"input_schema"`
}

// ServerStatus reports the health of an MCP connection.
type ServerStatus struct {
	Name          string `json:"name"`
	Connected     bool   `json:"connected"`
	ToolCount     int    `json:"tool_count"`
	ServerName    string `json:"server_name,omitempty"`
	ServerVersion string `json:"server_version,omitempty"`
	Error         string `json:"error,omitempty"`
}

// ToolCallResult is the response from calling an MCP tool.
type ToolCallResult struct {
	Content string `json:"content"`
	IsError bool   `json:"is_error"`
}
