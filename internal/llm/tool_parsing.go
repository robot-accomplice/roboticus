package llm

import (
	"encoding/json"
	"fmt"
	"strings"
)


// ParseToolCallsFromText extracts tool calls embedded in LLM text output.
// This is the fallback parser for models that don't use structured tool calls.
// Matches Rust's parse_tool_calls: scans for {"tool_call": ...} blocks with
// brace-depth tracking and truncation recovery.
//
// Called after the format-specific unmarshaller when ToolCalls is empty but
// content contains tool call markers.
func ParseToolCallsFromText(content string) []ToolCall {
	var calls []ToolCall

	// Forward scan for "tool_call" markers.
	searchFrom := 0
	for searchFrom < len(content) {
		idx := strings.Index(content[searchFrom:], `"tool_call"`)
		if idx < 0 {
			break
		}
		idx += searchFrom

		// Find the opening brace before the marker.
		braceStart := -1
		for i := idx - 1; i >= searchFrom; i-- {
			if content[i] == '{' {
				braceStart = i
				break
			}
		}
		if braceStart < 0 {
			searchFrom = idx + 1
			continue
		}

		// Track brace depth to find the closing brace.
		depth := 0
		braceEnd := -1
	scanLoop:
		for i := braceStart; i < len(content); i++ {
			switch content[i] {
			case '{':
				depth++
			case '}':
				depth--
				if depth == 0 {
					braceEnd = i + 1
					break scanLoop
				}
			}
		}

		// Truncation recovery: if JSON not closed, append closing braces.
		jsonStr := ""
		if braceEnd > 0 {
			jsonStr = content[braceStart:braceEnd]
		} else if depth > 0 {
			// Append missing closing braces (Rust: truncation repair).
			jsonStr = content[braceStart:]
			for i := 0; i < depth; i++ {
				jsonStr += "}"
			}
		} else {
			searchFrom = idx + 1
			continue
		}

		// Parse the JSON block.
		tc, ok := extractToolInvocation(jsonStr)
		if ok {
			calls = append(calls, tc)
		}

		if braceEnd > 0 {
			searchFrom = braceEnd
		} else {
			searchFrom = idx + 1
		}
	}

	// Fallback: single tool call from end of response (Rust: parse_tool_call).
	if len(calls) == 0 {
		if tc, ok := parseToolCallFromEnd(content); ok {
			calls = append(calls, tc)
		}
	}

	return calls
}

// extractToolInvocation parses a JSON block into a ToolCall.
// Handles flexible field naming matching Rust's extract_tool_invocation:
// - name: checks "name", "tool_name", "tool"
// - params: checks "params", "arguments", "args", "input"
func extractToolInvocation(jsonStr string) (ToolCall, bool) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(jsonStr), &raw); err != nil {
		return ToolCall{}, false
	}

	// The tool call payload may be nested under "tool_call" key.
	var callPayload map[string]json.RawMessage
	if tc, ok := raw["tool_call"]; ok {
		// Try parsing as object.
		if err := json.Unmarshal(tc, &callPayload); err != nil {
			// Could be a string shorthand: {"tool_call": "bash", "params": {...}}
			var name string
			if err := json.Unmarshal(tc, &name); err == nil {
				// Shorthand: name is the tool_call value, params from root.
				params := extractParams(raw)
				return ToolCall{
					ID:   fmt.Sprintf("text_%s", name),
					Type: "function",
					Function: ToolCallFunc{
						Name:      name,
						Arguments: params,
					},
				}, true
			}
			return ToolCall{}, false
		}
	} else {
		callPayload = raw
	}

	// Extract name (priority: name > tool_name > tool).
	name := extractString(callPayload, "name", "tool_name", "tool")
	if name == "" {
		return ToolCall{}, false
	}

	// Extract params (priority: params > arguments > args > input).
	params := extractParams(callPayload)

	return ToolCall{
		ID:   fmt.Sprintf("text_%s", name),
		Type: "function",
		Function: ToolCallFunc{
			Name:      name,
			Arguments: params,
		},
	}, true
}

// parseToolCallFromEnd searches backward from end of content for a single tool call.
func parseToolCallFromEnd(content string) (ToolCall, bool) {
	// Find last "tool_call" marker.
	idx := strings.LastIndex(content, `"tool_call"`)
	if idx < 0 {
		return ToolCall{}, false
	}

	// Find preceding opening brace.
	for i := idx - 1; i >= 0; i-- {
		if content[i] == '{' {
			// Try to parse from here to end.
			jsonStr := content[i:]
			// Try with truncation repair.
			depth := 0
			for _, c := range jsonStr {
				switch c {
				case '{':
					depth++
				case '}':
					depth--
				}
			}
			for depth > 0 {
				jsonStr += "}"
				depth--
			}
			if tc, ok := extractToolInvocation(jsonStr); ok {
				return tc, true
			}
			break
		}
	}
	return ToolCall{}, false
}

func extractString(m map[string]json.RawMessage, keys ...string) string {
	for _, key := range keys {
		if v, ok := m[key]; ok {
			var s string
			if err := json.Unmarshal(v, &s); err == nil {
				return s
			}
		}
	}
	return ""
}

func extractParams(m map[string]json.RawMessage) string {
	for _, key := range []string{"params", "arguments", "args", "input"} {
		if v, ok := m[key]; ok {
			return string(v)
		}
	}
	return "{}"
}

// ---------- Provider error classification (Rust parity) ----------

// ClassifyProviderError maps a raw error string into one of 8 canonical
// categories, matching Rust's classify_provider_error().
func ClassifyProviderError(raw string) string {
	lower := strings.ToLower(raw)

	switch {
	case strings.Contains(lower, "circuit breaker"):
		return "provider temporarily unavailable"
	case strings.Contains(lower, "no api key") || strings.Contains(lower, "no provider configured"):
		return "no provider configured for this model"
	case strings.Contains(lower, "401") || strings.Contains(lower, "403") || strings.Contains(lower, "authentication"):
		return "provider authentication error"
	case strings.Contains(lower, "429") || strings.Contains(lower, "rate limit") || strings.Contains(lower, "rate_limit"):
		return "provider rate limit reached"
	case strings.Contains(lower, "402") || strings.Contains(lower, "quota") || strings.Contains(lower, "billing") || strings.Contains(lower, "credit"):
		return "provider quota or billing issue"
	case strings.Contains(lower, "500") || strings.Contains(lower, "502") || strings.Contains(lower, "503") || strings.Contains(lower, "504"):
		return "provider server error"
	case strings.Contains(lower, "request failed") || strings.Contains(lower, "timeout") || strings.Contains(lower, "connection"):
		return "network error reaching provider"
	default:
		return "provider error"
	}
}

// ProviderFailureUserMessage generates a user-facing message for a provider
// failure, matching Rust's provider_failure_user_message().
// When messageStored is true the user's input was persisted and will be retried;
// otherwise the user should re-send.
func ProviderFailureUserMessage(lastError string, messageStored bool) string {
	classified := ClassifyProviderError(lastError)
	if messageStored {
		return fmt.Sprintf("I wasn't able to process your message (%s). Your message has been saved and I'll try again shortly.", classified)
	}
	return fmt.Sprintf("I wasn't able to process your message (%s). Could you please try sending it again?", classified)
}
